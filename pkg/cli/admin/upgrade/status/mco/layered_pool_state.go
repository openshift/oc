// This is a modified https://github.com/openshift/machine-config-operator/blob/11d5151a784c7d4be5255ea41acfbf5092eda592/pkg/controller/common/layered_pool_state.go
// TODO: Replace this file with the original MCO code when transitioning to server-side
package mco

import (
	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"
)

// This is intended to provide a singular way to interrogate MachineConfigPool
// objects to determine if they're in a specific state or not. The eventual
// goal is to use this to mutate the MachineConfigPool object to provide a
// single and consistent interface for that purpose. In this current state, we
// do not perform any mutations.
type LayeredPoolState struct {
	pool *mcfgv1.MachineConfigPool
}

func NewLayeredPoolState(pool *mcfgv1.MachineConfigPool) *LayeredPoolState {
	return &LayeredPoolState{pool: pool}
}

// Determines if a MachineConfigPool is layered by looking for the layering
// enabled label.
func (l *LayeredPoolState) IsLayered() bool {
	if l.pool == nil {
		return false
	}

	if l.pool.Labels == nil {
		return false
	}

	if _, ok := l.pool.Labels[LayeringEnabledPoolLabel]; ok {
		return true
	}
	return false
}

// Returns the OS image, if one is present.
func (l *LayeredPoolState) GetOSImage() string {
	osImage := l.pool.Annotations[ExperimentalNewestLayeredImageEquivalentConfigAnnotationKey]
	return osImage
}

// Determines if a given MachineConfigPool has an available OS image. Returns
// false if the annotation is missing or set to an empty string.
func (l *LayeredPoolState) HasOSImage() bool {
	val, ok := l.pool.Annotations[ExperimentalNewestLayeredImageEquivalentConfigAnnotationKey]
	return ok && val != ""
}
