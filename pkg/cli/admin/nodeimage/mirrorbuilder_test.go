package nodeimage

import (
	"reflect"
	"testing"

	ocpv1 "github.com/openshift/api/config/v1"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var defaultIdmsSetList = ocpv1.ImageDigestMirrorSetList{
	TypeMeta: metav1.TypeMeta{},
	ListMeta: metav1.ListMeta{
		ResourceVersion: "15",
	},
	Items: []ocpv1.ImageDigestMirrorSet{
		{
			TypeMeta:   metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{Name: "image-digest-mirror", Namespace: "test"},
			Spec: ocpv1.ImageDigestMirrorSetSpec{
				ImageDigestMirrors: []ocpv1.ImageDigestMirrors{
					{
						Source: "quay.io/openshift-release-dev/ocp-v4.0-art-dev",
						Mirrors: []ocpv1.ImageMirror{
							"virthost.test:5000/localimages/local-release-image",
						},
					},
					{
						Source: "registry.ci.openshift.org/ocp/release",
						Mirrors: []ocpv1.ImageMirror{
							"virthost.test:5000/localimages/local-dev",
						},
					},
				},
			},
		},
	},
}

var defaultIdms = `apiVersion: config.openshift.io/v1
kind: ImageDigestMirrorSet
metadata:
  name: image-digest
spec:
  imageDigestMirrors:
  - mirrors:
    - virthost.test:5000/localimages/local-release-image
    source: quay.io/openshift-release-dev/ocp-v4.0-art-dev
  - mirrors:
    - virthost.test:5000/localimages/local-dev
    source: registry.ci.openshift.org/ocp/release
status: {}
`

var defaultIcspList = operatorv1alpha1.ImageContentSourcePolicyList{
	TypeMeta: metav1.TypeMeta{},
	ListMeta: metav1.ListMeta{
		ResourceVersion: "15",
	},
	Items: []operatorv1alpha1.ImageContentSourcePolicy{
		{
			TypeMeta:   metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{Name: "image-policy", Namespace: "test"},
			Spec: operatorv1alpha1.ImageContentSourcePolicySpec{
				RepositoryDigestMirrors: []operatorv1alpha1.RepositoryDigestMirrors{
					{
						Source: "quay.io/openshift-release-dev/ocp-v4.0-art-dev",
						Mirrors: []string{
							"virthost.test:5000/localimages/local-release-image",
						},
					},
					{
						Source: "registry.ci.openshift.org/ocp/release",
						Mirrors: []string{
							"virthost.test:5000/localimages/local-dev",
						},
					},
				},
			},
		},
	},
}

var icspListDifferentThanIdms = operatorv1alpha1.ImageContentSourcePolicyList{
	TypeMeta: metav1.TypeMeta{},
	ListMeta: metav1.ListMeta{
		ResourceVersion: "15",
	},
	Items: []operatorv1alpha1.ImageContentSourcePolicy{
		{
			TypeMeta:   metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{Name: "image-policy", Namespace: "test"},
			Spec: operatorv1alpha1.ImageContentSourcePolicySpec{
				RepositoryDigestMirrors: []operatorv1alpha1.RepositoryDigestMirrors{
					{
						Source: "example.com/ocp-v4.0-art-dev",
						Mirrors: []string{
							"localimages/local-release-image",
						},
					},
					{
						Source: "example.com/ocp/release",
						Mirrors: []string{
							"localimages/local-dev",
						},
					},
				},
			},
		},
	},
}

var icspPlusIdms = `apiVersion: config.openshift.io/v1
kind: ImageDigestMirrorSet
metadata:
  name: image-digest
spec:
  imageDigestMirrors:
  - mirrors:
    - virthost.test:5000/localimages/local-release-image
    source: quay.io/openshift-release-dev/ocp-v4.0-art-dev
  - mirrors:
    - virthost.test:5000/localimages/local-dev
    source: registry.ci.openshift.org/ocp/release
  - mirrors:
    - localimages/local-release-image
    source: example.com/ocp-v4.0-art-dev
  - mirrors:
    - localimages/local-dev
    source: example.com/ocp/release
status: {}
`

func TestGetIdmsContents(t *testing.T) {
	testCases := []struct {
		name          string
		idmsSetList   *ocpv1.ImageDigestMirrorSetList
		icspSetList   *operatorv1alpha1.ImageContentSourcePolicyList
		expectedIdms  []byte
		expectedError string
	}{
		{
			name:         "IDMS Only mirrors",
			idmsSetList:  &defaultIdmsSetList,
			icspSetList:  nil,
			expectedIdms: []byte(defaultIdms),
		},
		{
			name:         "ICSP Only mirrors",
			idmsSetList:  nil,
			icspSetList:  &defaultIcspList,
			expectedIdms: []byte(defaultIdms),
		},
		{
			name:         "IDMS and ICSP mirrors the same",
			idmsSetList:  &defaultIdmsSetList,
			icspSetList:  &defaultIcspList,
			expectedIdms: []byte(defaultIdms),
		},
		{
			name:         "IDMS and ICSP mirrors are different",
			idmsSetList:  &defaultIdmsSetList,
			icspSetList:  &icspListDifferentThanIdms,
			expectedIdms: []byte(icspPlusIdms),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			idmsContents, err := getIdmsContents(nil, tc.idmsSetList, tc.icspSetList)

			if tc.expectedError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !reflect.DeepEqual(tc.expectedIdms, idmsContents) {
					t.Errorf("Expected:\n%s\ngot:\n%s", tc.expectedIdms, idmsContents)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error not received: %s", tc.expectedError)
				}
			}
		})
	}
}
