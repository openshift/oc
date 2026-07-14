package preflight

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	operatorv1client "github.com/openshift/client-go/operator/clientset/versioned"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

// ClientSideValidator implements Validator interface using client-side validation.
// It duplicates CCO's preflight validation logic by directly querying the API server.
type ClientSideValidator struct {
	kubeClient     kubernetes.Interface
	configClient   configv1client.Interface
	operatorClient operatorv1client.Interface
}

// NewClientSideValidator creates a new client-side validator
func NewClientSideValidator(
	kubeClient kubernetes.Interface,
	configClient configv1client.Interface,
	operatorClient operatorv1client.Interface,
) *ClientSideValidator {
	return &ClientSideValidator{
		kubeClient:     kubeClient,
		configClient:   configClient,
		operatorClient: operatorClient,
	}
}

// Validate checks if transition from current to target topology is possible.
// Uses tiered validation approach:
// - Phase 1: Run ALL Error-severity checks (blocking issues: transition support, feature gates, version)
// - Phase 2: Run ALL Warning-severity checks (cluster readiness: nodes, operators, etcd)
// Short-circuits after Phase 1 if any Error checks fail to avoid wasting time on cluster state checks.
func (v *ClientSideValidator) Validate(ctx context.Context, current, target TopologyState) (*ValidationResult, error) {
	result := NewValidationResult(current, target)

	// ========================================================================
	// PHASE 1: Error-severity checks (blocking - cannot be bypassed)
	// ========================================================================
	// These checks determine if the transition is fundamentally supported.
	// Show ALL blocking issues to the user before short-circuiting.

	// Check 1: Is the control plane transition supported? (CEL-based validation)
	// This validates what the CCO will accept in spec.controlPlaneTopology
	result.AddCheck(v.validateSupportedTransition(ctx, current, target))

	// Check 2: Is MutableTopology feature gate enabled?
	result.AddCheck(v.validateFeatureGateEnabled(ctx))

	// Short-circuit if ANY Error-severity checks failed
	// Don't waste time checking cluster state if transition isn't even valid
	if result.HasErrorCheckFailures() {
		return result, nil
	}

	// ========================================================================
	// PHASE 2: Warning-severity checks (cluster readiness - bypassable)
	// ========================================================================
	// These checks validate cluster state is ready for the transition.
	// Can be bypassed with --allow-transition-with-warnings.

	result.AddCheck(v.validateClusterOperatorsStable(ctx))
	result.AddCheck(v.validateControlPlaneNodeCount(ctx, RequiredControlPlaneNodeCount))
	result.AddCheck(v.validateExactInfrastructureNodeCount(ctx, RequiredInfrastructureNodeCount))
	result.AddCheck(v.validateControlPlaneNodesSchedulable(ctx, RequiredControlPlaneNodeCount))
	result.AddCheck(v.validateControlPlaneNodesReady(ctx, RequiredControlPlaneNodeCount))
	result.AddCheck(v.validateEtcdQuorum(ctx))
	result.AddCheck(v.validateEtcdNotProgressing(ctx))
	result.AddCheck(v.validateEtcdVotingMembers(ctx, RequiredEtcdVotingMembers))

	return result, nil
}

// Helper functions for creating CheckResults with common patterns

// checkUnknown creates a CheckResult with Unknown status (API error)
func checkUnknown(name string, severity CheckSeverity, message string, err error) CheckResult {
	return CheckResult{
		Name:     name,
		Severity: severity,
		Status:   CheckStatusUnknown,
		Message:  fmt.Sprintf("%s: %v", message, err),
	}
}

// checkFailed creates a CheckResult with Failed status
func checkFailed(name string, severity CheckSeverity, message string) CheckResult {
	return CheckResult{
		Name:     name,
		Severity: severity,
		Status:   CheckStatusFailed,
		Message:  message,
	}
}

// checkPassed creates a CheckResult with Passed status
func checkPassed(name string, severity CheckSeverity) CheckResult {
	return CheckResult{
		Name:     name,
		Severity: severity,
		Status:   CheckStatusPassed,
	}
}

