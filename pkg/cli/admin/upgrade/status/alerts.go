package status

import (
	"time"

	configv1 "github.com/openshift/api/config/v1"
)

// AllowedAlerts will be included in the health upgrade evaluation, even if they were triggered before the upgrade began.
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
	AlertName           string `json:"alertname,omitempty"`
	Name                string `json:"name,omitempty"`
	Namespace           string `json:"namespace,omitempty"`
	PodDisruptionBudget string `json:"poddisruptionbudget,omitempty"`
	Reason              string `json:"reason,omitempty"`
	Severity            string `json:"severity,omitempty"`
}

type AlertAnnotations struct {
	Description string `json:"description,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Runbook     string `json:"runbook_url,omitempty"`
	Message     string `json:"message,omitempty"`
}

type Alert struct {
	Labels                  AlertLabels      `json:"labels,omitempty"`
	Annotations             AlertAnnotations `json:"annotations,omitempty"`
	State                   string           `json:"state,omitempty"`
	Value                   string           `json:"value,omitempty"`
	ActiveAt                time.Time        `json:"activeAt,omitempty"`
	PartialResponseStrategy string           `json:"partialResponseStrategy,omitempty"`
}

// AlertData stores alert data returned by thanos
type AlertData struct {
	Status string `json:"status"`
	Data   Data   `json:"data"`
}

type Data struct {
	Alerts []Alert `json:"alerts"`
}

func parseAlertDataToInsights(alertData AlertData, startedAt time.Time) []updateInsight {
	var alerts = alertData.Data.Alerts
	var updateInsights []updateInsight

	for _, alert := range alerts {
		var alertName string
		if alertName = alert.Labels.AlertName; alertName == "" {
			continue
		}

		var description string
		startedDuringUpdate := startedAt.Before(alert.ActiveAt)
		affectsUpdates := allowedAlerts.Contains(alertName)

		if affectsUpdates {
			if startedDuringUpdate {
				description = "Alert known to affect updates started firing during the update."
			} else {
				description = "Alert known to affect updates has been firing since before the update started."
			}
		} else if startedDuringUpdate {
			description = "Alert started firing during the update."
		} else {
			// Do not show alerts that were firing before the update started unless they are on the allowlist
			continue
		}

		if alert.State == "pending" {
			continue
		}
		var level impactLevel
		if level = alertImpactLevel(alert.Labels.Severity); level < warningImpactLevel {
			continue
		}

		var runbook string
		if runbook = alert.Annotations.Runbook; runbook == "" {
			runbook = "<alert does not have a runbook_url annotation>"
		}

		switch {
		case alert.Annotations.Message != "" && alert.Annotations.Description != "":
			description += " The alert description is: " + alert.Annotations.Description + " | " + alert.Annotations.Message
		case alert.Annotations.Description != "":
			description += " The alert description is: " + alert.Annotations.Description
		case alert.Annotations.Message != "":
			description += " The alert description is: " + alert.Annotations.Message
		default:
			description += " The alert has no description."
		}

		var summary string
		if summary = alert.Annotations.Summary; summary == "" {
			summary = alertName
		}

		updateInsights = append(updateInsights, updateInsight{
			startedAt: alert.ActiveAt,
			impact: updateInsightImpact{
				level:       level,
				impactType:  unknownImpactType,
				summary:     "Alert is firing: " + summary,
				description: description,
			},
			remediation: updateInsightRemediation{reference: runbook},
			scope: updateInsightScope{
				scopeType: scopeTypeCluster,
				resources: []scopeResource{{
					kind:      scopeGroupKind{group: configv1.GroupName, kind: "Alert"},
					namespace: alert.Labels.Namespace,
					name:      alertName,
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
