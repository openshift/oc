package preflight

/*
================================================================================
CLIENT-SIDE VALIDATOR TESTS
================================================================================

This file tests the ClientSideValidator implementation which duplicates CCO's
preflight validation logic for topology transitions. Tests use fake clients to
verify the orchestration logic and one representative test per validation pattern.

TIERED VALIDATION APPROACH:
  Phase 1: ALL Error-severity checks (blocking - show complete picture)
    - Supported transition (CEL-based: only SNO -> HA)
    - Feature gate enabled (MutableTopology)

  Phase 2: ALL Warning-severity checks (cluster readiness - bypassable)
    - Cluster operators stable
    - Control plane node count/schedulability/readiness
    - etcd quorum/stability/voting member count

  Short-circuit after Phase 1 if ANY Error checks fail to avoid wasting time
  checking cluster state when transition isn't even valid.

--------------------------------------------------------------------------------
TEST COVERAGE
--------------------------------------------------------------------------------

ORCHESTRATION
  - SNO -> HighlyAvailable, all checks pass     - Full integration of 10 checks
  - Already at target topology                  - Error check short-circuit logic
  - Unsupported transition (HA -> SNO)          - CEL transition validation
  - Infrastructure stays SingleReplica          - Validates infra must transition with CP
  - Error checks pass, Warning checks fail      - Tiered validation behavior
  - All Warning checks fail simultaneously      - Comprehensive failure scenario

FEATURE GATE (3 tests - complex parsing)
  - MutableTopology enabled                     - Version array, enabled list parsing
  - FeatureGate resource missing                - Error handling
  - MutableTopology disabled                    - CheckStatusFailed branch coverage

VALIDATORS (8 subtests - smoke tests per pattern)
  - ClusterOperators stable                     - Condition checking pattern
  - Control plane node count (3)                - Counting pattern
  - Infrastructure node count (0)               - Inverse counting (NOT control-plane)
  - Control plane nodes schedulable             - Taint checking
  - Control plane nodes ready                   - Node condition iteration
  - Etcd quorum available                       - library-go v1helpers usage
  - Etcd not progressing                        - Etcd-specific conditions
  - Etcd voting members (3)                     - ConfigMap data parsing

TYPES (3 tests - status computation only)
  - ValidationResult status all pass            - Status computation logic
  - ValidationResult status some fail           - Status computation logic
  - ClientSideValidator interface compliance    - Interface implementation

--------------------------------------------------------------------------------
*/

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	fakeconfigclient "github.com/openshift/client-go/config/clientset/versioned/fake"
	fakeoperatorclient "github.com/openshift/client-go/operator/clientset/versioned/fake"
)

