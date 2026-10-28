package transition

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	configv1 "github.com/openshift/api/config/v1"
	fakeconfigclient "github.com/openshift/client-go/config/clientset/versioned/fake"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	ktesting "k8s.io/client-go/testing"
)

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

/*
================================================================================
TRANSITION COMMAND TESTS
================================================================================

This file tests the `oc adm transition topology` command implementation.
Tests use fake clients with action recording to verify API call behavior.

--------------------------------------------------------------------------------
TEST COVERAGE
--------------------------------------------------------------------------------

COMMAND STRUCTURE
  - NewCmdTransition                        - Command metadata, flags, subcommands

VALIDATION
  - Topology flag validation                - Valid/invalid control-plane/infrastructure values
  - Flag dependencies                       - --confirm requires a target topology

DISCOVERY MODE
  - Discovery mode from SingleReplica       - Shows available transition
  - Discovery mode from HighlyAvailable     - Shows available transitions

INITIATE MODE
  - Patches Infrastructure with --confirm   - Verifies Update action with correct topology
  - Default dry-run without --confirm       - Verifies no Update action, dry-run output
  - Already at target topology              - Early exit with success message, no validation

STATUS COMMAND
  - Status command output                   - 6 scenarios: transition in progress, stable,
                                              preflight failed, unsupported, empty infra topology,
                                              missing condition
  - Transitions array display               - nil, empty, single, multiple, no evaluations
  - Output struct construction              - Condition and transition output mapping

--------------------------------------------------------------------------------
*/

