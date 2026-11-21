package top

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/discovery"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	kubectltop "k8s.io/kubectl/pkg/cmd/top"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/describe"
	"k8s.io/kubectl/pkg/metricsutil"
	"k8s.io/kubectl/pkg/util/completion"
	"k8s.io/kubectl/pkg/util/templates"
	metricsapi "k8s.io/metrics/pkg/apis/metrics"
	metricsV1api "k8s.io/metrics/pkg/apis/metrics/v1"
	metricsV1beta1api "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

const (
	sortByCPU    = "cpu"
	sortByMemory = "memory"
)

var (
	topNodeLong = templates.LongDesc(`
		Display resource (CPU/memory) usage of nodes.

		The top node command allows you to see the resource consumption of nodes.
		In addition to the standard metrics columns, a ROLES column is printed
		immediately after NAME using the same node-role.kubernetes.io/* labels
		as "oc get nodes".`)

	topNodeExample = templates.Examples(`
		# Show metrics for all nodes
		oc adm top node

		# Show metrics for a given node
		oc adm top node NODE_NAME

		# Show metrics sorted by CPU
		oc adm top node --sort-by=cpu
	`)
)

// TopNodeOptions contains all the options for running the top-node cli command.
type TopNodeOptions struct {
	ResourceName       string
	Selector           string
	SortBy             string
	NoHeaders          bool
	UseProtocolBuffers bool
	ShowCapacity       bool
	ShowSwap           bool

	NodeClient      corev1client.CoreV1Interface
	DiscoveryClient discovery.DiscoveryInterface
	MetricsClient   metricsclientset.Interface

	genericiooptions.IOStreams
}

// NewTopNodeOptions returns a TopNodeOptions with defaults.
func NewTopNodeOptions(streams genericiooptions.IOStreams) *TopNodeOptions {
	return &TopNodeOptions{
		IOStreams:          streams,
		UseProtocolBuffers: true,
	}
}

// NewCmdTopNode implements the OpenShift cli adm top node command.
func NewCmdTopNode(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewTopNodeOptions(streams)

	cmd := &cobra.Command{
		Use:                   "node [NAME | -l label]",
		DisableFlagsInUseLine: true,
		Short:                 "Display resource (CPU/memory) usage of nodes",
		Long:                  topNodeLong,
		Example:               topNodeExample,
		ValidArgsFunction:     completion.ResourceNameCompletionFunc(f, "node"),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
		Aliases: []string{"nodes", "no"},
	}
	kcmdutil.AddLabelSelectorFlagVar(cmd, &o.Selector)
	cmd.Flags().StringVar(&o.SortBy, "sort-by", o.SortBy, "If non-empty, sort nodes list using specified field. The field can be either 'cpu' or 'memory'.")
	cmd.Flags().BoolVar(&o.NoHeaders, "no-headers", o.NoHeaders, "If present, print output without headers")
	cmd.Flags().BoolVar(&o.UseProtocolBuffers, "use-protocol-buffers", o.UseProtocolBuffers, "Enables using protocol-buffers to access Metrics API.")
	cmd.Flags().BoolVar(&o.ShowCapacity, "show-capacity", o.ShowCapacity, "Print node resources based on Capacity instead of Allocatable(default) of the nodes.")
	cmd.Flags().BoolVar(&o.ShowSwap, "show-swap", o.ShowSwap, "Print node resources related to swap memory.")

	return cmd
}

// Complete turns a partially defined TopNodeOptions into a solvent structure.
func (o *TopNodeOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) == 1 {
		o.ResourceName = args[0]
	} else if len(args) > 1 {
		return kcmdutil.UsageErrorf(cmd, "%s", cmd.Use)
	}

	clientset, err := f.KubernetesClientSet()
	if err != nil {
		return err
	}

	o.DiscoveryClient = clientset.DiscoveryClient

	config, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	if o.UseProtocolBuffers {
		config.ContentType = "application/vnd.kubernetes.protobuf"
	}
	o.MetricsClient, err = metricsclientset.NewForConfig(config)
	if err != nil {
		return err
	}

	o.NodeClient = clientset.CoreV1()
	return nil
}

// Validate checks that TopNodeOptions are valid.
func (o *TopNodeOptions) Validate() error {
	if len(o.SortBy) > 0 {
		if o.SortBy != sortByCPU && o.SortBy != sortByMemory {
			return errors.New("--sort-by accepts only cpu or memory")
		}
	}
	if len(o.ResourceName) > 0 && len(o.Selector) > 0 {
		return errors.New("only one of NAME or --selector can be provided")
	}
	return nil
}

