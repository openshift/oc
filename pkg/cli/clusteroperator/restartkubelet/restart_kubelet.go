package restartkubelet

import (
	"context"
	_ "embed"
	"fmt"
	"sync"
	"time"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/oc/pkg/helpers/conditions"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
)

var (
	nodeKind = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Node"}

	//go:embed restart-namespace.yaml
	namespaceYaml []byte
	namespace     = resourceread.ReadNamespaceV1OrDie(namespaceYaml)

	//go:embed restart-pod-template.yaml
	podYaml []byte
	pod     = resourceread.ReadPodV1OrDie(podYaml)
)

type RestartKubeletRuntime struct {
	ResourceFinder genericclioptions.ResourceFinder
	KubeClient     kubernetes.Interface

	DryRun bool

	ImagePullSpec            string
	NumberOfNodesInParallel  int
	PercentOfNodesInParallel int

	Printer printers.ResourcePrinter
	genericclioptions.IOStreams
}

func (o *RestartKubeletRuntime) Run(ctx context.Context) error {
	interestingNodes, err := o.GetInterestingNodes(ctx)
	if err != nil {
		return fmt.Errorf("unable to get interesting nodes: %w", err)
	}

	numberOfNodesInParallel := 1
	switch {
	case o.NumberOfNodesInParallel > 0:
		numberOfNodesInParallel = o.NumberOfNodesInParallel
	case o.PercentOfNodesInParallel > 0:
		numberOfNodesInParallel = len(interestingNodes) / o.PercentOfNodesInParallel
	}
	if numberOfNodesInParallel < 1 {
		numberOfNodesInParallel = 1
	}

	// create a namespace to work in
	nsName := "!!-dry-run"
	if !o.DryRun {
		ns, err := o.KubeClient.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create namespace: %w", err)
		}
		nsName = ns.Name
		fmt.Fprintf(o.Out, "Created namespace/%v\n", ns.Name)
		defer func() {
			// using new context so the cancel doesn't stop the cleanup.
			err := o.KubeClient.CoreV1().Namespaces().Delete(context.Background(), ns.Name, metav1.DeleteOptions{})
			if err != nil {
				fmt.Fprintf(o.ErrOut, "failed to cleanup namespace: %v", err)
			}
		}()
	}

	workCh := make(chan *corev1.Node, numberOfNodesInParallel)
	// producer
	go func(ctx context.Context, nodes []*corev1.Node) {
		defer close(workCh)
		for i := range nodes {
			select {
			case workCh <- nodes[i]:
			case <-ctx.Done():
				return
			}
		}
	}(ctx, interestingNodes)

	// consumer
	wg := sync.WaitGroup{}
	errCh := make(chan error, len(interestingNodes))
	for i := 0; i < numberOfNodesInParallel; i++ {
		wg.Add(1)
		go func(ctx context.Context) {
			defer wg.Done()
			for {
				select {
				case node, stillReady := <-workCh:
					if !stillReady {
						return
					}
					if restartErr := o.RestartNode(ctx, nsName, node); restartErr != nil {
						errCh <- restartErr
					}
				case <-ctx.Done():
					return
				}
			}
		}(ctx)
	}
	wg.Wait()
	close(errCh)
	errs := []error{}
	for err := range errCh {
		errs = append(errs, err)
	}

	return utilerrors.NewAggregate(errs)
}

func (o *RestartKubeletRuntime) RestartNode(ctx context.Context, namespaceName string, node *corev1.Node) error {
	if o.DryRun {
		fmt.Fprintf(o.Out, "node/%v kubelet restarted\n", node.Name)
		return nil
	}

	// it should only take X minutes per node
	timeLimitedCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	restartObj := pod.DeepCopy()
	restartObj.Namespace = namespaceName
	restartObj.Spec.NodeName = node.Name
	restartObj.Spec.Containers[0].Image = o.ImagePullSpec
	createdPod, err := o.KubeClient.CoreV1().Pods(namespaceName).Create(timeLimitedCtx, restartObj, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("node/%v failed failed creating pod in --namespace=%v: %w", node.Name, namespaceName, err)
	}
	//fmt.Fprintf(o.Out, "Restarting node/%v using --namespace=%v pod/%v\n", node.Name, namespaceName, createdPod.Name)

	lastWatchEvent, watchErr := watchtools.UntilWithSync(timeLimitedCtx,
		cache.NewListWatchFromClient(
			o.KubeClient.CoreV1().RESTClient(), "pods", namespaceName, fields.OneTermEqualSelector("metadata.name", createdPod.Name)),
		&corev1.Pod{},
		nil,
		conditions.PodDone,
	)
	if watchErr != nil {
		// TODO inspect and report the pod state.
		retErr := fmt.Errorf("node/%v failed waiting for restart using --namespace=%v pod/%v: %w", node.Name, namespaceName, createdPod.Name, watchErr)
		fmt.Fprintln(o.ErrOut, retErr.Error())
		return retErr
	}
	finalPodState := lastWatchEvent.Object.(*corev1.Pod)

	switch {
	case finalPodState.Status.Phase == corev1.PodSucceeded:
		fmt.Fprintf(o.Out, "node/%v kubelet restarted\n", node.Name)
		_ = o.KubeClient.CoreV1().Pods(namespaceName).Delete(timeLimitedCtx, createdPod.Name, metav1.DeleteOptions{})

	case finalPodState.Status.Phase == corev1.PodFailed:
		terminationInfo := finalPodState.Status.ContainerStatuses[0].LastTerminationState.Terminated
		if terminationInfo == nil {
			retErr := fmt.Errorf("node/%v kubelet restart failed --namespace=%v pod/%v, state unknown", node.Name, namespaceName, createdPod.Name)
			fmt.Fprintln(o.ErrOut, retErr.Error())
			return retErr

		}
		retErr := fmt.Errorf("node/%v kubelet restart failed --namespace=%v pod/%v, exitCode=%v, finalLog=%v", node.Name, namespaceName, createdPod.Name, terminationInfo.ExitCode, terminationInfo.Message)
		fmt.Fprintln(o.ErrOut, retErr.Error())
		return fmt.Errorf("node/%v kubelet restart failed, exitCode=%v", node.Name, terminationInfo.ExitCode)
	default:
		retErr := fmt.Errorf("node/%v kubelet restart hit unknown state, --namespace=%v pod/%v", node.Name, namespaceName, createdPod.Name)
		fmt.Fprintln(o.ErrOut, retErr.Error())
		return retErr
	}

	return nil
}

// GetInterestingNodes uses the resourcefinder to retrieve all the nodes. We do this so that our CLI options are consistent
// and so that we have the values so that we can get a percentage.  Ordinarily we process as we visit for memory efficiency,
// but in this case the number of nodes is expected to be small
func (o *RestartKubeletRuntime) GetInterestingNodes(ctx context.Context) ([]*corev1.Node, error) {
	visitor := o.ResourceFinder.Do()

	ret := []*corev1.Node{}

	nodeReader := func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}

		if nodeKind != info.Object.GetObjectKind().GroupVersionKind() {
			return fmt.Errorf("command must only be pointed at node")
		}

		uncastObj, ok := info.Object.(*unstructured.Unstructured)
		if !ok {
			return fmt.Errorf("not unstructured: %w", err)
		}
		node := &corev1.Node{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uncastObj.Object, node); err != nil {
			return fmt.Errorf("not a node: %w", err)
		}
		ret = append(ret, node)

		return nil
	}

	// TODO need to wire context through the visitorFns
	err := visitor.Visit(nodeReader)
	if err != nil {
		return nil, err
	}
	return ret, nil
}
