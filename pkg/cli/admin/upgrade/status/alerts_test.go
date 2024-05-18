package status

import (
	"reflect"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
)

func TestParseAlertDataToInsights(t *testing.T) {
	now := time.Now()

	// Define test cases
	tests := []struct {
		name          string
		alertData     AlertData
		startedAt     time.Time
		expectedCount int
	}{
		{
			name: "Empty Alerts",
			alertData: AlertData{
				Data: Data{Alerts: []Alert{}},
			},
			startedAt:     now,
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
			startedAt:     now,
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
			startedAt:     now,
			expectedCount: 0,
		},
		{
			name: "Alert Active Before Start Time, Allowed",
			alertData: AlertData{
				Data: Data{
					Alerts: []Alert{
						{ActiveAt: now.Add(-20 * time.Minute), Labels: AlertLabels{Severity: "info", Namespace: "default", AlertName: "PodDisruptionBudgetAtLimit"}, Annotations: AlertAnnotations{Summary: "PodDisruptionBudgetAtLimit is at limit"}},
						{ActiveAt: now.Add(-20 * time.Minute), Labels: AlertLabels{Severity: "info", Namespace: "default", AlertName: "AlertmanagerReceiversNotConfigured"}, Annotations: AlertAnnotations{Summary: "Receivers (notification integrations) are not configured on Alertmanager"}},
					},
				},
			},
			startedAt:     now,
			expectedCount: 1,
		},
		{
			name: "Alert Active Before Start Time, Not Allowed",
			alertData: AlertData{
				Data: Data{
					Alerts: []Alert{
						{ActiveAt: now.Add(-20 * time.Minute), Labels: AlertLabels{Severity: "info", Namespace: "default", AlertName: "AlertmanagerReceiversNotConfigured"}, Annotations: AlertAnnotations{Summary: "Receivers (notification integrations) are not configured on Alertmanager"}},
					},
				},
			},
			startedAt:     now,
			expectedCount: 0,
		},
	}

	// Execute test cases
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			insights := parseAlertDataToInsights(tt.alertData, tt.startedAt)
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
						level:      alertImpactLevel("critical"),
						impactType: unknownImpactType,
						summary:    "Alert: Node is down",
					},
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
			expectedInsights: []updateInsight{},
		},
	}

	// Execute test cases
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			insights := parseAlertDataToInsights(tt.alertData, tt.startedAt)
			if !reflect.DeepEqual(insights, tt.expectedInsights) {
				t.Errorf("parseAlertDataToInsights() got %#v, want %#v", insights, tt.expectedInsights)
			}
		})
	}

}