// TestValidate_SNOToHA_AllChecksPass tests the happy path: SNO → HighlyAvailable with all checks passing
func TestValidate_SNOToHA_AllChecksPass(t *testing.T) {
	// Create healthy cluster state
	kubeClient := fake.NewClientset(
		// 3 healthy, schedulable, ready control plane nodes
		newFakeNode("master-0", true, false, true, true),
		newFakeNode("master-1", true, false, true, true),
		newFakeNode("master-2", true, false, true, true),
		// etcd-endpoints ConfigMap with 3 voting members
		newFakeEtcdConfigMap(3),
	)

	configClient := fakeconfigclient.NewSimpleClientset(
		// MutableTopology feature gate enabled
		newFakeFeatureGate(true),
		// 3 stable ClusterOperators
		newFakeClusterOperator("kube-apiserver", true, false, false),
		newFakeClusterOperator("etcd", true, false, false),
		newFakeClusterOperator("network", true, false, false),
	)

	operatorClient := fakeoperatorclient.NewSimpleClientset(
		// Healthy etcd operator: quorum available, not progressing
		newFakeEtcdOperator(true, false),
	)

	validator := NewClientSideValidator(kubeClient, configClient, operatorClient)

	// Validate SNO → HA transition
	current := TopologyState{ControlPlane: configv1.SingleReplicaTopologyMode, Infrastructure: configv1.SingleReplicaTopologyMode}
	target := TopologyState{ControlPlane: configv1.HighlyAvailableTopologyMode, Infrastructure: configv1.HighlyAvailableTopologyMode}
	result, err := validator.Validate(context.Background(), current, target)

	// Should complete without API error
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Build expected result
	expected := &ValidationResult{
		Current: current,
		Target:  target,
		Status:  ValidationStatusReady,
		Checks: []CheckResult{
			// Error-severity checks (Phase 1)
			{Name: CheckNameSupportedTransition, Severity: CheckSeverityError, Status: CheckStatusPassed},
			{Name: CheckNameFeatureGateEnabled, Severity: CheckSeverityError, Status: CheckStatusPassed},
			// Warning-severity checks (Phase 2)
			{Name: CheckNameClusterOperatorsStable, Severity: CheckSeverityWarning, Status: CheckStatusPassed},
			{Name: CheckNameControlPlaneNodeCount, Severity: CheckSeverityWarning, Status: CheckStatusPassed},
			{Name: CheckNameInfrastructureNodeCount, Severity: CheckSeverityWarning, Status: CheckStatusPassed},
			{Name: CheckNameControlPlaneNodesSchedulable, Severity: CheckSeverityWarning, Status: CheckStatusPassed},
			{Name: CheckNameControlPlaneNodesReady, Severity: CheckSeverityWarning, Status: CheckStatusPassed},
			{Name: CheckNameEtcdQuorum, Severity: CheckSeverityWarning, Status: CheckStatusPassed},
			{Name: CheckNameEtcdNotProgressing, Severity: CheckSeverityWarning, Status: CheckStatusPassed},
			{Name: CheckNameEtcdVotingMembers, Severity: CheckSeverityWarning, Status: CheckStatusPassed},
		},
	}

	// Compare using cmp.Diff (ignore Message field which can vary)
	opts := cmp.Options{
		cmp.Comparer(func(a, b CheckResult) bool {
			return a.Name == b.Name &&
				a.Severity == b.Severity &&
				a.Status == b.Status
		}),
	}

	if diff := cmp.Diff(expected, result, opts); diff != "" {
		t.Errorf("ValidationResult mismatch (-want +got):\n%s", diff)
	}
}

// TestValidate_AlreadyAtTarget tests validation when already at target topology
func TestValidate_AlreadyAtTarget(t *testing.T) {
	// Create validator with feature gate enabled (other resources not needed for this test)
	validator := NewClientSideValidator(
		fake.NewClientset(),
		fakeconfigclient.NewSimpleClientset(
			newFakeFeatureGate(true),
		),
		fakeoperatorclient.NewSimpleClientset(),
	)

	// Attempt to transition when already at HA: HA → HA
	current := TopologyState{ControlPlane: configv1.HighlyAvailableTopologyMode, Infrastructure: configv1.HighlyAvailableTopologyMode}
	target := TopologyState{ControlPlane: configv1.HighlyAvailableTopologyMode, Infrastructure: configv1.HighlyAvailableTopologyMode}
	result, err := validator.Validate(context.Background(), current, target)

	// Should complete without API error
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Build expected result
	expected := &ValidationResult{
		Current: current,
		Target:  target,
		Status:  ValidationStatusNotReady,
		Checks: []CheckResult{
			{Name: CheckNameSupportedTransition, Severity: CheckSeverityError, Status: CheckStatusFailed},
			{Name: CheckNameFeatureGateEnabled, Severity: CheckSeverityError, Status: CheckStatusPassed},
		},
	}

	// Compare using cmp.Diff (ignore Message field which can vary)
	opts := cmp.Options{
		cmp.Comparer(func(a, b CheckResult) bool {
			return a.Name == b.Name &&
				a.Severity == b.Severity &&
				a.Status == b.Status
		}),
	}

	if diff := cmp.Diff(expected, result, opts); diff != "" {
		t.Errorf("ValidationResult mismatch (-want +got):\n%s", diff)
	}

	// Verify the failed check has the expected message
	if !strings.Contains(result.Checks[0].Message, "cluster is already at target topology") {
		t.Errorf("expected Supported Transition message to contain 'cluster is already at target topology', got: %s", result.Checks[0].Message)
	}
}

