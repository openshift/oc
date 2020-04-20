package manifest

import (
	"io/ioutil"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	operatorv1alpha1client "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1alpha1"
	imagereference "github.com/openshift/library-go/pkg/image/reference"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func securityOpts(icspList []operatorv1alpha1.ImageContentSourcePolicy, icspFile string) *SecurityOptions {
	return &SecurityOptions{
		Insecure:                     true,
		SkipVerification:             true,
		ImageContentSourcePolicyFile: icspFile,
		ImageContentSourcePolicyList: icspList,
		CachedContext:                nil,
	}
}

func icspFile(t *testing.T) string {
	icspFile, err := ioutil.TempFile("/tmp", "test.*.icsp.yaml")
	if err != nil {
		t.Errorf("error creating test icsp file: %v", err)
	}
	icsp := `
apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  name: release
spec:
  repositoryDigestMirrors:
  - mirrors:
    - someregistry/match/file
    source: quay.io/ocp-test/release
  - mirrors:
    - someregistry/match/file
    source: quay.io/ocp-test/another-source
`
	err = ioutil.WriteFile(icspFile.Name(), []byte(icsp), 0)
	if err != nil {
		t.Errorf("error wriing to test icsp file: %v", err)
	}
	return icspFile.Name()
}

func TestAlternativeImageSources(t *testing.T) {
	tests := []struct {
		name                 string
		icspList             []operatorv1alpha1.ImageContentSourcePolicy
		icspFile             string
		image                string
		imageSourcesExpected []string
	}{
		{
			name: "multiple ICSPs",
			icspList: []operatorv1alpha1.ImageContentSourcePolicy{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "release",
					},
					Spec: operatorv1alpha1.ImageContentSourcePolicySpec{
						RepositoryDigestMirrors: []operatorv1alpha1.RepositoryDigestMirrors{
							{
								Source: "quay.io/multiple/icsps",
								Mirrors: []string{
									"someregistry/somerepo/release",
								},
							},
							{
								Source: "quay.io/ocp-test/another-release",
								Mirrors: []string{
									"someregistry/somerepo/release",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another",
					},
					Spec: operatorv1alpha1.ImageContentSourcePolicySpec{
						RepositoryDigestMirrors: []operatorv1alpha1.RepositoryDigestMirrors{
							{
								Source: "quay.io/multiple/icsps",
								Mirrors: []string{
									"anotherregistry/anotherrepo/release",
								},
							},
						},
					},
				},
			},
			icspFile:             "",
			image:                "quay.io/multiple/icsps:4.5",
			imageSourcesExpected: []string{"someregistry/somerepo/release", "anotherregistry/anotherrepo/release"},
		},
		{
			name:                 "sources match ICSP file",
			icspList:             []operatorv1alpha1.ImageContentSourcePolicy{},
			icspFile:             icspFile(t),
			image:                "quay.io/ocp-test/release:4.6",
			imageSourcesExpected: []string{"someregistry/match/file"},
		},
		{
			name:                 "no match ICSP file",
			icspList:             []operatorv1alpha1.ImageContentSourcePolicy{},
			icspFile:             icspFile(t),
			image:                "quay.io/passed/image:4.5",
			imageSourcesExpected: []string{"quay.io/passed/image"},
		},
		{
			name: "ICSP mirrors match image",
			icspList: []operatorv1alpha1.ImageContentSourcePolicy{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "release",
					},
					Spec: operatorv1alpha1.ImageContentSourcePolicySpec{
						RepositoryDigestMirrors: []operatorv1alpha1.RepositoryDigestMirrors{
							{
								Source: "quay.io/ocp-test/release",
								Mirrors: []string{
									"someregistry/mirrors/match",
								},
							},
						},
					},
				},
			},
			icspFile:             "",
			image:                "quay.io/ocp-test/release:4.5",
			imageSourcesExpected: []string{"someregistry/mirrors/match"},
		},
		{
			name: "ICSP source matches image",
			icspList: []operatorv1alpha1.ImageContentSourcePolicy{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "release",
					},
					Spec: operatorv1alpha1.ImageContentSourcePolicySpec{
						RepositoryDigestMirrors: []operatorv1alpha1.RepositoryDigestMirrors{
							{
								Source: "quay.io/source/matches",
								Mirrors: []string{
									"someregistry/somerepo/release",
								},
							},
						},
					},
				},
			},
			icspFile:             "",
			image:                "quay.io/source/matches:4.5",
			imageSourcesExpected: []string{"someregistry/somerepo/release"},
		},
		{
			name:                 "no ICSP",
			icspList:             nil,
			icspFile:             "",
			image:                "quay.io/ocp-test/release:4.5",
			imageSourcesExpected: []string{},
		},
	}
	for _, tt := range tests {
		imageRef, err := imagereference.Parse(tt.image)
		if err != nil {
			t.Errorf("parsing image reference error = %v", err)
		}
		secOpts := securityOpts(tt.icspList, tt.icspFile)
		secOpts.ICSPClientFn = func() (operatorv1alpha1client.ImageContentSourcePolicyInterface, error) {
			return nil, nil
		}
		var expectedRefs []imagereference.DockerImageReference
		for _, expected := range tt.imageSourcesExpected {
			expectedRef, err := imagereference.Parse(expected)
			if err != nil {
				t.Errorf("parsing image reference error = %v", err)
			}
			expectedRefs = append(expectedRefs, expectedRef)
		}

		if len(tt.icspFile) > 0 {
			err := secOpts.AddImageSourcePoliciesFromFile(tt.image)
			if err != nil {
				t.Errorf("add ICSP from file error = %v", err)
			}
		}
		a := &addAlternativeImageSources{}
		altSources, err := a.AddImageSources(imageRef, secOpts.ImageContentSourcePolicyList)
		if err != nil {
			t.Errorf("registry client Context error = %v", err)
		}
		if !reflect.DeepEqual(expectedRefs, altSources) {
			t.Errorf("AddAlternativeImageSource got = %v, want %v, diff = %v", altSources, expectedRefs, cmp.Diff(altSources, expectedRefs))
		}
	}
}
