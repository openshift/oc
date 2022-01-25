package catalog

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	"github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/oc/pkg/cli/image/imagesource"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestWriteToMapping(t *testing.T) {
	tests := []struct {
		name    string
		mapping map[imagesource.TypedImageReference]imagesource.TypedImageReference
		wantErr bool
		want    []string
	}{
		{
			name: "src is tagged",
			mapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "quay.io/halkyonio/operator:v0.1.8"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "olmtest",
						Name:      "halkyonio-operator",
						Tag:       "v0.1.8",
						ID:        "",
					},
				},
			},
			want: []string{"quay.io/halkyonio/operator:v0.1.8=quay.io/olmtest/halkyonio-operator:v0.1.8"},
		},
		{
			name: "src has digest",
			mapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "olmtest",
						Name:      "strimzi-operator",
						Tag:       "2b13d275",
						ID:        "sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4",
					},
				},
			},
			want: []string{"docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4=quay.io/olmtest/strimzi-operator:2b13d275"},
		},
		{
			name: "multiple",
			mapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "olmtest",
						Name:      "strimzi-operator",
						Tag:       "2b13d275",
						ID:        "sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4",
					},
				},
				mustParseRef(t, "quay.io/halkyonio/operator:v0.1.8"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "olmtest",
						Name:      "halkyonio-operator",
						Tag:       "v0.1.8",
						ID:        "",
					},
				},
			},
			want: []string{
				"docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4=quay.io/olmtest/strimzi-operator:2b13d275",
				"quay.io/halkyonio/operator:v0.1.8=quay.io/olmtest/halkyonio-operator:v0.1.8",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := writeToMapping(&buf, tt.mapping); (err != nil) != tt.wantErr {
				t.Errorf("writeToMapping() error = %v, wantErr %v", err, tt.wantErr)
			}
			got := strings.Split(buf.String(), "\n")
			if err := ElementsMatch(got[:len(got)-1], tt.want); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestGetRegistryMapping(t *testing.T) {
	type args struct {
		scope   string
		mapping map[imagesource.TypedImageReference]imagesource.TypedImageReference
	}
	tests := []struct {
		name string
		args args
		want map[string]string
	}{
		{
			name: "src is tagged - skip mirrors",
			args: args{
				scope: "",
				mapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
					mustParseRef(t, "quay.io/halkyonio/operator:v0.1.8"): {
						Type: imagesource.DestinationRegistry,
						Ref: reference.DockerImageReference{
							Registry:  "quay.io",
							Namespace: "halkyonio",
							Name:      "operator",
							Tag:       "v0.1.8",
							ID:        "",
						},
					},
				},
			},
			want: map[string]string{},
		},
		{
			name: "src is tagged and icsp with registy scope - skip mirror",
			args: args{
				scope: "registry",
				mapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
					mustParseRef(t, "quay.io/halkyonio/operator:v0.1.8"): {
						Type: imagesource.DestinationRegistry,
						Ref: reference.DockerImageReference{
							Registry:  "quay.io",
							Namespace: "halkyonio",
							Name:      "operator",
							Tag:       "v0.1.8",
							ID:        "",
						},
					},
				},
			},
			want: map[string]string{},
		},
		{
			name: "src has digest",
			args: args{
				mapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
					mustParseRef(t, "docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4"): {
						Type: imagesource.DestinationRegistry,
						Ref: reference.DockerImageReference{
							Registry:  "quay.io",
							Namespace: "olmtest",
							Name:      "strimzi-operator",
							Tag:       "2b13d275",
							ID:        "sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4",
						},
					},
				},
			},
			want: map[string]string{"docker.io/strimzi/operator": "quay.io/olmtest/strimzi-operator"},
		},
		{
			name: "src has digest and icsp with registry scope",
			args: args{
				scope: "registry",
				mapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
					mustParseRef(t, "docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4"): {
						Type: imagesource.DestinationRegistry,
						Ref: reference.DockerImageReference{
							Registry:  "quay.io",
							Namespace: "olmtest",
							Name:      "strimzi-operator",
							Tag:       "2b13d275",
							ID:        "sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4",
						},
					},
				},
			},
			want: map[string]string{"docker.io": "quay.io"},
		},
		{
			name: "multiple",
			args: args{
				mapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
					mustParseRef(t, "docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4"): {
						Type: imagesource.DestinationRegistry,
						Ref: reference.DockerImageReference{
							Registry:  "quay.io",
							Namespace: "olmtest",
							Name:      "strimzi-operator",
							Tag:       "2b13d275",
							ID:        "sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4",
						},
					},
					mustParseRef(t, "quay.io/halkyonio/operator:v0.1.8"): {
						Type: imagesource.DestinationRegistry,
						Ref: reference.DockerImageReference{
							Registry:  "quay.io",
							Namespace: "olmtest",
							Name:      "halkyonio-operator",
							Tag:       "v0.1.8",
							ID:        "",
						},
					},
				},
			},
			want: map[string]string{"docker.io/strimzi/operator": "quay.io/olmtest/strimzi-operator"},
		},
		{
			name: "multiple with icsp registry scope",
			args: args{
				scope: "registry",
				mapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
					mustParseRef(t, "docker.io/strimzi/operator@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4"): {
						Type: imagesource.DestinationRegistry,
						Ref: reference.DockerImageReference{
							Registry:  "quay.io",
							Namespace: "olmtest",
							Name:      "strimzi-operator",
							Tag:       "2b13d275",
							ID:        "sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4",
						},
					},
					mustParseRef(t, "quay.io/halkyonio/operator:v0.1.8"): {
						Type: imagesource.DestinationRegistry,
						Ref: reference.DockerImageReference{
							Registry:  "quay.io",
							Namespace: "olmtest",
							Name:      "halkyonio-operator",
							Tag:       "v0.1.8",
							ID:        "",
						},
					},
				},
			},
			want: map[string]string{"docker.io": "quay.io"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getRegistryMapping(os.Stdout, tt.args.scope, tt.args.mapping)
			if len(got) != len(tt.want) {
				t.Errorf("Received map length != expected map length")
			}
			for k := range tt.want {
				if got[k] != tt.want[k] {
					t.Errorf("Expeced Map DNE actual map %v", got)
				}
			}
		})
	}
}