// TestValidate_UnsupportedTransition tests HA → SNO (one-way only)
func TestValidate_UnsupportedTransition(t *testing.T) {
	// Create validator with feature gate enabled (other resources not needed for this test)
	validator := NewClientSideValidator(
		fake.NewClientset(),
		fakeconfigclient.NewSimpleClientset(
			newFakeFeatureGate(true),
		),
		fakeoperatorclient.NewSimpleClientset(),
	)

	// Attempt unsupported transition: HA → SNO
	current := TopologyState{ControlPlane: configv1.HighlyAvailableTopologyMode, Infrastructure: configv1.HighlyAvailableTopologyMode}
	target := TopologyState{ControlPlane: configv1.SingleReplicaTopologyMode, Infrastructure: configv1.SingleReplicaTopologyMode}
	result, err := validator.Validate(context.Background(), current, target)

	// Should complete without API error
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have Not Ready status due to Error-severity check failure
	if result.Status != ValidationStatusNotReady {
		t.Errorf("expected status=Not Ready for unsupported transition, got %s", result.Status)
	}

	// Should have at least one check (the supported transition check)
	if len(result.Checks) == 0 {
		t.Fatal("expected at least one check result")
	}

	// First check should be "Supported Transition" which fails (HA -> SNO not supported)
	expectedCheck := CheckResult{
		Name:     CheckNameSupportedTransition,
		Severity: CheckSeverityError,
		Status:   CheckStatusFailed,
		Message:  "only SingleReplica -> HighlyAvailable transitions are supported (current state: control plane HighlyAvailable, infrastructure HighlyAvailable)",
	}
	if diff := cmp.Diff(expectedCheck, result.Checks[0]); diff != "" {
		t.Errorf("unexpected Supported Transition check (-want +got):\n%s", diff)
	}

	// Should short-circuit after Error-severity checks, so only 2 checks should run
	if len(result.Checks) != 2 {
		t.Errorf("expected only 2 checks (short-circuit after error phase), got %d", len(result.Checks))
	}
}

// TestValidate_InfrastructureStaysAtSingleReplica tests that trying to transition CP to HA
// while keeping infrastructure at SingleReplica is rejected
func TestValidate_InfrastructureStaysAtSingleReplica(t *testing.T) {
	// Create validator with feature gate enabled (other resources not needed for this test)
	validator := NewClientSideValidator(
		fake.NewClientset(),
		fakeconfigclient.NewSimpleClientset(
			newFakeFeatureGate(true),
		),
		fakeoperatorclient.NewSimpleClientset(),
	)

	// Attempt invalid transition: CP SNO → HA, but Infra stays at SingleReplica
	current := TopologyState{ControlPlane: configv1.SingleReplicaTopologyMode, Infrastructure: configv1.SingleReplicaTopologyMode}
	target := TopologyState{ControlPlane: configv1.HighlyAvailableTopologyMode, Infrastructure: configv1.SingleReplicaTopologyMode}
	result, err := validator.Validate(context.Background(), current, target)

	// Should complete without API error
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have Not Ready status due to Error-severity check failure
	if result.Status != ValidationStatusNotReady {
		t.Errorf("expected status=Not Ready for invalid infrastructure target, got %s", result.Status)
	}

	// Should have at least one check (the supported transition check)
	if len(result.Checks) == 0 {
		t.Fatal("expected at least one check result")
	}

	// First check should be "Supported Transition" which fails
	expectedCheck := CheckResult{
		Name:     CheckNameSupportedTransition,
		Severity: CheckSeverityError,
		Status:   CheckStatusFailed,
		Message:  "infrastructure must transition to HighlyAvailable with control plane (infrastructure: SingleReplica -> SingleReplica, control plane: SingleReplica -> HighlyAvailable)",
	}
	if diff := cmp.Diff(expectedCheck, result.Checks[0]); diff != "" {
		t.Errorf("unexpected Supported Transition check (-want +got):\n%s", diff)
	}

	// Should short-circuit after Error-severity checks, so only 2 checks should run
	if len(result.Checks) != 2 {
		t.Errorf("expected only 2 checks (short-circuit after error phase), got %d", len(result.Checks))
	}
}

