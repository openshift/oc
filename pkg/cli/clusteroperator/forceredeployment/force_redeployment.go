package forceredeployment

import (
	"context"
	"fmt"
	"strings"
	"time"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	configv1 "github.com/openshift/api/config/v1"
	operatorapplyv1 "github.com/openshift/client-go/operator/applyconfigurations/operator/v1"
	operatorclient "github.com/openshift/client-go/operator/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
)

var (
	clusterOperatorKind = schema.GroupVersionKind{Group: "config.openshift.io", Version: "v1", Kind: "ClusterOperator"}
)

const (
	ForceRedeploymentFieldManager = "force-redeployment"
)

type operandRedeployFunc func(ctx context.Context, clusterOperator *configv1.ClusterOperator) error

type ForceRedeploymentRuntime struct {
	ResourceFinder genericclioptions.ResourceFinder
	KubeClient     kubernetes.Interface
	OperatorClient operatorclient.Interface

	DryRun      bool
	RedeployFns map[string]operandRedeployFunc

	Printer printers.ResourcePrinter

	genericclioptions.IOStreams
}

func (r *ForceRedeploymentRuntime) Run(ctx context.Context) error {
	visitor := r.ResourceFinder.Do()

	// TODO need to wire context through the visitorFns
	err := visitor.Visit(r.forceRedeploymentOfInfo)
	if err != nil {
		return err
	}
	return nil
}

func (r *ForceRedeploymentRuntime) forceRedeploymentOfInfo(info *resource.Info, err error) error {
	// we need a real context.
	ctx := context.TODO()

	if err != nil {
		return err
	}

	if clusterOperatorKind != info.Object.GetObjectKind().GroupVersionKind() {
		return fmt.Errorf("command must only be pointed at clusteroperators")
	}

	uncastObj, ok := info.Object.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("not unstructured: %w", err)
	}
	clusterOperator := &configv1.ClusterOperator{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uncastObj.Object, clusterOperator); err != nil {
		return fmt.Errorf("not a secret: %w", err)
	}

	return r.forceRedeploymentOfClusterOperator(ctx, clusterOperator)
}

func (r *ForceRedeploymentRuntime) forceRedeploymentOfClusterOperator(ctx context.Context, clusterOperator *configv1.ClusterOperator) error {
	redeployFn, ok := r.RedeployFns[clusterOperator.Name]
	if !ok {
		redeployFn = r.operatorOnlyRedeploy
	}

	err := redeployFn(ctx, clusterOperator)
	if err != nil {
		retErr := fmt.Errorf("clusteroperator/%v failure redeploying: %w", clusterOperator.Name, err)
		fmt.Fprintln(r.ErrOut, retErr)
		return retErr
	}

	clusterOperator.GetObjectKind().SetGroupVersionKind(clusterOperatorKind)
	return r.Printer.PrintObj(clusterOperator, r.Out)
}

func (r *ForceRedeploymentRuntime) applyOptions() metav1.ApplyOptions {
	if r.DryRun {
		return metav1.ApplyOptions{
			DryRun:       []string{metav1.DryRunAll},
			Force:        true,
			FieldManager: ForceRedeploymentFieldManager,
		}
	}
	return metav1.ApplyOptions{
		Force:        true,
		FieldManager: ForceRedeploymentFieldManager,
	}
}

func (r *ForceRedeploymentRuntime) deleteOptions() metav1.DeleteOptions {
	if r.DryRun {
		return metav1.DeleteOptions{
			DryRun: []string{metav1.DryRunAll},
		}
	}
	return metav1.DeleteOptions{}
}

