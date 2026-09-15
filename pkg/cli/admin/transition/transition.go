package transition

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	operatorv1client "github.com/openshift/client-go/operator/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/cli/admin/transition/preflight"
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

		# Validate transition readiness (dry-run, default behavior)
		oc adm transition topology --control-plane=HighlyAvailable --infrastructure=HighlyAvailable

		# Initiate transition to HighlyAvailable topology (both control plane and infrastructure)
		oc adm transition topology --control-plane=HighlyAvailable --infrastructure=HighlyAvailable --confirm

		# Bypass warning-severity preflight check failures (not recommended)
		oc adm transition topology --control-plane=HighlyAvailable --infrastructure=HighlyAvailable --confirm --allow-transition-with-warnings

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

	// AllowTransitionWithWarnings bypasses Warning-severity check failures (--allow-transition-with-warnings flag)
	allowTransitionWithWarnings bool

	kubeClient     kubernetes.Interface
	configClient   configv1client.Interface
	operatorClient operatorv1client.Interface

	// Validator for preflight checks
	validator preflight.Validator

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
	cmd.Flags().BoolVar(&o.allowTransitionWithWarnings, "allow-transition-with-warnings", o.allowTransitionWithWarnings, "Bypass warning-severity preflight check failures (requires --confirm)")

	return cmd
}

// complete sets up all required fields from the factory
func (o *transitionOptions) complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	// Get REST config
	restConfig, err := f.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to get REST config: %w", err)
	}

	// Create clients
	o.kubeClient, err = kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	o.configClient, err = configv1client.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create config client: %w", err)
	}

	o.operatorClient, err = operatorv1client.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create operator client: %w", err)
	}

	// Create validator
	o.validator = preflight.NewClientSideValidator(
		o.kubeClient,
		o.configClient,
		o.operatorClient,
	)

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

	// --allow-transition-with-warnings requires --confirm flag
	if o.allowTransitionWithWarnings && !o.confirm {
		return fmt.Errorf("--allow-transition-with-warnings requires --confirm flag")
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

// runInitiateMode validates cluster readiness and initiates topology transition
func (o *transitionOptions) runInitiateMode(ctx context.Context, currentCP, currentInfra configv1.TopologyMode) error {
	// Prepare topology states for validation
	current := preflight.TopologyState{
		ControlPlane:   currentCP,
		Infrastructure: currentInfra,
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

	target := preflight.TopologyState{
		ControlPlane:   targetCP,
		Infrastructure: targetInfra,
	}

	// Check if cluster is already at target topology (no transition needed)
	if current.ControlPlane == target.ControlPlane && current.Infrastructure == target.Infrastructure {
		_, err := fmt.Fprintf(o.Out, "Cluster is already at target topology:\n  Control Plane:    %s\n  Infrastructure:   %s\n\nNo transition needed.\n", current.ControlPlane, current.Infrastructure)
		return err
	}

	// Step 1: Run preflight validation
	if _, err := fmt.Fprintf(o.Out, "Running preflight validation...\n\n"); err != nil {
		return err
	}

	result, err := o.validator.Validate(ctx, current, target)
	if err != nil {
		return fmt.Errorf("preflight validation failed: %w", err)
	}

	// Step 2: Display validation results
	if _, err := fmt.Fprintf(o.Out, "Topology Transition:\n  Control Plane:    %s -> %s\n  Infrastructure:   %s -> %s", current.ControlPlane, target.ControlPlane, current.Infrastructure, target.Infrastructure); err != nil {
		return err
	}

	// Note if infrastructure topology is derived (not explicitly specified by user)
	if o.infrastructure == "" && current.Infrastructure != target.Infrastructure {
		if _, err := fmt.Fprintf(o.Out, " (managed by controller)"); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(o.Out, "\nStatus: %s\n\n", result.Status); err != nil {
		return err
	}

	// Group checks by severity
	var errorChecks, warningChecks []preflight.CheckResult
	for _, check := range result.Checks {
		if check.Severity == preflight.CheckSeverityError {
			errorChecks = append(errorChecks, check)
		} else {
			warningChecks = append(warningChecks, check)
		}
	}

	// Display Error-severity checks first
	if len(errorChecks) > 0 {
		if _, err := fmt.Fprintf(o.Out, "BLOCKING CHECKS (cannot be bypassed):\n"); err != nil {
			return err
		}
		for _, check := range errorChecks {
			if _, err := fmt.Fprintf(o.Out, "  %s\n", check.String()); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(o.Out, "\n"); err != nil {
			return err
		}
	}

	// Display Warning-severity checks
	if len(warningChecks) > 0 {
		if _, err := fmt.Fprintf(o.Out, "READINESS CHECKS (can be bypassed with --allow-transition-with-warnings):\n"); err != nil {
			return err
		}
		for _, check := range warningChecks {
			if _, err := fmt.Fprintf(o.Out, "  %s\n", check.String()); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(o.Out, "\n"); err != nil {
			return err
		}
	}

	// Step 3: Check for Error-severity failures (blocking - cannot proceed)
	if result.HasErrorCheckFailures() {
		return fmt.Errorf("cannot proceed with transition - see errors listed above")
	}

	// Step 4: Check for Warning-severity failures
	if result.HasWarningCheckFailures() && !o.allowTransitionWithWarnings {
		return fmt.Errorf("cluster not ready for transition - use --allow-transition-with-warnings with --confirm to bypass (not recommended)")
	}

	// Step 5: If warnings bypassed, show warning message
	if result.HasWarningCheckFailures() && o.allowTransitionWithWarnings {
		if _, err := fmt.Fprintf(o.Out, "warning: Proceeding despite failed preflight checks (--allow-transition-with-warnings)\nwarning: This may result in cluster instability or transition failure\n\n"); err != nil {
			return err
		}
	}

	// Step 6: If no --confirm, show dry-run message
	if !o.confirm {
		_, err := fmt.Fprintf(o.Out, "\nDry run: Would patch Infrastructure spec:\n  spec.controlPlaneTopology:    %s\n\nNote: The cluster-config-operator will update both status.controlPlaneTopology\n      and status.infrastructureTopology to %s based on this change.\n\nAdd --confirm to apply this transition\n", target.ControlPlane, target.ControlPlane)
		return err
	}

	// Step 7: Apply transition
	return o.applyTransition(ctx, target.ControlPlane, target.Infrastructure)
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
