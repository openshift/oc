package describe

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/describe"
	"k8s.io/kubectl/pkg/scheme"

	"github.com/openshift/api/apps"
	appsv1 "github.com/openshift/api/apps/v1"
	appstypedclient "github.com/openshift/client-go/apps/clientset/versioned/typed/apps/v1"
	"github.com/openshift/library-go/pkg/apps/appsutil"
	"github.com/openshift/library-go/pkg/image/imageutil"
	appsedges "github.com/openshift/oc/pkg/helpers/graph/appsgraph"
	appsgraph "github.com/openshift/oc/pkg/helpers/graph/appsgraph/nodes"
	"github.com/openshift/oc/pkg/helpers/graph/genericgraph"
	kubegraph "github.com/openshift/oc/pkg/helpers/graph/kubegraph/nodes"
	"github.com/openshift/oc/pkg/helpers/legacy"
)

const (
	// maxDisplayDeployments is the number of deployments to show when describing
	// deployment configuration.
	maxDisplayDeployments = 3

	// maxDisplayDeploymentsEvents is the number of events to display when
	// describing the deployment configuration.
	// TODO: Make the estimation of this number more sophisticated and make this
	// number configurable via DescriberSettings
	maxDisplayDeploymentsEvents = 8
)

// DeploymentConfigDescriber generates information about a DeploymentConfig
type DeploymentConfigDescriber struct {
	appsClient appstypedclient.AppsV1Interface
	kubeClient kubernetes.Interface

	config *appsv1.DeploymentConfig
}

// NewDeploymentConfigDescriber returns a new DeploymentConfigDescriber
func NewDeploymentConfigDescriber(client appstypedclient.AppsV1Interface, kclient kubernetes.Interface, config *appsv1.DeploymentConfig) *DeploymentConfigDescriber {
	return &DeploymentConfigDescriber{
		appsClient: client,
		kubeClient: kclient,
		config:     config,
	}
}

// Describe returns the description of a DeploymentConfig
func (d *DeploymentConfigDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	var deploymentConfig *appsv1.DeploymentConfig
	if d.config != nil {
		// If a deployment config is already provided use that.
		// This is used by `oc rollback --dry-run`.
		deploymentConfig = d.config
	} else {
		var err error
		deploymentConfig, err = d.appsClient.DeploymentConfigs(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
	}

	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, deploymentConfig.ObjectMeta)
		var (
			deploymentsHistory   []*corev1.ReplicationController
			activeDeploymentName string
		)

		if d.config == nil {
			if rcs, err := d.kubeClient.CoreV1().ReplicationControllers(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: appsutil.ConfigSelector(deploymentConfig.Name).String()}); err == nil {
				deploymentsHistory = make([]*corev1.ReplicationController, 0, len(rcs.Items))
				for i := range rcs.Items {
					deploymentsHistory = append(deploymentsHistory, &rcs.Items[i])
				}
			}
		}

		if deploymentConfig.Status.LatestVersion == 0 {
			formatString(out, "Latest Version", "Not deployed")
		} else {
			formatString(out, "Latest Version", strconv.FormatInt(deploymentConfig.Status.LatestVersion, 10))
		}

		printDeploymentConfigSpec(d.kubeClient, *deploymentConfig, out)
		fmt.Fprintln(out)

		latestDeploymentName := appsutil.LatestDeploymentNameForConfig(deploymentConfig)
		if activeDeployment := appsutil.ActiveDeployment(deploymentsHistory); activeDeployment != nil {
			activeDeploymentName = activeDeployment.Name
		}

		var deployment *corev1.ReplicationController
		isNotDeployed := len(deploymentsHistory) == 0
		for _, item := range deploymentsHistory {
			if item.Name == latestDeploymentName {
				deployment = item
			}
		}
		if deployment == nil {
			isNotDeployed = true
		}

		if isNotDeployed {
			formatString(out, "Latest Deployment", "<none>")
		} else {
			header := fmt.Sprintf("Deployment #%d (latest)", appsutil.DeploymentVersionFor(deployment))
			// Show details if the current deployment is the active one or it is the
			// initial deployment.
			printDeploymentRc(deployment, d.kubeClient, out, header, (deployment.Name == activeDeploymentName) || len(deploymentsHistory) == 1)
		}

		// We don't show the deployment history when running `oc rollback --dry-run`.
		if d.config == nil && !isNotDeployed {
			var sorted []*corev1.ReplicationController
			// TODO(rebase-1.6): we should really convert the describer to use a versioned clientset
			for i := range deploymentsHistory {
				sorted = append(sorted, deploymentsHistory[i])
			}
			sort.Sort(sort.Reverse(OverlappingControllers(sorted)))
			counter := 1
			for _, item := range sorted {
				if item.Name != latestDeploymentName && deploymentConfig.Name == appsutil.DeploymentConfigNameFor(item) {
					header := fmt.Sprintf("Deployment #%d", appsutil.DeploymentVersionFor(item))
					printDeploymentRc(item, d.kubeClient, out, header, item.Name == activeDeploymentName)
					counter++
				}
				if counter == maxDisplayDeployments {
					break
				}
			}
		}

		if settings.ShowEvents {
			// Events
			if events, err := d.kubeClient.CoreV1().Events(deploymentConfig.Namespace).Search(scheme.Scheme, deploymentConfig); err == nil && events != nil {
				latestDeploymentEvents := &corev1.EventList{Items: []corev1.Event{}}
				for i := len(events.Items); i != 0 && i > len(events.Items)-maxDisplayDeploymentsEvents; i-- {
					latestDeploymentEvents.Items = append(latestDeploymentEvents.Items, events.Items[i-1])
				}
				fmt.Fprintln(out)
				pw := describe.NewPrefixWriter(out)
				describe.DescribeEvents(latestDeploymentEvents, pw)
			}
		}
		return nil
	})
}

