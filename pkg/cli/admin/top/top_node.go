package top

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	resourcehelper "k8s.io/component-helpers/resource"
	"k8s.io/kubectl/pkg/cmd/top"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/completion"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	topNodeLong = templates.LongDesc(`
		Display resource (CPU/memory) usage of nodes.

		The top-node command allows you to see the resource consumption of nodes.

		With --overcommitment, show aggregate pod requests and limits versus node
		allocatable capacity (the Allocated resources section from oc describe node)
		for quick capacity and overcommit troubleshooting. This mode does not require
		the Metrics API.`)

	topNodeExample = templates.Examples(`
		# Show metrics for all nodes
		oc adm top node

		# Show metrics for a given node
		oc adm top node NODE_NAME

		# Show request/limit overcommitment for all nodes
		oc adm top node --overcommitment

		# Show overcommitment for worker nodes only
		oc adm top node --overcommitment -l node-role.kubernetes.io/worker=`)
)

// TopNodeOptions extends kubectl top node with OpenShift-specific flags.
type TopNodeOptions struct {
	top.TopNodeOptions

	Overcommitment bool
}

// NewCmdTopNode creates the oc adm top node command, wrapping kubectl and adding --overcommitment.
func NewCmdTopNode(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := &TopNodeOptions{
		TopNodeOptions: top.TopNodeOptions{
			IOStreams:          streams,
			UseProtocolBuffers: true,
		},
	}

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
	cmd.Flags().BoolVar(&o.Overcommitment, "overcommitment", false, "Print aggregate pod requests and limits versus node allocatable (like oc describe node Allocated resources). Does not require Metrics API.")

	return cmd
}

func (o *TopNodeOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if o.Overcommitment {
		if len(args) == 1 {
			o.ResourceName = args[0]
		} else if len(args) > 1 {
			return kcmdutil.UsageErrorf(cmd, "%s", cmd.Use)
		}
		clientset, err := f.KubernetesClientSet()
		if err != nil {
			return err
		}
		o.NodeClient = clientset.CoreV1()
		return nil
	}
	return o.TopNodeOptions.Complete(f, cmd, args)
}

func (o *TopNodeOptions) Validate() error {
	if o.Overcommitment {
		if len(o.SortBy) > 0 {
			if o.SortBy != "cpu" && o.SortBy != "memory" {
				return errors.New("--sort-by accepts only cpu or memory")
			}
		}
		if len(o.ResourceName) > 0 && len(o.Selector) > 0 {
			return errors.New("only one of NAME or --selector can be provided")
		}
		if o.ShowCapacity {
			return errors.New("--show-capacity cannot be used with --overcommitment")
		}
		if o.ShowSwap {
			return errors.New("--show-swap cannot be used with --overcommitment")
		}
		return nil
	}
	return o.TopNodeOptions.Validate()
}

func (o *TopNodeOptions) Run() error {
	if o.Overcommitment {
		return o.runOvercommitment()
	}
	return o.RunTopNode()
}

func (o *TopNodeOptions) runOvercommitment() error {
	selector := labels.Everything()
	var err error
	if len(o.Selector) > 0 {
		selector, err = labels.Parse(o.Selector)
		if err != nil {
			return err
		}
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

	if len(nodes) == 0 {
		return errors.New("no nodes found")
	}

	podsByNode, err := listNonTerminatedPodsByNode(o.NodeClient, o.ResourceName)
	if err != nil {
		return err
	}

	rows := make([]overcommitRow, 0, len(nodes))
	for _, node := range nodes {
		allocatable := node.Status.Allocatable
		reqs, limits := aggregatePodRequestsAndLimits(podsByNode[node.Name])
		rows = append(rows, newOvercommitRow(node.Name, allocatable, reqs, limits))
	}

	sort.Slice(rows, func(i, j int) bool {
		switch o.SortBy {
		case "cpu":
			if rows[i].cpuReqFraction == rows[j].cpuReqFraction {
				return rows[i].name < rows[j].name
			}
			return rows[i].cpuReqFraction > rows[j].cpuReqFraction
		case "memory":
			if rows[i].memReqFraction == rows[j].memReqFraction {
				return rows[i].name < rows[j].name
			}
			return rows[i].memReqFraction > rows[j].memReqFraction
		default:
			return rows[i].name < rows[j].name
		}
	})

	return printOvercommitment(o.Out, rows, o.NoHeaders)
}

func listNonTerminatedPodsByNode(client corev1client.CoreV1Interface, nodeName string) (map[string][]corev1.Pod, error) {
	fieldParts := []fields.Selector{
		fields.OneTermNotEqualSelector("status.phase", string(corev1.PodSucceeded)),
		fields.OneTermNotEqualSelector("status.phase", string(corev1.PodFailed)),
	}
	if nodeName != "" {
		fieldParts = append([]fields.Selector{
			fields.OneTermEqualSelector("spec.nodeName", nodeName),
		}, fieldParts...)
	}

	podList, err := client.Pods(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
		FieldSelector: fields.AndSelectors(fieldParts...).String(),
	})
	if err != nil {
		return nil, err
	}

	byNode := make(map[string][]corev1.Pod)
	for i := range podList.Items {
		pod := podList.Items[i]
		if pod.Spec.NodeName == "" {
			continue
		}
		byNode[pod.Spec.NodeName] = append(byNode[pod.Spec.NodeName], pod)
	}
	return byNode, nil
}

func aggregatePodRequestsAndLimits(pods []corev1.Pod) (reqs, limits corev1.ResourceList) {
	reqs, limits = corev1.ResourceList{}, corev1.ResourceList{}
	for i := range pods {
		podReqs := resourcehelper.PodRequests(&pods[i], resourcehelper.PodResourcesOptions{SkipPodLevelResources: false})
		podLimits := resourcehelper.PodLimits(&pods[i], resourcehelper.PodResourcesOptions{SkipPodLevelResources: false})
		addResourceList(reqs, podReqs)
		addResourceList(limits, podLimits)
	}
	return reqs, limits
}

func addResourceList(total, add corev1.ResourceList) {
	for name, quantity := range add {
		if existing, ok := total[name]; !ok {
			total[name] = quantity.DeepCopy()
		} else {
			existing.Add(quantity)
			total[name] = existing
		}
	}
}

type overcommitRow struct {
	name string

	cpuReq, cpuLim resource.Quantity
	memReq, memLim resource.Quantity

	cpuReqFraction, cpuLimFraction float64
	memReqFraction, memLimFraction float64
}

func newOvercommitRow(name string, allocatable, reqs, limits corev1.ResourceList) overcommitRow {
	cpuReq := reqs[corev1.ResourceCPU]
	cpuLim := limits[corev1.ResourceCPU]
	memReq := reqs[corev1.ResourceMemory]
	memLim := limits[corev1.ResourceMemory]

	row := overcommitRow{
		name:   name,
		cpuReq: cpuReq,
		cpuLim: cpuLim,
		memReq: memReq,
		memLim: memLim,
	}

	if cpuAlloc := allocatable.Cpu(); cpuAlloc.MilliValue() != 0 {
		row.cpuReqFraction = float64(cpuReq.MilliValue()) / float64(cpuAlloc.MilliValue()) * 100
		row.cpuLimFraction = float64(cpuLim.MilliValue()) / float64(cpuAlloc.MilliValue()) * 100
	}
	if memAlloc := allocatable.Memory(); memAlloc.Value() != 0 {
		row.memReqFraction = float64(memReq.Value()) / float64(memAlloc.Value()) * 100
		row.memLimFraction = float64(memLim.Value()) / float64(memAlloc.Value()) * 100
	}
	return row
}

func printOvercommitment(out io.Writer, rows []overcommitRow, noHeaders bool) error {
	w := tabwriter.NewWriter(out, 0, 8, 2, ' ', 0)
	if !noHeaders {
		fmt.Fprintln(w, "NAME\tCPU_REQUESTS\tCPU_LIMITS\tMEMORY_REQUESTS\tMEMORY_LIMITS")
	}
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s (%d%%)\t%s (%d%%)\t%s (%d%%)\t%s (%d%%)\n",
			row.name,
			row.cpuReq.String(), int64(row.cpuReqFraction),
			row.cpuLim.String(), int64(row.cpuLimFraction),
			row.memReq.String(), int64(row.memReqFraction),
			row.memLim.String(), int64(row.memLimFraction),
		)
	}
	return w.Flush()
}
