package status

import (
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

	SortReleasesBySemanticVersions(actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("%v != %v", actual, expected)
	}
}
