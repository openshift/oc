package rollout

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	kexternalclientset "k8s.io/client-go/kubernetes"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/templates"

	appsv1 "github.com/openshift/api/apps/v1"
	"github.com/openshift/library-go/pkg/apps/appsutil"
	"github.com/openshift/oc/pkg/cli/set"
)

type RetryOptions struct {
	PrintFlags *genericclioptions.PrintFlags

	Resources         []string
	Builder           func() *resource.Builder
	Mapper            meta.RESTMapper
	Clientset         kexternalclientset.Interface
	Namespace         string
	ExplicitNamespace bool

	ToPrinter func(string) (printers.ResourcePrinter, error)

	resource.FilenameOptions
	genericclioptions.IOStreams
}

var (
	rolloutRetryLong = templates.LongDesc(`
		If a rollout fails, you may opt to retry it (if the error was transient). Some rollouts may
		never successfully complete - in which case you can use the rollout latest to force a redeployment.
		If a deployment config has completed rolling out successfully at least once in the past, it would be
		automatically rolled back in the event of a new failed rollout. Note that you would still need
		to update the erroneous deployment config in order to have its template persisted across your
		application.
`)

	rolloutRetryExample = templates.Examples(`
		# Retry the latest failed deployment based on 'frontend'
		# The deployer pod and any hook pods are deleted for the latest failed deployment
		arvan paas rollout retry dc/frontend
`)
)

func NewRolloutRetryOptions(streams genericclioptions.IOStreams) *RetryOptions {
	return &RetryOptions{
		PrintFlags: genericclioptions.NewPrintFlags("already retried").WithTypeSetter(scheme.Scheme),
		IOStreams:  streams,
	}
}

func NewCmdRolloutRetry(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewRolloutRetryOptions(streams)
	cmd := &cobra.Command{
		Use:     "retry (TYPE NAME | TYPE/NAME) [flags]",
		Long:    rolloutRetryLong,
		Example: rolloutRetryExample,
		Short:   "Retry the latest failed rollout",
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run())
		},
	}

	o.PrintFlags.AddFlags(cmd)

	usage := "Filename, directory, or URL to a file identifying the resource to get from a server."
	kcmdutil.AddFilenameOptionFlags(cmd, &o.FilenameOptions, usage)
	return cmd
}

func (o *RetryOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	var err error
	if len(args) == 0 && len(o.FilenameOptions.Filenames) == 0 {
		return kcmdutil.UsageErrorf(cmd, cmd.Use)
	}

	o.Resources = args

	o.Mapper, err = f.ToRESTMapper()
	if err != nil {
		return err
	}

	o.Namespace, o.ExplicitNamespace, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	config, err := f.ToRESTConfig()
	if err != nil {
		return err
	}

	o.Clientset, err = kexternalclientset.NewForConfig(config)
	if err != nil {
		return err
	}

	o.ToPrinter = func(operation string) (printers.ResourcePrinter, error) {
		o.PrintFlags.NamePrintFlags.Operation = operation
		return o.PrintFlags.ToPrinter()
	}

	o.Builder = f.NewBuilder

	return err
}

