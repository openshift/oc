package transition

// Stub types for openshift/api PR #3029 (topology transition status).
// TODO(vendor): Replace with vendored types when openshift/api PR #3029 merges.
// See: https://github.com/openshift/api/pull/3029

import (
	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// TopologyTransitionsEvaluatedConditionType indicates whether supported transitions have been evaluated.
	// True = evaluation complete; absent = transitions list is stale.
	TopologyTransitionsEvaluatedConditionType = "TopologyTransitionsEvaluated"

	// TopologyTransitionCompletedConditionType reports completion status of a transition.
	// True = completed, False = in progress or blocked, Unknown = no transition requested.
	TopologyTransitionCompletedConditionType = "TopologyTransitionCompleted"

	// TopologyTransitionAvailableConditionType indicates whether a specific transition is available.
	// Required in every transition's evaluations. True = all checks pass, False = one or more failed.
	TopologyTransitionAvailableConditionType = "TopologyTransitionAvailable"
)

// TopologyTransitionStatus reports transition availability and lifecycle status.
type TopologyTransitionStatus struct {
	// Conditions reports topology transition progress and evaluation state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Transitions contains each supported transition type and its availability.
	Transitions []TopologyTransition `json:"transitions,omitempty"`
}

// TopologyTransition represents a supported transition between topology states.
type TopologyTransition struct {
	// Source is the topology state this transition was evaluated from.
	Source APITopologyState `json:"source"`

	// Target is the topology state this transition would move to.
	Target APITopologyState `json:"target"`

	// Evaluations contains the availability condition and individual check results.
	Evaluations []metav1.Condition `json:"evaluations"`
}

// APITopologyState describes control-plane and infrastructure topology at one end of a transition.
type APITopologyState struct {
	ControlPlaneTopology   configv1.TopologyMode `json:"controlPlaneTopology"`
	InfrastructureTopology configv1.TopologyMode `json:"infrastructureTopology"`
}
