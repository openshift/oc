package transition

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
)

type topologyState struct {
	controlPlane   configv1.TopologyMode
	infrastructure configv1.TopologyMode
}

type topologyValidator interface {
	Validate(ctx context.Context, current, target topologyState) error
}

// noopValidator leaves transition validation to the API and cluster-config-operator.
type noopValidator struct{}

func (noopValidator) Validate(ctx context.Context, current, target topologyState) error {
	return nil
}