// OverlappingControllers sorts a list of controllers by creation timestamp, using their names as a tie breaker.
// From
// https://github.com/kubernetes/kubernetes/blob/9eab226947d73a77cbf8474188f216cd64cd5fef/pkg/controller/replication/replication_controller_utils.go#L81-L92
// and modified to use internal instead of versioned objects.
type OverlappingControllers []*corev1.ReplicationController

func (o OverlappingControllers) Len() int      { return len(o) }
func (o OverlappingControllers) Swap(i, j int) { o[i], o[j] = o[j], o[i] }

func (o OverlappingControllers) Less(i, j int) bool {
	if o[i].CreationTimestamp.Equal(&o[j].CreationTimestamp) {
		return o[i].Name < o[j].Name
	}
	return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
}

func multilineStringArray(sep, indent string, args ...string) string {
	for i, s := range args {
		if strings.HasSuffix(s, "\n") {
			s = strings.TrimSuffix(s, "\n")
		}
		if strings.Contains(s, "\n") {
			s = "\n" + indent + strings.Join(strings.Split(s, "\n"), "\n"+indent)
		}
		args[i] = s
	}
	strings.TrimRight(args[len(args)-1], "\n ")
	return strings.Join(args, " ")
}

func printStrategy(strategy appsv1.DeploymentStrategy, indent string, w *tabwriter.Writer) {
	if strategy.CustomParams != nil {
		if len(strategy.CustomParams.Image) == 0 {
			fmt.Fprintf(w, "%sImage:\t%s\n", indent, "<default>")
		} else {
			fmt.Fprintf(w, "%sImage:\t%s\n", indent, strategy.CustomParams.Image)
		}

		if len(strategy.CustomParams.Environment) > 0 {
			fmt.Fprintf(w, "%sEnvironment:\t%s\n", indent, formatLabels(convertEnv(strategy.CustomParams.Environment)))
		}

		if len(strategy.CustomParams.Command) > 0 {
			fmt.Fprintf(w, "%sCommand:\t%v\n", indent, multilineStringArray(" ", "\t  ", strategy.CustomParams.Command...))
		}
	}

	if strategy.RecreateParams != nil {
		pre := strategy.RecreateParams.Pre
		mid := strategy.RecreateParams.Mid
		post := strategy.RecreateParams.Post
		if pre != nil {
			printHook("Pre-deployment", pre, indent, w)
		}
		if mid != nil {
			printHook("Mid-deployment", mid, indent, w)
		}
		if post != nil {
			printHook("Post-deployment", post, indent, w)
		}
	}

	if strategy.RollingParams != nil {
		pre := strategy.RollingParams.Pre
		post := strategy.RollingParams.Post
		if pre != nil {
			printHook("Pre-deployment", pre, indent, w)
		}
		if post != nil {
			printHook("Post-deployment", post, indent, w)
		}
	}
}

