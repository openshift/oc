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
			level:      infoImpactLevel,
			impactType: noneImpactType,
			summary:    "Something with no impact that happened right now",
		},
	},
	{
		startedAt: now.Add(-10 * time.Second),
		impact: updateInsightImpact{
			level:      warningImpactLevel,
			impactType: updateSpeedImpactType,
			summary:    "Something that slows the update",
		},
	},
	{
		startedAt: now.Add(-20 * time.Second),
		impact: updateInsightImpact{
			level:      errorImpactLevel,
			impactType: clusterCapacityImpactType,
			summary:    "Something that limits cluster capacity",
		},
	},
	{
		startedAt: now.Add(-5 * time.Second),
		impact: updateInsightImpact{
			level:      errorImpactLevel,
			impactType: apiAvailabilityImpactType,
			summary:    "Something that broke API and happened recently",
		},
	},
	{
		startedAt: now.Add(-time.Hour),
		impact: updateInsightImpact{
			level:      criticalInfoLevel,
			impactType: dataLossImpactType,
			summary:    "KTHXBAI DATA",
		},
	},
}

func TestAssessUpdateInsights_Sorts(t *testing.T) {
	assessedAt := now
	healthData := assessUpdateInsights(insights, 2*time.Hour, assessedAt)
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
	if diff := cmp.Diff(expected, messages); diff != "" {
		t.Fatalf("Output differs from expected :\n%s", diff)
	}
}

func TestAssessUpdateInsights_NoInsightsCreatesAllIsWellInfo(t *testing.T) {
	assessedAt := now
	healthData := assessUpdateInsights(nil, 2*time.Hour, assessedAt)
	expected := updateHealthData{
		evaluatedAt: assessedAt,
		insights: []updateInsight{
			{
				startedAt: assessedAt.Add(-2 * time.Hour),
				impact: updateInsightImpact{
					level:      infoImpactLevel,
					impactType: noneImpactType,
					summary:    "Upgrade is proceeding well",
				},
			},
		},
	}

	if diff := cmp.Diff(expected, healthData, cmp.AllowUnexported(updateHealthData{}, updateInsight{}, updateInsightScope{}, updateInsightImpact{})); diff != "" {
		t.Fatalf("Output differs from expected :\n%s", diff)
	}
}

func TestUpdateHealthData_Write(t *testing.T) {
	var w strings.Builder
	healthData := assessUpdateInsights(insights, 2*time.Hour, now)
	if err := healthData.Write(&w); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	var expected = "= Update Health =\n" +
		"SINCE   LEVEL      IMPACT             MESSAGE\n" +
		"1h      Critical   Data Loss          KTHXBAI DATA\n" +
		"5s      Error      API Availability   Something that broke API and happened recently\n" +
		"20s     Error      Cluster Capacity   Something that limits cluster capacity\n" +
		"10s     Warning    Update Speed       Something that slows the update\n" +
		"0s      Info       None               Something with no impact that happened right now\n"

	if diff := cmp.Diff(expected, w.String()); diff != "" {
		t.Fatalf("Output differs from expected :\n%s", diff)
	}
}
