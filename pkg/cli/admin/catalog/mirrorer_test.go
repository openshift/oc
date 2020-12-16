package catalog

import (
	"reflect"
	"testing"

	"github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/oc/pkg/cli/image/imagesource"
)

func existingExtractor(dir string) DatabaseExtractorFunc {
	return func(from imagesource.TypedImageReference) (s string, e error) {
		return dir, nil
	}
}

func noopMirror(map[imagesource.TypedImageReference]imagesource.TypedImageReference) error {
	return nil
}

func mustParse(t *testing.T, img string) imagesource.TypedImageReference {
	imgRef, err := imagesource.ParseReference(img)
	if err != nil {
		t.Errorf("couldn't parse image ref %s: %v", img, err)
	}
	return imgRef
}

func TestMirror(t *testing.T) {
	type fields struct {
		ImageMirrorer     ImageMirrorerFunc
		DatabaseExtractor DatabaseExtractorFunc
		Source            imagesource.TypedImageReference
		Dest              imagesource.TypedImageReference
	}
	tests := []struct {
		name    string
		fields  fields
		want    map[imagesource.TypedImageReference]imagesource.TypedImageReference
		wantErr error
	}{
		{
			name: "maps related images and bundle images",
			fields: fields{
				ImageMirrorer:     noopMirror,
				DatabaseExtractor: existingExtractor("testdata/test.db"),
				Source:            mustParse(t, "quay.io/example/image:tag"),
				Dest:              mustParse(t, "localhost:5000"),
			},
			want: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "quay.io/example/image:tag"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "example",
						Name:      "image",
						Tag:       "tag",
						ID:        "",
					},
				},
				mustParseRef(t, "quay.io/test/prometheus.0.14.0:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "test",
						Name:      "prometheus.0.14.0",
						Tag:       "latest",
						ID:        "",
					},
				},
				mustParseRef(t, "quay.io/coreos/etcd-operator@sha256:db563baa8194fcfe39d1df744ed70024b0f1f9e9b55b5923c2f3a413c44dc6b8"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "coreos",
						Name:      "etcd-operator",
						Tag:       "b56e2636",
						ID:        "sha256:db563baa8194fcfe39d1df744ed70024b0f1f9e9b55b5923c2f3a413c44dc6b8",
					},
				},
				mustParseRef(t, "quay.io/test/etcd.0.9.0:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "test",
						Name:      "etcd.0.9.0",
						Tag:       "latest",
						ID:        "",
					},
				},
				mustParseRef(t, "quay.io/coreos/prometheus-operator@sha256:0e92dd9b5789c4b13d53e1319d0a6375bcca4caaf0d698af61198061222a576d"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "coreos",
						Name:      "prometheus-operator",
						Tag:       "7f39d12d",
						ID:        "sha256:0e92dd9b5789c4b13d53e1319d0a6375bcca4caaf0d698af61198061222a576d",
					},
				},
				mustParseRef(t, "quay.io/coreos/prometheus-operator@sha256:3daa69a8c6c2f1d35dcf1fe48a7cd8b230e55f5229a1ded438f687debade5bcf"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "coreos",
						Name:      "prometheus-operator",
						Tag:       "1ebe036a",
						ID:        "sha256:3daa69a8c6c2f1d35dcf1fe48a7cd8b230e55f5229a1ded438f687debade5bcf",
					},
				},
				mustParseRef(t, "quay.io/test/prometheus.0.22.2:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "test",
						Name:      "prometheus.0.22.2",
						Tag:       "latest",
						ID:        "",
					},
				},
				mustParseRef(t, "quay.io/coreos/etcd-operator@sha256:c0301e4686c3ed4206e370b42de5a3bd2229b9fb4906cf85f3f30650424abec2"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "coreos",
						Name:      "etcd-operator",
						Tag:       "2f1eb95",
						ID:        "sha256:c0301e4686c3ed4206e370b42de5a3bd2229b9fb4906cf85f3f30650424abec2",
					},
				},
				mustParseRef(t, "quay.io/coreos/prometheus-operator@sha256:5037b4e90dbb03ebdefaa547ddf6a1f748c8eeebeedf6b9d9f0913ad662b5731"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "coreos",
						Name:      "prometheus-operator",
						Tag:       "76771fef",
						ID:        "sha256:5037b4e90dbb03ebdefaa547ddf6a1f748c8eeebeedf6b9d9f0913ad662b5731",
					},
				},
				mustParseRef(t, "quay.io/test/etcd.0.9.2:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "test",
						Name:      "etcd.0.9.2",
						Tag:       "latest",
						ID:        "",
					},
				},
				mustParseRef(t, "quay.io/test/prometheus.0.15.0:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "test",
						Name:      "prometheus.0.15.0",
						Tag:       "latest",
						ID:        "",
					},
				},
			},
		},
		{
			name: "maps into a single registry namespace",
			fields: fields{
				ImageMirrorer:     noopMirror,
				DatabaseExtractor: existingExtractor("testdata/test.db"),
				Source:            mustParse(t, "quay.io/example/image:tag"),
				Dest:              mustParse(t, "localhost:5000/org"),
			},
			want: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "quay.io/example/image:tag"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "org",
						Name:      "example-image",
						Tag:       "tag",
						ID:        "",
					},
				},
				mustParseRef(t, "quay.io/test/prometheus.0.14.0:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "org",
						Name:      "test-prometheus.0.14.0",
						Tag:       "latest",
						ID:        "",
					},
				},
				mustParseRef(t, "quay.io/coreos/etcd-operator@sha256:db563baa8194fcfe39d1df744ed70024b0f1f9e9b55b5923c2f3a413c44dc6b8"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "org",
						Name:      "coreos-etcd-operator",
						Tag:       "b56e2636",
						ID:        "sha256:db563baa8194fcfe39d1df744ed70024b0f1f9e9b55b5923c2f3a413c44dc6b8",
					},
				},
				mustParseRef(t, "quay.io/test/etcd.0.9.0:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "org",
						Name:      "test-etcd.0.9.0",
						Tag:       "latest",
						ID:        "",
					},
				},
				mustParseRef(t, "quay.io/coreos/prometheus-operator@sha256:0e92dd9b5789c4b13d53e1319d0a6375bcca4caaf0d698af61198061222a576d"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "org",
						Name:      "coreos-prometheus-operator",
						Tag:       "7f39d12d",
						ID:        "sha256:0e92dd9b5789c4b13d53e1319d0a6375bcca4caaf0d698af61198061222a576d",
					},
				},
				mustParseRef(t, "quay.io/coreos/prometheus-operator@sha256:3daa69a8c6c2f1d35dcf1fe48a7cd8b230e55f5229a1ded438f687debade5bcf"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "org",
						Name:      "coreos-prometheus-operator",
						Tag:       "1ebe036a",
						ID:        "sha256:3daa69a8c6c2f1d35dcf1fe48a7cd8b230e55f5229a1ded438f687debade5bcf",
					},
				},
				mustParseRef(t, "quay.io/test/prometheus.0.22.2:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "org",
						Name:      "test-prometheus.0.22.2",
						Tag:       "latest",
						ID:        "",
					},
				},
				mustParseRef(t, "quay.io/coreos/etcd-operator@sha256:c0301e4686c3ed4206e370b42de5a3bd2229b9fb4906cf85f3f30650424abec2"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "org",
						Name:      "coreos-etcd-operator",
						Tag:       "2f1eb95",
						ID:        "sha256:c0301e4686c3ed4206e370b42de5a3bd2229b9fb4906cf85f3f30650424abec2",
					},
				},
				mustParseRef(t, "quay.io/coreos/prometheus-operator@sha256:5037b4e90dbb03ebdefaa547ddf6a1f748c8eeebeedf6b9d9f0913ad662b5731"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "org",
						Name:      "coreos-prometheus-operator",
						Tag:       "76771fef",
						ID:        "sha256:5037b4e90dbb03ebdefaa547ddf6a1f748c8eeebeedf6b9d9f0913ad662b5731",
					},
				},
				mustParseRef(t, "quay.io/test/etcd.0.9.2:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "org",
						Name:      "test-etcd.0.9.2",
						Tag:       "latest",
						ID:        "",
					},
				},
				mustParseRef(t, "quay.io/test/prometheus.0.15.0:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "org",
						Name:      "test-prometheus.0.15.0",
						Tag:       "latest",
						ID:        "",
					},
				},
			},
		},
		{
			name: "maps into a single quay namespace",
			fields: fields{
				ImageMirrorer:     noopMirror,
				DatabaseExtractor: existingExtractor("testdata/test.db"),
				Source:            mustParse(t, "quay.io/example/image:tag"),
				Dest:              mustParse(t, "quay.io/org"),
			},
			want: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "quay.io/example/image:tag"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "org",
						Name:      "example-image",
						Tag:       "tag",
						ID:        "",
					},
				},
				mustParseRef(t, "quay.io/test/prometheus.0.14.0:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "org",
						Name:      "test-prometheus.0.14.0",
						Tag:       "latest",
						ID:        "",
					},
				},
				mustParseRef(t, "quay.io/coreos/etcd-operator@sha256:db563baa8194fcfe39d1df744ed70024b0f1f9e9b55b5923c2f3a413c44dc6b8"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "org",
						Name:      "coreos-etcd-operator",
						Tag:       "b56e2636",
						ID:        "sha256:db563baa8194fcfe39d1df744ed70024b0f1f9e9b55b5923c2f3a413c44dc6b8",
					},
				},
				mustParseRef(t, "quay.io/test/etcd.0.9.0:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "org",
						Name:      "test-etcd.0.9.0",
						Tag:       "latest",
						ID:        "",
					},
				},
				mustParseRef(t, "quay.io/coreos/prometheus-operator@sha256:0e92dd9b5789c4b13d53e1319d0a6375bcca4caaf0d698af61198061222a576d"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "org",
						Name:      "coreos-prometheus-operator",
						Tag:       "7f39d12d",
						ID:        "sha256:0e92dd9b5789c4b13d53e1319d0a6375bcca4caaf0d698af61198061222a576d",
					},
				},
				mustParseRef(t, "quay.io/coreos/prometheus-operator@sha256:3daa69a8c6c2f1d35dcf1fe48a7cd8b230e55f5229a1ded438f687debade5bcf"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "org",
						Name:      "coreos-prometheus-operator",
						Tag:       "1ebe036a",
						ID:        "sha256:3daa69a8c6c2f1d35dcf1fe48a7cd8b230e55f5229a1ded438f687debade5bcf",
					},
				},
				mustParseRef(t, "quay.io/test/prometheus.0.22.2:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "org",
						Name:      "test-prometheus.0.22.2",
						Tag:       "latest",
						ID:        "",
					},
				},
				mustParseRef(t, "quay.io/coreos/etcd-operator@sha256:c0301e4686c3ed4206e370b42de5a3bd2229b9fb4906cf85f3f30650424abec2"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "org",
						Name:      "coreos-etcd-operator",
						Tag:       "2f1eb95",
						ID:        "sha256:c0301e4686c3ed4206e370b42de5a3bd2229b9fb4906cf85f3f30650424abec2",
					},
				},
				mustParseRef(t, "quay.io/coreos/prometheus-operator@sha256:5037b4e90dbb03ebdefaa547ddf6a1f748c8eeebeedf6b9d9f0913ad662b5731"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "org",
						Name:      "coreos-prometheus-operator",
						Tag:       "76771fef",
						ID:        "sha256:5037b4e90dbb03ebdefaa547ddf6a1f748c8eeebeedf6b9d9f0913ad662b5731",
					},
				},
				mustParseRef(t, "quay.io/test/etcd.0.9.2:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "org",
						Name:      "test-etcd.0.9.2",
						Tag:       "latest",
						ID:        "",
					},
				},
				mustParseRef(t, "quay.io/test/prometheus.0.15.0:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "org",
						Name:      "test-prometheus.0.15.0",
						Tag:       "latest",
						ID:        "",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &IndexImageMirrorer{
				ImageMirrorer:     tt.fields.ImageMirrorer,
				DatabaseExtractor: tt.fields.DatabaseExtractor,
				Source:            tt.fields.Source,
				Dest:              tt.fields.Dest,
				MaxPathComponents: 2,
			}
			got, err := b.Mirror()
			if tt.wantErr != nil && tt.wantErr != err {
				t.Errorf("wanted err %v but got %v", tt.wantErr, err)
			}
			if tt.wantErr == nil && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			for k, v := range tt.want {
				w, ok := got[k]
				if !ok {
					t.Errorf("couldn't find wanted key %s", k)
					continue
				}
				if w != v {
					t.Errorf("incorrect mapping for %s - have %s, want %s", k, w, v)
				}
			}
			for k, v := range got {
				w, ok := tt.want[k]
				if !ok {
					t.Errorf("got unexpected key %s", k)
					continue
				}
				if w != v {
					t.Errorf("incorrect mapping for %s - have %s, want %s", k, v, w)
				}
			}
		})
	}
}

