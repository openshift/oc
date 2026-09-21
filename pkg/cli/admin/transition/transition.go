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
	"k8s.io/client-go/util/retry"
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
		control plane and infrastructure topology.

		Without flags, shows current topology and available transitions.
		With --control-plane and/or --infrastructure, validates readiness (dry-run).
		Add --confirm to initiate the transition.
		Use 'status' subcommand to monitor transition progress.

		Requires OC_ENABLE_CMD_TRANSITION_TOPOLOGY=true and the MutableTopology
		feature gate enabled on the cluster.
	`)

	transitionExample = templates.Examples(`
		# Show current topology and available transitions
		oc adm transition topology

		# Preview the topology change (dry-run, default behavior)
		oc adm transition topology --control-plane=HighlyAvailable --infrastructure=HighlyAvailable

		# Initiate transition to HighlyAvailable topology (both control plane and infrastructure)
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
		Long:  "Transition cluster resources such as control plane topology.",
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
		Short:   "Transition cluster control plane topology",
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

	// Mode 1: Discovery mode (no topology flags)
	if o.target.ControlPlane == "" && o.target.Infrastructure == "" {
		return o.runDiscoveryMode(ctx)
	}

	// Mode 2: Initiate mode (at least one topology flag provided)
	return o.runInitiateMode(ctx)
}

// runDiscoveryMode displays current topology and available transitions
func (o *transitionOptions) runDiscoveryMode(ctx context.Context) error {
	if _, err := fmt.Fprintf(o.Out, `
  Current Topology Configuration:
    Control Plane:\t%s
    Infrastructure:\t%s\n`,
		o.current.ControlPlane, o.current.Infrastructure); err != nil {
		return err
	}

	var output strings.Builder

	validTransitions, err := o.validator.GetValidTransitions(ctx)
	if err != nil {
		return fmt.Errorf("Unable to get the list of valid transitions: %w", err)
	}

	fmt.Fprintln(&output, "Available Transitions:")

	if len(validTransitions) == 0 {
		fmt.Fprintln(&output, "  No available transitions")

		_, err := fmt.Fprint(o.Out, output.String())
		return err
	}

	transitionFormat := `
  - Control Plane:  %s\t->\t%s
    Infrastructure: %s\t->\t%s`

	for _, transition := range validTransitions {
		fmt.Fprintf(&output, transitionFormat,
			o.current.ControlPlane, transition.ControlPlane,
			o.current.Infrastructure, transition.Infrastructure)
	}

	// Append the help text
	fmt.Fprintf(&output, `

  To validate transition readiness specify the target topology:
    oc adm transition topology --control-plane={target}

  To initiate transition provide the confirm flag:
    oc adm transition topology --control-plane={target} --confirm`)

	_, err = fmt.Fprintln(o.Out, output.String())
	return err
}

// runInitiateMode previews or initiates a topology transition.
func (o *transitionOptions) runInitiateMode(ctx context.Context) error {
	// Handle HA Compact if going from CP SNO -> HA and no infra target was specified
	if o.current.ControlPlane == configv1.SingleReplicaTopologyMode &&
		o.target.ControlPlane == configv1.HighlyAvailableTopologyMode &&
		o.current.Infrastructure == configv1.SingleReplicaTopologyMode &&
		o.target.Infrastructure == "" {
		o.target.Infrastructure = configv1.HighlyAvailableTopologyMode
	}

	// Check if cluster is already at target topology (no transition needed)
	if o.current.ControlPlane == o.target.ControlPlane && o.current.Infrastructure == o.target.Infrastructure {
		_, err := fmt.Fprintf(o.Out, `Cluster is already at target topology:
  Control Plane:   %s
  Infrastructure:  %s

  No transition initiated.
`, o.current.ControlPlane, o.current.Infrastructure)
		return err
	}

	if err := o.validator.Validate(ctx, o.target); err != nil {
		return fmt.Errorf("topology validation failed: %w", err)
	}

	// If no --confirm, show dry-run message
	if !o.args.confirm {
		_, err := fmt.Fprintf(o.Out, `
Dry run: Would patch Infrastructure spec:
  spec.controlPlaneTopology:
    %s

Note: The cluster-config-operator will update both status.controlPlaneTopology
    and status.infrastructureTopology to %s based on this change.

Add --confirm to apply this transition
`,
			string(o.target.ControlPlane), string(o.target.ControlPlane))
		return err
	}

	// Apply transition
	return o.applyTransition(ctx)
}

// applyTransition patches the Infrastructure resource to initiate the transition
func (o *transitionOptions) applyTransition(ctx context.Context) error {
	if _, err := fmt.Fprintf(o.Out, "Initiating topology transition...\n"); err != nil {
		return err
	}

	// Update Infrastructure resource with retry on conflict
	// Use RetryOnConflict to handle resourceVersion conflicts with other controllers
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get current Infrastructure resource
		infra, err := o.configClient.ConfigV1().Infrastructures().Get(ctx, infrastructureResourceName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get Infrastructure resource: %w", err)
		}

		// Patch control plane topology (spec only supports controlPlaneTopology)
		// NOTE: InfrastructureTopology only exists in status currently, not spec.
		// For SNO->HA transition, the cluster-config-operator will update both
		// controlPlaneTopology and infrastructureTopology in status.
		infra.Spec.ControlPlaneTopology = o.target.ControlPlane

		_, err = o.configClient.ConfigV1().Infrastructures().Update(ctx, infra, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to update Infrastructure resource: %w", err)
	}

	_, err = fmt.Fprintf(o.Out, `
Infrastructure spec patched:
  spec.controlPlaneTopology:
    %s

Note: The cluster-config-operator will update status.controlPlaneTopology
  and status.infrastructureTopology based on this change.

Transition initiated. Operators will reconfigure to the new topology.

Monitor transition progress with:
  oc adm transition status
`,
		string(o.target.ControlPlane))
	return err
}
