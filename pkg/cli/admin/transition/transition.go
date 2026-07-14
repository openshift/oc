package transition

import (
	"context"
	"fmt"
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
	// Target control plane topology (--control-plane flag)
	controlPlane string

	// Target infrastructure topology (--infrastructure flag)
	infrastructure string

	// Confirm actually applies the transition (--confirm flag)
	// Without this flag, the command runs in dry-run mode
	confirm bool

	configClient configv1client.Interface

	validator topologyValidator

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
			kcmdutil.CheckErr(o.validate())
			kcmdutil.CheckErr(o.run(cmd.Context()))
		},
	}

	cmd.Flags().StringVar(&o.controlPlane, "control-plane", o.controlPlane, "Target control plane topology (HighlyAvailable or SingleReplica)")
	cmd.Flags().StringVar(&o.infrastructure, "infrastructure", o.infrastructure, "Target infrastructure topology (HighlyAvailable or SingleReplica)")
	cmd.Flags().BoolVar(&o.confirm, "confirm", o.confirm, "Apply the transition (default is dry-run)")

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

	o.validator = noopValidator{}

	return nil
}

// validate validates the command options
func (o *transitionOptions) validate() error {
	validTopologies := map[string]bool{
		string(configv1.HighlyAvailableTopologyMode): true,
		string(configv1.SingleReplicaTopologyMode):   true,
	}

	// Validate control plane topology if specified
	if o.controlPlane != "" && !validTopologies[o.controlPlane] {
		return fmt.Errorf("invalid control plane topology %q, must be 'HighlyAvailable' or 'SingleReplica'", o.controlPlane)
	}

	// Validate infrastructure topology if specified
	if o.infrastructure != "" && !validTopologies[o.infrastructure] {
		return fmt.Errorf("invalid infrastructure topology %q, must be 'HighlyAvailable' or 'SingleReplica'", o.infrastructure)
	}

	// --confirm requires at least one of --control-plane or --infrastructure
	if o.confirm && o.controlPlane == "" && o.infrastructure == "" {
		return fmt.Errorf("--confirm requires at least one of --control-plane or --infrastructure")
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

	currentCP := infra.Status.ControlPlaneTopology
	currentInfra := infra.Status.InfrastructureTopology

	// Mode 1: Discovery mode (no topology flags)
	if o.controlPlane == "" && o.infrastructure == "" {
		return o.runDiscoveryMode(currentCP, currentInfra)
	}

	// Mode 2: Initiate mode (at least one topology flag provided)
	return o.runInitiateMode(ctx, currentCP, currentInfra)
}

// runDiscoveryMode displays current topology and available transitions
func (o *transitionOptions) runDiscoveryMode(currentCP, currentInfra configv1.TopologyMode) error {
	if _, err := fmt.Fprintf(o.Out, "Current Topology:\n  Control Plane:    %s\n  Infrastructure:   %s\n\n", currentCP, currentInfra); err != nil {
		return err
	}

	if currentCP == configv1.SingleReplicaTopologyMode && currentInfra == configv1.SingleReplicaTopologyMode {
		_, err := fmt.Fprintf(o.Out, "Available Transition:\n  Control Plane:    SingleReplica -> HighlyAvailable\n  (Infrastructure also transitions to HighlyAvailable)\n\nTo validate transition readiness:\n  oc adm transition topology --control-plane=HighlyAvailable\n\nTo initiate transition:\n  oc adm transition topology --control-plane=HighlyAvailable --confirm\n")
		return err
	} else {
		_, err := fmt.Fprintf(o.Out, "Available Transitions:\n  (none)\n\nNote: Only SingleReplica -> HighlyAvailable transitions are currently supported.\n      Both control plane and infrastructure must be SingleReplica to transition.\n")
		return err
	}
}

// runInitiateMode previews or initiates a topology transition.
func (o *transitionOptions) runInitiateMode(ctx context.Context, currentCP, currentInfra configv1.TopologyMode) error {
	// Prepare topology states for the validator.
	current := topologyState{
		controlPlane:   currentCP,
		infrastructure: currentInfra,
	}

	// Determine target topologies
	targetCP := currentCP
	if o.controlPlane != "" {
		targetCP = configv1.TopologyMode(o.controlPlane)
	}

	// Derive target infrastructure topology
	// If user specified --infrastructure, use that (for future flexibility)
	// Otherwise, derive based on control plane transition:
	// - SNO -> HA: infrastructure becomes HighlyAvailable (managed by controller)
	// - Future transitions (e.g., HA -> HAA): infrastructure stays HighlyAvailable
	targetInfra := currentInfra
	if o.infrastructure != "" {
		targetInfra = configv1.TopologyMode(o.infrastructure)
	} else if targetCP == configv1.HighlyAvailableTopologyMode {
		// For transitions to HighlyAvailable control plane, infrastructure also becomes HighlyAvailable
		targetInfra = configv1.HighlyAvailableTopologyMode
	}

	target := topologyState{
		controlPlane:   targetCP,
		infrastructure: targetInfra,
	}

	// Check if cluster is already at target topology (no transition needed)
	if current.controlPlane == target.controlPlane && current.infrastructure == target.infrastructure {
		_, err := fmt.Fprintf(o.Out, "Cluster is already at target topology:\n  Control Plane:    %s\n  Infrastructure:   %s\n\nNo transition needed.\n", current.controlPlane, current.infrastructure)
		return err
	}

	if o.validator != nil {
		if err := o.validator.Validate(ctx, current, target); err != nil {
			return fmt.Errorf("topology validation failed: %w", err)
		}
	}

	// If no --confirm, show dry-run message.
	if !o.confirm {
		_, err := fmt.Fprintf(o.Out, "\nDry run: Would patch Infrastructure spec:\n  spec.controlPlaneTopology:    %s\n\nNote: The cluster-config-operator will update both status.controlPlaneTopology\n      and status.infrastructureTopology to %s based on this change.\n\nAdd --confirm to apply this transition\n", target.controlPlane, target.controlPlane)
		return err
	}

	// Apply transition.
	return o.applyTransition(ctx, target.controlPlane, target.infrastructure)
}

// applyTransition patches the Infrastructure resource to initiate the transition
func (o *transitionOptions) applyTransition(ctx context.Context, targetCP, targetInfra configv1.TopologyMode) error {
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
		// NOTE: InfrastructureTopology only exists in status, not spec.
		// For SNO->HA transition, the cluster-config-operator will update both
		// controlPlaneTopology and infrastructureTopology in status.
		infra.Spec.ControlPlaneTopology = targetCP

		_, err = o.configClient.ConfigV1().Infrastructures().Update(ctx, infra, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to update Infrastructure resource: %w", err)
	}

	_, err = fmt.Fprintf(o.Out, "\nInfrastructure spec patched:\n  spec.controlPlaneTopology:    %s\n\nNote: The cluster-config-operator will update status.controlPlaneTopology\n      and status.infrastructureTopology based on this change.\n\nTransition initiated. Operators will reconfigure to the new topology.\n\nMonitor transition progress with:\n  oc adm transition status\n", targetCP)
	return err
}
