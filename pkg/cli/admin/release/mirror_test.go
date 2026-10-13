package release

import (
	"bytes"
	"reflect"
	"testing"

	apicfgv1 "github.com/openshift/api/config/v1"

	"k8s.io/apimachinery/pkg/util/diff"
)

func Test_dedupeSortSources(t *testing.T) {
	tests := []struct {
		name            string
		sources         []mirrorSet
		expectedSources []mirrorSet
	}{
		{
			name: "single Source single Mirror",
			sources: []mirrorSet{
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test"},
				},
			},
			expectedSources: []mirrorSet{
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test"},
				},
			},
		},
		{
			name: "single Source multiple Mirrors",
			sources: []mirrorSet{
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test"},
				},
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/another/test"},
				},
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test"},
				},
			},
			expectedSources: []mirrorSet{
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test", "registry/another/test"},
				},
			},
		},
		{
			name: "multiple Source single Mirrors",
			sources: []mirrorSet{
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test"},
				},
				{
					source:  "quay/another/test",
					mirrors: []string{"registry/ocp/test"},
				},
			},
			expectedSources: []mirrorSet{
				{
					source:  "quay/another/test",
					mirrors: []string{"registry/ocp/test"},
				},
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test"},
				},
			},
		},
		{
			name: "multiple Source multiple Mirrors",
			sources: []mirrorSet{
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test"},
				},
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/another/test"},
				},
				{
					source:  "quay/another/test",
					mirrors: []string{"registry/ocp/test"},
				},
				{
					source:  "quay/another/test",
					mirrors: []string{"registry/another/test"},
				},
			},
			expectedSources: []mirrorSet{
				{
					source:  "quay/another/test",
					mirrors: []string{"registry/ocp/test", "registry/another/test"},
				},
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test", "registry/another/test"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uniqueSources := dedupeSortSources(tt.sources)
			if !reflect.DeepEqual(uniqueSources, tt.expectedSources) {
				t.Errorf("%s", diff.ObjectGoPrintSideBySide(uniqueSources, tt.expectedSources))
			}
		})
	}
}

func Test_convertMirrorSetToImageDigestMirrors(t *testing.T) {
	tests := []struct {
		name            string
		sources         []mirrorSet
		expectedSources []apicfgv1.ImageDigestMirrors
	}{
		{
			name: "single Source single Mirror",
			sources: []mirrorSet{
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test"},
				},
			},
			expectedSources: []apicfgv1.ImageDigestMirrors{
				{
					Source:  "quay/ocp/test",
					Mirrors: []apicfgv1.ImageMirror{"registry/ocp/test"},
				},
			},
		},
		{
			name: "single Source multiple Mirrors",
			sources: []mirrorSet{
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test"},
				},
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/another/test"},
				},
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test"},
				},
			},
			expectedSources: []apicfgv1.ImageDigestMirrors{
				{
					Source:  "quay/ocp/test",
					Mirrors: []apicfgv1.ImageMirror{"registry/ocp/test"},
				},
				{
					Source:  "quay/ocp/test",
					Mirrors: []apicfgv1.ImageMirror{"registry/another/test"},
				},
				{
					Source:  "quay/ocp/test",
					Mirrors: []apicfgv1.ImageMirror{"registry/ocp/test"},
				},
			},
		},
		{
			name: "multiple Source single Mirrors",
			sources: []mirrorSet{
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test"},
				},
				{
					source:  "quay/another/test",
					mirrors: []string{"registry/ocp/test"},
				},
			},
			expectedSources: []apicfgv1.ImageDigestMirrors{
				{
					Source:  "quay/ocp/test",
					Mirrors: []apicfgv1.ImageMirror{"registry/ocp/test"},
				},
				{
					Source:  "quay/another/test",
					Mirrors: []apicfgv1.ImageMirror{"registry/ocp/test"},
				},
			},
		},
		{
			name: "multiple Source multiple Mirrors",
			sources: []mirrorSet{
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test"},
				},
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/another/test"},
				},
				{
					source:  "quay/another/test",
					mirrors: []string{"registry/ocp/test"},
				},
				{
					source:  "quay/another/test",
					mirrors: []string{"registry/another/test"},
				},
			},
			expectedSources: []apicfgv1.ImageDigestMirrors{
				{
					Source:  "quay/ocp/test",
					Mirrors: []apicfgv1.ImageMirror{"registry/ocp/test"},
				},
				{
					Source:  "quay/ocp/test",
					Mirrors: []apicfgv1.ImageMirror{"registry/another/test"},
				},
				{
					Source:  "quay/another/test",
					Mirrors: []apicfgv1.ImageMirror{"registry/ocp/test"},
				},
				{
					Source:  "quay/another/test",
					Mirrors: []apicfgv1.ImageMirror{"registry/another/test"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idmsSources := convertMirrorSetToImageDigestMirrors(tt.sources)
			if !reflect.DeepEqual(idmsSources, tt.expectedSources) {
				t.Errorf("%s", diff.ObjectGoPrintSideBySide(idmsSources, tt.expectedSources))
			}
		})
	}
}

