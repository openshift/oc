package transition

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

		Without target flags, reads current topology and supported transitions from
		Infrastructure status. Shows each transition's availability and blocked checks.
		Missing or stale evaluations are not treated as available transitions.
		Use -o json or -o yaml to print the raw topologyTransitionStatus report,
		including conditions, retained results, and Infrastructure metadata.generation.
		Before using retained results, consumers must check that
		TopologyTransitionsEvaluated is True and its observedGeneration matches
		metadata.generation.
		With --control-plane and/or --infrastructure, validates readiness (dry-run).
		Add --confirm to initiate the transition.
		Use 'status' subcommand to monitor transition progress.

		Requires OC_ENABLE_CMD_TRANSITION_TOPOLOGY=true and the MutableTopology
		feature gate enabled on the cluster.
	`)

	transitionExample = templates.Examples(`
		# Show current topology and available transitions
		oc adm transition topology

		# Print the controller's discovery report as JSON or YAML
		oc adm transition topology -o json
		oc adm transition topology -o yaml

		# Preview infrastructure topology transition (dry-run)
		oc adm transition topology --infrastructure=HighlyAvailable

		# Preview control plane + infrastructure transition (dry-run)
		oc adm transition topology --control-plane=HighlyAvailable --infrastructure=HighlyAvailable

		# Initiate standalone infrastructure topology transition
		oc adm transition topology --infrastructure=HighlyAvailable --confirm

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

	configClient configv1client.Interface
	printFlags   *genericclioptions.PrintFlags

	genericclioptions.IOStreams
}

// newTransitionOptions creates a new transitionOptions with default values
func newTransitionOptions(streams genericclioptions.IOStreams) *transitionOptions {
	return &transitionOptions{
		IOStreams:  streams,
		printFlags: genericclioptions.NewPrintFlags(""),
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
	cmd.Flags().StringVarP(o.printFlags.OutputFormat, "output", "o", "", "Output format for discovery: json or yaml")

	return cmd
}

// complete sets up all required fields from the factory
func (o *transitionOptions) complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	// Get REST config
	restConfig, err := f.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to get REST config: %w", err)
	}

	o.configClient, err = configv1client.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create config client: %w", err)
	}

	o.validator = TransitionValidator{}

	return nil
}

// validateCommand validates the command options
func (o *transitionOptions) validateCommand() error {
	if format := *o.printFlags.OutputFormat; format != "" {
		if format != "json" && format != "yaml" {
			return fmt.Errorf("unsupported output format %q: must be json or yaml", format)
		}
		if o.args.targetControlPlaneTopology != "" || o.args.targetInfrastructureTopology != "" || o.args.confirm {
			return fmt.Errorf("--output cannot be combined with --control-plane, --infrastructure, or --confirm")
		}
	}

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
		return nil // Handle the empty arg passed through
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
	o.validator.Current = o.current
	o.validator.Transitions = infra.Status.TopologyTransitionStatus.Transitions

	// Mode 1: Discovery mode (no topology flags)
	if o.target.ControlPlane == "" && o.target.Infrastructure == "" {
		return o.runDiscoveryMode(infra)
	}

	evaluated := meta.FindStatusCondition(
		infra.Status.TopologyTransitionStatus.Conditions,
		configv1.TopologyTransitionsEvaluatedConditionType,
	)

	if evaluated == nil ||
		evaluated.Status != metav1.ConditionTrue ||
		evaluated.ObservedGeneration != infra.Generation {
		return fmt.Errorf("topology transition evaluations are unavailable or not current; wait for the controller to finish evaluating transitions")
	}

	// Mode 2: Initiate mode (at least one topology flag provided)
	return o.runInitiateMode(ctx)
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
		return o.printDryRun(effectiveTarget, cpChange, infraChange)
	}

	// Apply transition
	return o.applyTransition(ctx, cpChange, infraChange)
}

// printDryRun displays what the transition would change without applying it.
func (o *transitionOptions) printDryRun(effectiveTarget TopologyState, cpChange, infraChange bool) error {
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
	t := o.validator.FindTransition(effectiveTarget)
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

	// Merge-patch only the spec fields that change, so unspecified topology
	// fields keep their current values.
	spec := map[string]interface{}{}
	if cpChange {
		spec["controlPlaneTopology"] = string(o.target.ControlPlane)
	}
	if infraChange {
		spec["infrastructureTopology"] = string(o.target.Infrastructure)
	}

	patchBytes, err := json.Marshal(map[string]interface{}{"spec": spec})
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %w", err)
	}

	if _, err := o.configClient.ConfigV1().Infrastructures().Patch(
		ctx, infrastructureResourceName, types.MergePatchType, patchBytes, metav1.PatchOptions{}); err != nil {
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

	_, err = fmt.Fprint(o.Out, output.String())
	return err
}

// formatTopologyValue returns "(not set)" for empty topology values.
func formatTopologyValue(v string) string {
	if v == "" {
		return "(not set)"
	}
	return v
}
