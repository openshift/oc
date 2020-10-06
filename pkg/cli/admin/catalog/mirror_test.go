package catalog

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/openshift/oc/pkg/cli/image/imagesource"
)

func TestWriteToMapping(t *testing.T) {
	tests := []struct {
		name    string
		mapping map[string]Target
		remap   map[imagesource.TypedImageReference]imagesource.TypedImageReference
		wantErr bool
		want    []string
	}{
		{
			name: "src is tagged",
			mapping: map[string]Target{
				"quay.io/halkyonio/operator:v0.1.8": {
					WithDigest: "",
					WithTag:    "quay.io/olmtest/halkyonio-operator:v0.1.8",
				},
			},
			want: []string{"quay.io/halkyonio/operator:v0.1.8=quay.io/olmtest/halkyonio-operator:v0.1.8"},
		},
		{
			name: "src has digest",
			mapping: map[string]Target{
				"docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4": {
					WithDigest: "quay.io/olmtest/strimzi-operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4",
					WithTag:    "quay.io/olmtest/strimzi-operator:2b13d275",
				},
			},
			want: []string{"docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4=quay.io/olmtest/strimzi-operator:2b13d275"},
		},
		{
			name: "multiple",
			mapping: map[string]Target{
				"docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4": {
					WithDigest: "quay.io/olmtest/strimzi-operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4",
					WithTag:    "quay.io/olmtest/strimzi-operator:2b13d275",
				},
				"quay.io/halkyonio/operator:v0.1.8": {
					WithDigest: "",
					WithTag:    "quay.io/olmtest/halkyonio-operator:v0.1.8",
				},
			},
			want: []string{
				"docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4=quay.io/olmtest/strimzi-operator:2b13d275",
				"quay.io/halkyonio/operator:v0.1.8=quay.io/olmtest/halkyonio-operator:v0.1.8",
			},
		},
		{
			name: "remap",
			mapping: map[string]Target{
				"docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4": {
					WithDigest: "quay.io/olmtest/strimzi-operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4",
					WithTag:    "quay.io/olmtest/strimzi-operator:2b13d275",
				},
				"quay.io/halkyonio/operator:v0.1.8": {
					WithDigest: "",
					WithTag:    "quay.io/olmtest/halkyonio-operator:v0.1.8",
				},
			},
			remap: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4"),
				}: {
					Type: imagesource.DestinationFile,
					Ref:  mustParseRef(t, "operators/strimzi/operator:2b13d275"),
				},
			},
			want: []string{
				"file://operators/strimzi/operator:2b13d275=quay.io/olmtest/strimzi-operator:2b13d275",
				"quay.io/halkyonio/operator:v0.1.8=quay.io/olmtest/halkyonio-operator:v0.1.8",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := writeToMapping(&buf, tt.mapping, tt.remap); (err != nil) != tt.wantErr {
				t.Errorf("writeToMapping() error = %v, wantErr %v", err, tt.wantErr)
			}
			got := strings.Split(buf.String(), "\n")
			if err := ElementsMatch(got[:len(got)-1], tt.want); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestGenerateICSP(t *testing.T) {
	type args struct {
		name    string
		scope   string
		mapping map[string]Target
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr bool
	}{
		{
			name: "src is tagged - skip mirror",
			args: args{
				name: "catalog",
				mapping: map[string]Target{
					"quay.io/halkyonio/operator:v0.1.8": {
						WithDigest: "",
						WithTag:    "quay.io/olmtest/halkyonio-operator:v0.1.8",
					},
				},
			},
			want: []byte(
				`apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  name: catalog
spec:
  repositoryDigestMirrors: []
`,
			),
		},
		{
			name: "src is tagged and icsp with registy scope - skip mirror",
			args: args{
				name:  "catalog",
				scope: "registry",
				mapping: map[string]Target{
					"quay.io/halkyonio/operator:v0.1.8": {
						WithDigest: "",
						WithTag:    "quay.io/olmtest/halkyonio-operator:v0.1.8",
					},
				},
			},
			want: []byte(
				`apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  name: catalog
spec:
  repositoryDigestMirrors: []
`,
			),
		},
		{
			name: "src has digest",
			args: args{
				name: "catalog",
				mapping: map[string]Target{
					"docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4": {
						WithDigest: "quay.io/olmtest/strimzi-operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4",
						WithTag:    "quay.io/olmtest/strimzi-operator:2b13d275",
					},
				},
			},
			want: []byte(
				`apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  name: catalog
spec:
  repositoryDigestMirrors:
  - mirrors:
    - quay.io/olmtest/strimzi-operator
    source: docker.io/strimzi/operator
`,
			),
		},
		{
			name: "src has digest and icsp with registry scope",
			args: args{
				name:  "catalog",
				scope: "registry",
				mapping: map[string]Target{
					"docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4": {
						WithDigest: "quay.io/olmtest/strimzi-operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4",
						WithTag:    "quay.io/olmtest/strimzi-operator:2b13d275",
					},
				},
			},
			want: []byte(
				`apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  name: catalog
spec:
  repositoryDigestMirrors:
  - mirrors:
    - quay.io
    source: docker.io
`,
			),
		},
		{
			name: "multiple",
			args: args{
				name: "catalog",
				mapping: map[string]Target{
					"docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4": {
						WithDigest: "quay.io/olmtest/strimzi-operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4",
						WithTag:    "quay.io/olmtest/strimzi-operator:2b13d275",
					},
					"quay.io/halkyonio/operator:v0.1.8": {
						WithDigest: "",
						WithTag:    "quay.io/olmtest/halkyonio-operator:v0.1.8",
					},
				},
			},
			want: []byte(
				`apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  name: catalog
spec:
  repositoryDigestMirrors:
  - mirrors:
    - quay.io/olmtest/strimzi-operator
    source: docker.io/strimzi/operator
`,
			),
		},
		{
			name: "multiple with icsp registry scope",
			args: args{
				name:  "catalog",
				scope: "registry",
				mapping: map[string]Target{
					"docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4": {
						WithDigest: "quay.io/olmtest/strimzi-operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4",
						WithTag:    "quay.io/olmtest/strimzi-operator:2b13d275",
					},
					"quay.io/halkyonio/operator:v0.1.8": {
						WithDigest: "",
						WithTag:    "quay.io/olmtest/halkyonio-operator:v0.1.8",
					},
				},
			},
			want: []byte(
				`apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  name: catalog
spec:
  repositoryDigestMirrors:
  - mirrors:
    - quay.io
    source: docker.io
`,
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateICSP(os.Stdout, tt.args.name, tt.args.scope, tt.args.mapping)
			if (err != nil) != tt.wantErr {
				t.Errorf("generateICSP() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("generateICSP() got = %v, want %v, diff = %v", string(got), string(tt.want), cmp.Diff(got, tt.want))
			}
		})
	}
}

func ElementsMatch(listA, listB []string) error {
	aLen := len(listA)
	bLen := len(listB)

	if aLen != bLen {
		return fmt.Errorf("Len of the lists don't match , len listA %v, len listB %v", aLen, bLen)
	}

	visited := make([]bool, bLen)

	for i := 0; i < aLen; i++ {
		found := false
		element := listA[i]
		for j := 0; j < bLen; j++ {
			if visited[j] {
				continue
			}
			if element == listB[j] {
				visited[j] = true
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("element %s appears more times in %s than in %s", element, listA, listB)
		}
	}
	return nil
}
