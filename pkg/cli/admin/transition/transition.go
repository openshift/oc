package transition

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

const (
	apiRequestTimeout          = 2 * time.Minute
	infrastructureResourceName = "cluster"
)

// Cluster-config-operator (CCO) resource and condition types
const (
	// clusterConfigOperatorResourceName is the name of the cluster-scoped Config resource
	clusterConfigOperatorResourceName = "cluster"

	// topologyTransitionControllerProgressingCondition indicates topology transition is in progress
	topologyTransitionControllerProgressingCondition = "TopologyTransitionControllerProgressing"

	// topologyTransitionControllerUpgradeableCondition indicates if cluster can be upgraded during transition
	topologyTransitionControllerUpgradeableCondition = "TopologyTransitionControllerUpgradeable"

	// topologyTransitionPreflightCheckFailedReason indicates preflight validation failed
	topologyTransitionPreflightCheckFailedReason = "PreflightCheckFailed"

	// topologyTransitionUnsupportedTransitionReason indicates the requested transition is not supported
	topologyTransitionUnsupportedTransitionReason = "UnsupportedTransition"
)

var (
	transitionLong = templates.LongDesc(`
		Transition cluster control plane and infrastructure topology between
		Single-Node OpenShift (SNO) and Highly Available (HA) configurations.

		Supports transitioning from SingleReplica to HighlyAvailable for both
		control plane and infrastructure topology. Infrastructure topology can
		be transitioned independently when the control plane is already
		HighlyAvailable.

		Without flags, shows current topology and available transitions by
		reading the server-advertised transitions from the Infrastructure
		resource.

		With --control-plane and/or --infrastructure, validates readiness and
		shows what would change (dry-run). Add --confirm to initiate the
		transition.

		Use 'status' subcommand to monitor transition progress.

		Requires OC_ENABLE_CMD_TRANSITION_TOPOLOGY=true and the MutableTopology
		feature gate enabled on the cluster.
	`)

	transitionExample = templates.Examples(`
		# Show current topology and available transitions
		oc adm transition topology

		# Preview infrastructure topology transition (dry-run)
		oc adm transition topology --infrastructure=HighlyAvailable

		# Initiate standalone infrastructure topology transition
		oc adm transition topology --infrastructure=HighlyAvailable --confirm

		# Preview control plane + infrastructure transition (dry-run)
		oc adm transition topology --control-plane=HighlyAvailable --infrastructure=HighlyAvailable

		# Initiate both control plane and infrastructure transition
		oc adm transition topology --control-plane=HighlyAvailable --infrastructure=HighlyAvailable --confirm

		# Monitor transition progress
		oc adm transition status
	`)
)

// transitionOptions holds all options for the topology transition command
type transitionOptions struct {
	current, target TopologyState
	validator       TransitionValidator

	args struct {
		targetControlPlaneTopology   string
		targetInfrastructureTopology string

		// Confirm actually applies the transition (--confirm flag)
		// Without this flag, the command runs in dry-run mode
		confirm bool
	}

	configClient   configv1client.Interface
	topologyClient topologyTransitionClient

	genericclioptions.IOStreams
}

// newTransitionOptions creates a new transitionOptions with default values
func newTransitionOptions(streams genericclioptions.IOStreams) *transitionOptions {
	return &transitionOptions{
		IOStreams: streams,
	}
}

// NewCmdTransition creates the transition command with topology and status subcommands
func NewCmdTransition(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transition",
		Short: "Transition cluster resources",
		Long:  "Transition cluster resources such as control plane and infrastructure topology.",
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	// Add topology subcommand
	cmd.AddCommand(newCmdTopology(f, streams))
	// Add status subcommand (peer to topology, not nested)
	cmd.AddCommand(newCmdStatus(f, streams))

	return cmd
}

