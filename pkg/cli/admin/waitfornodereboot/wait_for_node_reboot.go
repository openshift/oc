package waitfornodereboot

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/openshift/oc/pkg/cli/admin/rebootmachineconfigpool"
)

var (
	nodeKind = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Node"}
)

const (
	RemoveOldTrustFieldManager = "remove-old-trust"
)

type WaitForNodeRebootRuntime struct {
	ResourceFinder genericclioptions.ResourceFinder
	KubeClient     kubernetes.Interface
	DynamicClient  dynamic.Interface

	RebootNumber int

	masterRebootNumber          int
	workerAndCustomRebootNumber int

	genericiooptions.IOStreams
}

func (r *WaitForNodeRebootRuntime) setRebootNumber(ctx context.Context) error {
	if r.RebootNumber > 0 {
		r.masterRebootNumber = r.RebootNumber
		r.workerAndCustomRebootNumber = r.RebootNumber
		return nil
	}

	masterMachineConfig, err := r.DynamicClient.Resource(rebootmachineconfigpool.MachineConfigResource).Get(ctx, rebootmachineconfigpool.MasterRebootingMachineConfigName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
	// do nothing.  If we need to use it, we'll fail
	case err != nil:
		return fmt.Errorf("unable to read existing state: %w", err)
	default:
		r.masterRebootNumber, err = rebootmachineconfigpool.GetRebootNumber(masterMachineConfig)
		if err != nil {
			return fmt.Errorf("unable to parse master reboot number: %w", err)
		}
	}

	workerMachineConfig, err := r.DynamicClient.Resource(rebootmachineconfigpool.MachineConfigResource).Get(ctx, rebootmachineconfigpool.WorkerRebootingMachineConfigName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
	// do nothing.  If we need to use it, we'll fail
	case err != nil:
		return fmt.Errorf("unable to read existing state: %w", err)
	default:
		r.workerAndCustomRebootNumber, err = rebootmachineconfigpool.GetRebootNumber(workerMachineConfig)
		if err != nil {
			return fmt.Errorf("unable to parse worker reboot number: %w", err)
		}
	}

	return nil
}

func (r *WaitForNodeRebootRuntime) Run(ctx context.Context) error {
	if err := r.setRebootNumber(ctx); err != nil {
		return err
	}

	hasMaster := false
	hasWorkerOrCustomPoolNode := false
	remainingNodes := []*corev1.Node{}
	visitor := r.ResourceFinder.Do()
	err := visitor.Visit(func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}
		if nodeKind != info.Object.GetObjectKind().GroupVersionKind() {
			return fmt.Errorf("command must only be pointed at nodes")
		}

		uncastObj, ok := info.Object.(*unstructured.Unstructured)
		if !ok {
			return fmt.Errorf("not unstructured: %w", err)
		}
		node := &corev1.Node{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uncastObj.Object, node); err != nil {
			return fmt.Errorf("not a node: %w", err)
		}
		if _, ok := node.Labels["node-role.kubernetes.io/master"]; ok {
			hasMaster = true
		} else {
			hasWorkerOrCustomPoolNode = true
		}

		remainingNodes = append(remainingNodes, node)

		return nil
	})
	if err != nil {
		return err
	}
	if hasMaster && r.masterRebootNumber <= 0 {
		return fmt.Errorf("inspecting a master node, but no master reboot number provided or detected")
	}
	if hasWorkerOrCustomPoolNode && r.workerAndCustomRebootNumber <= 0 {
		return fmt.Errorf("inspecting a worker node, but no worker reboot number provided or detected")
	}

	startingNodeCount := len(remainingNodes)
	nodesActivelyRebooting := []*corev1.Node{}
	for {
		newRemainingNodes, newNodesActivelyRebooting, err := r.waitForNodes(ctx, remainingNodes, nodesActivelyRebooting)
		if err != nil {
			return err
		}
		if len(newRemainingNodes) == 0 {
			fmt.Fprintf(r.Out, "All nodes rebooted\n")
			break
		}

		fmt.Fprintf(r.Out, "%d of %d nodes rebooted, %d rebooting to desired level: %v\n", startingNodeCount-len(newRemainingNodes), startingNodeCount, len(newNodesActivelyRebooting), time.Now().Format(time.RFC3339))
		fmt.Fprintln(r.Out)

		remainingNodes = newRemainingNodes
		nodesActivelyRebooting = newNodesActivelyRebooting
		select {
		case <-time.After(30 * time.Second):
		case <-ctx.Done():
			return err
		}
	}

	return nil
}

