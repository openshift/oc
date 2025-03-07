package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// HealthInsight is a piece of actionable information produced by an insight producer about the health
// of the cluster or an update
type HealthInsight struct {
	// startedAt is the time when the condition reported by the insight started
	// +required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	StartedAt metav1.Time `json:"startedAt"`

	// scope is list of objects involved in the insight
	// +required
	Scope InsightScope `json:"scope"`

	// impact describes the impact the reported condition has on the cluster or update
	// +required
	Impact InsightImpact `json:"impact"`

	// remediation contains information about how to resolve or prevent the reported condition
	// +required
	Remediation InsightRemediation `json:"remediation"`
}

// InsightScope is a list of resources involved in the insight
type InsightScope struct {
	// type is either ControlPlane or WorkerPool
	// +required
	Type ScopeType `json:"type"`

	// resources is a list of resources involved in the insight, of any group/kind
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=16
	Resources []ResourceRef `json:"resources,omitempty"`
}

// ScopeType is one of ControlPlane or WorkerPool
// +kubebuilder:validation:Enum=ControlPlane;WorkerPool
type ScopeType string

const (
	// ControlPlane is used for insights that are related to the control plane (including control plane pool or nodes)
	ControlPlaneScope ScopeType = "ControlPlane"
	// WorkerPool is used for insights that are related to a worker pools and nodes (excluding control plane)
	WorkerPoolScope ScopeType = "WorkerPool"
)

// InsightImpact describes the impact the reported condition has on the cluster or update
type InsightImpact struct {
	// level is the severity of the impact
	// +required
	Level InsightImpactLevel `json:"level"`

	// type is the type of the impact
	// +required
	Type InsightImpactType `json:"type"`

	// summary is a short summary of the impact
	// +required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:MinLength=1
	Summary string `json:"summary"`

	// description is a human-oriented, possibly longer-form description of the condition reported by the insight
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=4096
	Description string `json:"description,omitempty"`
}

// InsightImpactLevel describes the severity of the impact the reported condition has on the cluster or update
// +kubebuilder:validation:Enum=Unknown;Info;Warning;Error;Critical
type InsightImpactLevel string

const (
	// UnknownImpactLevel is used when the impact level is not known
	UnknownImpactLevel InsightImpactLevel = "Unknown"
	// info should be used for insights that are strictly informational or even positive (things go well or
	// something recently healed)
	InfoImpactLevel InsightImpactLevel = "Info"
	// warning should be used for insights that explain a minor or transient problem. Anything that requires
	// admin attention or manual action should not be a warning but at least an error.
	WarningImpactLevel InsightImpactLevel = "Warning"
	// error should be used for insights that inform about a problem that requires admin attention. Insights of
	// level error and higher should be as actionable as possible, and should be accompanied by links to documentation,
	// KB articles or other resources that help the admin to resolve the problem.
	ErrorImpactLevel InsightImpactLevel = "Error"
	// critical should be used rarely, for insights that inform about a severe problem, threatening with data
	// loss, destroyed cluster or other catastrophic consequences. Insights of this level should be accompanied by
	// links to documentation, KB articles or other resources that help the admin to resolve the problem, or at least
	// prevent the severe consequences from happening.
	CriticalInfoLevel InsightImpactLevel = "Critical"
)

// InsightImpactType describes the type of the impact the reported condition has on the cluster or update
// +kubebuilder:validation:Enum=None;Unknown;API Availability;Cluster Capacity;Application Availability;Application Outage;Data Loss;Update Speed;Update Stalled
type InsightImpactType string

const (
	NoneImpactType                    InsightImpactType = "None"
	UnknownImpactType                 InsightImpactType = "Unknown"
	ApiAvailabilityImpactType         InsightImpactType = "API Availability"
	ClusterCapacityImpactType         InsightImpactType = "Cluster Capacity"
	ApplicationAvailabilityImpactType InsightImpactType = "Application Availability"
	ApplicationOutageImpactType       InsightImpactType = "Application Outage"
	DataLossImpactType                InsightImpactType = "Data Loss"
	UpdateSpeedImpactType             InsightImpactType = "Update Speed"
	UpdateStalledImpactType           InsightImpactType = "Update Stalled"
)

// InsightRemediation contains information about how to resolve or prevent the reported condition
type InsightRemediation struct {
	// reference is a URL where administrators can find information to resolve or prevent the reported condition
	// +required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=uri
	// +kubebuilder:validation:MaxLength=512
	Reference string `json:"reference"`

	// estimatedFinish is the estimated time when the informer expects the condition to be resolved, if applicable.
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	EstimatedFinish *metav1.Time `json:"estimatedFinish,omitempty"`
}
