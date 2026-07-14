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

// Kubernetes and OpenShift resource names
const (
	// infrastructureResourceName is the name of the cluster-scoped Infrastructure resource
	infrastructureResourceName = "cluster"

	// etcdOperatorResourceName is the name of the cluster-scoped Etcd operator resource
	etcdOperatorResourceName = "cluster"

	// etcdNamespace is the namespace where etcd resources are located
	etcdNamespace = "openshift-etcd"

	// etcdEndpointsConfigMapName is the name of the ConfigMap containing etcd endpoints
	etcdEndpointsConfigMapName = "etcd-endpoints"
)

// ClusterOperator condition types
const (
	// clusterOperatorAvailable indicates the operand is available
	clusterOperatorAvailable configv1.ClusterStatusConditionType = configv1.OperatorAvailable

	// clusterOperatorProgressing indicates the operand is being updated
	clusterOperatorProgressing configv1.ClusterStatusConditionType = configv1.OperatorProgressing

	// clusterOperatorDegraded indicates the operand is degraded
	clusterOperatorDegraded configv1.ClusterStatusConditionType = configv1.OperatorDegraded
)

// Etcd operator condition types
const (
	// etcdMembersAvailableCondition indicates etcd has quorum
	etcdMembersAvailableCondition = "EtcdMembersAvailable"
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
	ControlPlane string

	// Target infrastructure topology (--infrastructure flag)
	Infrastructure string

	// Confirm actually applies the transition (--confirm flag)
	// Without this flag, the command runs in dry-run mode
	Confirm bool

	// AllowTransitionWithWarnings bypasses Warning-severity check failures (--allow-transition-with-warnings flag)
	AllowTransitionWithWarnings bool

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
			kcmdutil.CheckErr(o.run())
		},
	}

	cmd.Flags().StringVar(&o.ControlPlane, "control-plane", o.ControlPlane, "Target control plane topology (HighlyAvailable or SingleReplica)")
	cmd.Flags().StringVar(&o.Infrastructure, "infrastructure", o.Infrastructure, "Target infrastructure topology (HighlyAvailable or SingleReplica)")
	cmd.Flags().BoolVar(&o.Confirm, "confirm", false, "Apply the transition (default is dry-run)")
	cmd.Flags().BoolVar(&o.AllowTransitionWithWarnings, "allow-transition-with-warnings", false, "Bypass warning-severity preflight check failures (requires --confirm)")

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
	if o.ControlPlane != "" && !validTopologies[o.ControlPlane] {
		return fmt.Errorf("invalid control plane topology %q, must be 'HighlyAvailable' or 'SingleReplica'", o.ControlPlane)
	}

	// Validate infrastructure topology if specified
	if o.Infrastructure != "" && !validTopologies[o.Infrastructure] {
		return fmt.Errorf("invalid infrastructure topology %q, must be 'HighlyAvailable' or 'SingleReplica'", o.Infrastructure)
	}

	// --confirm requires at least one of --control-plane or --infrastructure
	if o.Confirm && o.ControlPlane == "" && o.Infrastructure == "" {
		return fmt.Errorf("--confirm requires at least one of --control-plane or --infrastructure")
	}

	// --allow-transition-with-warnings requires --confirm flag
	if o.AllowTransitionWithWarnings && !o.Confirm {
		return fmt.Errorf("--allow-transition-with-warnings requires --confirm flag")
	}

	return nil
}

// run executes the topology transition command
func (o *transitionOptions) run() error {
	// Create a timeout-scoped context for initial API calls (2 minutes)
	// This prevents unbounded waits on cluster API calls during validation
	apiCtx, apiCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer apiCancel()

	// Get current topologies from Infrastructure resource
	infra, err := o.configClient.ConfigV1().Infrastructures().Get(apiCtx, infrastructureResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get Infrastructure resource: %w", err)
	}

	currentCP := infra.Status.ControlPlaneTopology
	currentInfra := infra.Status.InfrastructureTopology

	// Mode 1: Discovery mode (no topology flags)
	if o.ControlPlane == "" && o.Infrastructure == "" {
		return o.runDiscoveryMode(currentCP, currentInfra)
	}

	// Mode 2: Initiate mode (at least one topology flag provided)
	return o.runInitiateMode(apiCtx, currentCP, currentInfra)
}

