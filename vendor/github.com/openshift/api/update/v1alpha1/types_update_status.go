package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// UpdateStatus reports status for in-progress cluster version updates
//
// Compatibility level 4: No compatibility is provided, the API can change at any point for any reason. These capabilities should not be used by applications needing long term support.
// +openshift:compatibility-gen:level=4
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:path=updatestatuses,scope=Cluster
// +openshift:api-approved.openshift.io=https://github.com/openshift/api/pull/2012
// +openshift:file-pattern=cvoRunLevel=0000_00,operatorName=cluster-version-operator,operatorOrdering=02
// +openshift:enable:FeatureGate=UpgradeStatus
// +kubebuilder:metadata:annotations="description=Provides health and status information about OpenShift cluster updates."
// +kubebuilder:metadata:annotations="displayName=UpdateStatuses"
type UpdateStatus struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec is empty for now, UpdateStatus is purely status-reporting API. In the future spec may be used to hold
	// configuration to drive what information is surfaced and how
	// +required
	Spec UpdateStatusSpec `json:"spec"`
	// status exposes the health and status of the ongoing cluster update
	// +optional
	Status UpdateStatusStatus `json:"status"`
}

// UpdateStatusSpec is empty for now, UpdateStatus is purely status-reporting API. In the future spec may be used
// to hold configuration to drive what information is surfaced and how
type UpdateStatusSpec struct {
}

// +k8s:deepcopy-gen=true

// UpdateStatusStatus is the API about in-progress updates. It aggregates and summarizes UpdateInsights produced by
// update informers
type UpdateStatusStatus struct {
	// conditions provide details about the controller operational matters
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	// +kubebuilder:validation:MaxItems=10
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// controlPlane contains a summary and insights related to the control plane update
	// +optional
	ControlPlane ControlPlane `json:"controlPlane"`

	// workerPools contains summaries and insights related to the worker pools update
	// +listType=map
	// +listMapKey=name
	// +patchStrategy=merge
	// +patchMergeKey=name
	// +optional
	// +kubebuilder:validation:MaxItems=32
	WorkerPools []Pool `json:"workerPools,omitempty" patchStrategy:"merge" patchMergeKey:"name"`
}

// ControlPlane contains a summary and insights related to the control plane update
type ControlPlane struct {
	// conditions provides details about the control plane update
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	// +kubebuilder:validation:MaxItems=10
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// resource is the resource that represents the control plane. It will typically be a ClusterVersion resource
	// in standalone OpenShift and HostedCluster in Hosted Control Planes.
	//
	// Note: By OpenShift API conventions, in isolation this should probably be a specialized reference type that allows
	// only the "correct" resource types to be referenced (here, ClusterVersion and HostedCluster). However, because we
	// use resource references in many places and this API is intended to be consumed by clients, not produced, consistency
	// seems to be more valuable than type safety for producers.
	// +required
	Resource ResourceRef `json:"resource"`

	// poolResource is the resource that represents control plane node pool, typically a MachineConfigPool. This field
	// is optional because some form factors (like Hosted Control Planes) do not have dedicated control plane node pools.
	//
	// Note: By OpenShift API conventions, in isolation this should probably be a specialized reference type that allows
	// only the "correct" resource types to be referenced (here, MachineConfigPool). However, because we use resource
	// references in many places and this API is intended to be consumed by clients, not produced, consistency seems to be
	// more valuable than type safety for producers.
	// +optional
	PoolResource *PoolResourceRef `json:"poolResource,omitempty"`

	// informers is a list of insight producers, each carries a list of insights relevant for control plane
	// +listType=map
	// +listMapKey=name
	// +patchStrategy=merge
	// +patchMergeKey=name
	// +optional
	// +kubebuilder:validation:MaxItems=16
	Informers []ControlPlaneInformer `json:"informers,omitempty" patchStrategy:"merge" patchMergeKey:"name"`
}

// ControlPlaneConditionType are types of conditions that can be reported on control plane level
type ControlPlaneConditionType string

const (
	// Updating is the condition type that communicate whether the whole control plane is updating or not
	ControlPlaneUpdating ControlPlaneConditionType = "Updating"
)

// ControlPlaneUpdatingReason are well-known reasons for the Updating condition
// +kubebuilder:validation:Enum=ClusterVersionProgressing;ClusterVersionNotProgressing;CannotDetermineUpdating
type ControlPlaneUpdatingReason string