// Run displays node metrics with a ROLES column.
func (o *TopNodeOptions) Run() error {
	var err error
	selector := labels.Everything()
	if len(o.Selector) > 0 {
		selector, err = labels.Parse(o.Selector)
		if err != nil {
			return err
		}
	}

	apiGroups, err := o.DiscoveryClient.ServerGroups()
	if err != nil {
		return err
	}

	metricsAPIAvailable := kubectltop.SupportedMetricsAPIVersionAvailable(apiGroups)
	if metricsAPIAvailable == "" {
		return errors.New("Metrics API not available")
	}

	metrics, err := getNodeMetricsFromMetricsAPI(o.MetricsClient, o.ResourceName, selector, metricsAPIAvailable)
	if err != nil {
		return err
	}

	if len(metrics.Items) == 0 {
		return errors.New("metrics not available yet")
	}

	var nodes []corev1.Node
	if len(o.ResourceName) > 0 {
		node, err := o.NodeClient.Nodes().Get(context.TODO(), o.ResourceName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		nodes = append(nodes, *node)
	} else {
		nodeList, err := o.NodeClient.Nodes().List(context.TODO(), metav1.ListOptions{
			LabelSelector: selector.String(),
		})
		if err != nil {
			return err
		}
		nodes = append(nodes, nodeList.Items...)
	}

	availableResources := make(map[string]corev1.ResourceList)
	nodeRoles := make(map[string]string)
	nodesMissingResources := make(map[string][]string)

	for _, n := range nodes {
		if !o.ShowCapacity {
			availableResources[n.Name] = n.Status.Allocatable
		} else {
			availableResources[n.Name] = n.Status.Capacity
		}

		nodeRoles[n.Name] = formatNodeRoles(findNodeRoles(&n))

		if n.Status.NodeInfo.Swap != nil && n.Status.NodeInfo.Swap.Capacity != nil {
			swapCapacity := *n.Status.NodeInfo.Swap.Capacity
			availableResources[n.Name]["swap"] = *resource.NewQuantity(swapCapacity, resource.BinarySI)
		} else {
			nodesMissingResources[n.Name] = append(nodesMissingResources[n.Name], metricsutil.ResourceSwap)
		}
	}

	return printNodeMetrics(o.Out, metrics.Items, availableResources, nodeRoles, nodesMissingResources, o.NoHeaders, o.SortBy, o.ShowSwap)
}

// findNodeRoles returns the roles of a given node.
// The roles are determined by looking for:
// * a node-role.kubernetes.io/<role>="" label
// * a kubernetes.io/role="<role>" label
func findNodeRoles(node *corev1.Node) []string {
	roles := sets.New[string]()
	for k, v := range node.Labels {
		switch {
		case strings.HasPrefix(k, describe.LabelNodeRolePrefix):
			if role := strings.TrimPrefix(k, describe.LabelNodeRolePrefix); len(role) > 0 {
				roles.Insert(role)
			}
		case k == describe.NodeLabelRole && v != "":
			roles.Insert(v)
		}
	}
	return sets.List(roles)
}

func formatNodeRoles(roles []string) string {
	if len(roles) == 0 {
		return "<none>"
	}
	return strings.Join(roles, ",")
}

func printNodeMetrics(out io.Writer, metrics []metricsapi.NodeMetrics, availableResources map[string]corev1.ResourceList, nodeRoles map[string]string, nodesMissingResources map[string][]string, noHeaders bool, sortBy string, showSwap bool) error {
	if len(metrics) == 0 {
		return nil
	}

	measuredResources := []corev1.ResourceName{
		corev1.ResourceCPU,
		corev1.ResourceMemory,
	}
	columns := []string{"NAME", "ROLES", "CPU(cores)", "CPU(%)", "MEMORY(bytes)", "MEMORY(%)"}
	if showSwap {
		measuredResources = append(measuredResources, metricsutil.ResourceSwap)
		columns = append(columns, "SWAP(bytes)", "SWAP(%)")
	}

	w := printers.GetNewTabWriter(out)
	defer w.Flush()

	sort.Sort(metricsutil.NewNodeMetricsSorter(metrics, sortBy))

	if !noHeaders {
		for _, name := range columns {
			fmt.Fprintf(w, "%v\t", name)
		}
		fmt.Fprint(w, "\n")
	}

	var usage corev1.ResourceList
	for _, m := range metrics {
		m.Usage.DeepCopyInto(&usage)
		role := nodeRoles[m.Name]
		if role == "" {
			role = "<none>"
		}
		printNodeMetricsLine(w, m.Name, role, usage, availableResources[m.Name], measuredResources, nodesMissingResources[m.Name])
		delete(availableResources, m.Name)
	}

	// print lines for nodes of which the metrics is unreachable.
	for nodeName := range availableResources {
		role := nodeRoles[nodeName]
		if role == "" {
			role = "<none>"
		}
		fmt.Fprintf(w, "%v\t", nodeName)
		fmt.Fprintf(w, "%v\t", role)
		unknownMetricsStatus := "<unknown>"
		for range measuredResources {
			fmt.Fprintf(w, "%v\t", unknownMetricsStatus)
			fmt.Fprintf(w, "%v\t", unknownMetricsStatus)
		}
		fmt.Fprint(w, "\n")
	}
	return nil
}

func printNodeMetricsLine(out io.Writer, name, role string, metrics, available corev1.ResourceList, measuredResources []corev1.ResourceName, missingResources []string) {
	fmt.Fprintf(out, "%v\t", name)
	fmt.Fprintf(out, "%v\t", role)
	for _, res := range measuredResources {
		if slices.Contains(missingResources, string(res)) {
			fmt.Fprintf(out, "%s\t%s\t", "<unknown>", "<unknown>")
			continue
		}

		quantity := metrics[res]
		printSingleResourceUsage(out, res, quantity)
		fmt.Fprint(out, "\t")
		if availableQty, found := available[res]; found {
			fraction := 0.0
			if !availableQty.IsZero() {
				fraction = float64(quantity.MilliValue()) / float64(availableQty.MilliValue()) * 100
			}
			fmt.Fprintf(out, "%d%%\t", int64(fraction))
		}
	}
	fmt.Fprint(out, "\n")
}

func printSingleResourceUsage(out io.Writer, resourceType corev1.ResourceName, quantity resource.Quantity) {
	switch resourceType {
	case corev1.ResourceCPU:
		fmt.Fprintf(out, "%vm", quantity.MilliValue())
	case corev1.ResourceMemory, metricsutil.ResourceSwap:
		fmt.Fprintf(out, "%vMi", quantity.Value()/(1024*1024))
	default:
		fmt.Fprintf(out, "%v", quantity.Value())
	}
}

func getNodeMetricsFromMetricsAPI(metricsClient metricsclientset.Interface, resourceName string, selector labels.Selector, metricsVersion string) (*metricsapi.NodeMetricsList, error) {
	var err error
	if metricsVersion == "v1" {
		versionedMetrics := &metricsV1api.NodeMetricsList{}
		mc := metricsClient.MetricsV1()
		nm := mc.NodeMetricses()
		if resourceName != "" {
			m, err := nm.Get(context.TODO(), resourceName, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			versionedMetrics.Items = []metricsV1api.NodeMetrics{*m}
		} else {
			versionedMetrics, err = nm.List(context.TODO(), metav1.ListOptions{LabelSelector: selector.String()})
			if err != nil {
				return nil, err
			}
		}
		metrics := &metricsapi.NodeMetricsList{}
		err = metricsV1api.Convert_v1_NodeMetricsList_To_metrics_NodeMetricsList(versionedMetrics, metrics, nil)
		if err != nil {
			return nil, err
		}
		return metrics, nil
	}
	// fallback to metric v1beta1
	versionedMetrics := &metricsV1beta1api.NodeMetricsList{}
	mc := metricsClient.MetricsV1beta1()
	nm := mc.NodeMetricses()
	if resourceName != "" {
		m, err := nm.Get(context.TODO(), resourceName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		versionedMetrics.Items = []metricsV1beta1api.NodeMetrics{*m}
	} else {
		versionedMetrics, err = nm.List(context.TODO(), metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return nil, err
		}
	}
	metrics := &metricsapi.NodeMetricsList{}
	err = metricsV1beta1api.Convert_v1beta1_NodeMetricsList_To_metrics_NodeMetricsList(versionedMetrics, metrics, nil)
	if err != nil {
		return nil, err
	}
	return metrics, nil
}
