package status

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	configv1 "github.com/openshift/api/config/v1"
)

func TestParseAlertDataToInsights(t *testing.T) {
	now := time.Now()

	// Define test cases
	tests := []struct {
		name          string
		alertData     AlertData
		expectedCount int
	}{
		{
			name: "Empty Alerts",
			alertData: AlertData{
				Data: Data{Alerts: []Alert{}},
			},
			expectedCount: 0,
		},
		{
			name: "Alert Active After Start Time",
			alertData: AlertData{
				Data: Data{
					Alerts: []Alert{
						{ActiveAt: now.Add(10 * time.Minute), Labels: AlertLabels{Severity: "critical", Namespace: "default", AlertName: "NodeDown"}, Annotations: AlertAnnotations{Summary: "Node is down"}},
						{ActiveAt: now.Add(-10 * time.Minute), Labels: AlertLabels{Severity: "warning", Namespace: "default", AlertName: "DiskSpaceLow"}, Annotations: AlertAnnotations{Summary: "Disk space low"}},
					},
				},
			},
			expectedCount: 1,
		},
		{
			name: "Alert Active Before Start Time",
			alertData: AlertData{
				Data: Data{
					Alerts: []Alert{
						{ActiveAt: now.Add(-10 * time.Minute), Labels: AlertLabels{Severity: "warning", Namespace: "default", AlertName: "DiskSpaceLow"}, Annotations: AlertAnnotations{Summary: "Disk space low"}},
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "Alert Active Before Start Time, Allowed",
			alertData: AlertData{
				Data: Data{
					Alerts: []Alert{
						{ActiveAt: now.Add(-20 * time.Minute), Labels: AlertLabels{Severity: "warning", Namespace: "default", AlertName: "PodDisruptionBudgetAtLimit"}, Annotations: AlertAnnotations{Summary: "PodDisruptionBudgetAtLimit is at limit"}},
						{ActiveAt: now.Add(-20 * time.Minute), Labels: AlertLabels{Severity: "warning", Namespace: "default", AlertName: "AlertmanagerReceiversNotConfigured"}, Annotations: AlertAnnotations{Summary: "Receivers (notification integrations) are not configured on Alertmanager"}},
					},
				},
			},
			expectedCount: 1,
		},
		{
			name: "Alert Active Before Start Time, Not Allowed",
			alertData: AlertData{
				Data: Data{
					Alerts: []Alert{
						{ActiveAt: now.Add(-20 * time.Minute), Labels: AlertLabels{Severity: "warning", Namespace: "default", AlertName: "AlertmanagerReceiversNotConfigured"}, Annotations: AlertAnnotations{Summary: "Receivers (notification integrations) are not configured on Alertmanager"}},
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "Info Alert Active After Start Time, Not Allowed",
			alertData: AlertData{
				Data: Data{
					Alerts: []Alert{
						{ActiveAt: now.Add(10 * time.Minute), Labels: AlertLabels{Severity: "info", Namespace: "default", AlertName: "NodeDown"}, Annotations: AlertAnnotations{Summary: "Node is down"}},
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "Info Alert Active After Start Time, Not Allowed",
			alertData: AlertData{
				Data: Data{
					Alerts: []Alert{
						{ActiveAt: now.Add(10 * time.Minute), Labels: AlertLabels{Severity: "info", Namespace: "default", AlertName: "NodeDown"}, Annotations: AlertAnnotations{Summary: "Node is down"}},
					},
				},
			},
			expectedCount: 0,
		},
	}

	// Execute test cases
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			insights := parseAlertDataToInsights(tt.alertData, now)
			if got := len(insights); got != tt.expectedCount {
				t.Errorf("parseAlertDataToInsights() = %v, want %v", got, tt.expectedCount)
			}
		})
	}
}

func TestParseAlertDataToInsightsWithData(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name             string
		alertData        AlertData
		startedAt        time.Time
		expectedInsights []updateInsight
	}{
		{
			name: "Alert Active After Start Time",
			alertData: AlertData{
				Data: Data{
					Alerts: []Alert{
						{ActiveAt: now.Add(10 * time.Minute), Labels: AlertLabels{Severity: "critical", Namespace: "default", AlertName: "NodeDown"}, Annotations: AlertAnnotations{Summary: "Node is down"}},
					},
				},
			},
			startedAt: now,
			expectedInsights: []updateInsight{
				{
					startedAt: now.Add(10 * time.Minute),
					impact: updateInsightImpact{
						level:       alertImpactLevel("critical"),
						impactType:  unknownImpactType,
						summary:     "Alert is firing: Node is down",
						description: "Alert started firing during the update. The alert has no description.",
					},
					remediation: updateInsightRemediation{reference: "<alert does not have a runbook_url annotation>"},
					scope: updateInsightScope{
						scopeType: scopeTypeCluster,
						resources: []scopeResource{
							{
								kind:      scopeGroupKind{group: configv1.GroupName, kind: "Alert"},
								namespace: "default",
								name:      "NodeDown",
							},
						},
					},
				},
			},
		},
		{
			name: "Alert Active Before Start Time",
			alertData: AlertData{
				Data: Data{
					Alerts: []Alert{
						{ActiveAt: now.Add(-10 * time.Minute), Labels: AlertLabels{Severity: "warning", Namespace: "default", AlertName: "DiskSpaceLow"}, Annotations: AlertAnnotations{Summary: "Disk space low"}},
					},
				},
			},
			startedAt:        now,
			expectedInsights: nil,
		},
	}

	// Execute test cases
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			insights := parseAlertDataToInsights(tt.alertData, tt.startedAt)
			if diff := cmp.Diff(tt.expectedInsights, insights, allowUnexportedInsightStructs); diff != "" {
				t.Errorf("parseAlertDataToInsights() differs from expected:\n%s", diff)
			}
		})
	}

}
