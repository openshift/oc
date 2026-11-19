package recommend

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSortConditionalUpdatesBySemanticVersions(t *testing.T) {
	expected := []configv1.ConditionalUpdate{
		{Release: configv1.Release{Version: "10.0.0"}},
		{Release: configv1.Release{Version: "2.0.10"}},
		{Release: configv1.Release{Version: "2.0.5"}},
		{Release: configv1.Release{Version: "2.0.1"}},
		{Release: configv1.Release{Version: "2.0.0"}},
		{Release: configv1.Release{Version: "not-sem-ver-2"}},
		{Release: configv1.Release{Version: "not-sem-ver-1"}},
	}

	actual := make([]configv1.ConditionalUpdate, len(expected))
	for i, j := range rand.Perm(len(expected)) {
		actual[i] = expected[j]
	}

	sortConditionalUpdatesBySemanticVersions(actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("%v != %v", actual, expected)
	}
}

func TestNotRecommendedCondition(t *testing.T) {
	tests := []struct {
		name     string
		update   configv1.ConditionalUpdate
		expected *metav1.Condition
	}{
		{
			name:   "no risks returns nil",
			update: configv1.ConditionalUpdate{},
		},
		{
			name: "risks without conditions produces correct message",
			update: configv1.ConditionalUpdate{
				Release: configv1.Release{Version: "4.16.5"},
				Risks:   []configv1.ConditionalUpdateRisk{{Name: "RiskB"}, {Name: "RiskA"}},
			},
			expected: &metav1.Condition{
				Type:    "Recommended",
				Status:  "Unknown",
				Reason:  "NoConditions",
				Message: "Conditional update to 4.16.5 has risks (RiskA, RiskB), but no conditions.",
			},
		},
		{
			name: "single risk correct",
			update: configv1.ConditionalUpdate{
				Release: configv1.Release{Version: "4.16.5"},
				Risks:   []configv1.ConditionalUpdateRisk{{Name: "TechPreviewNoUpgrade"}},
			},
			expected: &metav1.Condition{
				Type:    "Recommended",
				Status:  "Unknown",
				Reason:  "NoConditions",
				Message: "Conditional update to 4.16.5 has risks (TechPreviewNoUpgrade), but no conditions.",
			},
		},
		{
			name: "recommended true returns nil",
			update: configv1.ConditionalUpdate{
				Risks:      []configv1.ConditionalUpdateRisk{{Name: "R1"}},
				Conditions: []metav1.Condition{{Type: "Recommended", Status: metav1.ConditionTrue}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := notRecommendedCondition(tt.update)
			if diff := cmp.Diff(tt.expected, got); diff != "" {
				t.Errorf("unexpected result (-want +got):\n%s", diff)
			}
		})
	}
}
