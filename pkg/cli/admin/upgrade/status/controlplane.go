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
)

type operators struct {
	Total       int
	Available   int
	Progressing int
	Degraded    int
}

type controlPlaneStatusDisplayData struct {
	Assessment assessmentState
	Completion float64
	Duration   time.Duration
	Operators  operators
}

const (
	unavailableWarningThreshold = 5 * time.Minute
	unavailableErrorThreshold   = 20 * time.Minute
	degradedWarningThreshold    = 5 * time.Minute
	degradedErrorThreshold      = 40 * time.Minute
)

func coInsights(name string, available v1.ClusterOperatorStatusCondition, degraded v1.ClusterOperatorStatusCondition, evaluated time.Time) []updateInsight {
	var insights []updateInsight
	if available.Status == v1.ConditionFalse && evaluated.After(available.LastTransitionTime.Time.Add(unavailableWarningThreshold)) {
		insight := updateInsight{
			startedAt: available.LastTransitionTime.Time,
			scope:     updateInsightScope{scopeType: scopeTypeControlPlane, resources: []scopeResource{{kind: scopeKindClusterOperator, name: name}}},
			impact: updateInsightImpact{
				level:      warningImpactLevel,
				impactType: apiAvailabilityImpactType,
				summary:    fmt.Sprintf("Cluster Operator %s is unavailable | %s: %s", name, available.Reason, strings.ReplaceAll(available.Message, "\n", ` // `)),
			},
		}
		if evaluated.After(available.LastTransitionTime.Time.Add(unavailableErrorThreshold)) {
			insight.impact.level = errorImpactLevel
		}
		insights = append(insights, insight)
	}
	if degraded.Status == v1.ConditionTrue && evaluated.After(degraded.LastTransitionTime.Time.Add(degradedWarningThreshold)) {
		insight := updateInsight{
			startedAt: degraded.LastTransitionTime.Time,
			scope:     updateInsightScope{scopeType: scopeTypeControlPlane, resources: []scopeResource{{kind: scopeKindClusterOperator, name: name}}},
			impact: updateInsightImpact{
				level:      warningImpactLevel,
				impactType: apiAvailabilityImpactType,
				summary:    fmt.Sprintf("Cluster Operator %s is degraded | %s: %s", name, degraded.Reason, strings.ReplaceAll(degraded.Message, "\n", ` // `)),
			},
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

		var updated bool
		for _, version := range operator.Status.Versions {
			if version.Name == "operator" && version.Version == targetVersion {
				updated = true
				completed++
				break
			}
		}
		available := v1.ClusterOperatorStatusCondition{}
		progressing := v1.ClusterOperatorStatusCondition{}
		degraded := v1.ClusterOperatorStatusCondition{}

		displayData.Operators.Total++
		for _, condition := range operator.Status.Conditions {
			switch {
			case condition.Type == v1.OperatorAvailable:
				available = condition
			case condition.Type == v1.OperatorProgressing:
				progressing = condition
			case condition.Type == v1.OperatorDegraded:
				degraded = condition
			}
		}

		if available.Status == v1.ConditionTrue {
			displayData.Operators.Available++
		}
		if degraded.Status == v1.ConditionTrue {
			displayData.Operators.Degraded++
		}
		if progressing.Status == v1.ConditionTrue && !updated {
			displayData.Operators.Progressing++
		}
		insights = append(insights, coInsights(operator.Name, available, degraded, at)...)
	}

	if completed == displayData.Operators.Total {
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

	displayData.Completion = float64(completed) / float64(displayData.Operators.Total) * 100.0
	return displayData, insights
}

var controlPlaneStatusTemplate = template.Must(template.New("controlPlaneStatus").Parse(controlPlaneStatusTemplateRaw))

func (d *controlPlaneStatusDisplayData) Write(f io.Writer) error {
	return controlPlaneStatusTemplate.Execute(f, d)
}

const controlPlaneStatusTemplateRaw = `= Control Plane =
Assessment:      {{ .Assessment }}
Completion:      {{ printf "%.0f" .Completion }}%
Duration:        {{ .Duration }}
Operator Status: {{ .Operators.Total }} Total, {{ .Operators.Available }} Available, {{ .Operators.Progressing }} Progressing, {{ .Operators.Degraded }} Degraded
`
