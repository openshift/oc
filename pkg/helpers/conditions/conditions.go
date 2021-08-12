package conditions

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	watchtools "k8s.io/client-go/tools/watch"
	krun "k8s.io/kubectl/pkg/cmd/run"
)

// ErrContainerTerminated is returned by PodContainerRunning in the intermediate
// state where the pod indicates it's still running, but its container is already terminated
var ErrContainerTerminated = fmt.Errorf("container terminated")
var ErrNonZeroExitCode = fmt.Errorf("non-zero exit code from debug container")

type PodWaitNotifyFunc func(pod *corev1.Pod, container corev1.ContainerStatus) error

// PodContainerRunning returns false until the named container has ContainerStatus running (at least once),
// and will return an error if the pod is deleted, runs to completion, or the container pod is not available.
func PodContainerRunning(containerName string, coreClient corev1client.CoreV1Interface, notifyFn PodWaitNotifyFunc) watchtools.ConditionFunc {
	return func(event watch.Event) (bool, error) {
		switch event.Type {
		case watch.Deleted:
			return false, errors.NewNotFound(schema.GroupResource{Resource: "pods"}, "")
		}
		switch t := event.Object.(type) {
		case *corev1.Pod:
			switch t.Status.Phase {
			case corev1.PodRunning, corev1.PodPending:
				// notify the caller about any containers that are in waiting status
				if notifyFn != nil {
					for _, s := range t.Status.InitContainerStatuses {
						if s.State.Waiting == nil {
							continue
						}
						if err := notifyFn(t, s); err != nil {
							return false, err
						}
						// only the first waiting init container is relevant
						break
					}
					for _, s := range t.Status.ContainerStatuses {
						if s.Name != containerName {
							continue
						}
						if s.State.Waiting != nil {
							if err := notifyFn(t, s); err != nil {
								return false, err
							}
						}
						break
					}
				}
			case corev1.PodFailed, corev1.PodSucceeded:
				// If the pod is terminal, exit if any container has a non-zero exit code, otherwise return a generic
				// completion
				for _, s := range append(append([]corev1.ContainerStatus{}, t.Status.InitContainerStatuses...), t.Status.ContainerStatuses...) {
					if s.State.Terminated != nil && s.State.Terminated.ExitCode != 0 {
						return false, ErrNonZeroExitCode
					}
				}
				return false, krun.ErrPodCompleted
			}

			for _, s := range append(append([]corev1.ContainerStatus{}, t.Status.InitContainerStatuses...), t.Status.ContainerStatuses...) {
				if s.Name != containerName {
					continue
				}
				if s.State.Terminated != nil {
					return false, ErrContainerTerminated
				}
				return s.State.Running != nil, nil
			}
			return false, nil
		}
		return false, nil
	}
}
