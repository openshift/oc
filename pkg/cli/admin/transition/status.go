package transition

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	metav1helpers "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
	"sigs.k8s.io/yaml"
)

var (
	statusLong = templates.LongDesc(`
		Monitor topology transition progress.

		Displays the current control plane and infrastructure topology status,
		and shows the cluster-config-operator transition conditions to monitor
		transition progress.
	`)

	statusExample = templates.Examples(`
		# Monitor transition progress
		oc adm transition status
	`)
)

// statusOptions holds options for the status subcommand
type statusOptions struct {
	configClient configv1client.Interface

	genericclioptions.IOStreams
}

// newCmdStatus creates the status subcommand
func newCmdStatus(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := &statusOptions{
		IOStreams: streams,
	}

	cmd := &cobra.Command{
		Use:     "status",
		Short:   "Monitor topology transition progress",
		Long:    statusLong,
		Example: statusExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.complete(f, cmd, args))
			kcmdutil.CheckErr(o.run(cmd.Context()))
		},
	}

	return cmd
}

// complete sets up all required fields from the factory
func (o *statusOptions) complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	restConfig, err := f.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to get REST config: %w", err)
	}

	o.configClient, err = configv1client.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create config client: %w", err)
	}

	return nil
}

// run executes the status subcommand
func (o *statusOptions) run(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, apiRequestTimeout)
	defer cancel()

	infra, err := o.configClient.ConfigV1().Infrastructures().Get(ctx, infrastructureResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get Infrastructure resource: %w", err)
	}

	if err := o.printTopologyStatus(infra); err != nil {
		return err
	}

	return o.printTopologyTransitionStatus(infra)
}

// printTopologyStatus will output the Control Plane and Infrastructure topologies from spec and status
func (o *statusOptions) printTopologyStatus(infra *configv1.Infrastructure) error {

	notSet := "(not set)"
	cpSpecTopology := string(infra.Spec.ControlPlaneTopology)
	if cpSpecTopology == "" {
		cpSpecTopology = notSet
	}

	cpStatusTopology := string(infra.Status.ControlPlaneTopology)
	if cpStatusTopology == "" {
		cpStatusTopology = notSet
	}

	infraStatusTopology := string(infra.Status.InfrastructureTopology)
	if infraStatusTopology == "" {
		infraStatusTopology = notSet
	}

	var output strings.Builder
	statusOutput := `
Control Plane Topology:
  Spec (desired):   %s
  Status (current): %s

Infrastructure Topology:
  Status (current): %s
`
	fmt.Fprintf(&output, statusOutput, cpSpecTopology, cpStatusTopology, infraStatusTopology)

	if _, err := io.WriteString(o.Out, output.String()); err != nil {
		return err
	}

	return nil
}

// printTopologyTransitionStatus outputs the topology transition status
// conditions and the available transitions, marshalled from
// transitionStatusOutput.
func (o *statusOptions) printTopologyTransitionStatus(infra *configv1.Infrastructure) error {

	// Pull out the relevant conditions from infrastructure API.
	evaluatedCond := metav1helpers.FindStatusCondition(infra.Status.TopologyTransitionStatus.Conditions, configv1.TopologyTransitionsEvaluatedConditionType)
	completedCond := metav1helpers.FindStatusCondition(infra.Status.TopologyTransitionStatus.Conditions, configv1.TopologyTransitionCompletedConditionType)

	output := transitionStatusBody{
		Evaluated: newTransitionConditionOutput(evaluatedCond),
		Completed: newTransitionConditionOutput(completedCond),
	}
	for _, transition := range infra.Status.TopologyTransitionStatus.Transitions {
		output.AvailableTransitions = append(output.AvailableTransitions, newAvailableTransitionOutput(transition))
	}

	marshalled, err := yaml.Marshal(transitionStatusHeader{output})
	if err != nil {
		return fmt.Errorf("failed to marshal transition status output: %w", err)
	}
	_, err = fmt.Fprintln(o.Out, string(marshalled))
	return err
}

// transitionStatusHeader provides the root level node of the transition status output
// as a nicety for pretty output
type transitionStatusHeader struct {
	TransitionHeader transitionStatusBody `json:"Transistion Status"`
}

// transitionStatusBody encapsulates the topology transition status output so
// it can be marshalled to JSON or YAML.
type transitionStatusBody struct {
	// Evaluated is the TopologyTransitionsEvaluated condition of the transition status.
	Evaluated transitionConditionOutput `json:"Evaluated"`

	// Completed is the TopologyTransitionCompleted condition of the transition status.
	Completed transitionConditionOutput `json:"Completed"`

	// AvailableTransitions lists the transitions the cluster may perform
	// together with their evaluations. Omitted when no transitions are available.
	AvailableTransitions []availableTransitionOutput `json:"Available Transitions,omitempty"`
}

// transitionConditionOutput describes a topology transition condition. Type is
// only set for transition evaluations, where it labels the evaluation.
type transitionConditionOutput struct {
	Type    string `json:"Type,omitempty"`
	Status  string `json:"Status,omitempty"`
	Reason  string `json:"Reason,omitempty"`
	Message string `json:"Message"`
}

// availableTransitionOutput describes a transition from a Source to a Target
// topology together with the evaluations performed for it.
type availableTransitionOutput struct {
	Source      transitionTopologyOutput    `json:"Source"`
	Target      transitionTopologyOutput    `json:"Target"`
	Evaluations []transitionConditionOutput `json:"Evaluations"`
}

// transitionTopologyOutput describes a control plane and infrastructure topology pair.
type transitionTopologyOutput struct {
	ControlPlaneTopology   string `json:"Control Plane Topology"`
	InfrastructureTopology string `json:"Infrastructure Topology"`
}

// conditionNotAvailable is reported for topology transition conditions that
// are not present in the Infrastructure status.
const conditionNotAvailable = "Condition not available"

// newTransitionConditionOutput converts a topology transition condition into
// its output representation. A nil condition maps to a "Condition not
// available" message: the controller always reports both conditions once
// TopologyTransitionStatus is set, but that status is optional and may be
// absent on clusters where the topology controller does not report it.
func newTransitionConditionOutput(cond *metav1.Condition) transitionConditionOutput {
	if cond == nil {
		return transitionConditionOutput{Message: conditionNotAvailable}
	}
	return transitionConditionOutput{
		Status:  string(cond.Status),
		Reason:  cond.Reason,
		Message: cond.Message,
	}
}

// newAvailableTransitionOutput converts a topology transition and its
// evaluations into their output representation.
func newAvailableTransitionOutput(transition configv1.TopologyTransition) availableTransitionOutput {
	output := availableTransitionOutput{
		Source:      newTransitionTopologyOutput(transition.Source),
		Target:      newTransitionTopologyOutput(transition.Target),
		Evaluations: make([]transitionConditionOutput, 0, len(transition.Evaluations)),
	}
	for _, eval := range transition.Evaluations {
		evalOutput := newTransitionConditionOutput(&eval)
		evalOutput.Type = eval.Type
		output.Evaluations = append(output.Evaluations, evalOutput)
	}
	return output
}

// newTransitionTopologyOutput converts a topology state into its output representation.
func newTransitionTopologyOutput(state configv1.TopologyState) transitionTopologyOutput {
	return transitionTopologyOutput{
		ControlPlaneTopology:   string(state.ControlPlaneTopology),
		InfrastructureTopology: string(state.InfrastructureTopology),
	}
}