// TestValidateFeatureGateEnabled_Enabled tests feature gate check with MutableTopology enabled
func TestValidateFeatureGateEnabled_Enabled(t *testing.T) {
	validator := NewClientSideValidator(
		fake.NewClientset(),
		fakeconfigclient.NewSimpleClientset(
			newFakeFeatureGate(true),
		),
		fakeoperatorclient.NewSimpleClientset(),
	)

	result := validator.validateFeatureGateEnabled(context.Background())

	expected := CheckResult{
		Name:     CheckNameFeatureGateEnabled,
		Severity: CheckSeverityError,
		Status:   CheckStatusPassed,
		Message:  "", // checkPassed returns empty message
	}
	if diff := cmp.Diff(expected, result); diff != "" {
		t.Errorf("unexpected Feature Gate Enabled check (-want +got):\n%s", diff)
	}
}

// TestValidateFeatureGateEnabled_Missing tests feature gate check with FeatureGate resource missing
func TestValidateFeatureGateEnabled_Missing(t *testing.T) {
	validator := NewClientSideValidator(
		fake.NewClientset(),
		fakeconfigclient.NewSimpleClientset(),
		fakeoperatorclient.NewSimpleClientset(),
	)

	result := validator.validateFeatureGateEnabled(context.Background())

	expected := CheckResult{
		Name:     CheckNameFeatureGateEnabled,
		Severity: CheckSeverityError,
		Status:   CheckStatusUnknown,
		// Message contains fake client error details, ignore it
	}
	if diff := cmp.Diff(expected, result, cmpopts.IgnoreFields(CheckResult{}, "Message")); diff != "" {
		t.Errorf("unexpected Feature Gate Enabled check (-want +got):\n%s", diff)
	}
}

// TestValidateFeatureGateEnabled_Disabled tests feature gate check with MutableTopology disabled
func TestValidateFeatureGateEnabled_Disabled(t *testing.T) {
	validator := NewClientSideValidator(
		fake.NewClientset(),
		fakeconfigclient.NewSimpleClientset(newFakeFeatureGate(false)),
		fakeoperatorclient.NewSimpleClientset(),
	)

	result := validator.validateFeatureGateEnabled(context.Background())

	expected := CheckResult{
		Name:     CheckNameFeatureGateEnabled,
		Severity: CheckSeverityError,
		Status:   CheckStatusFailed,
		Message:  "MutableTopology feature gate is not enabled (required for topology transitions)",
	}
	if diff := cmp.Diff(expected, result); diff != "" {
		t.Errorf("unexpected Feature Gate Enabled check (-want +got):\n%s", diff)
	}
}

// Individual validator tests have been consolidated into validator_helpers_test.go
// using a table-driven approach for better maintainability and reduced code duplication.

// ============================================================================
// HELPER FUNCTIONS FOR BUILDING TEST FIXTURES
// ============================================================================

