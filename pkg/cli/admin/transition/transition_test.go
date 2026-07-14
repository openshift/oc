package transition

import (
	"context"
	"errors"
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	fakeconfigclient "github.com/openshift/client-go/config/clientset/versioned/fake"
	fakeoperatorclient "github.com/openshift/client-go/operator/clientset/versioned/fake"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
  - Flag dependencies                       - --confirm/--allow-* require correct flags

DISCOVERY MODE
  - Discovery mode from SingleReplica       - Shows available transition
  - Discovery mode from HighlyAvailable     - Shows no transitions, not supported note

INITIATE MODE
  - Patches Infrastructure with --confirm   - Verifies Update action with correct topology
  - Default dry-run without --confirm       - Verifies no Update action, dry-run output
  - Already at target topology              - Early exit with success message, no validation

STATUS COMMAND
  - Status command output                   - 5 scenarios: transition in progress, stable,
                                              preflight failed, unsupported, empty infra topology

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
			o.controlPlane = tc.controlPlane
			o.infrastructure = tc.infrastructure

			err := o.validate()

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
			o.controlPlane = tc.controlPlane
			o.infrastructure = tc.infrastructure
			o.confirm = tc.confirm

			err := o.validate()

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

	err := o.runDiscoveryMode(configv1.SingleReplicaTopologyMode, configv1.SingleReplicaTopologyMode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()

	// Should show current topology for both control plane and infrastructure
	if !strings.Contains(output, "Current Topology:") {
		t.Errorf("expected current topology header in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Control Plane:") {
		t.Errorf("expected control plane topology in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Infrastructure:") {
		t.Errorf("expected infrastructure topology in output, got:\n%s", output)
	}

	// Should show available transition for control plane
	if !strings.Contains(output, "Control Plane:    SingleReplica -> HighlyAvailable") {
		t.Errorf("expected available control plane transition in output, got:\n%s", output)
	}

	// Should indicate infrastructure also transitions
	if !strings.Contains(output, "Infrastructure also transitions") {
		t.Errorf("expected note about infrastructure transition in output, got:\n%s", output)
	}

	// Should show command to initiate with just --control-plane flag
	if !strings.Contains(output, "--control-plane=HighlyAvailable --confirm") {
		t.Errorf("expected initiate command with --control-plane flag in output, got:\n%s", output)
	}
}

// TestRunDiscoveryMode_HighlyAvailable tests discovery mode output for HA
func TestRunDiscoveryMode_HighlyAvailable(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()
	o := newTransitionOptions(streams)

	err := o.runDiscoveryMode(configv1.HighlyAvailableTopologyMode, configv1.HighlyAvailableTopologyMode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()

	// Should show current topology for both control plane and infrastructure
	if !strings.Contains(output, "Current Topology:") {
		t.Errorf("expected current topology header in output, got:\n%s", output)
	}

	// Should show no available transitions
	if !strings.Contains(output, "(none") {
		t.Errorf("expected no available transitions in output, got:\n%s", output)
	}

	// Should mention what transitions are supported
	if !strings.Contains(output, "Only SingleReplica -> HighlyAvailable") {
		t.Errorf("expected note about supported transition types in output, got:\n%s", output)
	}
}

func TestRunDiscoveryModeReturnsWriteError(t *testing.T) {
	wantErr := errors.New("write failed")
	o := newTransitionOptions(genericclioptions.IOStreams{Out: failingWriter{err: wantErr}})

	if err := o.runDiscoveryMode(configv1.SingleReplicaTopologyMode, configv1.SingleReplicaTopologyMode); !errors.Is(err, wantErr) {
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

	o := &transitionOptions{
		controlPlane:   "HighlyAvailable",
		infrastructure: "HighlyAvailable",
		confirm:        true, // Apply the transition
		IOStreams:      streams,
		configClient:   configClient,
	}

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

	o := &transitionOptions{
		controlPlane:   "HighlyAvailable",
		infrastructure: "HighlyAvailable",
		// No --confirm flag, so defaults to dry-run
		IOStreams:    streams,
		configClient: configClient,
	}

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

	o := &transitionOptions{
		controlPlane:   "SingleReplica",
		infrastructure: "SingleReplica",
		IOStreams:      streams,
		configClient:   configClient,
	}

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
	if !strings.Contains(output, "No transition needed") {
		t.Error("expected output to indicate no transition needed")
	}

	// Verify validation was NOT run
	if strings.Contains(output, "Running preflight validation") {
		t.Error("expected validation to be skipped when already at target")
	}
}

func TestRunInitiateModeReturnsWriteError(t *testing.T) {
	wantErr := errors.New("write failed")
	o := &transitionOptions{
		controlPlane: "HighlyAvailable",
		IOStreams: genericclioptions.IOStreams{
			Out: failingWriter{err: wantErr},
		},
	}

	if err := o.runInitiateMode(context.Background(), configv1.SingleReplicaTopologyMode, configv1.SingleReplicaTopologyMode); !errors.Is(err, wantErr) {
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
		progressingStatus      configv1.ConditionStatus
		progressingReason      string
		progressingMessage     string
		upgradeableStatus      configv1.ConditionStatus
		upgradeableReason      string
		upgradeableMessage     string
		expectedOutputContains []string
		expectError            bool
	}{
		{
			name:               "transition in progress",
			infraSpec:          configv1.HighlyAvailableTopologyMode,
			infraStatus:        configv1.SingleReplicaTopologyMode,
			infraTopology:      configv1.SingleReplicaTopologyMode,
			progressingStatus:  configv1.ConditionTrue,
			progressingReason:  "TopologyTransitionInProgress",
			progressingMessage: "Transitioning control plane topology from SingleReplica to HighlyAvailable",
			upgradeableStatus:  configv1.ConditionFalse,
			upgradeableReason:  "TopologyTransitionInProgress",
			upgradeableMessage: "Cluster upgrade is not allowed during topology transition",
			expectedOutputContains: []string{
				"Control Plane Topology:",
				"Spec (desired):   HighlyAvailable",
				"Status (current): SingleReplica",
				"Infrastructure Topology:",
				"Transitioning control plane topology from SingleReplica to HighlyAvailable",
				"Progressing: True",
				"Upgradeable: False",
			},
		},
		{
			name:               "no transition",
			infraSpec:          configv1.HighlyAvailableTopologyMode,
			infraStatus:        configv1.HighlyAvailableTopologyMode,
			infraTopology:      configv1.HighlyAvailableTopologyMode,
			upgradeableStatus:  configv1.ConditionTrue,
			upgradeableReason:  "AsExpected",
			upgradeableMessage: "No topology transition in progress",
			expectedOutputContains: []string{
				"Control Plane Topology:",
				"Spec (desired):   HighlyAvailable",
				"Status (current): HighlyAvailable",
				"Infrastructure Topology:",
				"Status (current): HighlyAvailable",
				"No transition in progress",
				"Upgradeable: True",
			},
		},
		{
			name:               "preflight failed",
			infraSpec:          configv1.HighlyAvailableTopologyMode,
			infraStatus:        configv1.SingleReplicaTopologyMode,
			infraTopology:      configv1.SingleReplicaTopologyMode,
			progressingStatus:  configv1.ConditionFalse,
			progressingReason:  topologyTransitionPreflightCheckFailedReason,
			progressingMessage: "Cluster operators are not stable",
			upgradeableStatus:  configv1.ConditionFalse,
			upgradeableReason:  topologyTransitionPreflightCheckFailedReason,
			upgradeableMessage: "Cluster upgrade is not allowed while a topology transition is pending",
			expectedOutputContains: []string{
				"Preflight checks failed",
				"Progressing: False",
				"Reason: PreflightCheckFailed",
			},
		},
		{
			name:               "unsupported transition",
			infraSpec:          configv1.SingleReplicaTopologyMode,
			infraStatus:        configv1.HighlyAvailableTopologyMode,
			infraTopology:      configv1.HighlyAvailableTopologyMode,
			progressingStatus:  configv1.ConditionFalse,
			progressingReason:  topologyTransitionUnsupportedTransitionReason,
			progressingMessage: "Transition from HighlyAvailable to SingleReplica is not supported",
			upgradeableStatus:  configv1.ConditionFalse,
			upgradeableReason:  topologyTransitionUnsupportedTransitionReason,
			upgradeableMessage: "Cluster upgrade is not allowed while a topology transition is requested",
			expectedOutputContains: []string{
				"Unsupported transition",
				"Progressing: False",
				"Reason: UnsupportedTransition",
			},
		},
		{
			name:               "empty infrastructure topology",
			infraSpec:          configv1.HighlyAvailableTopologyMode,
			infraStatus:        configv1.HighlyAvailableTopologyMode,
			infraTopology:      "", // Empty infrastructure topology
			upgradeableStatus:  configv1.ConditionTrue,
			upgradeableReason:  "AsExpected",
			upgradeableMessage: "No topology transition in progress",
			expectedOutputContains: []string{
				"Control Plane Topology:",
				"Infrastructure Topology:",
				"Status (current): (not set)",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			streams, _, out, _ := genericclioptions.NewTestIOStreams()

			// Create fake infrastructure
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

			// Create cluster-config-operator Config resource with conditions
			operatorConfig := &operatorv1.Config{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Status:     operatorv1.ConfigStatus{},
			}

			if tc.progressingStatus != "" {
				operatorConfig.Status.Conditions = append(operatorConfig.Status.Conditions,
					operatorv1.OperatorCondition{
						Type:    topologyTransitionControllerProgressingCondition,
						Status:  operatorv1.ConditionStatus(tc.progressingStatus),
						Reason:  tc.progressingReason,
						Message: tc.progressingMessage,
					},
				)
			}

			if tc.upgradeableStatus != "" {
				operatorConfig.Status.Conditions = append(operatorConfig.Status.Conditions,
					operatorv1.OperatorCondition{
						Type:    topologyTransitionControllerUpgradeableCondition,
						Status:  operatorv1.ConditionStatus(tc.upgradeableStatus),
						Reason:  tc.upgradeableReason,
						Message: tc.upgradeableMessage,
					},
				)
			}

			configClient := fakeconfigclient.NewSimpleClientset(infra)

			// Manually add Config to tracker since it's not yet registered in the scheme
			operatorClient := fakeoperatorclient.NewSimpleClientset()
			gvr := schema.GroupVersionResource{
				Group:    "operator.openshift.io",
				Version:  "v1",
				Resource: "configs",
			}
			if err := operatorClient.Tracker().Add(operatorConfig); err != nil {
				// If add fails due to scheme issue, try creating via GVR
				if err := operatorClient.Tracker().Create(gvr, operatorConfig, ""); err != nil {
					t.Fatalf("failed to add operatorConfig to tracker: %v", err)
				}
			}

			// Create statusOptions
			o := &statusOptions{
				IOStreams:      streams,
				configClient:   configClient,
				operatorClient: operatorClient,
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

func TestStatusCommandErrors(t *testing.T) {
	tests := []struct {
		name           string
		failResource   string
		out            failingWriter
		wantWriteError bool
	}{
		{name: "Infrastructure read", failResource: "infrastructures"},
		{name: "operator Config read", failResource: "configs"},
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
			operatorClient := fakeoperatorclient.NewSimpleClientset()
			if tc.failResource == "infrastructures" {
				configClient.PrependReactor("get", tc.failResource, func(ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("read failed")
				})
			}
			if tc.failResource == "configs" {
				operatorClient.PrependReactor("get", tc.failResource, func(ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("read failed")
				})
			}

			o := &statusOptions{IOStreams: streams, configClient: configClient, operatorClient: operatorClient}
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