// listNodes fetches all nodes or returns an Unknown CheckResult on error
func (v *ClientSideValidator) listNodes(ctx context.Context, checkName string, severity CheckSeverity) (*corev1.NodeList, *CheckResult) {
	nodes, err := v.kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		result := checkUnknown(checkName, severity, "failed to list nodes", err)
		return nil, &result
	}
	return nodes, nil
}

// getEtcd fetches the Etcd operator CR or returns an Unknown CheckResult on error
func (v *ClientSideValidator) getEtcd(ctx context.Context, checkName string, severity CheckSeverity) (*operatorv1.Etcd, *CheckResult) {
	etcd, err := v.operatorClient.OperatorV1().Etcds().Get(ctx, EtcdOperatorResourceName, metav1.GetOptions{})
	if err != nil {
		result := checkUnknown(checkName, severity, "failed to get etcd operator CR", err)
		return nil, &result
	}
	return etcd, nil
}

// validateSupportedTransition checks if the control plane and infrastructure transition is supported.
// This validates what will be set in spec.controlPlaneTopology and the expected status changes.
//
// Constraint priority (most fundamental first):
// 1. Already at target (no-op check)
// 2. Current state must be SNO (only SNO → HA transitions are supported)
// 3. Control plane must be transitioning to HighlyAvailable (not SingleReplica, infrastructure cannot transition alone)
// 4. Infrastructure must also transition to HighlyAvailable (both topologies transition together)
//
// This ordering ensures that attempts to transition from HighlyAvailable state show the fundamental
// constraint ("only SNO → HA supported") rather than derivative constraints.
func (v *ClientSideValidator) validateSupportedTransition(ctx context.Context, current, target TopologyState) CheckResult {
	// Check if control plane is transitioning
	cpTransitioning := current.ControlPlane != target.ControlPlane
	infraTransitioning := current.Infrastructure != target.Infrastructure

	// Constraint 1: If nothing is transitioning, cluster is already at target
	if !cpTransitioning && !infraTransitioning {
		return checkFailed(CheckNameSupportedTransition, CheckSeverityError,
			fmt.Sprintf("cluster is already at target topology (control plane: %s, infrastructure: %s)",
				current.ControlPlane, current.Infrastructure))
	}

	// Constraint 2: Current state must be SingleReplica for both topologies
	// This is the fundamental requirement - we only support SNO → HA transitions
	if current.ControlPlane != configv1.SingleReplicaTopologyMode || current.Infrastructure != configv1.SingleReplicaTopologyMode {
		return checkFailed(CheckNameSupportedTransition, CheckSeverityError,
			fmt.Sprintf("only SingleReplica -> HighlyAvailable transitions are supported (current state: control plane %s, infrastructure %s)",
				current.ControlPlane, current.Infrastructure))
	}

	// At this point: current state is SNO, something is transitioning

	// Constraint 3: Control plane must be transitioning to HighlyAvailable
	if !cpTransitioning {
		// Infrastructure is transitioning but control plane is not
		return checkFailed(CheckNameSupportedTransition, CheckSeverityError,
			fmt.Sprintf("control plane must transition to HighlyAvailable (infrastructure cannot transition alone)"))
	}

	if target.ControlPlane != configv1.HighlyAvailableTopologyMode {
		// Control plane is transitioning but not to HighlyAvailable
		return checkFailed(CheckNameSupportedTransition, CheckSeverityError,
			fmt.Sprintf("control plane target must be HighlyAvailable, got %s (only SingleReplica -> HighlyAvailable is allowed)",
				target.ControlPlane))
	}

	// At this point: current is SNO, control plane is transitioning SNO → HA

	// Constraint 4: Infrastructure must also transition to HighlyAvailable
	if target.Infrastructure != configv1.HighlyAvailableTopologyMode {
		return checkFailed(CheckNameSupportedTransition, CheckSeverityError,
			fmt.Sprintf("infrastructure must transition to HighlyAvailable with control plane (infrastructure: %s -> %s, control plane: %s -> %s)",
				current.Infrastructure, target.Infrastructure, current.ControlPlane, target.ControlPlane))
	}

	// All constraints satisfied - this is a valid SNO → HA transition
	return checkPassed(CheckNameSupportedTransition, CheckSeverityError)
}

