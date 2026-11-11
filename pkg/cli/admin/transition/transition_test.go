package transition

import (
	"context"
	"encoding/json"
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
  - Server-reported discovery               - See discovery_test.go

INITIATE MODE
  - Patches Infrastructure with --confirm   - Verifies Patch action with correct topology,
                                              including standalone infrastructure transitions
  - Default dry-run without --confirm       - Verifies no Patch action, dry-run output
  - Already at target topology              - Early exit with success message, no validation
  - Server-advertised transition validation - Validates against status.topologyTransitionStatus

STATUS COMMAND
  - Status command output                   - 7 scenarios: transition in progress, stable,
                                              preflight failed, unsupported, empty infra topology,
                                              missing condition, transitions without conditions
  - Transitions array display               - nil, empty, single, multiple, no evaluations
  - Output struct construction              - Condition and transition output mapping

VALIDATOR
  - FindTransition                          - Source/target matching, wildcard target fields,
                                              disambiguation among transitions sharing a target
  - Validate                                - Unavailable transitions and prerequisite failure
                                              reporting; empty advertised-transition list

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
	requiredFlags := []string{"control-plane", "infrastructure", "confirm", "output"}
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

// findSubcommand finds a subcommand by name
func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, cmd := range parent.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}

// ================================================================================
// VALIDATION TESTS
// ================================================================================

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

// ================================================================================
// INITIATE MODE TESTS
// ================================================================================