// TestNewCmdTransition verifies command structure
func TestNewCmdTransition(t *testing.T) {
	streams := genericclioptions.NewTestIOStreamsDiscard()

	// Create command with nil factory (just testing structure, not execution)
	cmd := NewCmdTransition(nil, streams)

	// Verify parent transition command
	if cmd.Use != "transition" {
		t.Errorf("expected Use='transition', got %q", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("expected non-empty Short description")
	}

	// Verify topology subcommand exists
	topologyCmd := findSubcommand(cmd, "topology")
	if topologyCmd == nil {
		t.Fatal("expected 'topology' subcommand to exist")
	}

	// Verify topology command metadata
	if topologyCmd.Use != "topology" {
		t.Errorf("expected topology Use='topology', got %q", topologyCmd.Use)
	}

	if topologyCmd.Short == "" {
		t.Error("expected topology non-empty Short description")
	}

	if topologyCmd.Long == "" {
		t.Error("expected topology non-empty Long description")
	}

	if topologyCmd.Example == "" {
		t.Error("expected topology non-empty Example")
	}

	// Verify flags exist on topology command
	requiredFlags := []string{"control-plane", "infrastructure", "confirm"}
	for _, flagName := range requiredFlags {
		flag := topologyCmd.Flags().Lookup(flagName)
		if flag == nil {
			t.Errorf("expected flag %q to exist", flagName)
		}
	}

	// Verify status subcommand exists as peer to topology (under transition)
	statusCmd := findSubcommand(cmd, "status")
	if statusCmd == nil {
		t.Error("expected 'status' subcommand to exist under transition")
	} else {
		if statusCmd.Short == "" {
			t.Error("expected status subcommand to have Short description")
		}
	}
}

// TestValidate_TopologyFlags tests --control-plane and --infrastructure flag validation
func TestValidate_TopologyFlags(t *testing.T) {
	testCases := []struct {
		name           string
		controlPlane   string
		infrastructure string
		expectErr      bool
	}{
		{
			name:         "valid HighlyAvailable control plane",
			controlPlane: "HighlyAvailable",
			expectErr:    false,
		},
		{
			name:         "valid SingleReplica control plane",
			controlPlane: "SingleReplica",
			expectErr:    false,
		},
		{
			name:           "valid HighlyAvailable infrastructure",
			infrastructure: "HighlyAvailable",
			expectErr:      false,
		},
		{
			name:           "valid both topologies",
			controlPlane:   "HighlyAvailable",
			infrastructure: "HighlyAvailable",
			expectErr:      false,
		},
		{
			name:         "invalid control plane topology",
			controlPlane: "InvalidTopology",
			expectErr:    true,
		},
		{
			name:           "invalid infrastructure topology",
			infrastructure: "InvalidTopology",
			expectErr:      true,
		},
		{
			name:      "empty (discovery mode)",
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			streams := genericclioptions.NewTestIOStreamsDiscard()
			o := newTransitionOptions(streams)
			o.args.targetControlPlaneTopology = tc.controlPlane
			o.args.targetInfrastructureTopology = tc.infrastructure

			err := o.validateCommand()

			if tc.expectErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// TestValidate_FlagDependencies tests flag dependencies
func TestValidate_FlagDependencies(t *testing.T) {
	testCases := []struct {
		name           string
		controlPlane   string
		infrastructure string
		confirm        bool
		expectErr      bool
		errContains    string
	}{
		{
			name:        "--confirm requires at least one topology flag",
			confirm:     true,
			expectErr:   true,
			errContains: "--confirm requires at least one of --control-plane or --infrastructure",
		},
		{
			name:         "--control-plane alone is valid (dry-run mode)",
			controlPlane: "HighlyAvailable",
			expectErr:    false,
		},
		{
			name:           "--infrastructure alone is valid (dry-run mode)",
			infrastructure: "HighlyAvailable",
			expectErr:      false,
		},
		{
			name:         "--control-plane with --confirm is valid",
			controlPlane: "HighlyAvailable",
			confirm:      true,
			expectErr:    false,
		},
		{
			name:           "--infrastructure with --confirm is valid",
			infrastructure: "HighlyAvailable",
			confirm:        true,
			expectErr:      false,
		},
		{
			name:           "--control-plane and --infrastructure with --confirm is valid",
			controlPlane:   "HighlyAvailable",
			infrastructure: "HighlyAvailable",
			confirm:        true,
			expectErr:      false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			streams := genericclioptions.NewTestIOStreamsDiscard()
			o := newTransitionOptions(streams)
			o.args.targetControlPlaneTopology = tc.controlPlane
			o.args.targetInfrastructureTopology = tc.infrastructure
			o.args.confirm = tc.confirm

			err := o.validateCommand()

			if tc.expectErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			}
		})
	}
}

// TestRunDiscoveryMode_SingleReplica tests discovery mode output for SNO
func TestRunDiscoveryMode_SingleReplica(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()
	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.SingleReplicaTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	o.validator.Current = o.current

	err := o.runDiscoveryMode(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()

	// Should show current topology for both control plane and infrastructure
	if !strings.Contains(output, "Current Topology Configuration:") {
		t.Errorf("expected current topology header in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Control Plane:") {
		t.Errorf("expected control plane topology in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Infrastructure:") {
		t.Errorf("expected infrastructure topology in output, got:\n%s", output)
	}

	// Should show available transition for control plane
	if !strings.Contains(output, `Control Plane:  SingleReplica\t->\tHighlyAvailable`) {
		t.Errorf("expected available control plane transition in output, got:\n%s", output)
	}

	if !strings.Contains(output, `Infrastructure: SingleReplica\t->\tHighlyAvailable`) {
		t.Errorf("expected available infrastructure transition in output, got:\n%s", output)
	}

	if !strings.Contains(output, "--control-plane={target} --confirm") {
		t.Errorf("expected initiate command with --control-plane flag in output, got:\n%s", output)
	}
}

// TestRunDiscoveryMode_HighlyAvailable tests discovery mode output for HA
func TestRunDiscoveryMode_HighlyAvailable(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()
	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.HighlyAvailableTopologyMode,
	}
	o.validator.Current = o.current

	err := o.runDiscoveryMode(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()

	// Should show current topology for both control plane and infrastructure
	if !strings.Contains(output, "Current Topology Configuration:") {
		t.Errorf("expected current topology header in output, got:\n%s", output)
	}

	if !strings.Contains(output, `Control Plane:  HighlyAvailable\t->\tHighlyAvailable`) {
		t.Errorf("expected current topology as a valid no-op transition, got:\n%s", output)
	}
}

func TestRunDiscoveryModeReturnsWriteError(t *testing.T) {
	wantErr := errors.New("write failed")
	o := newTransitionOptions(genericclioptions.IOStreams{Out: failingWriter{err: wantErr}})
	o.current = TopologyState{
		ControlPlane:   configv1.SingleReplicaTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	o.validator.Current = o.current

	if err := o.runDiscoveryMode(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("runDiscoveryMode() error = %v, want %v", err, wantErr)
	}
}

// findSubcommand finds a subcommand by name
func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, cmd := range parent.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}

// TestRunInitiateMode_PatchesInfrastructure verifies that the command patches Infrastructure.spec.controlPlaneTopology
func TestRunInitiateMode_PatchesInfrastructure(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()

	// Create fake infrastructure resource (current: SingleReplica)
	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
		Spec: configv1.InfrastructureSpec{
			ControlPlaneTopology: configv1.SingleReplicaTopologyMode,
		},
	}

	configClient := fakeconfigclient.NewSimpleClientset(infra)

	o := newTransitionOptions(streams)
	o.target = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.HighlyAvailableTopologyMode,
	}
	o.args.confirm = true
	o.configClient = configClient

	// Run the command
	err := o.run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify Infrastructure was updated
	actions := configClient.Actions()
	var updateAction ktesting.UpdateAction
	for _, action := range actions {
		if action.GetVerb() == "update" && action.GetResource().Resource == "infrastructures" {
			updateAction = action.(ktesting.UpdateAction)
			break
		}
	}

	if updateAction == nil {
		t.Fatal("expected Infrastructure update action, got none")
	}

	updatedInfra := updateAction.GetObject().(*configv1.Infrastructure)
	if updatedInfra.Spec.ControlPlaneTopology != configv1.HighlyAvailableTopologyMode {
		t.Errorf("expected spec.controlPlaneTopology=%s, got %s",
			configv1.HighlyAvailableTopologyMode, updatedInfra.Spec.ControlPlaneTopology)
	}

	// Verify output confirms transition
	if !strings.Contains(out.String(), "Initiating topology transition") {
		t.Error("expected output to confirm transition initiation")
	}
}

// TestRunInitiateMode_DefaultDryRun verifies that without --confirm the command runs in dry-run mode
func TestRunInitiateMode_DefaultDryRun(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()

	// Create fake infrastructure resource
	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
		Spec: configv1.InfrastructureSpec{
			ControlPlaneTopology: configv1.SingleReplicaTopologyMode,
		},
	}

	configClient := fakeconfigclient.NewSimpleClientset(infra)

	o := newTransitionOptions(streams)
	o.target = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.HighlyAvailableTopologyMode,
	}
	o.configClient = configClient

	// Run the command
	err := o.run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify NO Infrastructure update occurred
	actions := configClient.Actions()
	for _, action := range actions {
		if action.GetVerb() == "update" {
			t.Error("expected no update action in dry-run mode")
		}
	}

	// Verify dry-run message in output
	if !strings.Contains(out.String(), "Dry run") {
		t.Error("expected output to indicate dry-run mode")
	}
	if !strings.Contains(out.String(), "Add --confirm") {
		t.Error("expected output to mention --confirm flag")
	}
}

// TestRunInitiateMode_AlreadyAtTarget verifies that when cluster is already at target,
// the command exits successfully without running validation or patching
func TestRunInitiateMode_AlreadyAtTarget(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()

	// Create fake infrastructure resource (already at SingleReplica)
	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
		Spec: configv1.InfrastructureSpec{
			ControlPlaneTopology: configv1.SingleReplicaTopologyMode,
		},
	}

	configClient := fakeconfigclient.NewSimpleClientset(infra)

	o := newTransitionOptions(streams)
	o.target = TopologyState{
		ControlPlane:   configv1.SingleReplicaTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	o.configClient = configClient

	// Run the command
	err := o.run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify NO Infrastructure update occurred (no-op)
	actions := configClient.Actions()
	for _, action := range actions {
		if action.GetVerb() == "update" {
			t.Error("expected no update action when already at target")
		}
	}

	output := out.String()

	// Verify success message
	if !strings.Contains(output, "Cluster is already at target topology") {
		t.Error("expected output to indicate cluster is already at target")
	}
	if !strings.Contains(output, "No transition initiated") {
		t.Error("expected output to indicate no transition was initiated")
	}

	// Verify validation was NOT run
	if strings.Contains(output, "Running preflight validation") {
		t.Error("expected validation to be skipped when already at target")
	}
}

func TestRunInitiateModeReturnsWriteError(t *testing.T) {
	wantErr := errors.New("write failed")
	o := newTransitionOptions(genericclioptions.IOStreams{Out: failingWriter{err: wantErr}})
	o.current = TopologyState{
		ControlPlane:   configv1.SingleReplicaTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	o.target = TopologyState{ControlPlane: configv1.HighlyAvailableTopologyMode}
	o.validator.Current = o.current

	if err := o.runInitiateMode(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("runInitiateMode() error = %v, want %v", err, wantErr)
	}
}

// TestStatusCommand tests the status subcommand output in various transition states
func TestStatusCommand(t *testing.T) {
	testCases := []struct {
		name                   string
		infraSpec              configv1.TopologyMode
		infraStatus            configv1.TopologyMode
		infraTopology          configv1.TopologyMode
		evaluatedStatus        metav1.ConditionStatus
		evaluatedReason        string
		evaluatedMessage       string
		completedStatus        metav1.ConditionStatus
		completedReason        string
		completedMessage       string
		expectedOutputContains []string
		expectError            bool
	}{
		{
			name:             "transition in progress",
			infraSpec:        configv1.HighlyAvailableTopologyMode,
			infraStatus:      configv1.SingleReplicaTopologyMode,
			infraTopology:    configv1.SingleReplicaTopologyMode,
			evaluatedStatus:  metav1.ConditionTrue,
			evaluatedReason:  "TransitionAvailable",
			evaluatedMessage: "Topology transition evaluations completed successfully",
			completedStatus:  metav1.ConditionFalse,
			completedReason:  "TopologyTransitionInProgress",
			completedMessage: "Transitioning control plane topology from SingleReplica to HighlyAvailable",
			expectedOutputContains: []string{
				"Control Plane Topology:",
				"Spec (desired):   HighlyAvailable",
				"Status (current): SingleReplica",
				"Infrastructure Topology:",
				"Transition Status:",
				`Completed:
  Message: Transitioning control plane topology from SingleReplica to HighlyAvailable
  Reason: TopologyTransitionInProgress
  Status: "False"`,
				`Evaluated:
  Message: Topology transition evaluations completed successfully
  Reason: TransitionAvailable
  Status: "True"`,
			},
		},
		{
			name:             "no transition",
			infraSpec:        configv1.HighlyAvailableTopologyMode,
			infraStatus:      configv1.HighlyAvailableTopologyMode,
			infraTopology:    configv1.HighlyAvailableTopologyMode,
			evaluatedStatus:  metav1.ConditionTrue,
			evaluatedReason:  "EvaluationComplete",
			evaluatedMessage: "Topology transition evaluations completed successfully",
			completedStatus:  metav1.ConditionTrue,
			completedReason:  "AsExpected",
			completedMessage: "No topology transition in progress",
			expectedOutputContains: []string{
				"Control Plane Topology:",
				"Spec (desired):   HighlyAvailable",
				"Status (current): HighlyAvailable",
				"Infrastructure Topology:",
				"Status (current): HighlyAvailable",
				"Transition Status:",
				`Completed:
  Message: No topology transition in progress
  Reason: AsExpected
  Status: "True"`,
				`Evaluated:
  Message: Topology transition evaluations completed successfully
  Reason: EvaluationComplete
  Status: "True"`,
			},
		},
		{
			name:             "preflight failed",
			infraSpec:        configv1.HighlyAvailableTopologyMode,
			infraStatus:      configv1.SingleReplicaTopologyMode,
			infraTopology:    configv1.SingleReplicaTopologyMode,
			evaluatedStatus:  metav1.ConditionTrue,
			evaluatedReason:  "EvaluationComplete",
			evaluatedMessage: "Topology transition evaluations completed successfully",
			completedStatus:  metav1.ConditionFalse,
			completedReason:  topologyTransitionPreflightCheckFailedReason,
			completedMessage: "Cluster operators are not stable",
			expectedOutputContains: []string{
				`Completed:
  Message: Cluster operators are not stable
  Reason: PreflightCheckFailed
  Status: "False"`,
				`Evaluated:
  Message: Topology transition evaluations completed successfully
  Reason: EvaluationComplete
  Status: "True"`,
			},
		},
		{
			name:             "unsupported transition",
			infraSpec:        configv1.SingleReplicaTopologyMode,
			infraStatus:      configv1.HighlyAvailableTopologyMode,
			infraTopology:    configv1.HighlyAvailableTopologyMode,
			evaluatedStatus:  metav1.ConditionTrue,
			evaluatedReason:  "EvaluationComplete",
			evaluatedMessage: "Topology transition evaluations completed successfully",
			completedStatus:  metav1.ConditionFalse,
			completedReason:  topologyTransitionUnsupportedTransitionReason,
			completedMessage: "Transition from HighlyAvailable to SingleReplica is not supported",
			expectedOutputContains: []string{
				`Completed:
  Message: Transition from HighlyAvailable to SingleReplica is not supported
  Reason: UnsupportedTransition
  Status: "False"`,
				`Evaluated:
  Message: Topology transition evaluations completed successfully
  Reason: EvaluationComplete
  Status: "True"`,
			},
		},
		{
			name:             "empty infrastructure topology",
			infraSpec:        configv1.HighlyAvailableTopologyMode,
			infraStatus:      configv1.HighlyAvailableTopologyMode,
			infraTopology:    "", // Empty infrastructure topology
			evaluatedStatus:  metav1.ConditionTrue,
			evaluatedReason:  "EvaluationComplete",
			evaluatedMessage: "Topology transition evaluations completed successfully",
			completedStatus:  metav1.ConditionTrue,
			completedReason:  "AsExpected",
			completedMessage: "No topology transition in progress",
			expectedOutputContains: []string{
				"Control Plane Topology:",
				"Infrastructure Topology:",
				"Status (current): (not set)",
				`Evaluated:
  Message: Topology transition evaluations completed successfully
  Reason: EvaluationComplete
  Status: "True"`,
			},
		},
		{
			name:             "completed condition not available",
			infraSpec:        configv1.HighlyAvailableTopologyMode,
			infraStatus:      configv1.HighlyAvailableTopologyMode,
			infraTopology:    configv1.HighlyAvailableTopologyMode,
			evaluatedStatus:  metav1.ConditionTrue,
			evaluatedReason:  "EvaluationComplete",
			evaluatedMessage: "Topology transition evaluations completed successfully",
			expectedOutputContains: []string{
				"Transition Status:",
				`Completed:
  Message: Condition not available`,
				`Evaluated:
  Message: Topology transition evaluations completed successfully
  Reason: EvaluationComplete
  Status: "True"`,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			streams, _, out, _ := genericclioptions.NewTestIOStreams()

			// Create fake infrastructure with TopologyTransitionStatus
			infra := &configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: configv1.InfrastructureSpec{
					ControlPlaneTopology: tc.infraSpec,
				},
				Status: configv1.InfrastructureStatus{
					ControlPlaneTopology:   tc.infraStatus,
					InfrastructureTopology: tc.infraTopology,
				},
			}

			// Add TopologyTransitionStatus conditions
			if tc.evaluatedStatus != "" || tc.completedStatus != "" {
				conditions := []metav1.Condition{}

				if tc.evaluatedStatus != "" {
					conditions = append(conditions, metav1.Condition{
						Type:    configv1.TopologyTransitionsEvaluatedConditionType,
						Status:  tc.evaluatedStatus,
						Reason:  tc.evaluatedReason,
						Message: tc.evaluatedMessage,
					})
				}

				if tc.completedStatus != "" {
					conditions = append(conditions, metav1.Condition{
						Type:    configv1.TopologyTransitionCompletedConditionType,
						Status:  tc.completedStatus,
						Reason:  tc.completedReason,
						Message: tc.completedMessage,
					})
				}

				infra.Status.TopologyTransitionStatus = configv1.TopologyTransitionStatus{
					Conditions: conditions,
				}
			}

			configClient := fakeconfigclient.NewSimpleClientset(infra)

			// Create statusOptions
			o := &statusOptions{
				IOStreams:    streams,
				configClient: configClient,
			}

			// Run status command
			err := o.run(context.Background())
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify output
			output := out.String()
			for _, expected := range tc.expectedOutputContains {
				if !strings.Contains(output, expected) {
					t.Errorf("expected output to contain %q, got:\n%s", expected, output)
				}
			}
		})
	}
}

// TestStatusCommand_WithTransitions tests the transitions array display functionality
func TestStatusCommand_WithTransitions(t *testing.T) {
	testCases := []struct {
		name                   string
		transitions            []configv1.TopologyTransition
		expectedOutputContains []string
		expectedOutputMissing  []string
	}{
		{
			name:        "nil transitions array",
			transitions: nil,
			expectedOutputMissing: []string{
				"Available Transitions:",
			},
		},
		{
			name:        "empty transitions array",
			transitions: []configv1.TopologyTransition{},
			expectedOutputMissing: []string{
				"Available Transitions:",
			},
		},
		{
			name: "single transition with evaluations",
			transitions: []configv1.TopologyTransition{
				{
					Source: configv1.TopologyState{
						ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
						InfrastructureTopology: configv1.SingleReplicaTopologyMode,
					},
					Target: configv1.TopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
					},
					Evaluations: []metav1.Condition{
						{
							Type:    configv1.TopologyTransitionAvailableConditionType,
							Status:  metav1.ConditionTrue,
							Reason:  "TransitionSupported",
							Message: "Transition from SingleReplica to HighlyAvailable is supported",
						},
						{
							Type:    "ClusterOperatorsStable",
							Status:  metav1.ConditionTrue,
							Reason:  "AllStable",
							Message: "All cluster operators are stable",
						},
					},
				},
			},
			expectedOutputContains: []string{
				"Available Transitions:",
				`Source:
    Control Plane Topology: SingleReplica
    Infrastructure Topology: SingleReplica`,
				`Target:
    Control Plane Topology: HighlyAvailable
    Infrastructure Topology: HighlyAvailable`,
				`  - Message: Transition from SingleReplica to HighlyAvailable is supported
    Reason: TransitionSupported
    Status: "True"
    Type: TopologyTransitionAvailable`,
				`  - Message: All cluster operators are stable
    Reason: AllStable
    Status: "True"
    Type: ClusterOperatorsStable`,
			},
		},
		{
			name: "transition without evaluations",
			transitions: []configv1.TopologyTransition{
				{
					Source: configv1.TopologyState{
						ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
						InfrastructureTopology: configv1.SingleReplicaTopologyMode,
					},
					Target: configv1.TopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
					},
				},
			},
			expectedOutputContains: []string{
				"Available Transitions:",
				"Evaluations: []",
			},
		},
		{
			name: "multiple transitions",
			transitions: []configv1.TopologyTransition{
				{
					Source: configv1.TopologyState{
						ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
						InfrastructureTopology: configv1.SingleReplicaTopologyMode,
					},
					Target: configv1.TopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
					},
					Evaluations: []metav1.Condition{
						{
							Type:    configv1.TopologyTransitionAvailableConditionType,
							Status:  metav1.ConditionTrue,
							Reason:  "TransitionSupported",
							Message: "Transition is supported",
						},
					},
				},
				{
					Source: configv1.TopologyState{
						ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
						InfrastructureTopology: configv1.SingleReplicaTopologyMode,
					},
					Target: configv1.TopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableArbiterMode,
						InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
					},
					Evaluations: []metav1.Condition{
						{
							Type:    configv1.TopologyTransitionAvailableConditionType,
							Status:  metav1.ConditionFalse,
							Reason:  "InsufficientNodes",
							Message: "Not enough worker nodes for HighlyAvailableArbiter topology",
						},
					},
				},
			},
			expectedOutputContains: []string{
				"Available Transitions:",
				`Target:
    Control Plane Topology: HighlyAvailable
    Infrastructure Topology: HighlyAvailable`,
				"Control Plane Topology: HighlyAvailableArbiter",
				"Reason: InsufficientNodes",
				"Message: Transition is supported",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			streams, _, out, _ := genericclioptions.NewTestIOStreams()

			// Create fake infrastructure with basic topology and transitions
			infra := &configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: configv1.InfrastructureSpec{
					ControlPlaneTopology: configv1.HighlyAvailableTopologyMode,
				},
				Status: configv1.InfrastructureStatus{
					ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
					InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
					TopologyTransitionStatus: configv1.TopologyTransitionStatus{
						Conditions: []metav1.Condition{
							{
								Type:    configv1.TopologyTransitionsEvaluatedConditionType,
								Status:  metav1.ConditionTrue,
								Reason:  "EvaluationComplete",
								Message: "Topology transition evaluations completed successfully",
							},
							{
								Type:    configv1.TopologyTransitionCompletedConditionType,
								Status:  metav1.ConditionTrue,
								Reason:  "AsExpected",
								Message: "No topology transition in progress",
							},
						},
						Transitions: tc.transitions,
					},
				},
			}

			configClient := fakeconfigclient.NewSimpleClientset(infra)

			// Create statusOptions
			o := &statusOptions{
				IOStreams:    streams,
				configClient: configClient,
			}

			// Run status command
			err := o.run(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify expected output
			output := out.String()
			for _, expected := range tc.expectedOutputContains {
				if !strings.Contains(output, expected) {
					t.Errorf("expected output to contain %q, got:\n%s", expected, output)
				}
			}

			// Verify missing output
			for _, missing := range tc.expectedOutputMissing {
				if strings.Contains(output, missing) {
					t.Errorf("expected output to NOT contain %q, got:\n%s", missing, output)
				}
			}

			// Verify multiple transitions are listed in the reported order
			if strings.Count(output, "- Evaluations:") == 2 {
				if first, second := strings.Index(output, "Transition is supported"), strings.Index(output, "HighlyAvailableArbiter"); first == -1 || second == -1 || first > second {
					t.Errorf("expected transitions to be listed in order, got:\n%s", output)
				}
			}
		})
	}
}

