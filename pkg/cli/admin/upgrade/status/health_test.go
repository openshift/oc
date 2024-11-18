package status

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

var now = time.Now()

var insights = []updateInsight{
	{
		startedAt: now,
		impact: updateInsightImpact{
			level:       infoImpactLevel,
			impactType:  noneImpactType,
			summary:     "Something with no impact that happened right now",
			description: "This is a test",
		},
		remediation: updateInsightRemediation{reference: "https://docs.openshift.com"},
	},
	{
		startedAt: now.Add(-10 * time.Second),
		impact: updateInsightImpact{
			level:       warningImpactLevel,
			impactType:  updateSpeedImpactType,
			summary:     "Something that slows the update",
			description: "Your pathetic hardware is slowing OCP down",
		},
		remediation: updateInsightRemediation{reference: "https://cs.wikipedia.org/wiki/Hardware"},
	},
	{
		startedAt: now.Add(-20 * time.Second),
		impact: updateInsightImpact{
			level:       errorImpactLevel,
			impactType:  clusterCapacityImpactType,
			summary:     "Something that limits cluster capacity",
			description: "Autoscaler is disabled, you should enable it",
		},
		remediation: updateInsightRemediation{reference: "https://docs.openshift.com/container-platform/4.14/nodes/pods/nodes-pods-autoscaling.html"},
	},
	{
		startedAt: now.Add(-5 * time.Second),
		impact: updateInsightImpact{
			level:       errorImpactLevel,
			impactType:  apiAvailabilityImpactType,
			summary:     "Something that broke API and happened recently",
			description: "Only one auth replica is available",
		},
		remediation: updateInsightRemediation{reference: "https://docs.openshift.com/container-platform/4.14/authentication/understanding-authentication.html"},
	},
	{
		startedAt: now.Add(-time.Hour),
		impact: updateInsightImpact{
			level:       criticalInfoLevel,
			impactType:  dataLossImpactType,
			summary:     "KTHXBAI DATA",
			description: "You lost all your data, hope you have a backup",
		},
		remediation: updateInsightRemediation{reference: "https://en.wikipedia.org/wiki/Backup/"},
	},
}

func TestAssessUpdateInsights_Sorts(t *testing.T) {
	assessedAt := now
	healthData, allowDetailed := assessUpdateInsights(insights, 2*time.Hour, assessedAt)
	var messages []string
	for _, insights := range healthData.insights {
		messages = append(messages, insights.impact.summary)
	}
	var expected = []string{
		"KTHXBAI DATA",
		"Something that broke API and happened recently",
		"Something that limits cluster capacity",
		"Something that slows the update",
		"Something with no impact that happened right now",
	}

	if !allowDetailed {
		t.Error("Non-artificial insights should be allowed for detailed output")
	}

	if diff := cmp.Diff(expected, messages); diff != "" {
		t.Fatalf("Output differs from expected :\n%s", diff)
	}
}

func TestAssessUpdateInsights_NoInsightsCreatesAllIsWellInfo(t *testing.T) {
	assessedAt := now
	healthData, allowDetailed := assessUpdateInsights(nil, 2*time.Hour, assessedAt)
	expected := updateHealthData{
		evaluatedAt: assessedAt,
		insights: []updateInsight{
			{
				startedAt: assessedAt.Add(-2 * time.Hour),
				impact: updateInsightImpact{
					level:      infoImpactLevel,
					impactType: noneImpactType,
					summary:    "Update is proceeding well",
				},
			},
		},
	}

	if allowDetailed {
		t.Error("All is well insight should not be allowed for detailed output")
	}

	if diff := cmp.Diff(expected, healthData, allowUnexportedInsightStructs); diff != "" {
		t.Fatalf("Output differs from expected :\n%s", diff)
	}
}