func Test_printICSPInstructions(t *testing.T) {
	testcases := []struct {
		mirrorSets []mirrorSet
		want       []byte
	}{
		{
			mirrorSets: []mirrorSet{
				{
					source:  "quay/another/test",
					mirrors: []string{"registry/another/test"},
				},
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test"},
				},
			},
			want: []byte(`
To use the new mirrored repository to install, add the following section to the install-config.yaml:

imageContentSources:
- mirrors:
  - registry/another/test
  source: quay/another/test
- mirrors:
  - registry/ocp/test
  source: quay/ocp/test


To use the new mirrored repository for upgrades, use the following to create an ImageContentSourcePolicy:

apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  name: example
spec:
  repositoryDigestMirrors:
  - mirrors:
    - registry/another/test
    source: quay/another/test
  - mirrors:
    - registry/ocp/test
    source: quay/ocp/test
`),
		},
	}

	for _, tc := range testcases {
		t.Run("", func(t *testing.T) {
			buf := new(bytes.Buffer)
			err := printICSPInstructions(buf, tc.mirrorSets)
			if err != nil {
				t.Errorf("unexpected error testing printICSPInstructions(): %v", err)
			}
			got := buf.Bytes()
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%s", diff.ObjectGoPrintSideBySide(got, tc.want))
			}
		})
	}
}

func Test_printIDMSInstructions(t *testing.T) {
	testcases := []struct {
		mirrorSets []mirrorSet
		want       []byte
	}{
		{
			mirrorSets: []mirrorSet{
				{
					source:  "quay/another/test",
					mirrors: []string{"registry/another/test"},
				},
				{
					source:  "quay/ocp/test",
					mirrors: []string{"registry/ocp/test"},
				},
			},
			want: []byte(`
To use the new mirrored repository to install, add the following section to the install-config.yaml:

imageDigestSources:
- mirrors:
  - registry/another/test
  source: quay/another/test
- mirrors:
  - registry/ocp/test
  source: quay/ocp/test


To use the new mirrored repository for upgrades, use the following to create an ImageDigestMirrorSet:

apiVersion: config.openshift.io/v1
kind: ImageDigestMirrorSet
metadata:
  name: example
spec:
  imageDigestMirrors:
  - mirrors:
    - registry/another/test
    source: quay/another/test
  - mirrors:
    - registry/ocp/test
    source: quay/ocp/test
`),
		},
	}

	for _, tc := range testcases {
		t.Run("", func(t *testing.T) {
			buf := new(bytes.Buffer)
			err := printIDMSInstructions(buf, tc.mirrorSets)
			if err != nil {
				t.Errorf("unexpected error testing printIDMSInstructions(): %v", err)
			}
			got := buf.Bytes()
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%s", diff.ObjectGoPrintSideBySide(got, tc.want))
			}
		})
	}
}
