package status

import (
	"fmt"
	"io"
	"strings"
	"text/template"
	"time"

	v1 "github.com/openshift/api/config/v1"
)

type assessmentState string

const (
	assessmentStateProgressing assessmentState = "Progressing"
	assessmentStateCompleted   assessmentState = "Completed"
	assessmentStatePending     assessmentState = "Pending"
	assessmentStateExcluded    assessmentState = "Excluded"
	assessmentStateDegraded    assessmentState = "Degraded"

	// clusterStatusFailing is set on the ClusterVersion status when a cluster
	// cannot reach the desired state.
	clusterStatusFailing = v1.ClusterStatusConditionType("Failing")
)

type operators struct {
	Total       int
	Unavailable int
	// Degraded is the count of operators that are available but degraded
	Degraded int
}

func (o operators) StatusSummary() string {
	res := []string{fmt.Sprintf("%d Healthy", o.Total-o.Unavailable-o.Degraded)}
	if o.Unavailable > 0 {
		res = append(res, fmt.Sprintf("%d Unavailable", o.Unavailable))
	}
	if o.Degraded > 0 {
		res = append(res, fmt.Sprintf("%d Available but degraded", o.Degraded))
	}
	return strings.Join(res, ", ")
}

type versions struct {
	target            string
	previous          string
	isTargetInstall   bool
	isPreviousPartial bool
}

func (v versions) String() string {
	if v.isTargetInstall {
		return fmt.Sprintf("%s (a new install)", v.target)
	}
	if v.isPreviousPartial {
		return fmt.Sprintf("%s (from incomplete %s)", v.target, v.previous)
	}
	return fmt.Sprintf("%s (from %s)", v.target, v.previous)
}

type controlPlaneStatusDisplayData struct {
	Assessment    assessmentState
	Completion    float64
	Duration      time.Duration
	Operators     operators
	TargetVersion versions
}

const (
	unavailableWarningThreshold = 5 * time.Minute
	unavailableErrorThreshold   = 20 * time.Minute
	degradedWarningThreshold    = 5 * time.Minute
	degradedErrorThreshold      = 40 * time.Minute
)

func coInsights(name string, available *v1.ClusterOperatorStatusCondition, degraded *v1.ClusterOperatorStatusCondition, evaluated time.Time) []updateInsight {
	coGroupKind := scopeGroupKind{group: v1.GroupName, kind: "ClusterOperator"}
	var insights []updateInsight
	if available != nil && available.Status == v1.ConditionFalse && evaluated.After(available.LastTransitionTime.Time.Add(unavailableWarningThreshold)) {
		insight := updateInsight{
			startedAt: available.LastTransitionTime.Time,
			scope:     updateInsightScope{scopeType: scopeTypeControlPlane, resources: []scopeResource{{kind: coGroupKind, name: name}}},
			impact: updateInsightImpact{
				level:       warningImpactLevel,
				impactType:  apiAvailabilityImpactType,
				summary:     fmt.Sprintf("Cluster Operator %s is unavailable (%s)", name, available.Reason),
				description: available.Message,
			},
			remediation: updateInsightRemediation{reference: "https://github.com/openshift/runbooks/blob/master/alerts/cluster-monitoring-operator/ClusterOperatorDown.md"},
		}
		if available.Message == "" {
			// Backfill the description if CO doesn't provide one
			insight.impact.description = "<no message>"
		}
		if evaluated.After(available.LastTransitionTime.Time.Add(unavailableErrorThreshold)) {
			insight.impact.level = errorImpactLevel
		}
		insights = append(insights, insight)
	}
	if degraded != nil && degraded.Status == v1.ConditionTrue && evaluated.After(degraded.LastTransitionTime.Time.Add(degradedWarningThreshold)) {
		insight := updateInsight{
			startedAt: degraded.LastTransitionTime.Time,
			scope:     updateInsightScope{scopeType: scopeTypeControlPlane, resources: []scopeResource{{kind: coGroupKind, name: name}}},
			impact: updateInsightImpact{
				level:       warningImpactLevel,
				impactType:  apiAvailabilityImpactType,
				summary:     fmt.Sprintf("Cluster Operator %s is degraded (%s)", name, degraded.Reason),
				description: degraded.Message,
			},
			remediation: updateInsightRemediation{reference: "https://github.com/openshift/runbooks/blob/master/alerts/cluster-monitoring-operator/ClusterOperatorDegraded.md"},
		}
		if degraded.Message == "" {
			// Backfill the description if CO doesn't provide one
			insight.impact.description = "<no message>"
		}
		if evaluated.After(degraded.LastTransitionTime.Time.Add(degradedErrorThreshold)) {
			insight.impact.level = errorImpactLevel
		}
		insights = append(insights, insight)
	}
	return insights
}

