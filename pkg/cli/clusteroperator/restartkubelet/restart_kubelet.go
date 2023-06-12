package restartkubelet

import (
	"context"
	_ "embed"

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
	return r.PerNodePodRuntime.Run(ctx)
}
