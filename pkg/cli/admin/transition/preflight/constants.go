package preflight

import (
	configv1 "github.com/openshift/api/config/v1"
)

// Topology transition requirements for HighlyAvailable Compact topology
const (
	// RequiredControlPlaneNodeCount is the minimum number of control plane nodes required for HA topology
	RequiredControlPlaneNodeCount = 3

	// RequiredInfrastructureNodeCount is the required number of infrastructure nodes for HA Compact topology
	// (must be 0 - no dedicated workers allowed)
	RequiredInfrastructureNodeCount = 0

	// RequiredEtcdVotingMembers is the required number of etcd voting members for HA topology
	RequiredEtcdVotingMembers = 3
)

// Kubernetes and OpenShift resource names
const (
	// InfrastructureResourceName is the name of the cluster-scoped Infrastructure resource
	InfrastructureResourceName = "cluster"

	// EtcdOperatorResourceName is the name of the cluster-scoped Etcd operator resource
	EtcdOperatorResourceName = "cluster"

	// FeatureGateResourceName is the name of the cluster-scoped FeatureGate resource
	FeatureGateResourceName = "cluster"

	// EtcdNamespace is the namespace where etcd resources are located
	EtcdNamespace = "openshift-etcd"

	// EtcdEndpointsConfigMapName is the name of the ConfigMap containing etcd endpoints
	// The number of keys in this ConfigMap equals the number of etcd voting members
	EtcdEndpointsConfigMapName = "etcd-endpoints"
)

// ClusterOperator condition types
const (
	// ClusterOperatorAvailable indicates the operand is available
	ClusterOperatorAvailable configv1.ClusterStatusConditionType = configv1.OperatorAvailable

	// ClusterOperatorProgressing indicates the operand is being updated
	ClusterOperatorProgressing configv1.ClusterStatusConditionType = configv1.OperatorProgressing

	// ClusterOperatorDegraded indicates the operand is degraded
	ClusterOperatorDegraded configv1.ClusterStatusConditionType = configv1.OperatorDegraded
)

// Etcd operator condition types
const (
	// EtcdMembersAvailableCondition indicates etcd has quorum
	EtcdMembersAvailableCondition = "EtcdMembersAvailable"

	// EtcdProgressingCondition indicates etcd is scaling/progressing
	EtcdProgressingCondition = "Progressing"
)

// Feature gate names
const (
	// MutableTopologyFeatureGate is the feature gate that enables topology transitions
	MutableTopologyFeatureGate = "MutableTopology"
)

// Check names (used in CheckResult.Name field)
const (
	CheckNameSupportedTransition          = "Supported Transition"
	CheckNameFeatureGateEnabled           = "Feature Gate Enabled"
	CheckNameClusterOperatorsStable       = "Cluster Operators Stable"
	CheckNameControlPlaneNodeCount        = "Control Plane Node Count"
	CheckNameInfrastructureNodeCount      = "Infrastructure Node Count"
	CheckNameControlPlaneNodesSchedulable = "Control Plane Nodes Schedulable"
	CheckNameControlPlaneNodesReady       = "Control Plane Nodes Ready"
	CheckNameEtcdQuorum                   = "etcd Quorum"
	CheckNameEtcdNotProgressing           = "etcd Not Progressing"
	CheckNameEtcdVotingMembers            = "etcd Voting Members"
)

// Node role labels
const (
	// NodeRoleLabelControlPlane is the label for control plane nodes
	NodeRoleLabelControlPlane = "node-role.kubernetes.io/control-plane"

	// NodeRoleLabelMaster is the legacy label for control plane nodes
	NodeRoleLabelMaster = "node-role.kubernetes.io/master"
)
