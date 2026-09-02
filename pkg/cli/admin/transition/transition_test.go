package transition

import (
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	fakeconfigclient "github.com/openshift/client-go/config/clientset/versioned/fake"
	fakeoperatorclient "github.com/openshift/client-go/operator/clientset/versioned/fake"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	fake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/openshift/oc/pkg/cli/admin/transition/preflight"
)

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
	requiredFlags := []string{"control-plane", "infrastructure", "confirm", "allow-transition-with-warnings"}
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
			o.ControlPlane = tc.controlPlane
			o.Infrastructure = tc.infrastructure

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
		name                        string
		controlPlane                string
		infrastructure              string
		confirm                     bool
		allowTransitionWithWarnings bool
		expectErr                   bool
		errContains                 string
	}{
		{
			name:        "--confirm requires at least one topology flag",
			confirm:     true,
			expectErr:   true,
			errContains: "--confirm requires at least one of --control-plane or --infrastructure",
		},
		{
			name:                        "--allow-transition-with-warnings requires --confirm",
			controlPlane:                "HighlyAvailable",
			allowTransitionWithWarnings: true,
			expectErr:                   true,
			errContains:                 "--allow-transition-with-warnings requires --confirm",
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
		{
			name:                        "--control-plane with --confirm and --allow is valid",
			controlPlane:                "HighlyAvailable",
			confirm:                     true,
			allowTransitionWithWarnings: true,
			expectErr:                   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			streams := genericclioptions.NewTestIOStreamsDiscard()
			o := newTransitionOptions(streams)
			o.ControlPlane = tc.controlPlane
			o.Infrastructure = tc.infrastructure
			o.Confirm = tc.confirm
			o.AllowTransitionWithWarnings = tc.allowTransitionWithWarnings

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

// findSubcommand finds a subcommand by name
func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, cmd := range parent.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}

// Helper functions for test fixtures

func newFakeNode(name string, master, worker, ready, schedulable bool) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{},
		},
		Spec: corev1.NodeSpec{
			Unschedulable: !schedulable,
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{},
		},
	}

	if master {
		node.Labels["node-role.kubernetes.io/master"] = ""
	}
	if worker {
		node.Labels["node-role.kubernetes.io/worker"] = ""
	}
	if ready {
		node.Status.Conditions = append(node.Status.Conditions, corev1.NodeCondition{
			Type:   corev1.NodeReady,
			Status: corev1.ConditionTrue,
		})
	}

	return node
}

func newFakeEtcdOperator(available, progressing bool) *operatorv1.Etcd {
	etcd := &operatorv1.Etcd{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
	}

	// EtcdStatus embeds StaticPodOperatorStatus which embeds OperatorStatus
	// Set Conditions directly on Status
	if available {
		etcd.Status.Conditions = append(etcd.Status.Conditions, operatorv1.OperatorCondition{
			Type:   "EtcdMembersAvailable",
			Status: operatorv1.ConditionTrue,
		})
	}
	if progressing {
		etcd.Status.Conditions = append(etcd.Status.Conditions, operatorv1.OperatorCondition{
			Type:   operatorv1.OperatorStatusTypeProgressing,
			Status: operatorv1.ConditionTrue,
		})
	}

	return etcd
}

func newFakeEtcdConfigMap(memberCount int) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "etcd-endpoints",
			Namespace: "openshift-etcd",
		},
		Data: map[string]string{},
	}

	for i := 0; i < memberCount; i++ {
		cm.Data[string(rune('a'+i))] = "etcd-member"
	}

	return cm
}

// newFakeClusterOperator creates a ClusterOperator with specified conditions
func newFakeClusterOperator(name string, available, progressing, degraded bool) *configv1.ClusterOperator {
	return &configv1.ClusterOperator{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: configv1.ClusterOperatorStatus{
			Conditions: []configv1.ClusterOperatorStatusCondition{
				{
					Type:   configv1.OperatorAvailable,
					Status: boolToConfigConditionStatus(available),
				},
				{
					Type:   configv1.OperatorProgressing,
					Status: boolToConfigConditionStatus(progressing),
				},
				{
					Type:   configv1.OperatorDegraded,
					Status: boolToConfigConditionStatus(degraded),
				},
			},
		},
	}
}

