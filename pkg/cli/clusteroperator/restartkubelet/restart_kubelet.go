package restartkubelet

import (
	"context"
	_ "embed"

	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/oc/pkg/cli/clusteroperator/pernodepod"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
)

var (
	//go:embed restart-pod-template.yaml
	podYaml []byte
	pod     = resourceread.ReadPodV1OrDie(podYaml)
)

type RestartKubeletRuntime struct {
	PerNodePodRuntime *pernodepod.PerNodePodRuntime
}

func (r *RestartKubeletRuntime) Run(ctx context.Context) error {
	return r.PerNodePodRuntime.Run(ctx, nil, createPod)
}

func createPod(ctx context.Context, namespaceName, nodeName, imagePullSpec string) (*corev1.Pod, error) {
	restartObj := pod.DeepCopy()
	restartObj.Namespace = namespaceName
	restartObj.Spec.NodeName = nodeName
	restartObj.Spec.Containers[0].Image = imagePullSpec
	return restartObj, nil
}