func (o RetryOptions) Run() error {
	allErrs := []error{}
	mapping, err := o.Mapper.RESTMapping(corev1.SchemeGroupVersion.WithKind("ReplicationController").GroupKind())
	if err != nil {
		return err
	}

	r := o.Builder().
		WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
		NamespaceParam(o.Namespace).DefaultNamespace().
		FilenameParam(o.ExplicitNamespace, &o.FilenameOptions).
		ResourceTypeOrNameArgs(true, o.Resources...).
		ContinueOnError().
		Latest().
		Flatten().
		Do()

	if err := r.Err(); err != nil {
		return err
	}

	infos, err := r.Infos()
	if err != nil {
		return err
	}

	for _, info := range infos {
		config, ok := info.Object.(*appsv1.DeploymentConfig)
		if !ok {
			allErrs = append(allErrs, kcmdutil.AddSourceToErr("retrying", info.Source, fmt.Errorf("expected deployment configuration, got %T", info.Object)))
			continue
		}
		if config.Spec.Paused {
			allErrs = append(allErrs, kcmdutil.AddSourceToErr("retrying", info.Source, fmt.Errorf("unable to retry paused deployment config %q", config.Name)))
			continue
		}
		if config.Status.LatestVersion == 0 {
			allErrs = append(allErrs, kcmdutil.AddSourceToErr("retrying", info.Source, fmt.Errorf("no rollouts found for %q", config.Name)))
			continue
		}

		latestDeploymentName := appsutil.LatestDeploymentNameForConfig(config)
		rc, err := o.Clientset.CoreV1().ReplicationControllers(config.Namespace).Get(context.TODO(), latestDeploymentName, metav1.GetOptions{})
		if err != nil {
			if kerrors.IsNotFound(err) {
				allErrs = append(allErrs, kcmdutil.AddSourceToErr("retrying", info.Source, fmt.Errorf("unable to find the latest rollout (#%d).\nYou can start a new rollout with 'arvan paas rollout latest dc/%s'.", config.Status.LatestVersion, config.Name)))
				continue
			}
			allErrs = append(allErrs, kcmdutil.AddSourceToErr("retrying", info.Source, fmt.Errorf("unable to fetch replication controller %q", config.Name)))
			continue
		}

		if !appsutil.IsFailedDeployment(rc) {
			message := fmt.Sprintf("rollout #%d is %s; only failed deployments can be retried.\n", config.Status.LatestVersion,
				strings.ToLower(appsutil.AnnotationFor(rc, appsv1.DeploymentStatusAnnotation)))
			if appsutil.IsCompleteDeployment(rc) {
				message += fmt.Sprintf("You can start a new deployment with 'arvan paas rollout latest dc/%s'.", config.Name)
			} else {
				message += fmt.Sprintf("Optionally, you can cancel this deployment with 'arvan paas rollout cancel dc/%s'.", config.Name)
			}
			allErrs = append(allErrs, kcmdutil.AddSourceToErr("retrying", info.Source, errors.New(message)))
			continue
		}

		// Delete the deployer pod as well as the deployment hooks pods, if any
		pods, err := o.Clientset.CoreV1().Pods(config.Namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: appsutil.DeployerPodSelector(
			latestDeploymentName).String()})
		if err != nil {
			allErrs = append(allErrs, kcmdutil.AddSourceToErr("retrying", info.Source, fmt.Errorf("failed to list deployer/hook pods for deployment #%d: %v", config.Status.LatestVersion, err)))
			continue
		}
		hasError := false
		for _, pod := range pods.Items {
			err := o.Clientset.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, *metav1.NewDeleteOptions(0))
			if err != nil {
				allErrs = append(allErrs, kcmdutil.AddSourceToErr("retrying", info.Source, fmt.Errorf("failed to delete deployer/hook pod %s for deployment #%d: %v", pod.Name, config.Status.LatestVersion, err)))
				hasError = true
			}
		}
		if hasError {
			continue
		}

		patches := set.CalculatePatchesExternal([]*resource.Info{{Object: rc, Mapping: mapping}}, func(info *resource.Info) (bool, error) {
			rc.Annotations[appsv1.DeploymentStatusAnnotation] = string(appsv1.DeploymentStatusNew)
			delete(rc.Annotations, appsv1.DeploymentStatusReasonAnnotation)
			delete(rc.Annotations, appsv1.DeploymentCancelledAnnotation)
			return true, nil
		})

		if len(patches) == 0 {
			printer, err := o.ToPrinter("already retried")
			if err != nil {
				allErrs = append(allErrs, kcmdutil.AddSourceToErr("retrying", info.Source, err))
				continue
			}
			if err := printer.PrintObj(info.Object, o.Out); err != nil {
				allErrs = append(allErrs, kcmdutil.AddSourceToErr("retrying", info.Source, err))
			}
			continue
		}

		if _, err := o.Clientset.CoreV1().ReplicationControllers(rc.Namespace).Patch(context.TODO(), rc.Name, types.StrategicMergePatchType, patches[0].Patch, metav1.PatchOptions{}); err != nil {
			allErrs = append(allErrs, kcmdutil.AddSourceToErr("retrying", info.Source, err))
			continue
		}
		printer, err := o.ToPrinter(fmt.Sprintf("retried rollout #%d", config.Status.LatestVersion))
		if err != nil {
			allErrs = append(allErrs, kcmdutil.AddSourceToErr("retrying", info.Source, err))
			continue
		}
		if err := printer.PrintObj(info.Object, o.Out); err != nil {
			allErrs = append(allErrs, kcmdutil.AddSourceToErr("retrying", info.Source, err))
			continue
		}
	}

	return utilerrors.NewAggregate(allErrs)
}