// newCmdTopology creates the topology transition subcommand
func newCmdTopology(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := newTransitionOptions(streams)

	cmd := &cobra.Command{
		Use:     "topology",
		Short:   "Transition cluster control plane and infrastructure topology",
		Long:    transitionLong,
		Example: transitionExample,
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.complete(f, cmd, args))
			kcmdutil.CheckErr(o.validateCommand())
			kcmdutil.CheckErr(o.run(cmd.Context()))
		},
	}

	cmd.Flags().StringVar(&o.args.targetControlPlaneTopology, "control-plane", "", "Target control plane topology (HighlyAvailable or SingleReplica)")
	cmd.Flags().StringVar(&o.args.targetInfrastructureTopology, "infrastructure", "", "Target infrastructure topology (HighlyAvailable or SingleReplica)")
	cmd.Flags().BoolVar(&o.args.confirm, "confirm", false, "Apply the transition (default is dry-run)")

	return cmd
}

// complete sets up all required fields from the factory
func (o *transitionOptions) complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	restConfig, err := f.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to get REST config: %w", err)
	}

	o.configClient, err = configv1client.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create config client: %w", err)
	}

	o.topologyClient = newRESTTopologyClient(o.configClient.ConfigV1().RESTClient())

	return nil
}

// validateCommand validates the command options
func (o *transitionOptions) validateCommand() error {
	validControlPlaneTopologies := map[string]bool{
		string(configv1.HighlyAvailableTopologyMode): true,
		string(configv1.SingleReplicaTopologyMode):   true,
	}

	validInfrastructureTopologies := map[string]bool{
		string(configv1.HighlyAvailableTopologyMode): true,
		string(configv1.SingleReplicaTopologyMode):   true,
	}

	// Control Plane Topology Arg
	err := o.validateTopologyCommand(o.args.targetControlPlaneTopology, validControlPlaneTopologies)
	if err != nil {
		return fmt.Errorf("invalid control plane topology '%s': %w", o.args.targetControlPlaneTopology, err)
	}
	o.target.ControlPlane = configv1.TopologyMode(o.args.targetControlPlaneTopology)

	// Infrastructure Topology Arg
	err = o.validateTopologyCommand(o.args.targetInfrastructureTopology, validInfrastructureTopologies)
	if err != nil {
		return fmt.Errorf("invalid infrastructure topology '%s': %w", o.args.targetInfrastructureTopology, err)
	}
	o.target.Infrastructure = configv1.TopologyMode(o.args.targetInfrastructureTopology)

	// --confirm requires at least one of --control-plane or --infrastructure
	if o.args.confirm &&
		o.args.targetControlPlaneTopology == "" &&
		o.args.targetInfrastructureTopology == "" {
		return fmt.Errorf("--confirm requires at least one of --control-plane or --infrastructure")
	}

	return nil
}

func (o *transitionOptions) validateTopologyCommand(value string, validTopologies map[string]bool) error {
	if value == "" {
		return nil
	}

	if !validTopologies[value] {
		return fmt.Errorf("must be one of: %s", strings.Join(slices.Collect(maps.Keys(validTopologies)), " "))
	}

	return nil
}

// run executes the topology transition command
func (o *transitionOptions) run(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, apiRequestTimeout)
	defer cancel()

	// Get current topologies from Infrastructure resource
	infra, err := o.configClient.ConfigV1().Infrastructures().Get(ctx, infrastructureResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get Infrastructure resource: %w", err)
	}

	o.current = TopologyState{
		ControlPlane:   infra.Status.ControlPlaneTopology,
		Infrastructure: infra.Status.InfrastructureTopology,
	}

	// Read server-advertised transitions from topologyTransitionStatus
	transitionStatus, err := o.topologyClient.getTopologyTransitionStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to read topology transition status: %w", err)
	}

	if transitionStatus != nil {
		o.validator.Transitions = transitionStatus.Transitions
	}
	o.validator.Current = o.current

	// Mode 1: Discovery mode (no topology flags)
	if o.target.ControlPlane == "" && o.target.Infrastructure == "" {
		return o.runDiscoveryMode(ctx, transitionStatus)
	}

	// Mode 2: Initiate mode (at least one topology flag provided)
	return o.runInitiateMode(ctx)
}