// newFakeFeatureGate creates a FeatureGate resource with MutableTopology enabled or disabled
func newFakeFeatureGate(enabled bool) *configv1.FeatureGate {
	fg := &configv1.FeatureGate{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.FeatureGateStatus{
			FeatureGates: []configv1.FeatureGateDetails{
				{
					Version:  "4.18",
					Enabled:  []configv1.FeatureGateAttributes{},
					Disabled: []configv1.FeatureGateAttributes{},
				},
			},
		},
	}

	if enabled {
		fg.Status.FeatureGates[0].Enabled = append(fg.Status.FeatureGates[0].Enabled, configv1.FeatureGateAttributes{
			Name: "MutableTopology",
		})
	} else {
		fg.Status.FeatureGates[0].Disabled = append(fg.Status.FeatureGates[0].Disabled, configv1.FeatureGateAttributes{
			Name: "MutableTopology",
		})
	}

	return fg
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

// newFakeNode creates a Node with specified properties
func newFakeNode(name string, controlPlane, worker, ready, schedulable bool) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{},
		},
		Spec: corev1.NodeSpec{},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{},
		},
	}

	// Add role labels
	if controlPlane {
		node.Labels["node-role.kubernetes.io/control-plane"] = ""
		node.Labels["node-role.kubernetes.io/master"] = "" // Legacy label
	}
	if worker {
		node.Labels["node-role.kubernetes.io/worker"] = ""
	}

	// Add Ready condition
	node.Status.Conditions = append(node.Status.Conditions, corev1.NodeCondition{
		Type:   corev1.NodeReady,
		Status: boolToConditionStatus(ready),
	})

	// Add unschedulable taint if not schedulable
	if !schedulable {
		node.Spec.Taints = append(node.Spec.Taints, corev1.Taint{
			Key:    "node.kubernetes.io/unschedulable",
			Effect: corev1.TaintEffectNoSchedule,
		})
	}

	return node
}

// newFakeEtcdOperator creates an Etcd operator resource with specified conditions
func newFakeEtcdOperator(quorum, progressing bool) *operatorv1.Etcd {
	etcd := &operatorv1.Etcd{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
	}
	// EtcdStatus embeds StaticPodOperatorStatus which embeds OperatorStatus
	// We need to set Conditions on the embedded OperatorStatus
	etcd.Status.Conditions = []operatorv1.OperatorCondition{
		{
			Type:   "EtcdMembersAvailable",
			Status: boolToOperatorConditionStatus(quorum),
		},
		{
			Type:   operatorv1.OperatorStatusTypeProgressing,
			Status: boolToOperatorConditionStatus(progressing),
		},
	}
	return etcd
}

// newFakeEtcdConfigMap creates a ConfigMap with etcd member list
// The number of keys in Data corresponds to the number of voting members
func newFakeEtcdConfigMap(votingMembers int) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "etcd-endpoints",
			Namespace: "openshift-etcd",
		},
		Data: map[string]string{},
	}

	// Add one entry per voting member
	// The actual format is: <node-name>: <etcd-endpoint-url>
	for i := 0; i < votingMembers; i++ {
		nodeName := fmt.Sprintf("master-%d", i)
		endpoint := fmt.Sprintf("https://10.0.0.%d:2379", i+1)
		cm.Data[nodeName] = endpoint
	}

	return cm
}

// boolToConditionStatus converts bool to corev1.ConditionStatus
func boolToConditionStatus(b bool) corev1.ConditionStatus {
	if b {
		return corev1.ConditionTrue
	}
	return corev1.ConditionFalse
}

// boolToConfigConditionStatus converts bool to configv1.ConditionStatus
func boolToConfigConditionStatus(b bool) configv1.ConditionStatus {
	if b {
		return configv1.ConditionTrue
	}
	return configv1.ConditionFalse
}

// boolToOperatorConditionStatus converts bool to operatorv1.ConditionStatus
func boolToOperatorConditionStatus(b bool) operatorv1.ConditionStatus {
	if b {
		return operatorv1.ConditionTrue
	}
	return operatorv1.ConditionFalse
}