// validateFeatureGateEnabled checks that the MutableTopology feature gate is enabled.
// This is required for the Infrastructure.spec.controlPlaneTopology field to be mutable.
func (v *ClientSideValidator) validateFeatureGateEnabled(ctx context.Context) CheckResult {
	featureGate, err := v.configClient.ConfigV1().FeatureGates().Get(ctx, FeatureGateResourceName, metav1.GetOptions{})
	if err != nil {
		return checkUnknown(CheckNameFeatureGateEnabled, CheckSeverityError, "failed to get FeatureGate resource", err)
	}

	// Check if FeatureGates status has at least one entry
	if len(featureGate.Status.FeatureGates) == 0 {
		return CheckResult{
			Name:     CheckNameFeatureGateEnabled,
			Severity: CheckSeverityError,
			Status:   CheckStatusFailed,
			Message:  "FeatureGate resource has no status entries",
		}
	}

	// Check if MutableTopology is enabled in the active version (first entry).
	// FeatureGate.Status.FeatureGates is a listType=map with listMapKey=version.
	// The first entry ([0]) is guaranteed to be the cluster's current version by the
	// cluster-config-operator's featuregate controller (see syncFeatureGates in
	// pkg/operator/featuregates/featuregate_controller.go):
	//   "desiredFeatureGates will include first, the current version's feature gates,
	//    then all the historical featuregates in order..."
	// This pattern is used throughout oc (e.g., release/info.go in FeatureGateDiff).
	activeVersion := featureGate.Status.FeatureGates[0]
	for _, enabledFeature := range activeVersion.Enabled {
		if string(enabledFeature.Name) == MutableTopologyFeatureGate {
			return checkPassed(CheckNameFeatureGateEnabled, CheckSeverityError)
		}
	}

	// MutableTopology not found in enabled features for active version
	return CheckResult{
		Name:     CheckNameFeatureGateEnabled,
		Severity: CheckSeverityError,
		Status:   CheckStatusFailed,
		Message:  fmt.Sprintf("%s feature gate is not enabled (required for topology transitions)", MutableTopologyFeatureGate),
	}
}

// validateClusterOperatorsStable checks that all ClusterOperators are stable.
// Stable means: Available=True, Progressing=False, Degraded=False
// Matches CCO's checkClusterOperatorsStable implementation.
func (v *ClientSideValidator) validateClusterOperatorsStable(ctx context.Context) CheckResult {
	operators, err := v.configClient.ConfigV1().ClusterOperators().List(ctx, metav1.ListOptions{})
	if err != nil {
		return checkUnknown(CheckNameClusterOperatorsStable, CheckSeverityWarning, "failed to list cluster operators", err)
	}

	if len(operators.Items) == 0 {
		return CheckResult{
			Name:     CheckNameClusterOperatorsStable,
			Severity: CheckSeverityWarning,
			Status:   CheckStatusFailed,
			Message:  "no cluster operators found",
		}
	}

	var unstable []string
	for _, co := range operators.Items {
		var issues []string
		availableSeen, progressingSeen, degradedSeen := false, false, false

		for _, cond := range co.Status.Conditions {
			switch cond.Type {
			case ClusterOperatorAvailable:
				availableSeen = true
				if cond.Status != configv1.ConditionTrue {
					issues = append(issues, "Available="+string(cond.Status))
				}
			case ClusterOperatorProgressing:
				progressingSeen = true
				if cond.Status != configv1.ConditionFalse {
					issues = append(issues, "Progressing="+string(cond.Status))
				}
			case ClusterOperatorDegraded:
				degradedSeen = true
				if cond.Status != configv1.ConditionFalse {
					issues = append(issues, "Degraded="+string(cond.Status))
				}
			}
		}

		if !availableSeen {
			issues = append(issues, "Available condition missing")
		}
		if !progressingSeen {
			issues = append(issues, "Progressing condition missing")
		}
		if !degradedSeen {
			issues = append(issues, "Degraded condition missing")
		}

		if len(issues) > 0 {
			unstable = append(unstable, fmt.Sprintf("%s: %s", co.Name, strings.Join(issues, ", ")))
		}
	}

	if len(unstable) > 0 {
		return CheckResult{
			Name:     CheckNameClusterOperatorsStable,
			Severity: CheckSeverityWarning,
			Status:   CheckStatusFailed,
			Message:  strings.Join(unstable, "; "),
		}
	}

	return checkPassed(CheckNameClusterOperatorsStable, CheckSeverityWarning)
}