func TestGenerateICSP(t *testing.T) {
	type args struct {
		name          string
		scope         string
		mapping       map[string]string
		icspByteLimit int
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr error
	}{
		{
			name: "src is tagged - skip mirror",
			args: args{
				name:          "catalog",
				mapping:       map[string]string{},
				icspByteLimit: maxICSPSize,
			},
			want: []byte(
				`apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  labels:
    operators.openshift.org/catalog: "true"
  name: catalog
spec:
  repositoryDigestMirrors: []
`,
			),
		},
		{
			name: "src is tagged and icsp with registy scope - skip mirror",
			args: args{
				name:          "catalog",
				scope:         "registry",
				mapping:       map[string]string{},
				icspByteLimit: maxICSPSize,
			},
			want: []byte(
				`apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  labels:
    operators.openshift.org/catalog: "true"
  name: catalog
spec:
  repositoryDigestMirrors: []
`,
			),
		},
		{
			name: "src has digest",
			args: args{
				name:          "catalog",
				mapping:       map[string]string{"docker.io/strimzi/operator": "quay.io/olmtest/strimzi-operator"},
				icspByteLimit: maxICSPSize,
			},
			want: []byte(
				`apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  labels:
    operators.openshift.org/catalog: "true"
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
				name:          "catalog",
				scope:         "registry",
				mapping:       map[string]string{"docker.io": "quay.io"},
				icspByteLimit: maxICSPSize,
			},
			want: []byte(
				`apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  labels:
    operators.openshift.org/catalog: "true"
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
				name:          "catalog",
				mapping:       map[string]string{"docker.io/strimzi/operator": "quay.io/olmtest/strimzi-operator"},
				icspByteLimit: maxICSPSize,
			},
			want: []byte(
				`apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  labels:
    operators.openshift.org/catalog: "true"
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
				name:          "catalog",
				scope:         "registry",
				mapping:       map[string]string{"docker.io": "quay.io"},
				icspByteLimit: maxICSPSize,
			},
			want: []byte(
				`apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  labels:
    operators.openshift.org/catalog: "true"
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
			name: "icsp byte limit set to 0",
			args: args{
				name:          "catalog",
				scope:         "registry",
				mapping:       map[string]string{"docker.io": "quay.io"},
				icspByteLimit: 0,
			},
			wantErr: fmt.Errorf("unable to add mirror {docker.io [quay.io]} to ICSP with the max-icsp-size set to 0"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateICSP(os.Stdout, tt.args.name, tt.args.icspByteLimit, tt.args.mapping)
			if tt.wantErr != nil {
				if err == nil || err.Error() != tt.wantErr.Error() {
					t.Errorf("generateICSP() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("generateICSP() got = %v, want %v, diff = %v", string(got), string(tt.want), cmp.Diff(got, tt.want))
			}
		})
	}
}

func TestGenerateCatalogSource(t *testing.T) {
	type args struct {
		source  imagesource.TypedImageReference
		mapping map[imagesource.TypedImageReference]imagesource.TypedImageReference
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr bool
	}{
		{
			name: "generates catalogsource",
			args: args{
				source: mustParseRef(t, "quay.io/the/index:1"),
				mapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
					mustParseRef(t, "quay.io/the/index:1"): {
						Type: imagesource.DestinationRegistry,
						Ref: reference.DockerImageReference{
							Registry:  "quay.io",
							Namespace: "the",
							Name:      "index",
							Tag:       "1",
							ID:        "",
						},
					},
				},
			},
			want: []byte(
				`apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: index
  namespace: openshift-marketplace
spec:
  image: quay.io/the/index:1
  sourceType: grpc
`,
			),
		},
		{
			name: "generates catalogsource with digest",
			args: args{
				source: mustParseRef(t, "quay.io/the/index:1"),
				mapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
					mustParseRef(t, "quay.io/the/index:1"): {
						Type: imagesource.DestinationRegistry,
						Ref: reference.DockerImageReference{
							Registry:  "quay.io",
							Namespace: "the",
							Name:      "index",
							Tag:       "1",
							ID:        "sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4",
						},
					},
				},
			},
			want: []byte(
				`apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: index
  namespace: openshift-marketplace
spec:
  image: quay.io/the/index@sha256:d134a9865524c29fcf75bbc4469013bc38d8a15cb5f41acfddb6b9e492f556e4
  sourceType: grpc
`,
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateCatalogSource(tt.args.source, tt.args.mapping)
			if (err != nil) != tt.wantErr {
				t.Errorf("generateCatalogSource() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("generateCatalogSource() got = %v, want %v, diff = %v", string(got), string(tt.want), cmp.Diff(got, tt.want))
			}
		})
	}
}

func TestGenerateICSPs(t *testing.T) {
	type args struct {
		name  string
		scope string
		limit int
	}
	tests := []struct {
		name            string
		args            args
		registryMapSize int
	}{
		{
			name: "Generated ICSPs are smaller than the byte limit",
			args: args{
				name:  "catalog",
				scope: "registry",
				limit: 1000,
			},
			registryMapSize: 100000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapping := map[imagesource.TypedImageReference]imagesource.TypedImageReference{}
			for i, byteCount := 0, 0; byteCount < tt.registryMapSize; i++ {
				key := fmt.Sprintf("foo-%d", i)
				value := fmt.Sprintf("bar-%d", i)
				mapping[imagesource.TypedImageReference{Ref: reference.DockerImageReference{Registry: key}}] = imagesource.TypedImageReference{Ref: reference.DockerImageReference{ID: value, Registry: value}}
				byteCount += len(key) + len(value)
			}

			got, err := generateICSPs(os.Stdout, tt.args.name, tt.args.scope, tt.args.limit, mapping)
			if err != nil {
				t.Error(err)
				return
			}

			for _, icsp := range got {
				// check that all ICSPs are under ICSP limit
				if icspBytes := len(icsp); icspBytes > tt.args.limit {
					t.Errorf("ICSP size (%d) exceeded limit (%d)", icspBytes, tt.args.limit)
					return
				}
				// convert Byte array into unstructured object
				unstructuredObj := unstructured.Unstructured{Object: map[string]interface{}{}}
				err = yaml.Unmarshal(icsp, unstructuredObj.Object)
				if err != nil {
					t.Error(err)
					return
				}

				// convert unstructured object into ICSP
				icspObject := &operatorv1alpha1.ImageContentSourcePolicy{}
				err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &icspObject)
				if err != nil {
					t.Error(err)
					return
				}

				// remove mappings found in ICSP from original mapping
				for _, repositoryDigestMirrors := range icspObject.Spec.RepositoryDigestMirrors {
					delete(mapping, imagesource.TypedImageReference{Ref: reference.DockerImageReference{Registry: repositoryDigestMirrors.Source}})
				}
			}
			// ensure that all mappings were seen in ICSPs
			if missingMaps := len(mapping); missingMaps != 0 {
				t.Errorf("generated ICSPs are missing %d mappings", missingMaps)
				return
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