// patchedTopologySpec returns the topology spec fields from the patch applied to
// the Infrastructure resource. It fails the test if no patch was recorded.
func patchedTopologySpec(t *testing.T, actions []ktesting.Action) (controlPlane, infrastructure configv1.TopologyMode) {
	t.Helper()
	for _, action := range actions {
		patchAction, ok := action.(ktesting.PatchAction)
		if !ok || action.GetResource().Resource != "infrastructures" {
			continue
		}
		var patch struct {
			Spec struct {
				ControlPlaneTopology   configv1.TopologyMode `json:"controlPlaneTopology,omitempty"`
				InfrastructureTopology configv1.TopologyMode `json:"infrastructureTopology,omitempty"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(patchAction.GetPatch(), &patch); err != nil {
			t.Fatalf("failed to unmarshal Infrastructure patch: %v", err)
		}
		return patch.Spec.ControlPlaneTopology, patch.Spec.InfrastructureTopology
	}
	t.Fatal("expected Infrastructure patch action, got none")
	return "", ""
}

// hasPatchAction reports whether any Infrastructure patch was recorded.
func hasPatchAction(actions []ktesting.Action) bool {
	for _, action := range actions {
		if action.GetVerb() == "patch" && action.GetResource().Resource == "infrastructures" {
			return true
		}
	}
	return false
}

func TestRunInitiateMode_InfrastructureOnlyPatch(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
	}

	transitions := []configv1.TopologyTransition{
		{
			Source: configv1.TopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.SingleReplicaTopologyMode,
			},
			Target: configv1.TopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
			},
			Evaluations: []metav1.Condition{
				{Type: configv1.TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Reason: "TransitionAvailable", Message: "All checks passed"},
			},
		},
	}

	configClient := fakeconfigclient.NewSimpleClientset(infra)

	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	o.target = TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode}
	o.validator = TransitionValidator{
		Current:     o.current,
		Transitions: transitions,
	}
	o.args.confirm = true
	o.configClient = configClient

	err := o.runInitiateMode(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify infrastructure topology was patched
	patchedCP, patchedInfra := patchedTopologySpec(t, configClient.Actions())
	if patchedInfra != configv1.HighlyAvailableTopologyMode {
		t.Errorf("expected patched infrastructure=%s, got %s",
			configv1.HighlyAvailableTopologyMode, patchedInfra)
	}
	// Control plane should NOT be patched (empty = unchanged)
	if patchedCP != "" {
		t.Errorf("expected patched control plane to be empty (unchanged), got %s", patchedCP)
	}

	output := out.String()
	if !strings.Contains(output, "spec.infrastructureTopology") {
		t.Errorf("expected output to mention spec.infrastructureTopology, got:\n%s", output)
	}
	if !strings.Contains(output, "oc adm transition status") {
		t.Errorf("expected output to suggest monitoring command, got:\n%s", output)
	}
}

func TestRunInitiateMode_ControlPlaneOnlyPatch(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
	}

	transitions := []configv1.TopologyTransition{
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
				{Type: configv1.TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Reason: "TransitionAvailable", Message: "All checks passed"},
			},
		},
	}

	configClient := fakeconfigclient.NewSimpleClientset(infra)

	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.SingleReplicaTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	// CP only — compact auto-fill should set infra too
	o.target = TopologyState{ControlPlane: configv1.HighlyAvailableTopologyMode}
	o.validator = TransitionValidator{
		Current:     o.current,
		Transitions: transitions,
	}
	o.args.confirm = true
	o.configClient = configClient

	err := o.runInitiateMode(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Compact auto-fill: should patch both
	patchedCP, patchedInfra := patchedTopologySpec(t, configClient.Actions())
	if patchedCP != configv1.HighlyAvailableTopologyMode {
		t.Errorf("expected patched control plane=%s, got %s",
			configv1.HighlyAvailableTopologyMode, patchedCP)
	}
	if patchedInfra != configv1.HighlyAvailableTopologyMode {
		t.Errorf("expected patched infrastructure=%s (auto-fill), got %s",
			configv1.HighlyAvailableTopologyMode, patchedInfra)
	}

	output := out.String()
	if !strings.Contains(output, "spec.controlPlaneTopology") {
		t.Errorf("expected output to mention spec.controlPlaneTopology, got:\n%s", output)
	}
}

func TestRunInitiateMode_DryRun_InfrastructureOnly(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
	}

	transitions := []configv1.TopologyTransition{
		{
			Source: configv1.TopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.SingleReplicaTopologyMode,
			},
			Target: configv1.TopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
			},
			Evaluations: []metav1.Condition{
				{Type: configv1.TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Reason: "TransitionAvailable", Message: "All checks passed"},
				{Type: "WorkerNodesReady", Status: metav1.ConditionTrue, Reason: "SufficientWorkers", Message: "3 workers ready"},
			},
		},
	}

	configClient := fakeconfigclient.NewSimpleClientset(infra)

	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	o.target = TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode}
	o.validator = TransitionValidator{
		Current:     o.current,
		Transitions: transitions,
	}
	o.args.confirm = false
	o.configClient = configClient

	err := o.runInitiateMode(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no patch was made
	if hasPatchAction(configClient.Actions()) {
		t.Error("expected no patch in dry-run mode")
	}

	output := out.String()
	if !strings.Contains(output, "Dry run") {
		t.Error("expected output to indicate dry-run mode")
	}
	if !strings.Contains(output, "spec.infrastructureTopology") {
		t.Error("expected output to mention spec.infrastructureTopology")
	}
	if !strings.Contains(output, "Add --confirm") {
		t.Error("expected output to suggest --confirm")
	}
	if !strings.Contains(output, "WorkerNodesReady") {
		t.Error("expected output to show prerequisite evaluation")
	}
}

func TestRunInitiateMode_AlreadyAtTarget(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
			InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
		},
	}

	configClient := fakeconfigclient.NewSimpleClientset(infra)

	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.HighlyAvailableTopologyMode,
	}
	o.target = TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode}
	o.configClient = configClient

	err := o.runInitiateMode(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hasPatchAction(configClient.Actions()) {
		t.Error("expected no patch when already at target")
	}

	output := out.String()
	if !strings.Contains(output, "Cluster is already at target topology") {
		t.Error("expected output to indicate already at target")
	}
	if !strings.Contains(output, "No transition initiated") {
		t.Error("expected output to indicate no transition was initiated")
	}
}

func TestRunInitiateMode_PatchError(t *testing.T) {
	streams := genericclioptions.NewTestIOStreamsDiscard()

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
	}

	transitions := []configv1.TopologyTransition{
		{
			Source: configv1.TopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.SingleReplicaTopologyMode,
			},
			Target: configv1.TopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
			},
			Evaluations: []metav1.Condition{
				{Type: configv1.TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Reason: "TransitionAvailable", Message: "All checks passed"},
			},
		},
	}

	configClient := fakeconfigclient.NewSimpleClientset(infra)
	configClient.PrependReactor("patch", "infrastructures", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("conflict")
	})

	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	o.target = TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode}
	o.validator = TransitionValidator{
		Current:     o.current,
		Transitions: transitions,
	}
	o.args.confirm = true
	o.configClient = configClient

	err := o.runInitiateMode(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to update Infrastructure resource") {
		t.Errorf("expected update error, got: %v", err)
	}
}

func TestRunInitiateModeReturnsWriteError(t *testing.T) {
	wantErr := errors.New("write failed")
	o := newTransitionOptions(genericclioptions.IOStreams{Out: failingWriter{err: wantErr}})
	o.current = TopologyState{
		ControlPlane:   configv1.SingleReplicaTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	// CP SNO->HA triggers compact auto-fill for infra
	o.target = TopologyState{ControlPlane: configv1.HighlyAvailableTopologyMode}
	o.validator = TransitionValidator{
		Current: o.current,
		Transitions: []configv1.TopologyTransition{
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
					{Type: configv1.TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Reason: "Available"},
				},
			},
		},
	}

	err := o.runInitiateMode(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestRun_InfraOnlyTransitionEndToEnd verifies that run() seeds the validator
// with the server-advertised transitions from Infrastructure status and
// completes an infrastructure-only transition against them.
func TestRun_InfraOnlyTransitionEndToEnd(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: infrastructureResourceName, Generation: 2},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
			TopologyTransitionStatus: configv1.TopologyTransitionStatus{
				Conditions: []metav1.Condition{
					{
						Type:               configv1.TopologyTransitionsEvaluatedConditionType,
						Status:             metav1.ConditionTrue,
						ObservedGeneration: 2,
						Reason:             "EvaluationComplete",
						Message:            "Topology transition evaluations completed successfully",
					},
				},
				Transitions: []configv1.TopologyTransition{
					{
						Source: configv1.TopologyState{
							ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
							InfrastructureTopology: configv1.SingleReplicaTopologyMode,
						},
						Target: configv1.TopologyState{
							ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
							InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
						},
						Evaluations: []metav1.Condition{
							{Type: configv1.TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Reason: "TransitionAvailable", Message: "All checks passed"},
						},
					},
				},
			},
		},
	}

	configClient := fakeconfigclient.NewSimpleClientset(infra)

	o := newTransitionOptions(streams)
	o.configClient = configClient
	o.target = TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode}
	o.args.confirm = true

	if err := o.run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The advertised transition must have been used for validation, and only
	// the infrastructure spec field may be patched.
	patchedCP, patchedInfra := patchedTopologySpec(t, configClient.Actions())
	if patchedInfra != configv1.HighlyAvailableTopologyMode {
		t.Errorf("expected patched infrastructure=%s, got %s",
			configv1.HighlyAvailableTopologyMode, patchedInfra)
	}
	if patchedCP != "" {
		t.Errorf("expected patched control plane to be empty (unchanged), got %s", patchedCP)
	}
	if !strings.Contains(out.String(), "Transition initiated") {
		t.Errorf("expected output to confirm initiation, got:\n%s", out.String())
	}
}

// TestRun_StaleEvaluations verifies that run() refuses initiate mode when the
// controller's transition evaluations are missing, not True, or stale relative
// to the Infrastructure generation.
func TestRun_StaleEvaluations(t *testing.T) {
	tests := []struct {
		name    string
		infra   *configv1.Infrastructure
		wantErr string
	}{
		{
			name: "no topologyTransitionStatus",
			infra: &configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{Name: infrastructureResourceName},
				Status: configv1.InfrastructureStatus{
					ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
					InfrastructureTopology: configv1.SingleReplicaTopologyMode,
				},
			},
			wantErr: "evaluations are unavailable or not current",
		},
		{
			name: "evaluation still in progress",
			infra: &configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{Name: infrastructureResourceName, Generation: 2},
				Status: configv1.InfrastructureStatus{
					ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
					InfrastructureTopology: configv1.SingleReplicaTopologyMode,
					TopologyTransitionStatus: configv1.TopologyTransitionStatus{
						Conditions: []metav1.Condition{
							{
								Type:               configv1.TopologyTransitionsEvaluatedConditionType,
								Status:             metav1.ConditionUnknown,
								ObservedGeneration: 2,
							},
						},
					},
				},
			},
			wantErr: "evaluations are unavailable or not current",
		},
		{
			name: "observed generation older than Infrastructure generation",
			infra: &configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{Name: infrastructureResourceName, Generation: 3},
				Status: configv1.InfrastructureStatus{
					ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
					InfrastructureTopology: configv1.SingleReplicaTopologyMode,
					TopologyTransitionStatus: configv1.TopologyTransitionStatus{
						Conditions: []metav1.Condition{
							{
								Type:               configv1.TopologyTransitionsEvaluatedConditionType,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 2,
							},
						},
					},
				},
			},
			wantErr: "evaluations are unavailable or not current",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			configClient := fakeconfigclient.NewSimpleClientset(tc.infra)

			o := newTransitionOptions(genericclioptions.NewTestIOStreamsDiscard())
			o.configClient = configClient
			o.target = TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode}
			o.args.confirm = true

			err := o.run(context.Background())
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected error to contain %q, got: %v", tc.wantErr, err)
			}
			if hasPatchAction(configClient.Actions()) {
				t.Error("expected no patch when evaluations are stale")
			}
		})
	}
}

// ================================================================================
// STATUS COMMAND TESTS
// ================================================================================

func TestStatusCommand(t *testing.T) {
	testCases := []struct {
		name                   string
		infraSpec              configv1.TopologyMode
		infraStatus            configv1.TopologyMode
		infraTopology          configv1.TopologyMode
		infraTopologySpec      configv1.TopologyMode
		evaluatedStatus        metav1.ConditionStatus
		evaluatedReason        string
		evaluatedMessage       string
		completedStatus        metav1.ConditionStatus
		completedReason        string
		completedMessage       string
		transitions            []configv1.TopologyTransition
		expectedOutputContains []string
	}{
		{
			name:              "transition in progress",
			infraSpec:         configv1.HighlyAvailableTopologyMode,
			infraStatus:       configv1.SingleReplicaTopologyMode,
			infraTopology:     configv1.SingleReplicaTopologyMode,
			infraTopologySpec: configv1.HighlyAvailableTopologyMode,
			evaluatedStatus:   metav1.ConditionTrue,
			evaluatedReason:   "TransitionAvailable",
			evaluatedMessage:  "Topology transition evaluations completed successfully",
			completedStatus:   metav1.ConditionFalse,
			completedReason:   "TopologyTransitionInProgress",
			completedMessage:  "Transitioning control plane topology from SingleReplica to HighlyAvailable",
			expectedOutputContains: []string{
				"Control Plane Topology:",
				"Infrastructure Topology:",
				"Spec (desired):   HighlyAvailable",
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
			name:              "no transition",
			infraSpec:         configv1.HighlyAvailableTopologyMode,
			infraStatus:       configv1.HighlyAvailableTopologyMode,
			infraTopology:     configv1.HighlyAvailableTopologyMode,
			infraTopologySpec: configv1.HighlyAvailableTopologyMode,
			evaluatedStatus:   metav1.ConditionTrue,
			evaluatedReason:   "EvaluationComplete",
			evaluatedMessage:  "Topology transition evaluations completed successfully",
			completedStatus:   metav1.ConditionTrue,
			completedReason:   "AsExpected",
			completedMessage:  "No topology transition in progress",
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
			infraTopology:    "",
			evaluatedStatus:  metav1.ConditionTrue,
			evaluatedReason:  "EvaluationComplete",
			evaluatedMessage: "Topology transition evaluations completed successfully",
			completedStatus:  metav1.ConditionTrue,
			completedReason:  "AsExpected",
			completedMessage: "No topology transition in progress",
			expectedOutputContains: []string{
				"Infrastructure Topology:",
				"Spec (desired):   (not set)",
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
		{
			name:              "empty conditions but transitions populated",
			infraSpec:         configv1.HighlyAvailableTopologyMode,
			infraStatus:       configv1.HighlyAvailableTopologyMode,
			infraTopology:     configv1.SingleReplicaTopologyMode,
			infraTopologySpec: configv1.HighlyAvailableTopologyMode,
			transitions: []configv1.TopologyTransition{
				{
					Source: configv1.TopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.SingleReplicaTopologyMode,
					},
					Target: configv1.TopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
					},
					Evaluations: []metav1.Condition{
						{Type: configv1.TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Reason: "TransitionAvailable", Message: "All checks passed"},
						{Type: "WorkerNodesReady", Status: metav1.ConditionTrue, Reason: "SufficientWorkers", Message: "3 workers ready"},
					},
				},
			},
			expectedOutputContains: []string{
				`Evaluated:
    Message: Condition not available`,
				`Completed:
    Message: Condition not available`,
				"Available Transitions:",
				"WorkerNodesReady",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			streams, _, out, _ := genericclioptions.NewTestIOStreams()

			infra := &configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: configv1.InfrastructureSpec{
					ControlPlaneTopology:   tc.infraSpec,
					InfrastructureTopology: tc.infraTopologySpec,
				},
				Status: configv1.InfrastructureStatus{
					ControlPlaneTopology:   tc.infraStatus,
					InfrastructureTopology: tc.infraTopology,
				},
			}

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
					Conditions:  conditions,
					Transitions: tc.transitions,
				}
			} else if tc.transitions != nil {
				infra.Status.TopologyTransitionStatus = configv1.TopologyTransitionStatus{
					Transitions: tc.transitions,
				}
			}

			configClient := fakeconfigclient.NewSimpleClientset(infra)

			o := newStatusOptions(streams)
			o.configClient = configClient

			err := o.run(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

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
				`    - Message: Transition from SingleReplica to HighlyAvailable is supported
      Reason: TransitionSupported
      Status: "True"
      Type: TopologyTransitionAvailable`,
				`    - Message: All cluster operators are stable
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

			o := newStatusOptions(streams)
			o.configClient = configClient

			err := o.run(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := out.String()
			for _, expected := range tc.expectedOutputContains {
				if !strings.Contains(output, expected) {
					t.Errorf("expected output to contain %q, got:\n%s", expected, output)
				}
			}

			for _, missing := range tc.expectedOutputMissing {
				if strings.Contains(output, missing) {
					t.Errorf("expected output to NOT contain %q, got:\n%s", missing, output)
				}
			}

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

			o := newStatusOptions(streams)
			o.configClient = configClient
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

// ================================================================================
// VALIDATOR TESTS
// ================================================================================

func TestValidator_FindTransition(t *testing.T) {
	transitions := []configv1.TopologyTransition{
		{
			Source: configv1.TopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.SingleReplicaTopologyMode,
			},
			Target: configv1.TopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
			},
		},
	}

	v := TransitionValidator{
		Current: TopologyState{
			ControlPlane:   configv1.HighlyAvailableTopologyMode,
			Infrastructure: configv1.SingleReplicaTopologyMode,
		},
		Transitions: transitions,
	}

	t.Run("match by infra only", func(t *testing.T) {
		found := v.FindTransition(TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode})
		if found == nil {
			t.Fatal("expected to find transition")
		}
	})

	t.Run("match by both", func(t *testing.T) {
		found := v.FindTransition(TopologyState{
			ControlPlane:   configv1.HighlyAvailableTopologyMode,
			Infrastructure: configv1.HighlyAvailableTopologyMode,
		})
		if found == nil {
			t.Fatal("expected to find transition")
		}
	})

	t.Run("no match on target", func(t *testing.T) {
		found := v.FindTransition(TopologyState{Infrastructure: configv1.SingleReplicaTopologyMode})
		if found != nil {
			t.Error("expected no match")
		}
	})

	t.Run("no match when source does not match current", func(t *testing.T) {
		v2 := TransitionValidator{
			Current: TopologyState{
				ControlPlane:   configv1.SingleReplicaTopologyMode,
				Infrastructure: configv1.SingleReplicaTopologyMode,
			},
			Transitions: transitions,
		}
		found := v2.FindTransition(TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode})
		if found != nil {
			t.Error("expected no match when source doesn't match current state")
		}
	})
}

func TestValidator_FindTransition_PicksCorrectSourceAmongSameTarget(t *testing.T) {
	snoToHA := configv1.TopologyTransition{
		Source: configv1.TopologyState{
			ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
		Target: configv1.TopologyState{
			ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
			InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
		},
		Evaluations: []metav1.Condition{
			{Type: configv1.TopologyTransitionAvailableConditionType, Status: metav1.ConditionFalse, Message: "Source mismatch"},
		},
	}
	infraSRToHA := configv1.TopologyTransition{
		Source: configv1.TopologyState{
			ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
		Target: configv1.TopologyState{
			ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
			InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
		},
		Evaluations: []metav1.Condition{
			{Type: configv1.TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Message: "All checks passed"},
		},
	}

	v := TransitionValidator{
		Current: TopologyState{
			ControlPlane:   configv1.HighlyAvailableTopologyMode,
			Infrastructure: configv1.SingleReplicaTopologyMode,
		},
		Transitions: []configv1.TopologyTransition{snoToHA, infraSRToHA},
	}

	found := v.FindTransition(TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode})
	if found == nil {
		t.Fatal("expected to find transition")
	}

	if found.Source.ControlPlaneTopology != configv1.HighlyAvailableTopologyMode {
		t.Errorf("expected to match infra-only transition (source CP=HA), got source CP=%s",
			found.Source.ControlPlaneTopology)
	}

	avail, _ := IsTransitionAvailable(found)
	if !avail {
		t.Error("expected the matched transition to be available")
	}
}

func TestValidator_Validate_NotAvailable(t *testing.T) {
	transitions := []configv1.TopologyTransition{
		{
			Source: configv1.TopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.SingleReplicaTopologyMode,
			},
			Target: configv1.TopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
			},
			Evaluations: []metav1.Condition{
				{Type: configv1.TopologyTransitionAvailableConditionType, Status: metav1.ConditionFalse, Reason: "Failed", Message: "Prerequisites not met"},
				{Type: "WorkerNodesReady", Status: metav1.ConditionFalse, Reason: "InsufficientWorkers", Message: "0 workers"},
			},
		},
	}

	v := TransitionValidator{
		Current: TopologyState{
			ControlPlane:   configv1.HighlyAvailableTopologyMode,
			Infrastructure: configv1.SingleReplicaTopologyMode,
		},
		Transitions: transitions,
	}

	err := v.Validate(TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.HighlyAvailableTopologyMode,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("expected 'not available' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "WorkerNodesReady") {
		t.Errorf("expected prerequisite details in error, got: %v", err)
	}
}

func TestValidator_Validate_NoTransitions(t *testing.T) {
	v := TransitionValidator{}
	err := v.Validate(TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no topology transitions are available") {
		t.Errorf("expected 'no topology transitions', got: %v", err)
	}
}
