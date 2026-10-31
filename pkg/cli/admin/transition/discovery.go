package transition

import (
	"encoding/json"
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// runDiscoveryMode displays the controller's report, without deriving transitions locally.
func (o *transitionOptions) runDiscoveryMode(infra *configv1.Infrastructure) error {
	if *o.printFlags.OutputFormat != "" {
		printer, err := o.printFlags.ToPrinter()
		if err != nil {
			return err
		}

		report := struct {
			Metadata struct {
				Generation int64 `json:"generation"`
			} `json:"metadata"`
			configv1.TopologyTransitionStatus
		}{TopologyTransitionStatus: infra.Status.TopologyTransitionStatus}
		report.Metadata.Generation = infra.Generation

		data, err := json.Marshal(report)
		if err != nil {
			return fmt.Errorf("failed to encode topology transition status: %w", err)
		}

		return printer.PrintObj(&runtime.Unknown{Raw: data}, o.Out)
	}

	var output strings.Builder
	fmt.Fprintf(&output, "Current Topology Configuration:\n  Control Plane:  %s\n  Infrastructure: %s\n\n",
		infra.Status.ControlPlaneTopology, infra.Status.InfrastructureTopology)

	report := &infra.Status.TopologyTransitionStatus
	evaluated := meta.FindStatusCondition(report.Conditions, configv1.TopologyTransitionsEvaluatedConditionType)
	if evaluated == nil {
		fmt.Fprintln(&output, "Discovery results are unavailable. The MutableTopology feature gate may be disabled, or the cluster has not reported results yet.")
	} else if evaluated.Status != metav1.ConditionTrue || evaluated.ObservedGeneration != infra.Generation {
		fmt.Fprintln(&output, "Discovery results are not current. Wait for the controller to finish evaluating transitions.")
		fmt.Fprintf(&output, "  %s: %s\n", evaluated.Reason, evaluated.Message)
	} else {
		printTransitionOptions(&output, report.Transitions)
	}

	_, err := fmt.Fprint(o.Out, output.String())
	return err
}

// printTransitionOptions lists reported transitions, their availability, and failed checks.
// It includes preview instructions only when at least one transition is available.
func printTransitionOptions(output *strings.Builder, transitions []configv1.TopologyTransition) {
	fmt.Fprintln(output, "Supported Transitions:")
	availableCount := 0
	for _, transition := range transitions {
		availability := meta.FindStatusCondition(transition.Evaluations, configv1.TopologyTransitionAvailableConditionType)
		state := "Unknown"
		if availability != nil {
			switch availability.Status {
			case metav1.ConditionTrue:
				state = "Available"
				availableCount++
			case metav1.ConditionFalse:
				state = "Blocked"
			}
		}

		fmt.Fprintf(output, "  - Control Plane:  %s -> %s\n    Infrastructure: %s -> %s\n    Availability:   %s\n",
			transition.Source.ControlPlaneTopology, transition.Target.ControlPlaneTopology,
			transition.Source.InfrastructureTopology, transition.Target.InfrastructureTopology, state)
		if availability != nil && availability.Status != metav1.ConditionTrue {
			fmt.Fprintf(output, "    %s: %s\n", availability.Reason, availability.Message)
		}
		for _, check := range transition.Evaluations {
			if check.Type != configv1.TopologyTransitionAvailableConditionType && check.Status != metav1.ConditionTrue {
				fmt.Fprintf(output, "    %s (%s): %s: %s\n", check.Type, check.Status, check.Reason, check.Message)
			}
		}
	}

	if availableCount == 0 {
		fmt.Fprintln(output, "No available transitions")
		return
	}

	fmt.Fprintln(output, "\nTo preview a transition, specify its target topology:")
	fmt.Fprintln(output, "  oc adm transition topology --control-plane={target} --infrastructure={target}")
	fmt.Fprintln(output, "Add --confirm to initiate the transition. Availability may change before the request is processed.")
}
