package status

import (
	"time"

	configv1 "github.com/openshift/api/config/v1"
)

// Alerts that will be included in the health upgrade evaluation, even if they were triggered before the upgrade began.
type AllowedAlerts map[string]struct{}

var allowedAlerts AllowedAlerts = map[string]struct{}{
	"PodDisruptionBudgetLimit":   {},
	"PodDisruptionBudgetAtLimit": {},
}

func (al AllowedAlerts) Contains(alert string) bool {
	_, exists := al[alert]
	return exists
}

type AlertLabels struct {
	AlertName string `json:"alertname,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Severity  string `json:"severity,omitempty"`
}

type AlertAnnotations struct {
	Description string `json:"description,omitempty"`
	Summary     string `json:"summary,omitempty"`
}

type Alert struct {
	Labels                  AlertLabels      `json:"labels,omitempty"`
	Annotations             AlertAnnotations `json:"annotations,omitempty"`
	State                   string           `json:"state,omitempty"`
	Value                   string           `json:"value,omitempty"`
	ActiveAt                time.Time        `json:"activeAt,omitempty"`
	PartialResponseStrategy string           `json:"partialResponseStrategy,omitempty"`
}

// Stores alert data returned by thanos
type AlertData struct {
	Status string `json:"status"`
	Data   Data   `json:"data"`
}

type Data struct {
	Alerts []Alert `json:"alerts"`
}

func parseAlertDataToInsights(alertData AlertData, startedAt time.Time) []updateInsight {
	var alerts []Alert = alertData.Data.Alerts
	var updateInsights []updateInsight = []updateInsight{}

	for _, alert := range alerts {
		if startedAt.After(alert.ActiveAt) && !allowedAlerts.Contains(alert.Labels.AlertName) {
			continue
		}
		updateInsights = append(updateInsights, updateInsight{
			startedAt: alert.ActiveAt,
			impact: updateInsightImpact{
				level:      alertImpactLevel(alert.Labels.Severity),
				impactType: unknownImpactType,
				summary:    "Alert: " + alert.Annotations.Summary,
			},
			scope: updateInsightScope{
				scopeType: scopeTypeCluster,
				resources: []scopeResource{{
					kind:      scopeGroupKind{group: configv1.GroupName, kind: "Alert"},
					namespace: alert.Labels.Namespace,
					name:      alert.Labels.AlertName,
				}},
			},
		})
	}
	return updateInsights
}

func alertImpactLevel(ail string) impactLevel {
	switch ail {
	case "warning":
		return warningImpactLevel
	case "critical":
		return criticalInfoLevel
	case "info":
		return infoImpactLevel
	default:
		return infoImpactLevel
	}
}
