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
	Current     TopologyState
	Transitions []configv1.TopologyTransition
}

// FindTransition finds the transition entry whose source matches the current
// cluster state and whose target matches the requested topology. Target fields
// that are empty (unspecified by the user) are treated as wildcards.
func (v TransitionValidator) FindTransition(target TopologyState) *configv1.TopologyTransition {
	for i := range v.Transitions {
		t := &v.Transitions[i]
		sourceMatch := t.Source.ControlPlaneTopology == v.Current.ControlPlane &&
			t.Source.InfrastructureTopology == v.Current.Infrastructure
		if !sourceMatch {
			continue
		}
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
func IsTransitionAvailable(t *configv1.TopologyTransition) (available bool, message string) {
	for _, eval := range t.Evaluations {
		if eval.Type == configv1.TopologyTransitionAvailableConditionType {
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
func GetFailedEvaluations(t *configv1.TopologyTransition) []metav1.Condition {
	var failed []metav1.Condition
	for _, eval := range t.Evaluations {
		if eval.Type == configv1.TopologyTransitionAvailableConditionType {
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
func GetPassedEvaluations(t *configv1.TopologyTransition) []metav1.Condition {
	var passed []metav1.Condition
	for _, eval := range t.Evaluations {
		if eval.Type == configv1.TopologyTransitionAvailableConditionType {
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