// validateControlPlaneNodeCount checks that at least the required number of control plane nodes exist.
// Control plane nodes are identified by node-role.kubernetes.io/control-plane or node-role.kubernetes.io/master labels.
func (v *ClientSideValidator) validateControlPlaneNodeCount(ctx context.Context, required int) CheckResult {
	nodes, checkResult := v.listNodes(ctx, CheckNameControlPlaneNodeCount, CheckSeverityWarning)
	if checkResult != nil {
		return *checkResult
	}

	count := 0
	for _, node := range nodes.Items {
		if isControlPlaneNode(&node) {
			count++
		}
	}

	if count < required {
		return CheckResult{
			Name:     CheckNameControlPlaneNodeCount,
			Severity: CheckSeverityWarning,
			Status:   CheckStatusFailed,
			Message:  fmt.Sprintf("need %d, have %d", required, count),
		}
	}

	return CheckResult{
		Name:     CheckNameControlPlaneNodeCount,
		Severity: CheckSeverityWarning,
		Status:   CheckStatusPassed,
	}
}

// validateExactInfrastructureNodeCount checks that exactly the required number of infrastructure nodes exist.
// Infrastructure nodes are nodes WITHOUT the control-plane role (i.e., worker nodes).
// For HA Compact topology, this should be 0 (no dedicated workers).
func (v *ClientSideValidator) validateExactInfrastructureNodeCount(ctx context.Context, required int) CheckResult {
	nodes, checkResult := v.listNodes(ctx, CheckNameInfrastructureNodeCount, CheckSeverityWarning)
	if checkResult != nil {
		return *checkResult
	}

	count := 0
	for _, node := range nodes.Items {
		if !isControlPlaneNode(&node) {
			count++
		}
	}

	if count != required {
		return CheckResult{
			Name:     CheckNameInfrastructureNodeCount,
			Severity: CheckSeverityWarning,
			Status:   CheckStatusFailed,
			Message:  fmt.Sprintf("need exactly %d, have %d (HighlyAvailable compact topology requires no dedicated worker nodes)", required, count),
		}
	}

	return CheckResult{
		Name:     CheckNameInfrastructureNodeCount,
		Severity: CheckSeverityWarning,
		Status:   CheckStatusPassed,
	}
}

// validateControlPlaneNodesSchedulable checks that the required number of control plane nodes are schedulable.
// Schedulable means the node does not have spec.Unschedulable=true and no NoSchedule taints.
func (v *ClientSideValidator) validateControlPlaneNodesSchedulable(ctx context.Context, required int) CheckResult {
	nodes, checkResult := v.listNodes(ctx, CheckNameControlPlaneNodesSchedulable, CheckSeverityWarning)
	if checkResult != nil {
		return *checkResult
	}

	schedulable := 0
	for _, node := range nodes.Items {
		if !isControlPlaneNode(&node) {
			continue
		}
		if !node.Spec.Unschedulable && !hasNoScheduleTaint(&node) {
			schedulable++
		}
	}

	if schedulable < required {
		return CheckResult{
			Name:     CheckNameControlPlaneNodesSchedulable,
			Severity: CheckSeverityWarning,
			Status:   CheckStatusFailed,
			Message:  fmt.Sprintf("need %d, have %d", required, schedulable),
		}
	}

	return CheckResult{
		Name:     CheckNameControlPlaneNodesSchedulable,
		Severity: CheckSeverityWarning,
		Status:   CheckStatusPassed,
	}
}

// validateControlPlaneNodesReady checks that the required number of control plane nodes are Ready.
// Ready means the node has a Ready=True condition.
func (v *ClientSideValidator) validateControlPlaneNodesReady(ctx context.Context, required int) CheckResult {
	nodes, checkResult := v.listNodes(ctx, CheckNameControlPlaneNodesReady, CheckSeverityWarning)
	if checkResult != nil {
		return *checkResult
	}

	ready := 0
	for _, node := range nodes.Items {
		if !isControlPlaneNode(&node) {
			continue
		}
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				ready++
				break
			}
		}
	}

	if ready < required {
		return CheckResult{
			Name:     CheckNameControlPlaneNodesReady,
			Severity: CheckSeverityWarning,
			Status:   CheckStatusFailed,
			Message:  fmt.Sprintf("need %d, have %d", required, ready),
		}
	}

	return CheckResult{
		Name:     CheckNameControlPlaneNodesReady,
		Severity: CheckSeverityWarning,
		Status:   CheckStatusPassed,
	}
}