// TestValidate_WarningFailures verifies behavior when Error checks pass but Warning checks fail.
// This tests the tiered validation approach: even though Error checks (transition support,
// feature gate) pass, Warning checks (cluster readiness) can still block the transition
// unless --allow-transition-with-warnings is used.
func TestValidate_WarningFailures(t *testing.T) {
	// Create fake clients with:
	//   - Supported transition: SNO → HA (Error check passes)
	//   - Feature gate enabled (Error check passes)
	//   - Degraded cluster operator (Warning check fails)

	configClient := fakeconfigclient.NewSimpleClientset(
		&configv1.Infrastructure{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Status: configv1.InfrastructureStatus{
				ControlPlaneTopology: configv1.SingleReplicaTopologyMode,
			},
		},
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
		// Degraded cluster operator → Warning check fails
		newFakeClusterOperator("kube-apiserver", true, false, true),
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

	v := NewClientSideValidator(kubeClient, configClient, operatorClient)

	current := TopologyState{ControlPlane: configv1.SingleReplicaTopologyMode, Infrastructure: configv1.SingleReplicaTopologyMode}
	target := TopologyState{ControlPlane: configv1.HighlyAvailableTopologyMode, Infrastructure: configv1.HighlyAvailableTopologyMode}
	result, err := v.Validate(context.Background(), current, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify Error checks passed (should have 2: Supported Transition, Feature Gate)
	errorChecksPassed := 0
	for _, check := range result.Checks {
		if check.Severity == CheckSeverityError && check.Status == CheckStatusPassed {
			errorChecksPassed++
		}
	}
	if errorChecksPassed < 2 {
		t.Errorf("expected at least 2 Error checks to pass, got %d", errorChecksPassed)
	}

	// Verify the ClusterOperators check specifically failed (degraded operator)
	clusterOperatorsCheckFailed := false
	for _, check := range result.Checks {
		if check.Name == CheckNameClusterOperatorsStable && check.Severity == CheckSeverityWarning && check.Status == CheckStatusFailed {
			clusterOperatorsCheckFailed = true
			break
		}
	}
	if !clusterOperatorsCheckFailed {
		t.Error("expected ClusterOperators check to fail due to degraded operator")
	}

	// Verify overall status is Not Ready
	if result.Status != ValidationStatusNotReady {
		t.Errorf("expected status=Not Ready when Warning checks fail, got %s", result.Status)
	}
}

func TestValidate_AllWarningChecksFail(t *testing.T) {
	// Create a cluster where ALL Warning checks fail simultaneously:
	//   - Degraded cluster operator (ClusterOperators check)
	//   - Unschedulable nodes (ControlPlaneNodesSchedulable check)
	//   - Not ready nodes (ControlPlaneNodesReady check)
	//   - Progressing etcd (EtcdNotProgressing check)
	//   - Wrong voting member count (EtcdVotingMembers check)
	//
	// Error checks still pass (supported transition, feature gate enabled)
	// to verify that Warning failures are properly reported.

	configClient := fakeconfigclient.NewSimpleClientset(
		&configv1.Infrastructure{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Status: configv1.InfrastructureStatus{
				ControlPlaneTopology: configv1.SingleReplicaTopologyMode,
			},
		},
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
		// Degraded cluster operator
		newFakeClusterOperator("kube-apiserver", true, false, true),
	)

	kubeClient := fake.NewClientset(
		// Nodes that are both unschedulable AND not ready
		newFakeNode("master-0", true, false, false, false),
		newFakeNode("master-1", true, false, false, false),
		newFakeNode("master-2", true, false, false, false),
		// Wrong voting member count (2 instead of 3)
		newFakeEtcdConfigMap(2),
	)

	operatorClient := fakeoperatorclient.NewSimpleClientset(
		// Etcd has quorum (required for Error checks) but is progressing (Warning check fails)
		newFakeEtcdOperator(true, true),
	)

	v := NewClientSideValidator(kubeClient, configClient, operatorClient)

	current := TopologyState{ControlPlane: configv1.SingleReplicaTopologyMode, Infrastructure: configv1.SingleReplicaTopologyMode}
	target := TopologyState{ControlPlane: configv1.HighlyAvailableTopologyMode, Infrastructure: configv1.HighlyAvailableTopologyMode}
	result, err := v.Validate(context.Background(), current, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify Error checks passed (Supported Transition, Feature Gate)
	errorChecksPassed := 0
	for _, check := range result.Checks {
		if check.Severity == CheckSeverityError && check.Status == CheckStatusPassed {
			errorChecksPassed++
		}
	}
	if errorChecksPassed < 2 {
		t.Errorf("expected at least 2 Error checks to pass, got %d", errorChecksPassed)
	}

	// Count how many Warning checks failed
	warningChecksFailed := 0
	failedCheckNames := []string{}
	for _, check := range result.Checks {
		if check.Severity == CheckSeverityWarning && check.Status == CheckStatusFailed {
			warningChecksFailed++
			failedCheckNames = append(failedCheckNames, check.Name)
		}
	}

	// Should have at least 5 Warning checks fail:
	// ClusterOperators, NodesSchedulable, NodesReady, EtcdNotProgressing, EtcdVotingMembers
	if warningChecksFailed < 5 {
		t.Errorf("expected at least 5 Warning checks to fail, got %d: %v", warningChecksFailed, failedCheckNames)
	}

	// Verify overall status is Not Ready
	if result.Status != ValidationStatusNotReady {
		t.Errorf("expected status=Not Ready when all Warning checks fail, got %s", result.Status)
	}
}

// TestValidate_AllAPICallsFail tests error handling when API calls fail.
// Verifies that Error-severity checks returning Unknown cause short-circuit behavior.
// When Feature Gate check returns Unknown (API error), validation stops before Warning checks.
func TestValidate_AllAPICallsFail(t *testing.T) {
	// Create fake clients with reactors that return errors for all API calls
	kubeClient := fake.NewSimpleClientset()

	// Inject error for Node List calls (used by 4 node validators)
	kubeClient.PrependReactor("list", "nodes", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, fmt.Errorf("injected node list error")
	})

	// Inject error for ConfigMap Get calls (used by etcd voting members validator)
	kubeClient.PrependReactor("get", "configmaps", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, fmt.Errorf("injected configmap get error")
	})

	// Inject error for ClusterOperator List calls (used by cluster operators validator)
	configClient := fakeconfigclient.NewSimpleClientset()
	configClient.PrependReactor("list", "clusteroperators", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, fmt.Errorf("injected clusteroperator list error")
	})

	// Inject error for FeatureGate Get calls (used by feature gate validator)
	configClient.PrependReactor("get", "featuregates", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, fmt.Errorf("injected featuregate get error")
	})

	// Inject error for Etcd Get calls (used by 2 etcd validators)
	operatorClient := fakeoperatorclient.NewSimpleClientset()
	operatorClient.PrependReactor("get", "etcds", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, fmt.Errorf("injected etcd get error")
	})

	v := NewClientSideValidator(kubeClient, configClient, operatorClient)

	current := TopologyState{ControlPlane: configv1.SingleReplicaTopologyMode, Infrastructure: configv1.SingleReplicaTopologyMode}
	target := TopologyState{ControlPlane: configv1.HighlyAvailableTopologyMode, Infrastructure: configv1.HighlyAvailableTopologyMode}
	result, err := v.Validate(context.Background(), current, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify short-circuit behavior: only Error-severity checks run before short-circuit
	if len(result.Checks) != 2 {
		t.Errorf("expected 2 checks (Error-severity only, then short-circuit), got %d", len(result.Checks))
		for _, check := range result.Checks {
			t.Logf("  %s: %s (severity: %s)", check.Name, check.Status, check.Severity)
		}
	}

	// Supported Transition should pass (CEL-based, no API call required)
	if result.Checks[0].Name != CheckNameSupportedTransition || result.Checks[0].Status != CheckStatusPassed {
		t.Errorf("expected Supported Transition to pass, got %s: %s", result.Checks[0].Status, result.Checks[0].Message)
	}

	// Feature Gate Enabled should be Unknown (API error)
	if len(result.Checks) > 1 && (result.Checks[1].Name != CheckNameFeatureGateEnabled || result.Checks[1].Status != CheckStatusUnknown) {
		t.Errorf("expected Feature Gate Enabled to be Unknown, got %s: %s", result.Checks[1].Status, result.Checks[1].Message)
	}

	// Overall status should be Unknown (Error check returned Unknown, causing short-circuit)
	if result.Status != ValidationStatusUnknown {
		t.Errorf("expected status=Unknown when Error-severity API calls fail, got %s", result.Status)
	}
}
