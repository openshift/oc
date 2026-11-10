package manifest

import (
	"context"
	"fmt"
	"testing"

	"github.com/distribution/distribution/v3"
	"github.com/distribution/distribution/v3/manifest/manifestlist"
	"github.com/distribution/distribution/v3/manifest/ocischema"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	imagespecv1 "github.com/opencontainers/image-spec/specs-go/v1"

	imagereference "github.com/openshift/library-go/pkg/image/reference"
)

type fakeManifestService struct {
	distribution.ManifestService
	manifests map[digest.Digest]distribution.Manifest
}

func (f fakeManifestService) Get(_ context.Context, dgst digest.Digest, _ ...distribution.ManifestServiceOption) (distribution.Manifest, error) {
	m, ok := f.manifests[dgst]
	if !ok {
		return nil, fmt.Errorf("manifest %s not found", dgst)
	}
	return m, nil
}

// ociIndex returns an OCI image index with one image manifest per architecture, and a service serving the images.
func ociIndex(t *testing.T, arches ...string) (*ocischema.DeserializedImageIndex, fakeManifestService) {
	service := fakeManifestService{manifests: map[digest.Digest]distribution.Manifest{}}
	var descriptors []imagespecv1.Descriptor
	for _, arch := range arches {
		image, err := ocischema.FromStruct(ocischema.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: imagespecv1.MediaTypeImageManifest,
			Config: imagespecv1.Descriptor{
				MediaType: imagespecv1.MediaTypeImageConfig,
				Digest:    digest.FromString(arch),
				Size:      int64(len(arch)),
			},
			Layers: []imagespecv1.Descriptor{},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, payload, err := image.Payload()
		if err != nil {
			t.Fatal(err)
		}
		dgst := digest.FromBytes(payload)
		service.manifests[dgst] = image
		descriptors = append(descriptors, imagespecv1.Descriptor{
			MediaType:   imagespecv1.MediaTypeImageManifest,
			Digest:      dgst,
			Size:        int64(len(payload)),
			Platform:    &imagespecv1.Platform{OS: "linux", Architecture: arch},
			Annotations: map[string]string{"arch": arch},
		})
	}
	index, err := ocischema.FromDescriptors(descriptors, map[string]string{"index": "annotation"})
	if err != nil {
		t.Fatal(err)
	}
	return index, service
}

func TestManifestsFromListOCIIndex(t *testing.T) {
	index, service := ociIndex(t, "amd64", "arm64")
	_, payload, err := index.Payload()
	if err != nil {
		t.Fatal(err)
	}
	indexDigest := digest.FromBytes(payload)

	all, list, listDigest, err := ManifestsFromList(context.Background(), indexDigest, index, service, imagereference.DockerImageReference{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if list != index || listDigest != indexDigest {
		t.Errorf("expected the index %s back, got %T %s", indexDigest, list, listDigest)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 child manifests, got %d", len(all))
	}
	for _, d := range index.Manifests {
		if all[d.Digest] != service.manifests[d.Digest] {
			t.Errorf("child %s missing from %v", d.Digest, all)
		}
	}
}

func TestProcessManifestListOCIIndexStaysOCI(t *testing.T) {
	index, service := ociIndex(t, "amd64", "arm64", "s390x")
	_, payload, err := index.Payload()
	if err != nil {
		t.Fatal(err)
	}
	filterFn := func(d *manifestlist.ManifestDescriptor, _ bool) bool { return d.Platform.Architecture != "s390x" }

	children, m, dgst, err := ProcessManifestList(context.Background(), digest.FromBytes(payload), index, service, imagereference.DockerImageReference{Name: "test"}, filterFn, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
	filtered, ok := m.(*ocischema.DeserializedImageIndex)
	if !ok {
		t.Fatalf("expected an OCI image index, got %T", m)
	}
	mediaType, filteredPayload, err := filtered.Payload()
	if err != nil {
		t.Fatal(err)
	}
	if mediaType != imagespecv1.MediaTypeImageIndex {
		t.Errorf("unexpected media type %s", mediaType)
	}
	if dgst != digest.FromBytes(filteredPayload) {
		t.Errorf("digest %s does not match the filtered payload", dgst)
	}
	if filtered.Annotations["index"] != "annotation" {
		t.Errorf("index annotations lost: %v", filtered.Annotations)
	}
	if len(filtered.Manifests) != 2 {
		t.Fatalf("expected 2 descriptors, got %d", len(filtered.Manifests))
	}
	for i, d := range filtered.Manifests {
		if d.MediaType != imagespecv1.MediaTypeImageManifest || d.Platform == nil || d.Platform.Architecture != index.Manifests[i].Platform.Architecture || d.Annotations["arch"] != d.Platform.Architecture {
			t.Errorf("descriptor %d not preserved: %#v", i, d)
		}
	}
}
