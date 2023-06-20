package restartkubelet

import (
	"context"
	_ "embed"
	"strings"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/oc/pkg/cli/admin/pernodepod"
	corev1 "k8s.io/api/core/v1"
)

var (
	//go:embed restart-pod-template.yaml
	podYaml []byte
	pod     = resourceread.ReadPodV1OrDie(podYaml)
)

type RestartKubeletRuntime struct {
	PerNodePodRuntime *pernodepod.PerNodePodRuntime

	CommandWhileKubeletIsOff string
}

func (r *RestartKubeletRuntime) Run(ctx context.Context) error {
	return r.PerNodePodRuntime.Run(ctx, nil, r.createPod)
}

func (r *RestartKubeletRuntime) createPod(ctx context.Context, namespaceName, nodeName, imagePullSpec string) (*corev1.Pod, error) {
	restartObj := pod.DeepCopy()
	restartObj.Namespace = namespaceName
	restartObj.Spec.NodeName = nodeName
	restartObj.Spec.Containers[0].Image = imagePullSpec
	restartObj.Spec.Containers[0].Command[2] = strings.ReplaceAll(restartObj.Spec.Containers[0].Command[2], "# optional command", r.CommandWhileKubeletIsOff)
	return restartObj, nil
}