func printHook(prefix string, hook *appsv1.LifecycleHook, indent string, w io.Writer) {
	if hook.ExecNewPod != nil {
		fmt.Fprintf(w, "%s%s hook (pod type, failure policy: %s):\n", indent, prefix, hook.FailurePolicy)
		fmt.Fprintf(w, "%s  Container:\t%s\n", indent, hook.ExecNewPod.ContainerName)
		fmt.Fprintf(w, "%s  Command:\t%v\n", indent, multilineStringArray(" ", "\t  ", hook.ExecNewPod.Command...))
		if len(hook.ExecNewPod.Env) > 0 {
			fmt.Fprintf(w, "%s  Env:\t%s\n", indent, formatLabels(convertEnv(hook.ExecNewPod.Env)))
		}
	}
	if len(hook.TagImages) > 0 {
		fmt.Fprintf(w, "%s%s hook (tag images, failure policy: %s):\n", indent, prefix, hook.FailurePolicy)
		for _, image := range hook.TagImages {
			fmt.Fprintf(w, "%s  Tag:\tcontainer %s to %s %s %s\n", indent, image.ContainerName, image.To.Kind, image.To.Name, image.To.Namespace)
		}
	}
}

func printTriggers(triggers []appsv1.DeploymentTriggerPolicy, w *tabwriter.Writer) {
	if len(triggers) == 0 {
		formatString(w, "Triggers", "<none>")
		return
	}

	printLabels := []string{}

	for _, t := range triggers {
		switch t.Type {
		case appsv1.DeploymentTriggerOnConfigChange:
			printLabels = append(printLabels, "Config")
		case appsv1.DeploymentTriggerOnImageChange:
			if len(t.ImageChangeParams.From.Name) > 0 {
				name, tag, _ := imageutil.SplitImageStreamTag(t.ImageChangeParams.From.Name)
				printLabels = append(printLabels, fmt.Sprintf("Image(%s@%s, auto=%v)", name, tag, t.ImageChangeParams.Automatic))
			}
		}
	}

	desc := strings.Join(printLabels, ", ")
	formatString(w, "Triggers", desc)
}

func printDeploymentConfigSpec(kc kubernetes.Interface, dc appsv1.DeploymentConfig, w *tabwriter.Writer) error {
	spec := dc.Spec
	// Selector
	formatString(w, "Selector", formatLabels(spec.Selector))

	// Replicas
	test := ""
	if spec.Test {
		test = " (test, will be scaled down between deployments)"
	}
	formatString(w, "Replicas", fmt.Sprintf("%d%s", spec.Replicas, test))

	if spec.Paused {
		formatString(w, "Paused", "yes")
	}

	// Autoscaling info
	// FIXME: The CrossVersionObjectReference should specify the Group
	printAutoscalingInfo(
		[]schema.GroupResource{
			apps.Resource("DeploymentConfig"),
			// this needs to remain as long as HPA supports putting in the "wrong" DC scheme
			legacy.Resource("DeploymentConfig"),
		},
		dc.Namespace, dc.Name, kc, w)

	// Triggers
	printTriggers(spec.Triggers, w)

	// Strategy
	formatString(w, "Strategy", spec.Strategy.Type)
	printStrategy(spec.Strategy, "  ", w)

	if dc.Spec.MinReadySeconds > 0 {
		formatString(w, "MinReadySeconds", fmt.Sprintf("%d", spec.MinReadySeconds))
	}

	// Pod template
	fmt.Fprintf(w, "Template:\n")
	describe.DescribePodTemplate(spec.Template, describe.NewPrefixWriter(w))

	return nil
}

// TODO: Move this upstream
func printAutoscalingInfo(res []schema.GroupResource, namespace, name string, kclient kubernetes.Interface, w *tabwriter.Writer) {
	hpaList, err := kclient.AutoscalingV1().HorizontalPodAutoscalers(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labels.Everything().String()})
	if err != nil {
		return
	}

	scaledBy := []autoscalingv1.HorizontalPodAutoscaler{}
	for _, hpa := range hpaList.Items {
		for _, r := range res {
			if hpa.Spec.ScaleTargetRef.Name == name && hpa.Spec.ScaleTargetRef.Kind == r.String() {
				scaledBy = append(scaledBy, hpa)
			}
		}
	}

	for _, hpa := range scaledBy {
		fmt.Fprintf(w, "Autoscaling:\tbetween %d and %d replicas", *hpa.Spec.MinReplicas, hpa.Spec.MaxReplicas)
		if hpa.Spec.TargetCPUUtilizationPercentage != nil {
			fmt.Fprintf(w, " targeting %d%% CPU over all the pods\n", *hpa.Spec.TargetCPUUtilizationPercentage)
		} else {
			fmt.Fprint(w, " (default autoscaling policy)\n")
		}
		// TODO: Print a warning in case of multiple hpas.
		// Related oc status PR: https://github.com/openshift/origin/pull/7799
		break
	}
}