// runDiscoveryMode displays current topology and available transitions from the server
func (o *transitionOptions) runDiscoveryMode(ctx context.Context, transitionStatus *TopologyTransitionStatus) error {
	infraSpec, err := o.topologyClient.getInfrastructureTopologySpec(ctx)
	if err != nil {
		return fmt.Errorf("failed to read infrastructure topology spec: %w", err)
	}

	cpSpecTopology := formatTopologyValue(string(o.current.ControlPlane))
	infraSpecTopology := formatTopologyValue(string(infraSpec))

	if _, err := fmt.Fprintf(o.Out, `
Current Topology Configuration:
  Control Plane:
    Spec (desired):   %s
    Status (current): %s
  Infrastructure:
    Spec (desired):   %s
    Status (current): %s
`, cpSpecTopology, formatTopologyValue(string(o.current.ControlPlane)),
		infraSpecTopology, formatTopologyValue(string(o.current.Infrastructure))); err != nil {
		return err
	}

	// Display evaluation status
	if transitionStatus != nil {
		if evalCond := findCondition(transitionStatus.Conditions, TopologyTransitionsEvaluatedConditionType); evalCond != nil {
			fmt.Fprintf(o.Out, "\nTransition Evaluation: %s (%s)\n", evalCond.Reason, evalCond.Message)
		}

		if completedCond := findCondition(transitionStatus.Conditions, TopologyTransitionCompletedConditionType); completedCond != nil {
			fmt.Fprintf(o.Out, "Transition Status:     %s (%s)\n", completedCond.Reason, completedCond.Message)
		}
	}

	// Display available transitions
	var output strings.Builder
	fmt.Fprintln(&output, "\nAvailable Transitions:")

	available := o.validator.GetAvailableTransitions()
	if len(available) == 0 {
		fmt.Fprintln(&output, "  No available transitions")
		_, err := fmt.Fprint(o.Out, output.String())
		return err
	}

	for _, t := range available {
		isAvailable, _ := IsTransitionAvailable(&t)
		status := "not available"
		if isAvailable {
			status = "available"
		}

		fmt.Fprintf(&output, "\n  %s [%s]\n", describeTransition(&t), status)

		for _, eval := range t.Evaluations {
			if eval.Type == TopologyTransitionAvailableConditionType {
				continue
			}
			marker := "+"
			if eval.Status == metav1.ConditionFalse {
				marker = "-"
			}
			fmt.Fprintf(&output, "    %s %s: %s\n", marker, eval.Type, eval.Message)
		}
	}

	// Append usage help
	fmt.Fprintf(&output, `
To validate transition readiness, specify the target topology:
  oc adm transition topology --control-plane={target}
  oc adm transition topology --infrastructure={target}

To initiate a transition, provide the confirm flag:
  oc adm transition topology --infrastructure=HighlyAvailable --confirm
`)

	_, err = fmt.Fprint(o.Out, output.String())
	return err
}

// runInitiateMode previews or initiates a topology transition.
func (o *transitionOptions) runInitiateMode(ctx context.Context) error {
	// HA Compact auto-fill: if transitioning CP from SNO->HA and no infra target specified,
	// auto-set infrastructure to HA as well (compact cluster behavior).
	if o.current.ControlPlane == configv1.SingleReplicaTopologyMode &&
		o.target.ControlPlane == configv1.HighlyAvailableTopologyMode &&
		o.current.Infrastructure == configv1.SingleReplicaTopologyMode &&
		o.target.Infrastructure == "" {
		o.target.Infrastructure = configv1.HighlyAvailableTopologyMode
	}

	// Determine effective target (fill in current values for unspecified flags)
	effectiveTarget := TopologyState{
		ControlPlane:   o.target.ControlPlane,
		Infrastructure: o.target.Infrastructure,
	}
	if effectiveTarget.ControlPlane == "" {
		effectiveTarget.ControlPlane = o.current.ControlPlane
	}
	if effectiveTarget.Infrastructure == "" {
		effectiveTarget.Infrastructure = o.current.Infrastructure
	}

	// Check if cluster is already at target topology
	if o.current.ControlPlane == effectiveTarget.ControlPlane &&
		o.current.Infrastructure == effectiveTarget.Infrastructure {
		_, err := fmt.Fprintf(o.Out, `Cluster is already at target topology:
  Control Plane:   %s
  Infrastructure:  %s

  No transition initiated.
`, o.current.ControlPlane, o.current.Infrastructure)
		return err
	}

	// Validate the transition against server-advertised available transitions
	if err := o.validator.Validate(effectiveTarget); err != nil {
		return fmt.Errorf("topology validation failed: %w", err)
	}

	// Determine what spec fields will change
	cpChange := o.target.ControlPlane != "" && o.target.ControlPlane != o.current.ControlPlane
	infraChange := o.target.Infrastructure != "" && o.target.Infrastructure != o.current.Infrastructure

	// If no --confirm, show dry-run preview
	if !o.args.confirm {
		return o.printDryRun(cpChange, infraChange)
	}

	// Apply transition
	return o.applyTransition(ctx, cpChange, infraChange)
}