func (r *ForceRedeploymentRuntime) deleteAllPodsInNamespace(ctx context.Context, operatorName, namespaceName string) error {
	podList, err := r.KubeClient.CoreV1().Pods(namespaceName).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	errs := []error{}
	podsToDeleteBeforePause := (len(podList.Items) / 10) + 1
	countSinceWait := 0
	for _, item := range podList.Items {
		if ctx.Err() != nil {
			return fmt.Errorf("stopped deleting pods in %v: %w", namespaceName, ctx.Err())
		}
		if countSinceWait > podsToDeleteBeforePause {
			countSinceWait = 0
			time.Sleep(1 * time.Second)
		}
		if ctx.Err() != nil {
			return fmt.Errorf("stopped deleting pods in %v: %w", namespaceName, ctx.Err())
		}

		countSinceWait++
		err := r.KubeClient.CoreV1().Pods(namespaceName).Delete(ctx, item.Name, r.deleteOptions())
		if err != nil && apierrors.IsNotFound(err) {
			retErr := fmt.Errorf("clusteroperator/%v --namespace=%v pods/%v delete failed: %w", operatorName, namespaceName, item.Name, err)
			fmt.Fprintln(r.Out, retErr.Error())
			errs = append(errs, retErr)
		} else {
			fmt.Fprintf(r.Out, "clusteroperator/%v --namespace=%v pods/%v deleted\n", operatorName, namespaceName, item.Name)
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (r *ForceRedeploymentRuntime) operatorOnlyRedeploy(ctx context.Context, clusterOperator *configv1.ClusterOperator) error {
	errs := []error{}

	for _, relatedObj := range clusterOperator.Status.RelatedObjects {
		isNamespace := len(relatedObj.Group) == 0 && relatedObj.Resource == "namespaces"
		if !isNamespace {
			continue
		}
		if !strings.HasPrefix(relatedObj.Name, "openshift-") {
			continue
		}
		// only force all the operator to restart for starters.
		if !strings.HasSuffix(relatedObj.Name, "-operator") {
			continue
		}

		if err := r.deleteAllPodsInNamespace(ctx, clusterOperator.Name, relatedObj.Name); err != nil {
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (r *ForceRedeploymentRuntime) allPodRedeploy(ctx context.Context, clusterOperator *configv1.ClusterOperator) error {
	errs := []error{}

	for _, relatedObj := range clusterOperator.Status.RelatedObjects {
		isNamespace := len(relatedObj.Group) == 0 && relatedObj.Resource == "namespaces"
		if !isNamespace {
			continue
		}
		if !strings.HasPrefix(relatedObj.Name, "openshift-") {
			continue
		}

		if err := r.deleteAllPodsInNamespace(ctx, clusterOperator.Name, relatedObj.Name); err != nil {
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (r *ForceRedeploymentRuntime) redeployEtcd(ctx context.Context, clusterOperator *configv1.ClusterOperator) error {
	errs := []error{}

	if err := r.deleteAllPodsInNamespace(ctx, clusterOperator.Name, "openshift-etcd-operator"); err != nil {
		errs = append(errs, err)
	}

	toApply := operatorapplyv1.Etcd("cluster").
		WithSpec(
			operatorapplyv1.EtcdSpec().
				WithForceRedeploymentReason(string(uuid.NewUUID())),
		)
	if _, err := r.OperatorClient.OperatorV1().Etcds().Apply(ctx, toApply, r.applyOptions()); err != nil {
		errs = append(errs, err)
	}
	return utilerrors.NewAggregate(errs)
}

func (r *ForceRedeploymentRuntime) redeployKubeAPIServer(ctx context.Context, clusterOperator *configv1.ClusterOperator) error {
	errs := []error{}

	if err := r.deleteAllPodsInNamespace(ctx, clusterOperator.Name, "openshift-kube-apiserver-operator"); err != nil {
		errs = append(errs, err)
	}

	toApply := operatorapplyv1.KubeAPIServer("cluster").
		WithSpec(
			operatorapplyv1.KubeAPIServerSpec().
				WithForceRedeploymentReason(string(uuid.NewUUID())),
		)
	if _, err := r.OperatorClient.OperatorV1().KubeAPIServers().Apply(ctx, toApply, r.applyOptions()); err != nil {
		errs = append(errs, err)
	}
	return utilerrors.NewAggregate(errs)
}

func (r *ForceRedeploymentRuntime) redeployKubeControllerManager(ctx context.Context, clusterOperator *configv1.ClusterOperator) error {
	errs := []error{}

	if err := r.deleteAllPodsInNamespace(ctx, clusterOperator.Name, "openshift-kube-controller-manager-operator"); err != nil {
		errs = append(errs, err)
	}

	toApply := operatorapplyv1.KubeControllerManager("cluster").
		WithSpec(
			operatorapplyv1.KubeControllerManagerSpec().
				WithForceRedeploymentReason(string(uuid.NewUUID())),
		)
	if _, err := r.OperatorClient.OperatorV1().KubeControllerManagers().Apply(ctx, toApply, r.applyOptions()); err != nil {
		errs = append(errs, err)
	}
	return utilerrors.NewAggregate(errs)
}

func (r *ForceRedeploymentRuntime) redeployKubeScheduler(ctx context.Context, clusterOperator *configv1.ClusterOperator) error {
	errs := []error{}

	if err := r.deleteAllPodsInNamespace(ctx, clusterOperator.Name, "openshift-kube-scheduler-operator"); err != nil {
		errs = append(errs, err)
	}

	toApply := operatorapplyv1.KubeScheduler("cluster").
		WithSpec(
			operatorapplyv1.KubeSchedulerSpec().
				WithForceRedeploymentReason(string(uuid.NewUUID())),
		)
	if _, err := r.OperatorClient.OperatorV1().KubeSchedulers().Apply(ctx, toApply, r.applyOptions()); err != nil {
		errs = append(errs, err)
	}
	return utilerrors.NewAggregate(errs)
}