func (r *WaitForNodeRebootRuntime) waitForNodes(ctx context.Context, nodes []*corev1.Node, nodesActivelyRebooting []*corev1.Node) ([]*corev1.Node, []*corev1.Node, error) {
	remainingNodes := []*corev1.Node{}
	newNodesActivelyRebooting := []*corev1.Node{}

	for i := range nodes {
		nodeName := nodes[i].Name
		currNode, err := r.KubeClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			continue
		case err != nil:
			fmt.Fprintf(r.ErrOut, "nodes/%v failed getting current: %v\n", nodeName, err)
			// this must requeue the node we have because the currNode from the API is empty.
			remainingNodes = append(remainingNodes, nodes[i])
			continue
		}

		targetRebootNumber := 0
		if _, ok := currNode.Labels["node-role.kubernetes.io/master"]; ok {
			targetRebootNumber = r.masterRebootNumber
		} else {
			targetRebootNumber = r.workerAndCustomRebootNumber
		}

		rebootNumbersForNode := r.rebootNumbersForNode(ctx, currNode)
		switch {
		case rebootNumbersForNode.fatalErr != nil:
			// fatal, we cannot continue. something is wrong with the shape of a node.
			return nil, nil, rebootNumbersForNode.fatalErr

		case len(rebootNumbersForNode.errs) > 0:
			// we cannot read the current state for some reason, but it might get better.
			// print the messages and requeue the node.
			for _, curr := range rebootNumbersForNode.errs {
				fmt.Fprintln(r.ErrOut, curr.Error())
			}
			remainingNodes = append(remainingNodes, currNode)

		case rebootNumbersForNode.currentRebootNumber >= targetRebootNumber:
			// this node successfully rebooted, print it out and don't requeue.
			fmt.Fprintf(r.Out, "nodes/%v rebooted\n", nodeName)

		case rebootNumbersForNode.desiredRebootNumber >= targetRebootNumber:
			// this node is actively trying to get to a level that meets our requirements.
			// Let the  user know (once) that this has started and keep track of it for a future summarization count.
			// requeue so we know when its done.
			previouslyPrinted := false
			for _, curr := range nodesActivelyRebooting {
				if curr.Name == nodeName {
					previouslyPrinted = true
					break
				}
			}
			if !previouslyPrinted {
				fmt.Fprintf(r.Out, "nodes/%v has started the cordon, drain, reboot to (or beyond) the our desired reboot number\n", nodeName)
			}
			newNodesActivelyRebooting = append(newNodesActivelyRebooting, currNode)
			remainingNodes = append(remainingNodes, currNode)

		default:
			// the node isn't rebooting to a level we want, so just wait patiently. Only print the message if desired, we can have
			// a lot of nodes, so we don't want to be super noisy.
			if klog.V(1).Enabled() {
				fmt.Fprintf(r.Out, "nodes/%v uses machineconfig/%v is at reboot number %d, we're waiting for reboot number %d\n", nodeName, rebootNumbersForNode.currentMachineConfigName, rebootNumbersForNode.currentRebootNumber, targetRebootNumber)
			}
			remainingNodes = append(remainingNodes, currNode)
		}
	}

	return remainingNodes, newNodesActivelyRebooting, nil
}

type rebootNumbers struct {
	currentMachineConfigName string
	desiredMachineConfigName string

	currentRebootNumber int
	desiredRebootNumber int

	errs     []error
	fatalErr error
}

func (r *WaitForNodeRebootRuntime) rebootNumbersForNode(ctx context.Context, currNode *corev1.Node) rebootNumbers {
	ret := rebootNumbers{}
	nodeName := currNode.Name

	{
		ret.currentMachineConfigName = currNode.Annotations["machineconfiguration.openshift.io/currentConfig"]
		if len(ret.currentMachineConfigName) == 0 {
			ret.fatalErr = fmt.Errorf("nodes/%v is missing .annotations[machineconfiguration.openshift.io/currentConfig]", nodeName)
			return ret
		}
		currentMachineConfig, err := r.DynamicClient.Resource(rebootmachineconfigpool.MachineConfigResource).Get(ctx, ret.currentMachineConfigName, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			ret.errs = append(ret.errs, fmt.Errorf("nodes/%v current machineconfig/%v is missing", nodeName, ret.currentMachineConfigName))
		case err != nil:
			ret.errs = append(ret.errs, fmt.Errorf("nodes/%v failed getting current machineconfig/%v: %v", nodeName, ret.currentMachineConfigName, err))
		default:
			ret.currentRebootNumber, err = rebootmachineconfigpool.GetRebootNumber(currentMachineConfig)
			if err != nil && klog.V(2).Enabled() {
				fmt.Fprintf(r.Out, "nodes/%v uses machineconfig/%v which has not rebooted at all: %v\n", nodeName, ret.currentMachineConfigName, err)
			}
		}
	}

	{
		ret.desiredMachineConfigName = currNode.Annotations["machineconfiguration.openshift.io/desiredConfig"]
		if len(ret.desiredMachineConfigName) == 0 {
			ret.fatalErr = fmt.Errorf("nodes/%v is missing .annotations[machineconfiguration.openshift.io/desiredConfig]", nodeName)
			return ret
		}
		desiredMachineConfig, err := r.DynamicClient.Resource(rebootmachineconfigpool.MachineConfigResource).Get(ctx, ret.desiredMachineConfigName, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			ret.errs = append(ret.errs, fmt.Errorf("nodes/%v desired machineconfig/%v is missing", nodeName, ret.desiredMachineConfigName))
		case err != nil:
			ret.errs = append(ret.errs, fmt.Errorf("nodes/%v failed getting desired machineconfig/%v: %v", nodeName, ret.desiredMachineConfigName, err))
		default:
			ret.desiredRebootNumber, err = rebootmachineconfigpool.GetRebootNumber(desiredMachineConfig)
			if err != nil && klog.V(2).Enabled() {
				fmt.Fprintf(r.Out, "nodes/%v desires machineconfig/%v which has not rebooted at all: %v\n", nodeName, ret.desiredMachineConfigName, err)
			}
		}
	}

	return ret
}