// printDryRun displays what the transition would change without applying it.
func (o *transitionOptions) printDryRun(cpChange, infraChange bool) error {
	var output strings.Builder
	fmt.Fprintln(&output, "Dry run: The following changes would be applied:")

	if cpChange {
		fmt.Fprintf(&output, "  spec.controlPlaneTopology:   %s -> %s\n",
			o.current.ControlPlane, o.target.ControlPlane)
	}
	if infraChange {
		fmt.Fprintf(&output, "  spec.infrastructureTopology: %s -> %s\n",
			o.current.Infrastructure, o.target.Infrastructure)
	}

	// Show prerequisite evaluations if available
	t := o.validator.FindTransition(TopologyState{
		ControlPlane:   o.target.ControlPlane,
		Infrastructure: o.target.Infrastructure,
	})
	if t != nil {
		passed := GetPassedEvaluations(t)
		failed := GetFailedEvaluations(t)

		if len(passed) > 0 || len(failed) > 0 {
			fmt.Fprintln(&output, "\n  Prerequisites:")
			for _, p := range passed {
				fmt.Fprintf(&output, "    + %s: %s\n", p.Type, p.Message)
			}
			for _, f := range failed {
				fmt.Fprintf(&output, "    - %s: %s\n", f.Type, f.Message)
			}
		}
	}

	fmt.Fprintln(&output, "\nAdd --confirm to apply this transition")

	_, err := fmt.Fprint(o.Out, output.String())
	return err
}

// applyTransition patches the Infrastructure resource to initiate the transition.
func (o *transitionOptions) applyTransition(ctx context.Context, cpChange, infraChange bool) error {
	if _, err := fmt.Fprintf(o.Out, "Initiating topology transition...\n"); err != nil {
		return err
	}

	var patchCP, patchInfra configv1.TopologyMode
	if cpChange {
		patchCP = o.target.ControlPlane
	}
	if infraChange {
		patchInfra = o.target.Infrastructure
	}

	if err := o.topologyClient.patchTopologySpec(ctx, patchCP, patchInfra); err != nil {
		return fmt.Errorf("failed to update Infrastructure resource: %w", err)
	}

	var output strings.Builder
	fmt.Fprintln(&output, "\nInfrastructure spec patched:")
	if cpChange {
		fmt.Fprintf(&output, "  spec.controlPlaneTopology:   %s\n", o.target.ControlPlane)
	}
	if infraChange {
		fmt.Fprintf(&output, "  spec.infrastructureTopology: %s\n", o.target.Infrastructure)
	}

	fmt.Fprintln(&output, "\nTransition initiated. Monitor progress with:")
	fmt.Fprintln(&output, "  oc adm transition status")

	_, err := fmt.Fprint(o.Out, output.String())
	return err
}

// formatTopologyValue returns "(not set)" for empty topology values.
func formatTopologyValue(v string) string {
	if v == "" {
		return "(not set)"
	}
	return v
}

// findCondition returns the condition with the given type, or nil.
func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}
