package icsp

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGenerateIDMS(t *testing.T) {
	for _, tc := range []struct {
		name  string
		icsps []operatorv1alpha1.ImageContentSourcePolicy
		want  []byte
	}{
		{

			name: "convert multiple icsps to idms",
			icsps: []operatorv1alpha1.ImageContentSourcePolicy{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "multiple-convert-1",
						Labels:      map[string]string{"icspIdx": "1"},
						Annotations: map[string]string{"icspCon": "idms"},
					},
					Spec: operatorv1alpha1.ImageContentSourcePolicySpec{
						RepositoryDigestMirrors: []operatorv1alpha1.RepositoryDigestMirrors{
							{Source: "source.example.com", Mirrors: []string{"z1.example.com", "y2.example.com", "x3.example.com"}},
							{Source: "source.example.net", Mirrors: []string{"z1.example.net", "y2.example.net", "x3.example.net"}},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "multiple-convert-2",
						Labels:      map[string]string{"icspIdx": "2"},
						Annotations: map[string]string{"icspCon": "idms"},
					},
					Spec: operatorv1alpha1.ImageContentSourcePolicySpec{
						RepositoryDigestMirrors: []operatorv1alpha1.RepositoryDigestMirrors{
							{Source: "source.example.com", Mirrors: []string{"z1.example.com", "y2.example.com", "x3.example.com"}},
							{Source: "source.example.net", Mirrors: []string{"z1.example.net", "y2.example.net", "x3.example.net"}},
						},
					},
				},
			},
			want: []byte(
				`apiVersion: config.openshift.io/v1
kind: ImageDigestMirrorSet
metadata:
    annotations:
        icspCon: idms
    labels:
        icspIdx: "1"
    name: multiple-convert-1
spec:
    imageDigestMirrors:
        - mirrors:
            - z1.example.com
            - y2.example.com
            - x3.example.com
          source: source.example.com
        - mirrors:
            - z1.example.net
            - y2.example.net
            - x3.example.net
          source: source.example.net
---
apiVersion: config.openshift.io/v1
kind: ImageDigestMirrorSet
metadata:
    annotations:
        icspCon: idms
    labels:
        icspIdx: "2"
    name: multiple-convert-2
spec:
    imageDigestMirrors:
        - mirrors:
            - z1.example.com
            - y2.example.com
            - x3.example.com
          source: source.example.com
        - mirrors:
            - z1.example.net
            - y2.example.net
            - x3.example.net
          source: source.example.net
`,
			),
		},
		{

			name: "convert icsp to idms",
			icsps: []operatorv1alpha1.ImageContentSourcePolicy{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "convert",
						Labels:      map[string]string{"icspIdx": "1"},
						Annotations: map[string]string{"icspCon": "idms"},
					},
					Spec: operatorv1alpha1.ImageContentSourcePolicySpec{
						RepositoryDigestMirrors: []operatorv1alpha1.RepositoryDigestMirrors{
							{Source: "source.example.com", Mirrors: []string{"z1.example.com", "y2.example.com", "x3.example.com"}},
							{Source: "source.example.net", Mirrors: []string{"z1.example.net", "y2.example.net", "x3.example.net"}},
						},
					},
				},
			},
			want: []byte(
				`apiVersion: config.openshift.io/v1
kind: ImageDigestMirrorSet
metadata:
    annotations:
        icspCon: idms
    labels:
        icspIdx: "1"
    name: convert
spec:
    imageDigestMirrors:
        - mirrors:
            - z1.example.com
            - y2.example.com
            - x3.example.com
          source: source.example.com
        - mirrors:
            - z1.example.net
            - y2.example.net
            - x3.example.net
          source: source.example.net
`,
			),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := generateIDMS(tc.icsps)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(result, tc.want) {
				t.Errorf("generateIDMS() got = %v, want %v, diff = %v", string(result), string(tc.want), cmp.Diff(result, tc.want))
			}
		})
	}
}
