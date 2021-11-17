package upgrade

import (
	"errors"
	"math/rand"
	"reflect"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestSortReleasesBySemanticVersions(t *testing.T) {
	expected := []configv1.Release{
		{Version: "10.0.0"},
		{Version: "2.0.10"},
		{Version: "2.0.5"},
		{Version: "2.0.1"},
		{Version: "2.0.0"},
		{Version: "not-sem-ver-2"},
		{Version: "not-sem-ver-1"},
	}

	actual := make([]configv1.Release, len(expected))
	for i, j := range rand.Perm(len(expected)) {
		actual[i] = expected[j]
	}

	sortReleasesBySemanticVersions(actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("%v != %v", actual, expected)
	}
}

func TestCheckForUpgrade(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		conditions []configv1.ClusterOperatorStatusCondition
		expected   error
	}{
		{
			name: "no conditions",
		},
		{
			name: "invalid",
			conditions: []configv1.ClusterOperatorStatusCondition{{
				Type:    configv1.ClusterStatusConditionType("Invalid"),
				Status:  configv1.ConditionTrue,
				Reason:  "SomeReason",
				Message: "Some message.",
			}},
			expected: errors.New("the cluster version object is invalid, you must correct the invalid state first:\n\n  Reason: SomeReason\n  Message: Some message.\n\n"),
		},
		{
			name: "degraded and progressing",
			conditions: []configv1.ClusterOperatorStatusCondition{{
				Type:    configv1.OperatorProgressing,
				Status:  configv1.ConditionTrue,
				Reason:  "RollingOut",
				Message: "Updating to v2.",
			}, {
				Type:    configv1.OperatorDegraded,
				Status:  configv1.ConditionTrue,
				Reason:  "BadStuff",
				Message: "The widgets are slow.",
			}},
			expected: errors.New("the cluster is experiencing an upgrade-blocking error:\n\n  Reason: BadStuff\n  Message: The widgets are slow.\n\nthe cluster is already upgrading:\n\n  Reason: RollingOut\n  Message: Updating to v2.\n\n"),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			clusterVersion := &configv1.ClusterVersion{}
			clusterVersion.Status.Conditions = testCase.conditions
			actual := checkForUpgrade(clusterVersion)
			if !reflect.DeepEqual(actual, testCase.expected) {
				t.Errorf("%v != %v", actual, testCase.expected)
			}
		})
	}
}
