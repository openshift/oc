package accept

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

func Test_getAcceptRisks(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		existing []configv1.AcceptRisk
		replace  bool
		clear    bool
		plus     sets.Set[string]
		minus    sets.Set[string]
		expected []configv1.AcceptRisk
	}{
		{
			name: "all zeros",
		},
		{
			name:     "riskA, riskB + riskB + riskC - riskA - riskD",
			existing: []configv1.AcceptRisk{{Name: "riskA"}, {Name: "riskB"}},
			plus:     sets.New[string]("riskB", "riskC"),
			minus:    sets.New[string]("riskA", "riskD"),
			expected: []configv1.AcceptRisk{
				{Name: "riskB"},
				{Name: "riskC"},
			},
		},
		{
			name:     "replace",
			existing: []configv1.AcceptRisk{{Name: "riskA"}, {Name: "riskB"}},
			plus:     sets.New[string]("riskB", "riskC"),
			minus:    sets.New[string]("does not matter"),
			replace:  true,
			expected: []configv1.AcceptRisk{
				{Name: "riskB"},
				{Name: "riskC"},
			},
		},
		{
			name:     "clear",
			existing: []configv1.AcceptRisk{{Name: "riskA"}, {Name: "riskB"}},
			plus:     sets.New[string]("not important"),
			minus:    sets.New[string]("does not matter"),
			clear:    true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			actual := getAcceptRisks(testCase.existing, testCase.replace, testCase.clear, testCase.plus, testCase.minus)
			if diff := cmp.Diff(actual, testCase.expected); diff != "" {
				t.Errorf("getAcceptRisks() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