// TestNewTransitionConditionOutput verifies the conversion of topology
// transition conditions into their output representation
func TestNewTransitionConditionOutput(t *testing.T) {
	tests := []struct {
		name      string
		condition *metav1.Condition
		want      transitionConditionOutput
	}{
		{
			name: "missing condition",
			want: transitionConditionOutput{Message: conditionNotAvailable},
		},
		{
			name: "available condition",
			condition: &metav1.Condition{
				Status:  metav1.ConditionFalse,
				Reason:  "TopologyTransitionInProgress",
				Message: "Cluster upgrade is not allowed during topology transition",
			},
			want: transitionConditionOutput{
				Status:  "False",
				Reason:  "TopologyTransitionInProgress",
				Message: "Cluster upgrade is not allowed during topology transition",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, newTransitionConditionOutput(tc.condition)); diff != "" {
				t.Errorf("newTransitionConditionOutput() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestNewAvailableTransitionOutput verifies the conversion of a topology
// transition and its evaluations into their output representation
func TestNewAvailableTransitionOutput(t *testing.T) {
	transition := configv1.TopologyTransition{
		Source: configv1.TopologyState{
			ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
		Target: configv1.TopologyState{
			ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
			InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
		},
		Evaluations: []metav1.Condition{
			{
				Type:    configv1.TopologyTransitionAvailableConditionType,
				Status:  metav1.ConditionTrue,
				Reason:  "TransitionSupported",
				Message: "Transition from SingleReplica to HighlyAvailable is supported",
			},
		},
	}

	want := availableTransitionOutput{
		Source: transitionTopologyOutput{
			ControlPlaneTopology:   "SingleReplica",
			InfrastructureTopology: "SingleReplica",
		},
		Target: transitionTopologyOutput{
			ControlPlaneTopology:   "HighlyAvailable",
			InfrastructureTopology: "HighlyAvailable",
		},
		Evaluations: []transitionConditionOutput{
			{
				Type:    configv1.TopologyTransitionAvailableConditionType,
				Status:  "True",
				Reason:  "TransitionSupported",
				Message: "Transition from SingleReplica to HighlyAvailable is supported",
			},
		},
	}

	if diff := cmp.Diff(want, newAvailableTransitionOutput(transition)); diff != "" {
		t.Errorf("newAvailableTransitionOutput() mismatch (-want +got):\n%s", diff)
	}
}

func TestStatusCommandErrors(t *testing.T) {
	tests := []struct {
		name           string
		failResource   string
		out            failingWriter
		wantWriteError bool
	}{
		{name: "Infrastructure read", failResource: "infrastructures"},
		{name: "output write", out: failingWriter{err: errors.New("write failed")}, wantWriteError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			streams := genericclioptions.NewTestIOStreamsDiscard()
			if tc.wantWriteError {
				streams.Out = tc.out
			}
			configClient := fakeconfigclient.NewSimpleClientset(&configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{Name: infrastructureResourceName},
			})
			if tc.failResource == "infrastructures" {
				configClient.PrependReactor("get", tc.failResource, func(ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("read failed")
				})
			}

			o := &statusOptions{IOStreams: streams, configClient: configClient}
			err := o.run(context.Background())
			if err == nil {
				t.Fatal("run() error = nil, want error")
			}
			if tc.wantWriteError && !errors.Is(err, tc.out.err) {
				t.Fatalf("run() error = %v, want %v", err, tc.out.err)
			}
		})
	}
}
