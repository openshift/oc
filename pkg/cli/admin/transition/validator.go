package transition

import (
	"context"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
)

type TopologyState struct {
	ControlPlane   configv1.TopologyMode
	Infrastructure configv1.TopologyMode
}

// noopValidator leaves transition validation to the API and cluster-config-operator.
type TransitionValidator struct {
	Current TopologyState
}

func (v TransitionValidator) Validate(ctx context.Context, target TopologyState) error {
	validTransitions, err := v.GetValidTransitions(ctx)
	if err != nil {
		return err
	}

	for _, vt := range validTransitions {
		if vt.ControlPlane == target.ControlPlane && vt.Infrastructure == target.Infrastructure {
			return nil
		}
	}

	return fmt.Errorf("invalid transition specified")
}

// Only SNO -> HA Compact allowed currently
func (v TransitionValidator) GetValidTransitions(ctx context.Context) ([]TopologyState, error) {
	validTransitions := make([]TopologyState, 0, 2)

	// Transitioning to the current state is valid, but will just be a no-op
	validTransitions = append(validTransitions, v.Current)

	if v.Current.ControlPlane == configv1.SingleReplicaTopologyMode &&
		v.Current.Infrastructure != configv1.HighlyAvailableTopologyMode {
		validTransitions = append(validTransitions, TopologyState{
			ControlPlane:   configv1.HighlyAvailableTopologyMode,
			Infrastructure: configv1.HighlyAvailableTopologyMode,
		})
	}

	return validTransitions, nil
}
