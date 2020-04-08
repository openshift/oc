package catalog

import (
	"github.com/openshift/library-go/pkg/image/reference"
	"reflect"
	"testing"

	"github.com/openshift/oc/pkg/cli/image/imagesource"
)

func existingExtractor(dir string) DatabaseExtractorFunc {
	return func(from imagesource.TypedImageReference) (s string, e error) {
		return dir, nil
	}
}

func noopMirror(map[string]Target) error {
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
		want    map[string]Target
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
			want: map[string]Target{
				"quay.io/test/prometheus.0.14.0": {
					WithTag: "localhost:5000/test/prometheus.0.14.0:ce7b31e2",
				},
				"quay.io/coreos/etcd-operator@sha256:db563baa8194fcfe39d1df744ed70024b0f1f9e9b55b5923c2f3a413c44dc6b8": {
					WithTag:    "localhost:5000/coreos/etcd-operator:b56e2636",
					WithDigest: "localhost:5000/coreos/etcd-operator@sha256:db563baa8194fcfe39d1df744ed70024b0f1f9e9b55b5923c2f3a413c44dc6b8",
				},
				"quay.io/test/etcd.0.9.0": {
					WithTag: "localhost:5000/test/etcd.0.9.0:eee5548c",
				},
				"quay.io/coreos/prometheus-operator@sha256:0e92dd9b5789c4b13d53e1319d0a6375bcca4caaf0d698af61198061222a576d": {
					WithTag:    "localhost:5000/coreos/prometheus-operator:7f39d12d",
					WithDigest: "localhost:5000/coreos/prometheus-operator@sha256:0e92dd9b5789c4b13d53e1319d0a6375bcca4caaf0d698af61198061222a576d",
				},
				"quay.io/coreos/prometheus-operator@sha256:3daa69a8c6c2f1d35dcf1fe48a7cd8b230e55f5229a1ded438f687debade5bcf": {
					WithTag:    "localhost:5000/coreos/prometheus-operator:1ebe036a",
					WithDigest: "localhost:5000/coreos/prometheus-operator@sha256:3daa69a8c6c2f1d35dcf1fe48a7cd8b230e55f5229a1ded438f687debade5bcf",
				},
				"quay.io/test/prometheus.0.22.2": {
					WithTag: "localhost:5000/test/prometheus.0.22.2:d044a13d",
				},
				"quay.io/coreos/etcd-operator@sha256:c0301e4686c3ed4206e370b42de5a3bd2229b9fb4906cf85f3f30650424abec2": {
					WithTag:    "localhost:5000/coreos/etcd-operator:2f1eb95",
					WithDigest: "localhost:5000/coreos/etcd-operator@sha256:c0301e4686c3ed4206e370b42de5a3bd2229b9fb4906cf85f3f30650424abec2",
				},
				"quay.io/coreos/prometheus-operator@sha256:5037b4e90dbb03ebdefaa547ddf6a1f748c8eeebeedf6b9d9f0913ad662b5731": {
					WithTag:    "localhost:5000/coreos/prometheus-operator:76771fef",
					WithDigest: "localhost:5000/coreos/prometheus-operator@sha256:5037b4e90dbb03ebdefaa547ddf6a1f748c8eeebeedf6b9d9f0913ad662b5731",
				},
				"quay.io/test/etcd.0.9.2": {
					WithTag: "localhost:5000/test/etcd.0.9.2:f0e557b2",
				},
				"quay.io/test/prometheus.0.15.0": {
					WithTag: "localhost:5000/test/prometheus.0.15.0:b5586049",
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
			want: map[string]Target{
				"quay.io/test/prometheus.0.14.0": {
					WithTag: "localhost:5000/org/test-prometheus.0.14.0:ce7b31e2",
				},
				"quay.io/coreos/etcd-operator@sha256:db563baa8194fcfe39d1df744ed70024b0f1f9e9b55b5923c2f3a413c44dc6b8": {
					WithTag:    "localhost:5000/org/coreos-etcd-operator:b56e2636",
					WithDigest: "localhost:5000/org/coreos-etcd-operator@sha256:db563baa8194fcfe39d1df744ed70024b0f1f9e9b55b5923c2f3a413c44dc6b8",
				},
				"quay.io/test/etcd.0.9.0": {
					WithTag: "localhost:5000/org/test-etcd.0.9.0:eee5548c",
				},
				"quay.io/coreos/prometheus-operator@sha256:0e92dd9b5789c4b13d53e1319d0a6375bcca4caaf0d698af61198061222a576d": {
					WithTag:    "localhost:5000/org/coreos-prometheus-operator:7f39d12d",
					WithDigest: "localhost:5000/org/coreos-prometheus-operator@sha256:0e92dd9b5789c4b13d53e1319d0a6375bcca4caaf0d698af61198061222a576d",
				},
				"quay.io/coreos/prometheus-operator@sha256:3daa69a8c6c2f1d35dcf1fe48a7cd8b230e55f5229a1ded438f687debade5bcf": {
					WithTag:    "localhost:5000/org/coreos-prometheus-operator:1ebe036a",
					WithDigest: "localhost:5000/org/coreos-prometheus-operator@sha256:3daa69a8c6c2f1d35dcf1fe48a7cd8b230e55f5229a1ded438f687debade5bcf",
				},
				"quay.io/test/prometheus.0.22.2": {
					WithTag: "localhost:5000/org/test-prometheus.0.22.2:d044a13d",
				},
				"quay.io/coreos/etcd-operator@sha256:c0301e4686c3ed4206e370b42de5a3bd2229b9fb4906cf85f3f30650424abec2": {
					WithTag:    "localhost:5000/org/coreos-etcd-operator:2f1eb95",
					WithDigest: "localhost:5000/org/coreos-etcd-operator@sha256:c0301e4686c3ed4206e370b42de5a3bd2229b9fb4906cf85f3f30650424abec2",
				},
				"quay.io/coreos/prometheus-operator@sha256:5037b4e90dbb03ebdefaa547ddf6a1f748c8eeebeedf6b9d9f0913ad662b5731": {
					WithTag:    "localhost:5000/org/coreos-prometheus-operator:76771fef",
					WithDigest: "localhost:5000/org/coreos-prometheus-operator@sha256:5037b4e90dbb03ebdefaa547ddf6a1f748c8eeebeedf6b9d9f0913ad662b5731",
				},
				"quay.io/test/etcd.0.9.2": {
					WithTag: "localhost:5000/org/test-etcd.0.9.2:f0e557b2",
				},
				"quay.io/test/prometheus.0.15.0": {
					WithTag: "localhost:5000/org/test-prometheus.0.15.0:b5586049",
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
			want: map[string]Target{
				"quay.io/test/prometheus.0.14.0": {
					WithTag: "quay.io/org/test-prometheus.0.14.0:ce7b31e2",
				},
				"quay.io/coreos/etcd-operator@sha256:db563baa8194fcfe39d1df744ed70024b0f1f9e9b55b5923c2f3a413c44dc6b8": {
					WithTag:    "quay.io/org/coreos-etcd-operator:b56e2636",
					WithDigest: "quay.io/org/coreos-etcd-operator@sha256:db563baa8194fcfe39d1df744ed70024b0f1f9e9b55b5923c2f3a413c44dc6b8",
				},
				"quay.io/test/etcd.0.9.0": {
					WithTag: "quay.io/org/test-etcd.0.9.0:eee5548c",
				},
				"quay.io/coreos/prometheus-operator@sha256:0e92dd9b5789c4b13d53e1319d0a6375bcca4caaf0d698af61198061222a576d": {
					WithTag:    "quay.io/org/coreos-prometheus-operator:7f39d12d",
					WithDigest: "quay.io/org/coreos-prometheus-operator@sha256:0e92dd9b5789c4b13d53e1319d0a6375bcca4caaf0d698af61198061222a576d",
				},
				"quay.io/coreos/prometheus-operator@sha256:3daa69a8c6c2f1d35dcf1fe48a7cd8b230e55f5229a1ded438f687debade5bcf": {
					WithTag:    "quay.io/org/coreos-prometheus-operator:1ebe036a",
					WithDigest: "quay.io/org/coreos-prometheus-operator@sha256:3daa69a8c6c2f1d35dcf1fe48a7cd8b230e55f5229a1ded438f687debade5bcf",
				},
				"quay.io/test/prometheus.0.22.2": {
					WithTag: "quay.io/org/test-prometheus.0.22.2:d044a13d",
				},
				"quay.io/coreos/etcd-operator@sha256:c0301e4686c3ed4206e370b42de5a3bd2229b9fb4906cf85f3f30650424abec2": {
					WithTag:    "quay.io/org/coreos-etcd-operator:2f1eb95",
					WithDigest: "quay.io/org/coreos-etcd-operator@sha256:c0301e4686c3ed4206e370b42de5a3bd2229b9fb4906cf85f3f30650424abec2",
				},
				"quay.io/coreos/prometheus-operator@sha256:5037b4e90dbb03ebdefaa547ddf6a1f748c8eeebeedf6b9d9f0913ad662b5731": {
					WithTag:    "quay.io/org/coreos-prometheus-operator:76771fef",
					WithDigest: "quay.io/org/coreos-prometheus-operator@sha256:5037b4e90dbb03ebdefaa547ddf6a1f748c8eeebeedf6b9d9f0913ad662b5731",
				},
				"quay.io/test/etcd.0.9.2": {
					WithTag: "quay.io/org/test-etcd.0.9.2:f0e557b2",
				},
				"quay.io/test/prometheus.0.15.0": {
					WithTag: "quay.io/org/test-prometheus.0.15.0:b5586049",
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

func mustParseRef(t *testing.T, ref string) reference.DockerImageReference {
	parsed, err := reference.Parse(ref)
	if err != nil {
		t.Error(err)
		t.Fail()
	}
	return parsed
}

func TestMappingForImages(t *testing.T) {
	type args struct {
		images        map[string]struct{}
		dest          imagesource.TypedImageReference
		maxComponents int
	}
	tests := []struct {
		name        string
		args        args
		wantMapping map[string]Target
		wantErrs    []error
	}{
		{
			name: "tagged image to registry",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image:tag": {},
				},
				dest: imagesource.TypedImageReference{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "quay.io"),
				},
				maxComponents: 2,
			},
			wantMapping: map[string]Target{
				"docker.io/my/image:tag": {
					WithDigest: "",
					WithTag:    "quay.io/my/image:tag",
				},
			},
		},
		{
			name: "untagged image to registry",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image": {},
				},
				dest: imagesource.TypedImageReference{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "quay.io"),
				},
				maxComponents: 2,
			},
			wantMapping: map[string]Target{
				"docker.io/my/image": {
					WithDigest: "",
					WithTag:    "quay.io/my/image:4f9407ca",
				},
			},
		},
		{
			name: "digest image to registry",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				dest: imagesource.TypedImageReference{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "quay.io"),
				},
				maxComponents: 2,
			},
			wantMapping: map[string]Target{
				"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {
					WithDigest: "quay.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					WithTag:    "quay.io/my/image:a1d77056",
				},
			},
		},
		{
			name: "tagged image to registry with port",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image:tag": {},
				},
				dest: imagesource.TypedImageReference{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "localhost:5000"),
				},
				maxComponents: 2,
			},
			wantMapping: map[string]Target{
				"docker.io/my/image:tag": {
					WithDigest: "",
					WithTag:    "localhost:5000/my/image:tag",
				},
			},
		},
		{
			name: "digest image to registry with port",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				dest: imagesource.TypedImageReference{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "localhost:5000"),
				},
				maxComponents: 2,
			},
			wantMapping: map[string]Target{
				"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {
					WithDigest: "localhost:5000/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					WithTag:    "localhost:5000/my/image:a1d77056",
				},
			},
		},
		{
			name: "tagged image to org",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image:tag": {},
				},
				dest: imagesource.TypedImageReference{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "quay.io/my-org"),
				},
				maxComponents: 2,
			},
			wantMapping: map[string]Target{
				"docker.io/my/image:tag": {
					WithDigest: "",
					WithTag:    "quay.io/my-org/my-image:tag",
				},
			},
		},
		{
			name: "untagged image to org",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image": {},
				},
				dest: imagesource.TypedImageReference{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "quay.io/my-org"),
				},
				maxComponents: 2,
			},
			wantMapping: map[string]Target{
				"docker.io/my/image": {
					WithDigest: "",
					WithTag:    "quay.io/my-org/my-image:4f9407ca",
				},
			},
		},
		{
			name: "digest image to org",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				dest: imagesource.TypedImageReference{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "quay.io/my-org"),
				},
				maxComponents: 2,
			},
			wantMapping: map[string]Target{
				"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {
					WithDigest: "quay.io/my-org/my-image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					WithTag:    "quay.io/my-org/my-image:a1d77056",
				},
			},
		},
		{
			name: "tagged image to org, max 3 components",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image:tag": {},
				},
				dest: imagesource.TypedImageReference{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "quay.io/my-org"),
				},
				maxComponents: 3,
			},
			wantMapping: map[string]Target{
				"docker.io/my/image:tag": {
					WithDigest: "",
					WithTag:    "quay.io/my-org/my/image:tag",
				},
			},
		},
		{
			name: "untagged image to org, max 3 components",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image": {},
				},
				dest: imagesource.TypedImageReference{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "quay.io/my-org"),
				},
				maxComponents: 3,
			},
			wantMapping: map[string]Target{
				"docker.io/my/image": {
					WithDigest: "",
					WithTag:    "quay.io/my-org/my/image:4f9407ca",
				},
			},
		},
		{
			name: "digest image to org, max 3 components",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				dest: imagesource.TypedImageReference{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "quay.io/my-org"),
				},
				maxComponents: 3,
			},
			wantMapping: map[string]Target{
				"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {
					WithDigest: "quay.io/my-org/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					WithTag:    "quay.io/my-org/my/image:a1d77056",
				},
			},
		},
		{
			name: "digest image to nested org, max 3 components",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				dest: imagesource.TypedImageReference{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "quay.io/my-org/sub-org"),
				},
				maxComponents: 3,
			},
			wantMapping: map[string]Target{
				"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {
					WithDigest: "quay.io/my-org/sub-org/my-image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					WithTag:    "quay.io/my-org/sub-org/my-image:a1d77056",
				},
			},
		},
		{
			name: "digest image to nested org, max 4 components",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				dest: imagesource.TypedImageReference{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "quay.io/my-org/sub-org"),
				},
				maxComponents: 4,
			},
			wantMapping: map[string]Target{
				"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {
					WithDigest: "quay.io/my-org/sub-org/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					WithTag:    "quay.io/my-org/sub-org/my/image:a1d77056",
				},
			},
		},
		{
			name: "digest image to nested org, no max",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				dest: imagesource.TypedImageReference{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "quay.io/my-org/sub-org"),
				},
				maxComponents: 0,
			},
			wantMapping: map[string]Target{
				"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {
					WithDigest: "quay.io/my-org/sub-org/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					WithTag:    "quay.io/my-org/sub-org/my/image:a1d77056",
				},
			},
		},
		{
			name: "digest image to nested org, max 1",
			args: args{
				images: map[string]struct{}{
					"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {},
				},
				dest: imagesource.TypedImageReference{
					Type: imagesource.DestinationRegistry,
					Ref:  mustParseRef(t, "quay.io/my-org/sub-org"),
				},
				maxComponents: 1,
			},
			wantMapping: map[string]Target{
				"docker.io/my/image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de": {
					WithDigest: "quay.io/my-org-sub-org-my-image@sha256:154d7e0295a94fb3d2a97309d711186a98a7308da37a5cd3d50360c6b2ba57de",
					WithTag:    "quay.io/my-org-sub-org-my-image:a1d77056",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMapping, gotErrs := mappingForImages(tt.args.images, tt.args.dest, tt.args.maxComponents)
			if !reflect.DeepEqual(gotMapping, tt.wantMapping) {
				t.Errorf("mappingForImages() gotMapping = %v, want %v", gotMapping, tt.wantMapping)
			}
			if !reflect.DeepEqual(gotErrs, tt.wantErrs) {
				t.Errorf("mappingForImages() gotErrs = %v, want %v", gotErrs, tt.wantErrs)
			}
		})
	}
}