const (
	// ClusterVersionProgressing is used for Updating=True set because we observed a ClusterVersion resource to
	// have Progressing=True condition
	ControlPlaneClusterVersionProgressing ControlPlaneUpdatingReason = "ClusterVersionProgressing"
	// ClusterVersionNotProgressing is used for Updating=False set because we observed a ClusterVersion resource to
	// have Progressing=False condition
	ControlPlaneClusterVersionNotProgressing ControlPlaneUpdatingReason = "ClusterVersionNotProgressing"
	// CannotDetermineUpdating is used with Updating=Unknown. This covers many different actual reasons such as
	// missing or Unknown Progressing condition on ClusterVersion, but it does not seem useful to track the individual
	// reasons to that granularity for Updating=Unknown
	ControlPlaneCannotDetermineUpdating ControlPlaneUpdatingReason = "CannotDetermineUpdating"
)

// Pool contains a summary and insights related to a node pool update
type Pool struct {
	// conditions provide details about the pool
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	// +kubebuilder:validation:MaxItems=10
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// name is the name of the pool
	// +required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// resource is the resource that represents the pool
	//
	// Note: By OpenShift API conventions, in isolation this should probably be a specialized reference type that allows
	// only the "correct" resource types to be referenced (here, MachineConfigPool or NodePool). However, because we use
	// resource references in many places and this API is intended to be consumed by clients, not produced, consistency
	// seems to be more valuable than type safety for producers.
	// +required
	Resource PoolResourceRef `json:"resource"`

	// informers is a list of insight producers, each carries a list of insights
	// +listType=map
	// +listMapKey=name
	// +patchStrategy=merge
	// +patchMergeKey=name
	// +optional
	// +kubebuilder:validation:MaxItems=16
	Informers []WorkerPoolInformer `json:"informers,omitempty" patchStrategy:"merge" patchMergeKey:"name"`
}

// ControlPlaneInformer is an insight producer identified by a name, carrying a list of insights it produced
type ControlPlaneInformer struct {
	// name is the name of the insight producer
	// +required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// insights is a list of insights produced by this producer
	// +listType=map
	// +listMapKey=uid
	// +patchStrategy=merge
	// +patchMergeKey=uid
	// +optional
	// +kubebuilder:validation:MaxItems=128
	Insights []ControlPlaneInsight `json:"insights,omitempty" patchStrategy:"merge" patchMergeKey:"uid"`
}

// WorkerPoolInformer is an insight producer identified by a name, carrying a list of insights it produced
type WorkerPoolInformer struct {
	// name is the name of the insight producer
	// +required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// insights is a list of insights produced by this producer
	// +listType=map
	// +listMapKey=uid
	// +patchStrategy=merge
	// +patchMergeKey=uid
	// +optional
	// +kubebuilder:validation:MaxItems=1024
	Insights []WorkerPoolInsight `json:"insights,omitempty" patchStrategy:"merge" patchMergeKey:"uid"`
}

// ControlPlaneInsight is a unique piece of either status/progress or update health information produced by update informer
type ControlPlaneInsight struct {
	// uid identifies the insight over time
	// +required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	UID string `json:"uid"`

	// acquiredAt is the time when the data was acquired by the producer
	// +required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	AcquiredAt metav1.Time `json:"acquiredAt"`

	ControlPlaneInsightUnion `json:",inline"`
}

// WorkerPoolInsight is a unique piece of either status/progress or update health information produced by update informer
type WorkerPoolInsight struct {
	// uid identifies the insight over time
	// +required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	UID string `json:"uid"`

	// acquiredAt is the time when the data was acquired by the producer
	// +required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	AcquiredAt metav1.Time `json:"acquiredAt"`

	WorkerPoolInsightUnion `json:",inline"`
}