// runDiscoveryMode displays current topology and available transitions
func (o *transitionOptions) runDiscoveryMode(currentCP, currentInfra configv1.TopologyMode) error {
	fmt.Fprintf(o.Out, "Current Topology:\n")
	fmt.Fprintf(o.Out, "  Control Plane:    %s\n", currentCP)
	fmt.Fprintf(o.Out, "  Infrastructure:   %s\n\n", currentInfra)

	if currentCP == configv1.SingleReplicaTopologyMode && currentInfra == configv1.SingleReplicaTopologyMode {
		fmt.Fprintf(o.Out, "Available Transition:\n")
		fmt.Fprintf(o.Out, "  Control Plane:    SingleReplica -> HighlyAvailable\n")
		fmt.Fprintf(o.Out, "  (Infrastructure also transitions to HighlyAvailable)\n\n")
		fmt.Fprintf(o.Out, "To validate transition readiness:\n")
		fmt.Fprintf(o.Out, "  oc adm transition topology --control-plane=HighlyAvailable\n\n")
		fmt.Fprintf(o.Out, "To initiate transition:\n")
		fmt.Fprintf(o.Out, "  oc adm transition topology --control-plane=HighlyAvailable --confirm\n")
	} else {
		fmt.Fprintf(o.Out, "Available Transitions:\n")
		fmt.Fprintf(o.Out, "  (none)\n\n")
		fmt.Fprintf(o.Out, "Note: Only SingleReplica -> HighlyAvailable transitions are currently supported.\n")
		fmt.Fprintf(o.Out, "      Both control plane and infrastructure must be SingleReplica to transition.\n")
	}

	return nil
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
	if o.ControlPlane != "" {
		targetCP = configv1.TopologyMode(o.ControlPlane)
	}

	// Derive target infrastructure topology
	// If user specified --infrastructure, use that (for future flexibility)
	// Otherwise, derive based on control plane transition:
	// - SNO -> HA: infrastructure becomes HighlyAvailable (managed by controller)
	// - Future transitions (e.g., HA -> HAA): infrastructure stays HighlyAvailable
	targetInfra := currentInfra
	if o.Infrastructure != "" {
		targetInfra = configv1.TopologyMode(o.Infrastructure)
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
		fmt.Fprintf(o.Out, "Cluster is already at target topology:\n")
		fmt.Fprintf(o.Out, "  Control Plane:    %s\n", current.ControlPlane)
		fmt.Fprintf(o.Out, "  Infrastructure:   %s\n\n", current.Infrastructure)
		fmt.Fprintf(o.Out, "No transition needed.\n")
		return nil
	}

	// Step 1: Run preflight validation
	fmt.Fprintf(o.Out, "Running preflight validation...\n\n")

	result, err := o.validator.Validate(ctx, current, target)
	if err != nil {
		return fmt.Errorf("preflight validation failed: %w", err)
	}

	// Step 2: Display validation results
	fmt.Fprintf(o.Out, "Topology Transition:\n")
	fmt.Fprintf(o.Out, "  Control Plane:    %s -> %s\n", current.ControlPlane, target.ControlPlane)
	fmt.Fprintf(o.Out, "  Infrastructure:   %s -> %s", current.Infrastructure, target.Infrastructure)

	// Note if infrastructure topology is derived (not explicitly specified by user)
	if o.Infrastructure == "" && current.Infrastructure != target.Infrastructure {
		fmt.Fprintf(o.Out, " (managed by controller)")
	}
	fmt.Fprintf(o.Out, "\n")

	fmt.Fprintf(o.Out, "Status: %s\n\n", result.Status)

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
		fmt.Fprintf(o.Out, "BLOCKING CHECKS (cannot be bypassed):\n")
		for _, check := range errorChecks {
			fmt.Fprintf(o.Out, "  %s\n", check.String())
		}
		fmt.Fprintf(o.Out, "\n")
	}

	// Display Warning-severity checks
	if len(warningChecks) > 0 {
		fmt.Fprintf(o.Out, "READINESS CHECKS (can be bypassed with --allow-transition-with-warnings):\n")
		for _, check := range warningChecks {
			fmt.Fprintf(o.Out, "  %s\n", check.String())
		}
		fmt.Fprintf(o.Out, "\n")
	}

	// Step 3: Check for Error-severity failures (blocking - cannot proceed)
	if result.HasErrorCheckFailures() {
		return fmt.Errorf("cannot proceed with transition - see errors listed above")
	}

	// Step 4: Check for Warning-severity failures
	if result.HasWarningCheckFailures() && !o.AllowTransitionWithWarnings {
		return fmt.Errorf("cluster not ready for transition - use --allow-transition-with-warnings with --confirm to bypass (not recommended)")
	}

	// Step 5: If warnings bypassed, show warning message
	if result.HasWarningCheckFailures() && o.AllowTransitionWithWarnings {
		fmt.Fprintf(o.Out, "warning: Proceeding despite failed preflight checks (--allow-transition-with-warnings)\n")
		fmt.Fprintf(o.Out, "warning: This may result in cluster instability or transition failure\n\n")
	}

	// Step 6: If no --confirm, show dry-run message
	if !o.Confirm {
		fmt.Fprintf(o.Out, "\nDry run: Would patch Infrastructure spec:\n")
		fmt.Fprintf(o.Out, "  spec.controlPlaneTopology:    %s\n\n", target.ControlPlane)
		fmt.Fprintf(o.Out, "Note: The cluster-config-operator will update both status.controlPlaneTopology\n")
		fmt.Fprintf(o.Out, "      and status.infrastructureTopology to %s based on this change.\n\n", target.ControlPlane)
		fmt.Fprintf(o.Out, "Add --confirm to apply this transition\n")
		return nil
	}

	// Step 7: Apply transition
	return o.applyTransition(ctx, target.ControlPlane, target.Infrastructure)
}

// applyTransition patches the Infrastructure resource to initiate the transition
func (o *transitionOptions) applyTransition(ctx context.Context, targetCP, targetInfra configv1.TopologyMode) error {
	fmt.Fprintf(o.Out, "Initiating topology transition...\n")

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

	fmt.Fprintf(o.Out, "\nInfrastructure spec patched:\n")
	fmt.Fprintf(o.Out, "  spec.controlPlaneTopology:    %s\n\n", targetCP)
	fmt.Fprintf(o.Out, "Note: The cluster-config-operator will update status.controlPlaneTopology\n")
	fmt.Fprintf(o.Out, "      and status.infrastructureTopology based on this change.\n\n")
	fmt.Fprintf(o.Out, "Transition initiated. Operators will reconfigure to the new topology.\n\n")
	fmt.Fprintf(o.Out, "Monitor transition progress with:\n")
	fmt.Fprintf(o.Out, "  oc adm transition status\n")

	return nil
}
