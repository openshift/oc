package recommend

import (
	"math/rand"
	"reflect"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
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
