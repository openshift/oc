package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TopologyState describes the control-plane and infrastructure topology at one
// end of a topology transition.
type TopologyState struct {
	// controlPlaneTopology is the topology of the control-plane nodes. Valid values
	// are SingleReplica and HighlyAvailable. When set to SingleReplica, operators
	// avoid spending resources for high availability. When set to HighlyAvailable,
	// operators configure high availability as much as possible.
	// controlPlaneTopology is required.
	// +kubebuilder:validation:Enum=SingleReplica;HighlyAvailable
	// +required
	ControlPlaneTopology TopologyMode `json:"controlPlaneTopology,omitempty"`

	// infrastructureTopology is the topology of infrastructure services. Valid
	// values are SingleReplica and HighlyAvailable. When set to SingleReplica,
	// operators avoid spending resources for high availability. When set to
	// HighlyAvailable, operators configure high availability as much as possible.
	// infrastructureTopology is required.
	// +kubebuilder:validation:Enum=SingleReplica;HighlyAvailable
	// +required
	InfrastructureTopology TopologyMode `json:"infrastructureTopology,omitempty"`
}

const (
	// TopologyTransitionsEvaluatedConditionType indicates whether supported transition types have been evaluated.
	//
	// TopologyTransitionsEvaluated is Unknown before evaluation, True when evaluation
	// succeeds (even if no transitions are supported), and False when evaluation fails.
	// An absent condition means the transitions list must be treated as stale.
	TopologyTransitionsEvaluatedConditionType = "TopologyTransitionsEvaluated"

	// TopologyTransitionCompletedConditionType indicates the status of the current or most recent transition.
	//
	// TopologyTransitionCompletedConditionType is Unknown before a transition is requested, True when
	// a requested transition has completed successfully, and False while it is in progress or if it was blocked.
	TopologyTransitionCompletedConditionType = "TopologyTransitionCompleted"

	// TopologyTransitionAvailableConditionType indicates if a type of transition is available based on
	// the most recently evaluated state of the cluster.
	//
	// TopologyTransitionAvailableConditionType is Unknown before evaluation, True when evaluations
	// for the given transition succeed, and False when one or more evaluations for a transition fail.
	TopologyTransitionAvailableConditionType = "TopologyTransitionAvailable"
)

// TopologyTransitionStatus reports availability of each type of topology transition and contains the
// status of any initiated transition.
// When present, it must include conditions or transitions; either list may be empty.
// +kubebuilder:validation:MinProperties=1
type TopologyTransitionStatus struct {
	// conditions provides information on topology transition progress and the
	// evaluation of supported transition types. When omitted, or when the
	// TopologyTransitionsEvaluated condition is absent, transitions is stale.
	//
	// TopologyTransitionsEvaluatedConditionType and TopologyTransitionCompletedConditionType are the
	// only valid conditions at this scope. At most two conditions can be present.
	//
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:XValidation:rule="self.all(c, c.type in ['TopologyTransitionsEvaluated', 'TopologyTransitionCompleted'])",message="conditions may only contain TopologyTransitionsEvaluated and TopologyTransitionCompleted"
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// transitions contains each supported transition type and its availability.
	// An empty or omitted list means no transition evaluations have been reported.
	// Entries are stale when the TopologyTransitionsEvaluated condition is absent.
	//
	// At most one transition is supported currently (SNO to HA Compact)
	// +kubebuilder:validation:MaxItems=1
	// +optional
	// +listType=atomic
	Transitions []TopologyTransition `json:"transitions,omitempty"`
}

type TopologyTransition struct {
	// source is the control-plane and infrastructure topology this transition was
	// evaluated from. It may differ from the current topology while status is
	// being refreshed. source is required.
	// +required
	Source TopologyState `json:"source,omitempty,omitzero"`

	// target is the control-plane and infrastructure topology this transition would
	// move to. target is required.
	// +required
	Target TopologyState `json:"target,omitempty,omitzero"`

	// evaluations contains the availability condition for this transition and
	// conditions for the checks run against the cluster to determine availability.
	//
	// TopologyTransitionAvailableConditionType is required; other condition types
	// report individual checks. Between one and 32 conditions must be present.
	//
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:XValidation:rule="self.exists(c, c.type == 'TopologyTransitionAvailable')",message="evaluations must contain TopologyTransitionAvailable"
	// +required
	// +listType=map
	// +listMapKey=type
	Evaluations []metav1.Condition `json:"evaluations,omitempty"`
}