// ControlPlaneInsightUnion is the discriminated union of all insights types that can be reported for the control plane,
// identified by type field
type ControlPlaneInsightUnion struct {
	// type identifies the type of the update insight
	// +unionDiscriminator
	// +required
	// +kubebuilder:validation:Enum=ClusterVersion;ClusterOperator;MachineConfigPool;Node;Health
	Type InsightType `json:"type"`

	// clusterVersion is a status insight about the state of a control plane update, where
	// the control plane is represented by a ClusterVersion resource usually managed by CVO
	// +optional
	// +unionMember
	ClusterVersionStatusInsight *ClusterVersionStatusInsight `json:"clusterVersion,omitempty"`

	// clusterOperator is a status insight about the state of a control plane cluster operator update
	// represented by a ClusterOperator resource
	// +optional
	// +unionMember
	ClusterOperatorStatusInsight *ClusterOperatorStatusInsight `json:"clusterOperator,omitempty"`

	// machineConfigPool is a status insight about the state of a worker pool update, where the worker pool
	// is represented by a MachineConfigPool resource
	// +optional
	// +unionMember
	MachineConfigPoolStatusInsight *MachineConfigPoolStatusInsight `json:"machineConfigPool,omitempty"`

	// node is a status insight about the state of a worker node update, where the worker node is represented
	// by a Node resource
	// +optional
	// +unionMember
	NodeStatusInsight *NodeStatusInsight `json:"node,omitempty"`

	// health is a generic health insight about the update. It does not represent a status of any specific
	// resource but surfaces actionable information about the health of the cluster or an update
	// +optional
	// +unionMember
	HealthInsight *HealthInsight `json:"health,omitempty"`
}

// WorkerPoolInsightUnion is the discriminated union of insights types that can be reported for a worker pool,
// identified by type field
type WorkerPoolInsightUnion struct {
	// type identifies the type of the update insight
	// +unionDiscriminator
	// +required
	// +kubebuilder:validation:Enum=MachineConfigPool;Node;Health
	Type InsightType `json:"type"`

	// machineConfigPool is a status insight about the state of a worker pool update, where the worker pool
	// is represented by a MachineConfigPool resource
	// +optional
	// +unionMember
	MachineConfigPoolStatusInsight *MachineConfigPoolStatusInsight `json:"machineConfigPool,omitempty"`

	// node is a status insight about the state of a worker node update, where the worker node is represented
	// by a Node resource
	// +optional
	// +unionMember
	NodeStatusInsight *NodeStatusInsight `json:"node,omitempty"`

	// health is a generic health insight about the update. It does not represent a status of any specific
	// resource but surfaces actionable information about the health of the cluster or an update
	// +optional
	// +unionMember
	HealthInsight *HealthInsight `json:"health,omitempty"`
}

// InsightType identifies the type of the update insight as either one of the resource-specific status insight,
// or a generic health insight
type InsightType string

const (
	// Resource-specific status insights should be reported continuously during the update process and mostly communicate
	// progress and high-level state

	// ClusterVersion status insight reports progress and high-level state of a ClusterVersion resource, representing
	// control plane in standalone clusters
	ClusterVersionStatusInsightType InsightType = "ClusterVersion"
	// ClusterOperator status insight reports progress and high-level state of a ClusterOperator, representing a control
	// plane component
	ClusterOperatorStatusInsightType InsightType = "ClusterOperator"
	// MachineConfigPool status insight reports progress and high-level state of a MachineConfigPool resource, representing
	// a pool of nodes in clusters using Machine API
	MachineConfigPoolStatusInsightType InsightType = "MachineConfigPool"
	// Node status insight reports progress and high-level state of a Node resource, representing a node (both control
	// plane and worker) in a cluster
	NodeStatusInsightType InsightType = "Node"

	// Health insights are reported only when an informer observes a condition that requires admin attention
	HealthInsightType InsightType = "Health"
)

// ResourceRef is a reference to a kubernetes resource, typically involved in an insight
type ResourceRef struct {
	// group of the object being referenced, if any
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=253
	Group string `json:"group,omitempty"`

	// resource of object being referenced
	// +required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Resource string `json:"resource"`

	// name of the object being referenced
	// +required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// namespace of the object being referenced, if any
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Namespace string `json:"namespace,omitempty"`
}

// PoolResourceRef is a reference to a kubernetes resource that represents a node pool
type PoolResourceRef struct {
	ResourceRef `json:",inline"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// UpdateStatusList is a list of UpdateStatus resources
//
// Compatibility level 4: No compatibility is provided, the API can change at any point for any reason. These capabilities should not be used by applications needing long term support.
// +openshift:compatibility-gen:level=4
type UpdateStatusList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata"`

	// items is a  list of UpdateStatus resources
	// +optional
	// +kubebuilder:validation:MaxItems=32
	Items []UpdateStatus `json:"items"`
}
