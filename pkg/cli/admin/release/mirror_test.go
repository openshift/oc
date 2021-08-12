package release

import (
	"reflect"
	"testing"

	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"

	"k8s.io/apimachinery/pkg/util/diff"
)

func Test_dedupeSortSources(t *testing.T) {
	tests := []struct {
		name            string
		sources         []operatorv1alpha1.RepositoryDigestMirrors
		expectedSources []operatorv1alpha1.RepositoryDigestMirrors
	}{
		{
			name: "single Source single Mirror",
			sources: []operatorv1alpha1.RepositoryDigestMirrors{
				{
					Source:  "quay/ocp/test",
					Mirrors: []string{"registry/ocp/test"},
				},
			},
			expectedSources: []operatorv1alpha1.RepositoryDigestMirrors{
				{
					Source:  "quay/ocp/test",
					Mirrors: []string{"registry/ocp/test"},
				},
			},
		},
		{
			name: "single Source multiple Mirrors",
			sources: []operatorv1alpha1.RepositoryDigestMirrors{
				{
					Source:  "quay/ocp/test",
					Mirrors: []string{"registry/ocp/test"},
				},
				{
					Source:  "quay/ocp/test",
					Mirrors: []string{"registry/another/test"},
				},
				{
					Source:  "quay/ocp/test",
					Mirrors: []string{"registry/ocp/test"},
				},
			},
			expectedSources: []operatorv1alpha1.RepositoryDigestMirrors{
				{
					Source:  "quay/ocp/test",
					Mirrors: []string{"registry/ocp/test", "registry/another/test"},
				},
			},
		},
		{
			name: "multiple Source single Mirrors",
			sources: []operatorv1alpha1.RepositoryDigestMirrors{
				{
					Source:  "quay/ocp/test",
					Mirrors: []string{"registry/ocp/test"},
				},
				{
					Source:  "quay/another/test",
					Mirrors: []string{"registry/ocp/test"},
				},
			},
			expectedSources: []operatorv1alpha1.RepositoryDigestMirrors{
				{
					Source:  "quay/another/test",
					Mirrors: []string{"registry/ocp/test"},
				},
				{
					Source:  "quay/ocp/test",
					Mirrors: []string{"registry/ocp/test"},
				},
			},
		},
		{
			name: "multiple Source multiple Mirrors",
			sources: []operatorv1alpha1.RepositoryDigestMirrors{
				{
					Source:  "quay/ocp/test",
					Mirrors: []string{"registry/ocp/test"},
				},
				{
					Source:  "quay/ocp/test",
					Mirrors: []string{"registry/another/test"},
				},
				{
					Source:  "quay/another/test",
					Mirrors: []string{"registry/ocp/test"},
				},
				{
					Source:  "quay/another/test",
					Mirrors: []string{"registry/another/test"},
				},
			},
			expectedSources: []operatorv1alpha1.RepositoryDigestMirrors{
				{
					Source:  "quay/another/test",
					Mirrors: []string{"registry/ocp/test", "registry/another/test"},
				},
				{
					Source:  "quay/ocp/test",
					Mirrors: []string{"registry/ocp/test", "registry/another/test"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uniqueSources := dedupeSortSources(tt.sources)
			if !reflect.DeepEqual(uniqueSources, tt.expectedSources) {
				t.Errorf("%s", diff.ObjectReflectDiff(uniqueSources, tt.expectedSources))
			}
		})
	}
}
