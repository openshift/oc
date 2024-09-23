package inspect

import (
	"context"
	"fmt"
	"os"
	"path"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
)

// TODO someone may later choose to use discovery information to determine what to collect
func namespaceResourcesToCollect() []schema.GroupResource {
	return []schema.GroupResource{
		// this is actually a group which collects most useful things
		{Resource: "all"},
		{Resource: "configmaps"},
		{Resource: "egressfirewalls"},
		{Resource: "egressqoses"},
		{Resource: "events"},
		{Resource: "endpoints"},
		{Resource: "endpointslices"},
		{Resource: "networkpolicies"},
		{Resource: "persistentvolumeclaims"},
		{Resource: "poddisruptionbudgets"},
		{Resource: "secrets"},
		{Resource: "servicemonitors"},
	}
}

func (o *InspectOptions) gatherNamespaceData(baseDir, namespace string) error {
	fmt.Fprintf(o.Out, "Gathering data for ns/%s...\n", namespace)

	destDir := path.Join(baseDir, namespaceResourcesDirname, namespace)

	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	ns, err := o.kubeClient.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
	if err != nil { // If we can't get the namespace we need to exit out
		return err
	}
	ns.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Namespace"))

	errs := []error{}
	// write namespace.yaml file
	filename := fmt.Sprintf("%s.yaml", namespace)
	if err := o.fileWriter.WriteFromResource(path.Join(destDir, "/"+filename), ns); err != nil {
		errs = append(errs, err)
	}

	klog.V(1).Infof("    Collecting resources for namespace %q...\n", namespace)

	resourcesTypesToStore := map[schema.GroupVersionResource]bool{
		corev1.SchemeGroupVersion.WithResource("pods"): true,
	}
	resourcesToStore := map[schema.GroupVersionResource]runtime.Object{}

	// collect specific resource information for namespace
	for gvr := range resourcesTypesToStore {
		list, err := o.dynamicClient.Resource(gvr).Namespace(namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			errs = append(errs, err)
		}
		resourcesToStore[gvr] = list
	}

	klog.V(1).Infof("    Gathering pod data for namespace %q...\n", namespace)
	// gather specific pod data
	if pods := resourcesToStore[corev1.SchemeGroupVersion.WithResource("pods")]; pods != nil {
		nodeList, err := o.kubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			errs = append(errs, err)
		}
		var nodes []corev1.Node
		if nodeList.Items != nil {
			nodes = nodeList.Items
		}
		for _, pod := range pods.(*unstructured.UnstructuredList).Items {
			structuredPod := &corev1.Pod{}
			runtime.DefaultUnstructuredConverter.FromUnstructured(pod.Object, structuredPod)
			eligible, err := o.podEligibleForCollection(structuredPod, nodes)
			if err != nil {
				errs = append(errs, err)
			}
			if !eligible {
				klog.V(1).Infof("        Skipping gathering data for pod %q\n", pod.GetName())
				continue
			}
			klog.V(1).Infof("        Gathering data for pod %q\n", pod.GetName())
			if err := o.gatherPodData(path.Join(destDir, "/pods/"+pod.GetName()), namespace, structuredPod); err != nil {
				errs = append(errs, err)
				continue
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("one or more errors occurred while gathering pod-specific data for namespace: %s\n\n    %v", namespace, errors.NewAggregate(errs))
	}
	return nil
}

// podEligibleForCollection aims to filter out the daemonset pods running on the
// nodes whose mismatch with the given --node-selector, if data plane node count is greater than 18,
// in order to save significant storage size and reduces the collection duration.
func (o *InspectOptions) podEligibleForCollection(pod *corev1.Pod, nodes []corev1.Node) (bool, error) {
	isDaemonSet := false
	if len(pod.OwnerReferences) > 0 {
		for _, OwnerRef := range pod.OwnerReferences {
			if OwnerRef.Kind == "DaemonSet" {
				isDaemonSet = true
				break
			}
		}
	}
	if !isDaemonSet {
		return true, nil
	}

	workerNodeCount := 0
	for _, node := range nodes {
		if _, ok := node.Labels["node-role.kubernetes.io/control-plane"]; ok {
			continue
		}
		workerNodeCount++
	}
	const daemonsetCollectionMaxNodeSize = 18
	if workerNodeCount <= daemonsetCollectionMaxNodeSize {
		return true, nil
	}

	var podNode *corev1.Node
	for _, node := range nodes {
		if node.Name != pod.Spec.NodeName {
			continue
		}

		podNode = &node
		break
	}

	if podNode == nil {
		return false, nil
	}

	nodeSelector, err := labels.Parse(o.nodeSelector)
	if err != nil {
		return true, err
	}
	if nodeSelector.Matches(labels.Set(podNode.Labels)) {
		return true, nil
	}
	return false, nil
}
