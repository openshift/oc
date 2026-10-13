package transition

import (
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TopologyState is the internal representation of a topology pair used throughout the CLI.
type TopologyState struct {
	ControlPlane   configv1.TopologyMode
	Infrastructure configv1.TopologyMode
}

// TransitionValidator validates topology transitions using the server-advertised
// available transitions from status.topologyTransitionStatus.
type TransitionValidator struct {
	Current    TopologyState
	Transitions []TopologyTransition
}

// GetAvailableTransitions returns transitions from the server-advertised list,
// excluding the current state (no-op). Returns nil if no transitions are advertised.
func (v TransitionValidator) GetAvailableTransitions() []TopologyTransition {
	var available []TopologyTransition
	for _, t := range v.Transitions {
		if t.Target.ControlPlaneTopology == v.Current.ControlPlane &&
			t.Target.InfrastructureTopology == v.Current.Infrastructure {
			continue
		}
		available = append(available, t)
	}
	return available
}

// FindTransition finds the transition entry matching the requested target.
// It matches on the target topology state, ignoring fields that are empty
// (unspecified by the user).
func (v TransitionValidator) FindTransition(target TopologyState) *TopologyTransition {
	for i := range v.Transitions {
		t := &v.Transitions[i]
		cpMatch := target.ControlPlane == "" || t.Target.ControlPlaneTopology == target.ControlPlane
		infraMatch := target.Infrastructure == "" || t.Target.InfrastructureTopology == target.Infrastructure
		if cpMatch && infraMatch {
			return t
		}
	}
	return nil
}

// IsTransitionAvailable checks the evaluations of a transition for the
// TopologyTransitionAvailable condition and returns its status.
func IsTransitionAvailable(t *TopologyTransition) (available bool, message string) {
	for _, eval := range t.Evaluations {
		if eval.Type == TopologyTransitionAvailableConditionType {
			if eval.Status == metav1.ConditionTrue {
				return true, eval.Message
			}
			return false, eval.Message
		}
	}
	return false, "TopologyTransitionAvailable condition not found in evaluations"
}

// GetFailedEvaluations returns evaluations with status False, excluding the
// summary TopologyTransitionAvailable condition.
func GetFailedEvaluations(t *TopologyTransition) []metav1.Condition {
	var failed []metav1.Condition
	for _, eval := range t.Evaluations {
		if eval.Type == TopologyTransitionAvailableConditionType {
			continue
		}
		if eval.Status == metav1.ConditionFalse {
			failed = append(failed, eval)
		}
	}
	return failed
}

// GetPassedEvaluations returns evaluations with status True, excluding the
// summary TopologyTransitionAvailable condition.
func GetPassedEvaluations(t *TopologyTransition) []metav1.Condition {
	var passed []metav1.Condition
	for _, eval := range t.Evaluations {
		if eval.Type == TopologyTransitionAvailableConditionType {
			continue
		}
		if eval.Status == metav1.ConditionTrue {
			passed = append(passed, eval)
		}
	}
	return passed
}

// Validate checks whether the requested transition target is valid and available
// based on the server-advertised transitions.
func (v TransitionValidator) Validate(target TopologyState) error {
	if len(v.Transitions) == 0 {
		return fmt.Errorf("no topology transitions are available on this cluster")
	}

	t := v.FindTransition(target)
	if t == nil {
		return fmt.Errorf("no matching transition found for the requested target")
	}

	available, msg := IsTransitionAvailable(t)
	if !available {
		var details strings.Builder
		fmt.Fprintf(&details, "transition is not available: %s", msg)

		failed := GetFailedEvaluations(t)
		if len(failed) > 0 {
			fmt.Fprintln(&details)
			fmt.Fprintln(&details, "  Failed prerequisites:")
			for _, f := range failed {
				fmt.Fprintf(&details, "    - %s: %s\n", f.Type, f.Message)
			}
		}

		return fmt.Errorf("%s", details.String())
	}

	return nil
}

// describeTransition returns a human-readable description of a transition.
func describeTransition(t *TopologyTransition) string {
	cpChanged := t.Source.ControlPlaneTopology != t.Target.ControlPlaneTopology
	infraChanged := t.Source.InfrastructureTopology != t.Target.InfrastructureTopology

	switch {
	case cpChanged && infraChanged:
		return fmt.Sprintf("Control Plane: %s -> %s, Infrastructure: %s -> %s",
			t.Source.ControlPlaneTopology, t.Target.ControlPlaneTopology,
			t.Source.InfrastructureTopology, t.Target.InfrastructureTopology)
	case cpChanged:
		return fmt.Sprintf("Control Plane: %s -> %s (infrastructure unchanged)",
			t.Source.ControlPlaneTopology, t.Target.ControlPlaneTopology)
	case infraChanged:
		return fmt.Sprintf("Infrastructure: %s -> %s (control plane unchanged)",
			t.Source.InfrastructureTopology, t.Target.InfrastructureTopology)
	default:
		return "No topology changes"
	}
}