func mustParseRef(t *testing.T, ref string) imagesource.TypedImageReference {
	parsed, err := imagesource.ParseReference(ref)
	if err != nil {
		t.Error(err)
		t.Fail()
	}
	return parsed
}

func TestMappingForImages(t *testing.T) {
	type args struct {
		images        map[string]struct{}
		src           imagesource.TypedImageReference
		dest          imagesource.TypedImageReference
		maxComponents int
	}
	tests := []struct {
		name        string
		args        args
		wantMapping map[imagesource.TypedImageReference]imagesource.TypedImageReference
		wantErrs    []error
	}{
		{
			name: "tagged image to registry",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image:tag": {},
				},
				src:           mustParseRef(t, "quay.io/my-ns/my-index:1"),
				dest:          mustParseRef(t, "quay.io"),
				maxComponents: 2,
			},

			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image:tag"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my",
						Name:      "image",
						Tag:       "tag",
						ID:        "",
					},
				},
			},
		},
		{
			name: "namespaceless image to registry",
			args: args{
				images: map[string]struct{}{
					"registry.access.redhat.com/ubi8-minimal@sha256:9285da611437622492f9ef4229877efe302589f1401bbd4052e9bb261b3d4387": {},
				},
				src:           mustParseRef(t, "registry.access.redhat.com/ubi8-minimal@sha256:9285da611437622492f9ef4229877efe302589f1401bbd4052e9bb261b3d4387"),
				dest:          mustParseRef(t, "quay.io"),
				maxComponents: 2,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "registry.access.redhat.com/ubi8-minimal@sha256:9285da611437622492f9ef4229877efe302589f1401bbd4052e9bb261b3d4387"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "",
						Name:      "ubi8-minimal",
						Tag:       "51f124fa",
						ID:        "sha256:9285da611437622492f9ef4229877efe302589f1401bbd4052e9bb261b3d4387",
					},
				},
			},
		},
		{
			name: "untagged image to registry",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image": {},
				},
				src:           mustParseRef(t, "quay.io/my-ns/my-index:1"),
				dest:          mustParseRef(t, "quay.io"),
				maxComponents: 2,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my",
						Name:      "image",
						Tag:       "latest",
						ID:        "",
					},
				},
			},
		},
		{
			name: "tagged and untagged images to registry",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image":                  {},
					"docker.io/my/second-image:preserved": {},
				},
				src:           mustParseRef(t, "quay.io/my-ns/my-index:1"),
				dest:          mustParseRef(t, "quay.io"),
				maxComponents: 2,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my",
						Name:      "image",
						Tag:       "latest",
						ID:        "",
					},
				},
				mustParseRef(t, "docker.io/my/second-image:preserved"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my",
						Name:      "second-image",
						Tag:       "preserved",
						ID:        "",
					},
				},
			},
		},
		{
			name: "dest is local file",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image":                  {},
					"docker.io/my/second-image:preserved": {},
					"docker.io/my/digest-image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				src:           mustParseRef(t, "quay.io"),
				dest:          mustParseRef(t, "file://my-local-index/index"),
				maxComponents: 2,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image:latest"): {
					Type: imagesource.DestinationFile,
					Ref: reference.DockerImageReference{
						Registry:  "",
						Namespace: "my-local-index",
						Name:      "index/my/image",
						Tag:       "latest",
						ID:        "",
					},
				},
				mustParseRef(t, "docker.io/my/second-image:preserved"): {
					Type: imagesource.DestinationFile,
					Ref: reference.DockerImageReference{
						Registry:  "",
						Namespace: "my-local-index",
						Name:      "index/my/second-image",
						Tag:       "preserved",
						ID:        "",
					},
				},
				mustParseRef(t, "docker.io/my/digest-image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de"): {
					Type: imagesource.DestinationFile,
					Ref: reference.DockerImageReference{
						Registry:  "",
						Namespace: "my-local-index",
						Name:      "index/my/digest-image",
						Tag:       "dcbadf49",
						ID:        "sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					},
				},
			},
		},
		{
			name: "src is local file, remap images to registry",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image":                  {},
					"docker.io/my/second-image:preserved": {},
					"docker.io/my/digest-image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				src:           mustParseRef(t, "file://my-local-index/index"),
				dest:          mustParseRef(t, "quay.io"),
				maxComponents: 2,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "file://my-local-index/index/my/image"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my",
						Name:      "image",
						Tag:       "latest",
						ID:        "",
					},
				},
				mustParseRef(t, "file://my-local-index/index/my/second-image:preserved"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my",
						Name:      "second-image",
						Tag:       "preserved",
						ID:        "",
					},
				},
				mustParseRef(t, "file://my-local-index/index/my/digest-image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my",
						Name:      "digest-image",
						Tag:       "dcbadf49",
						ID:        "sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					},
				},
			},
		},
		{
			name: "digest image to registry",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				src:           mustParseRef(t, "quay.io/my-ns/my-index:1"),
				dest:          mustParseRef(t, "quay.io"),
				maxComponents: 2,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my",
						Name:      "image",
						Tag:       "a1d77056",
						ID:        "sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					},
				},
			},
		},
		{
			name: "tagged image to registry with port",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image:tag": {},
				},
				src:           mustParseRef(t, "quay.io/my-ns/my-index:1"),
				dest:          mustParseRef(t, "localhost:5000"),
				maxComponents: 2,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image:tag"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "my",
						Name:      "image",
						Tag:       "tag",
						ID:        "",
					},
				},
			},
		},
		{
			name: "digest image to registry with port",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				src:           mustParseRef(t, "quay.io/my-ns/my-index:1"),
				dest:          mustParseRef(t, "localhost:5000"),
				maxComponents: 2,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "localhost:5000",
						Namespace: "my",
						Name:      "image",
						Tag:       "a1d77056",
						ID:        "sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					},
				},
			},
		},
		{
			name: "tagged image to org",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image:tag": {},
				},
				src:           mustParseRef(t, "quay.io/my-ns/my-index:1"),
				dest:          mustParseRef(t, "quay.io/my-org"),
				maxComponents: 2,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image:tag"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my-org",
						Name:      "my-image",
						Tag:       "tag",
						ID:        "",
					},
				},
			},
		},
		{
			name: "untagged image to org",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image": {},
				},
				src:           mustParseRef(t, "quay.io/my-ns/my-index:1"),
				dest:          mustParseRef(t, "quay.io/my-org"),
				maxComponents: 2,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my-org",
						Name:      "my-image",
						Tag:       "latest",
						ID:        "",
					},
				},
			},
		},
		{
			name: "digest image to org",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				src:           mustParseRef(t, "quay.io/my-ns/my-index:1"),
				dest:          mustParseRef(t, "quay.io/my-org"),
				maxComponents: 2,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my-org",
						Name:      "my-image",
						Tag:       "a1d77056",
						ID:        "sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					},
				},
			},
		},
		{
			name: "tagged image to org, max 3 components",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image:tag": {},
				},
				src:           mustParseRef(t, "quay.io/my-ns/my-index:1"),
				dest:          mustParseRef(t, "quay.io/my-org"),
				maxComponents: 3,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image:tag"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my-org",
						Name:      "my/image",
						Tag:       "tag",
						ID:        "",
					},
				},
			},
		},
		{
			name: "untagged image to org, max 3 components",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image": {},
				},
				src:           mustParseRef(t, "quay.io/my-ns/my-index:1"),
				dest:          mustParseRef(t, "quay.io/my-org"),
				maxComponents: 3,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image:latest"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my-org",
						Name:      "my/image",
						Tag:       "latest",
						ID:        "",
					},
				},
			},
		},
		{
			name: "digest image to org, max 3 components",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				src:           mustParseRef(t, "quay.io/my-ns/my-index:1"),
				dest:          mustParseRef(t, "quay.io/my-org"),
				maxComponents: 3,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my-org",
						Name:      "my/image",
						Tag:       "a1d77056",
						ID:        "sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					},
				},
			},
		},
		{
			name: "digest image to nested org, max 3 components",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				src:           mustParseRef(t, "quay.io/my-ns/my-index:1"),
				dest:          mustParseRef(t, "quay.io/my-org/sub-org"),
				maxComponents: 3,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my-org",
						Name:      "sub-org/my-image",
						Tag:       "a1d77056",
						ID:        "sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					},
				},
			},
		},
		{
			name: "digest image to nested org, max 4 components",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				src:           mustParseRef(t, "quay.io/my-ns/my-index:1"),
				dest:          mustParseRef(t, "quay.io/my-org/sub-org"),
				maxComponents: 4,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my-org",
						Name:      "sub-org/my/image",
						Tag:       "a1d77056",
						ID:        "sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					},
				},
			},
		},
		{
			name: "digest image to nested org, no max",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				src:           mustParseRef(t, "quay.io/my-ns/my-index:1"),
				dest:          mustParseRef(t, "quay.io/my-org/sub-org"),
				maxComponents: 0,
			},
			wantMapping: map[imagesource.TypedImageReference]imagesource.TypedImageReference{
				mustParseRef(t, "docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de"): {
					Type: imagesource.DestinationRegistry,
					Ref: reference.DockerImageReference{
						Registry:  "quay.io",
						Namespace: "my-org",
						Name:      "sub-org/my/image",
						Tag:       "a1d77056",
						ID:        "sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMapping, gotErrs := mappingForImages(tt.args.images, tt.args.src, tt.args.dest, tt.args.maxComponents)

			if tt.wantErrs != nil && !reflect.DeepEqual(tt.wantErrs, gotErrs) {
				t.Errorf("wanted err %v but got %v", tt.wantErrs, gotErrs)
			}
			if tt.wantErrs == nil && gotErrs != nil {
				t.Errorf("unexpected error: %v", gotErrs)
			}

			for k, v := range tt.wantMapping {
				w, ok := gotMapping[k]
				if !ok {
					t.Errorf("couldn't find wanted key %#v", k)
					continue
				}
				if w != v {
					t.Errorf("incorrect mapping for %s - have %#v, want %#v", k, w, v)
				}
			}
			for k, v := range gotMapping {
				w, ok := tt.wantMapping[k]
				if !ok {
					t.Errorf("got unexpected key %#v", k)
					continue
				}
				if w != v {
					t.Errorf("incorrect mapping for %s - have %s, want %s", k, v, w)
				}
			}
		})
	}
}