// validateEtcdQuorum checks that etcd has quorum by checking the EtcdMembersAvailable condition.
func (v *ClientSideValidator) validateEtcdQuorum(ctx context.Context) CheckResult {
	etcd, checkResult := v.getEtcd(ctx, CheckNameEtcdQuorum, CheckSeverityWarning)
	if checkResult != nil {
		return *checkResult
	}

	if !v1helpers.IsOperatorConditionTrue(etcd.Status.Conditions, EtcdMembersAvailableCondition) {
		return CheckResult{
			Name:     CheckNameEtcdQuorum,
			Severity: CheckSeverityWarning,
			Status:   CheckStatusFailed,
			Message:  fmt.Sprintf("etcd does not have quorum (%s condition is not True)", EtcdMembersAvailableCondition),
		}
	}

	return CheckResult{
		Name:     CheckNameEtcdQuorum,
		Severity: CheckSeverityWarning,
		Status:   CheckStatusPassed,
	}
}

// validateEtcdNotProgressing checks that etcd is not currently scaling/progressing.
func (v *ClientSideValidator) validateEtcdNotProgressing(ctx context.Context) CheckResult {
	etcd, checkResult := v.getEtcd(ctx, CheckNameEtcdNotProgressing, CheckSeverityWarning)
	if checkResult != nil {
		return *checkResult
	}

	if v1helpers.IsOperatorConditionTrue(etcd.Status.Conditions, EtcdProgressingCondition) {
		return CheckResult{
			Name:     CheckNameEtcdNotProgressing,
			Severity: CheckSeverityWarning,
			Status:   CheckStatusFailed,
			Message:  "etcd is currently progressing (scaling in progress)",
		}
	}

	return CheckResult{
		Name:     CheckNameEtcdNotProgressing,
		Severity: CheckSeverityWarning,
		Status:   CheckStatusPassed,
	}
}

// validateEtcdVotingMembers checks that exactly the required number of etcd voting members exist.
// Reads the etcd-endpoints ConfigMap in openshift-etcd namespace.
// The number of keys in the ConfigMap Data equals the voting member count.
func (v *ClientSideValidator) validateEtcdVotingMembers(ctx context.Context, required int) CheckResult {
	cm, err := v.kubeClient.CoreV1().ConfigMaps(EtcdNamespace).Get(ctx, EtcdEndpointsConfigMapName, metav1.GetOptions{})
	if err != nil {
		return CheckResult{
			Name:     CheckNameEtcdVotingMembers,
			Severity: CheckSeverityWarning,
			Status:   CheckStatusUnknown,
			Message:  fmt.Sprintf("failed to get %s ConfigMap: %v", EtcdEndpointsConfigMapName, err),
		}
	}

	votingMembers := len(cm.Data)
	if votingMembers != required {
		return CheckResult{
			Name:     CheckNameEtcdVotingMembers,
			Severity: CheckSeverityWarning,
			Status:   CheckStatusFailed,
			Message:  fmt.Sprintf("need %d, have %d", required, votingMembers),
		}
	}

	return CheckResult{
		Name:     CheckNameEtcdVotingMembers,
		Severity: CheckSeverityWarning,
		Status:   CheckStatusPassed,
	}
}

// isControlPlaneNode returns true if the node has a control plane role label.
// Checks both node-role.kubernetes.io/control-plane and node-role.kubernetes.io/master (legacy).
func isControlPlaneNode(node *corev1.Node) bool {
	_, hasControlPlane := node.Labels[NodeRoleLabelControlPlane]
	_, hasMaster := node.Labels[NodeRoleLabelMaster]
	return hasControlPlane || hasMaster
}

// hasNoScheduleTaint returns true if the node has a NoSchedule taint.
func hasNoScheduleTaint(node *corev1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Effect == corev1.TaintEffectNoSchedule {
			return true
		}
	}
	return false
}