func printDeploymentRc(deployment *corev1.ReplicationController, kubeClient kubernetes.Interface, w io.Writer, header string, verbose bool) error {
	if len(header) > 0 {
		fmt.Fprintf(w, "%v:\n", header)
	}

	if verbose {
		fmt.Fprintf(w, "\tName:\t%s\n", deployment.Name)
	}
	timeAt := strings.ToLower(FormatRelativeTime(deployment.CreationTimestamp.Time))
	fmt.Fprintf(w, "\tCreated:\t%s ago\n", timeAt)
	fmt.Fprintf(w, "\tStatus:\t%s\n", appsutil.DeploymentStatusFor(deployment))
	if deployment.Spec.Replicas != nil {
		fmt.Fprintf(w, "\tReplicas:\t%d current / %d desired\n", deployment.Status.Replicas, *deployment.Spec.Replicas)
	}

	if verbose {
		fmt.Fprintf(w, "\tSelector:\t%s\n", formatLabels(deployment.Spec.Selector))
		fmt.Fprintf(w, "\tLabels:\t%s\n", formatLabels(deployment.Labels))
		running, waiting, succeeded, failed, err := getPodStatusForDeployment(deployment, kubeClient)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "\tPods Status:\t%d Running / %d Waiting / %d Succeeded / %d Failed\n", running, waiting, succeeded, failed)
	}

	return nil
}

func getPodStatusForDeployment(deployment *corev1.ReplicationController, kubeClient kubernetes.Interface) (running, waiting, succeeded, failed int,
	err error) {
	rcPods, err := kubeClient.CoreV1().Pods(deployment.Namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labels.Set(deployment.Spec.Selector).AsSelector().String()})
	if err != nil {
		return
	}
	for _, pod := range rcPods.Items {
		switch pod.Status.Phase {
		case corev1.PodRunning:
			running++
		case corev1.PodPending:
			waiting++
		case corev1.PodSucceeded:
			succeeded++
		case corev1.PodFailed:
			failed++
		}
	}
	return
}

type LatestDeploymentsDescriber struct {
	count      int
	appsClient appstypedclient.AppsV1Interface
	kubeClient kubernetes.Interface
}

// NewLatestDeploymentsDescriber lists the latest deployments limited to "count". In case count == -1, list back to the last successful.
func NewLatestDeploymentsDescriber(client appstypedclient.AppsV1Interface, kclient kubernetes.Interface, count int) *LatestDeploymentsDescriber {
	return &LatestDeploymentsDescriber{
		count:      count,
		appsClient: client,
		kubeClient: kclient,
	}
}

// Describe returns the description of the latest deployments for a config
func (d *LatestDeploymentsDescriber) Describe(namespace, name string) (string, error) {
	var f formatter

	config, err := d.appsClient.DeploymentConfigs(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	var deployments []corev1.ReplicationController
	if d.count == -1 || d.count > 1 {
		list, err := d.kubeClient.CoreV1().ReplicationControllers(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: appsutil.ConfigSelector(name).String()})
		if err != nil && !kerrors.IsNotFound(err) {
			return "", err
		}
		deployments = list.Items
	} else {
		deploymentName := appsutil.LatestDeploymentNameForConfig(config)
		deployment, err := d.kubeClient.CoreV1().ReplicationControllers(config.Namespace).Get(context.TODO(), deploymentName, metav1.GetOptions{})
		if err != nil && !kerrors.IsNotFound(err) {
			return "", err
		}
		if deployment != nil {
			deployments = []corev1.ReplicationController{*deployment}
		}
	}

	g := genericgraph.New()
	dcNode := appsgraph.EnsureDeploymentConfigNode(g, config)
	for i := range deployments {
		kubegraph.EnsureReplicationControllerNode(g, &deployments[i])
	}
	appsedges.AddTriggerDeploymentConfigsEdges(g, dcNode)
	appsedges.AddDeploymentConfigsDeploymentEdges(g, dcNode)
	activeDeployment, inactiveDeployments := appsedges.RelevantDeployments(g, dcNode)

	return tabbedString(func(out *tabwriter.Writer) error {
		descriptions := describeDeploymentConfigDeployments(f, dcNode, activeDeployment, inactiveDeployments, nil, d.count)
		for i, description := range descriptions {
			descriptions[i] = fmt.Sprintf("%v %v", name, description)
		}
		printLines(out, "", 0, descriptions...)
		return nil
	})
}