func TestAssessUpdateInsights_FiltersOutIncompleteInsights(t *testing.T) {
	assessedAt := now
	var insights = []updateInsight{
		{
			startedAt: now.Add(-20 * time.Second),
			impact: updateInsightImpact{
				level:      errorImpactLevel,
				impactType: clusterCapacityImpactType,
				// empty summary
				summary:     "",
				description: "Autoscaler is disabled, you should enable it",
			},
			remediation: updateInsightRemediation{reference: "https://docs.openshift.com/container-platform/4.14/nodes/pods/nodes-pods-autoscaling.html"},
		},
		{
			startedAt: now.Add(-5 * time.Second),
			impact: updateInsightImpact{
				level:      errorImpactLevel,
				impactType: apiAvailabilityImpactType,
				summary:    "Something that broke API and happened recently",
				// empty description
				description: "",
			},
			remediation: updateInsightRemediation{reference: "https://docs.openshift.com/container-platform/4.14/authentication/understanding-authentication.html"},
		},
		{
			startedAt: now.Add(-time.Hour),
			impact: updateInsightImpact{
				level:       criticalInfoLevel,
				impactType:  dataLossImpactType,
				summary:     "KTHXBAI DATA",
				description: "You lost all your data, hope you have a backup",
			},
			// empty reference
			remediation: updateInsightRemediation{reference: ""},
		},
	}

	healthData, allowDetailed := assessUpdateInsights(insights, 2*time.Hour, assessedAt)
	// All incomplete insights are filtered out, so we only get the synthetic "All is well" insight
	expected := updateHealthData{
		evaluatedAt: assessedAt,
		insights: []updateInsight{
			{
				startedAt: assessedAt.Add(-2 * time.Hour),
				impact: updateInsightImpact{
					level:      infoImpactLevel,
					impactType: noneImpactType,
					summary:    "Update is proceeding well",
				},
			},
		},
	}

	if allowDetailed {
		t.Error("All is well insight should not be allowed for detailed output")
	}

	if diff := cmp.Diff(expected, healthData, cmp.AllowUnexported(updateHealthData{}, updateInsight{}, updateInsightScope{}, updateInsightImpact{}, updateInsightRemediation{})); diff != "" {
		t.Fatalf("Output differs from expected :\n%s", diff)
	}
}

func TestUpdateHealthData_Write(t *testing.T) {
	testCases := []struct {
		name     string
		detailed bool
		expected string
	}{
		{
			name:     "not detailed",
			detailed: false,
			expected: "= Update Health =\n" +
				"SINCE   LEVEL      IMPACT             MESSAGE\n" +
				"1h      Critical   Data Loss          KTHXBAI DATA\n" +
				"5s      Error      API Availability   Something that broke API and happened recently\n" +
				"20s     Error      Cluster Capacity   Something that limits cluster capacity\n" +
				"10s     Warning    Update Speed       Something that slows the update\n" +
				"now     Info       None               Something with no impact that happened right now\n\n" +
				"Run with --details=health for additional description and links to related online documentation\n",
		},
		{
			name:     "detailed",
			detailed: true,
			expected: "= Update Health =\n" +
				"Message: KTHXBAI DATA\n  Since:       1h\n  Level:       Critical\n  Impact:      Data Loss\n  Reference:   https://en.wikipedia.org/wiki/Backup/\n  Description: You lost all your data, hope you have a backup\n\n" +
				"Message: Something that broke API and happened recently\n  Since:       5s\n  Level:       Error\n  Impact:      API Availability\n  Reference:   https://docs.openshift.com/container-platform/4.14/authentication/understanding-authentication.html\n  Description: Only one auth replica is available\n\n" +
				"Message: Something that limits cluster capacity\n  Since:       20s\n  Level:       Error\n  Impact:      Cluster Capacity\n  Reference:   https://docs.openshift.com/container-platform/4.14/nodes/pods/nodes-pods-autoscaling.html\n  Description: Autoscaler is disabled, you should enable it\n\n" +
				"Message: Something that slows the update\n  Since:       10s\n  Level:       Warning\n  Impact:      Update Speed\n  Reference:   https://cs.wikipedia.org/wiki/Hardware\n  Description: Your pathetic hardware is slowing OCP down\n\n" +
				"Message: Something with no impact that happened right now\n  Since:       now\n  Level:       Info\n  Impact:      None\n  Reference:   https://docs.openshift.com\n  Description: This is a test\n",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var w strings.Builder
			healthData, _ := assessUpdateInsights(insights, 2*time.Hour, now)
			if err := healthData.Write(&w, tc.detailed); err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if diff := cmp.Diff(tc.expected, w.String()); diff != "" {
				t.Fatalf("Output differs from expected :\n%s", diff)
			}
		})
	}
}

func TestShortDuration(t *testing.T) {
	testCases := []struct {
		duration string
		expected string
	}{
		{
			duration: "1s",
			expected: "1s",
		},
		{
			duration: "1m",
			expected: "1m",
		},
		{
			duration: "1h",
			expected: "1h",
		}, {
			duration: "1h1m1s",
			expected: "1h1m1s",
		}, {
			duration: "1h10m",
			expected: "1h10m",
		}, {
			duration: "1h0m10s",
			expected: "1h0m10s",
		},
		{
			duration: "10h10m0s",
			expected: "10h10m",
		},
		{
			duration: "10h10m10s",
			expected: "10h10m10s",
		},
		{
			duration: "0h10m0s",
			expected: "10m",
		},
		{
			duration: "0h0m0s",
			expected: "now",
		},
		{
			duration: "45.000368975s",
			expected: "45s",
		},
		{
			duration: "2m0.000368975s",
			expected: "2m",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.duration, func(t *testing.T) {
			d, err := time.ParseDuration(tc.duration)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if diff := cmp.Diff(tc.expected, shortDuration(d)); diff != "" {
				t.Fatalf("Output differs from expected :\n%s", diff)
			}
		})
	}
}