// boolToConfigConditionStatus converts bool to configv1.ConditionStatus
func boolToConfigConditionStatus(b bool) configv1.ConditionStatus {
	if b {
		return configv1.ConditionTrue
	}
	return configv1.ConditionFalse
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

	configClient := fakeconfigclient.NewSimpleClientset(
		infra,
		&configv1.FeatureGate{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Status: configv1.FeatureGateStatus{
				FeatureGates: []configv1.FeatureGateDetails{
					{
						Version: "4.18",
						Enabled: []configv1.FeatureGateAttributes{
							{Name: "MutableTopology"},
						},
					},
				},
			},
		},
		// Add stable cluster operators for preflight validation
		newFakeClusterOperator("kube-apiserver", true, false, false),
		newFakeClusterOperator("etcd", true, false, false),
		newFakeClusterOperator("network", true, false, false),
	)

	kubeClient := fake.NewClientset(
		newFakeNode("master-0", true, false, true, true),
		newFakeNode("master-1", true, false, true, true),
		newFakeNode("master-2", true, false, true, true),
		newFakeEtcdConfigMap(3),
	)

	operatorClient := fakeoperatorclient.NewSimpleClientset(
		newFakeEtcdOperator(true, false),
	)

	o := &transitionOptions{
		ControlPlane:   "HighlyAvailable",
		Infrastructure: "HighlyAvailable",
		Confirm:        true, // Apply the transition
		IOStreams:      streams,
		kubeClient:     kubeClient,
		configClient:   configClient,
		operatorClient: operatorClient,
		validator:      preflight.NewClientSideValidator(kubeClient, configClient, operatorClient),
	}

	// Run the command
	err := o.run()
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

	configClient := fakeconfigclient.NewSimpleClientset(
		infra,
		&configv1.FeatureGate{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Status: configv1.FeatureGateStatus{
				FeatureGates: []configv1.FeatureGateDetails{
					{
						Version: "4.18",
						Enabled: []configv1.FeatureGateAttributes{
							{Name: "MutableTopology"},
						},
					},
				},
			},
		},
		// Add stable cluster operators for preflight validation
		newFakeClusterOperator("kube-apiserver", true, false, false),
		newFakeClusterOperator("etcd", true, false, false),
		newFakeClusterOperator("network", true, false, false),
	)

	kubeClient := fake.NewClientset(
		newFakeNode("master-0", true, false, true, true),
		newFakeNode("master-1", true, false, true, true),
		newFakeNode("master-2", true, false, true, true),
		newFakeEtcdConfigMap(3),
	)

	operatorClient := fakeoperatorclient.NewSimpleClientset(
		newFakeEtcdOperator(true, false),
	)

	o := &transitionOptions{
		ControlPlane:   "HighlyAvailable",
		Infrastructure: "HighlyAvailable",
		// No --confirm flag, so defaults to dry-run
		IOStreams:      streams,
		kubeClient:     kubeClient,
		configClient:   configClient,
		operatorClient: operatorClient,
		validator:      preflight.NewClientSideValidator(kubeClient, configClient, operatorClient),
	}

	// Run the command
	err := o.run()
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
	kubeClient := fake.NewClientset()
	operatorClient := fakeoperatorclient.NewSimpleClientset()

	o := &transitionOptions{
		ControlPlane:   "SingleReplica",
		Infrastructure: "SingleReplica",
		IOStreams:      streams,
		kubeClient:     kubeClient,
		configClient:   configClient,
		operatorClient: operatorClient,
		validator:      preflight.NewClientSideValidator(kubeClient, configClient, operatorClient),
	}

	// Run the command
	err := o.run()
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
			err := o.run()
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