func assessControlPlaneStatus(cv *v1.ClusterVersion, operators []v1.ClusterOperator, at time.Time) (controlPlaneStatusDisplayData, []updateInsight) {
	var displayData controlPlaneStatusDisplayData
	var completed int
	var insights []updateInsight

	targetVersion := cv.Status.Desired.Version
	cvGvk := cv.GroupVersionKind()
	cvGroupKind := scopeGroupKind{group: cvGvk.Group, kind: cvGvk.Kind}
	cvScope := scopeResource{kind: cvGroupKind, name: cv.Name}

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, clusterStatusFailing); c == nil {
		insight := updateInsight{
			startedAt: at,
			scope:     updateInsightScope{scopeType: scopeTypeControlPlane, resources: []scopeResource{cvScope}},
			impact: updateInsightImpact{
				level:       warningImpactLevel,
				impactType:  updateStalledImpactType,
				summary:     fmt.Sprintf("Cluster Version %s has no %s condition", cv.Name, clusterStatusFailing),
				description: "Current status of Cluster Version reconciliation is unclear.  See 'oc -n openshift-cluster-version logs -l k8s-app=cluster-version-operator --tail -1' to debug.",
			},
			remediation: updateInsightRemediation{reference: "https://github.com/openshift/runbooks/blob/master/alerts/cluster-monitoring-operator/ClusterOperatorDegraded.md"},
		}
		insights = append(insights, insight)
	} else if c.Status != v1.ConditionFalse {
		insight := updateInsight{
			startedAt: c.LastTransitionTime.Time,
			scope:     updateInsightScope{scopeType: scopeTypeControlPlane, resources: []scopeResource{{kind: cvGroupKind, name: cv.Name}}},
			impact: updateInsightImpact{
				level:       warningImpactLevel,
				impactType:  updateStalledImpactType,
				summary:     fmt.Sprintf("Cluster Version %s is failing to proceed with the update (%s)", cv.Name, c.Reason),
				description: c.Message,
			},
			remediation: updateInsightRemediation{reference: "https://github.com/openshift/runbooks/blob/master/alerts/cluster-monitoring-operator/ClusterOperatorDegraded.md"},
		}
		insights = append(insights, insight)
	}

	for _, operator := range operators {
		var isPlatformOperator bool
		for annotation := range operator.Annotations {
			if strings.HasPrefix(annotation, "exclude.release.openshift.io/") ||
				strings.HasPrefix(annotation, "include.release.openshift.io/") {
				isPlatformOperator = true
				break
			}
		}
		if !isPlatformOperator {
			continue
		}

		for _, version := range operator.Status.Versions {
			if version.Name == "operator" && version.Version == targetVersion {
				completed++
				break
			}
		}
		var available *v1.ClusterOperatorStatusCondition
		var degraded *v1.ClusterOperatorStatusCondition

		displayData.Operators.Total++
		for _, condition := range operator.Status.Conditions {
			condition := condition
			switch {
			case condition.Type == v1.OperatorAvailable:
				available = &condition
			case condition.Type == v1.OperatorDegraded:
				degraded = &condition
			}
		}

		if available == nil || available.Status != v1.ConditionTrue {
			displayData.Operators.Unavailable++
		} else if degraded != nil && degraded.Status == v1.ConditionTrue {
			displayData.Operators.Degraded++
		}
		insights = append(insights, coInsights(operator.Name, available, degraded, at)...)
	}

	controlPlaneCompleted := completed == displayData.Operators.Total
	if controlPlaneCompleted {
		displayData.Assessment = assessmentStateCompleted
	} else {
		displayData.Assessment = assessmentStateProgressing
	}

	if len(cv.Status.History) > 0 {
		currentHistoryItem := cv.Status.History[0]
		if currentHistoryItem.State == v1.CompletedUpdate {
			displayData.Duration = currentHistoryItem.CompletionTime.Time.Sub(currentHistoryItem.StartedTime.Time)
		} else {
			displayData.Duration = at.Sub(currentHistoryItem.StartedTime.Time)
		}
	}

	versionData, versionInsights := versionsFromHistory(cv.Status.History, cvScope, controlPlaneCompleted)
	displayData.TargetVersion = versionData
	insights = append(insights, versionInsights...)

	displayData.Completion = float64(completed) / float64(displayData.Operators.Total) * 100.0
	return displayData, insights
}

func versionsFromHistory(history []v1.UpdateHistory, cvScope scopeResource, controlPlaneCompleted bool) (versions, []updateInsight) {
	versionData := versions{
		target:   "unknown",
		previous: "unknown",
	}
	if len(history) > 0 {
		versionData.target = history[0].Version
	}
	if len(history) == 1 {
		versionData.isTargetInstall = true
		return versionData, nil
	}
	if len(history) > 1 {
		versionData.previous = history[1].Version
		versionData.isPreviousPartial = history[1].State == v1.PartialUpdate
	}

	var insights []updateInsight
	if !controlPlaneCompleted && versionData.isPreviousPartial {
		lastComplete := "unknown"
		if len(history) > 2 {
			for _, item := range history[2:] {
				if item.State == v1.CompletedUpdate {
					lastComplete = item.Version
					break
				}
			}
		}
		insights = []updateInsight{
			{
				startedAt: history[0].StartedTime.Time,
				scope: updateInsightScope{
					scopeType: scopeTypeControlPlane,
					resources: []scopeResource{cvScope},
				},
				impact: updateInsightImpact{
					level:       warningImpactLevel,
					impactType:  noneImpactType,
					summary:     fmt.Sprintf("Previous update to %s never completed, last complete update was %s", versionData.previous, lastComplete),
					description: fmt.Sprintf("Current update to %s was initiated while the previous update to version %s was still in progress", versionData.target, versionData.previous),
				},
				remediation: updateInsightRemediation{
					reference: "https://docs.openshift.com/container-platform/latest/updating/troubleshooting_updates/gathering-data-cluster-update.html#gathering-clusterversion-history-cli_troubleshooting_updates",
				},
			},
		}
	}

	return versionData, insights
}

var controlPlaneStatusTemplate = template.Must(template.New("controlPlaneStatus").Parse(controlPlaneStatusTemplateRaw))

func (d *controlPlaneStatusDisplayData) Write(f io.Writer) error {
	return controlPlaneStatusTemplate.Execute(f, d)
}

const controlPlaneStatusTemplateRaw = `= Control Plane =
Assessment:      {{ .Assessment }}
Target Version:  {{ .TargetVersion }}
Completion:      {{ printf "%.0f" .Completion }}%
Duration:        {{ .Duration }}
Operator Status: {{ .Operators.StatusSummary }}
`
