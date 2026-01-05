package schema1

import (
	"context"
	"errors"

	"github.com/distribution/distribution/v3"
	"github.com/distribution/distribution/v3/manifest"
	"github.com/distribution/reference"
	"github.com/docker/libtrust"
	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// ReferenceManifestBuilder is a type for constructing manifests from schema1
// dependencies.
type ReferenceManifestBuilder struct {
	Manifest
	pk libtrust.PrivateKey
}

// NewReferenceManifestBuilder is used to build new manifests for the current
// schema version using schema1 dependencies.
//
// Deprecated: Docker Image Manifest v2, Schema 1 is deprecated since 2015.
// Use Docker Image Manifest v2, Schema 2, or the OCI Image Specification.
func NewReferenceManifestBuilder(pk libtrust.PrivateKey, ref reference.Named, architecture string) *ReferenceManifestBuilder {
	tag := ""
	if tagged, isTagged := ref.(reference.Tagged); isTagged {
		tag = tagged.Tag()
	}

	return &ReferenceManifestBuilder{
		Manifest: Manifest{
			Versioned: manifest.Versioned{
				SchemaVersion: 1,
			},
			Name:         ref.Name(),
			Tag:          tag,
			Architecture: architecture,
		},
		pk: pk,
	}
}

func (mb *ReferenceManifestBuilder) Build(ctx context.Context) (distribution.Manifest, error) {
	m := mb.Manifest
	if len(m.FSLayers) == 0 {
		return nil, errors.New("cannot build manifest with zero layers or history")
	}

	m.FSLayers = make([]FSLayer, len(mb.Manifest.FSLayers))
	m.History = make([]History, len(mb.Manifest.History))
	copy(m.FSLayers, mb.Manifest.FSLayers)
	copy(m.History, mb.Manifest.History)

	return Sign(&m, mb.pk)
}

// AppendReference adds a reference to the current ManifestBuilder.
//
// Deprecated: Docker Image Manifest v2, Schema 1 is deprecated since 2015.
// Use Docker Image Manifest v2, Schema 2, or the OCI Image Specification.
func (mb *ReferenceManifestBuilder) AppendReference(d v1.Descriptor) error {
	// Entries need to be prepended
	mb.Manifest.FSLayers = append([]FSLayer{{BlobSum: d.Digest}}, mb.Manifest.FSLayers...)
	return nil
}

// References returns the current references added to this builder.
//
// Deprecated: Docker Image Manifest v2, Schema 1 is deprecated since 2015.
// Use Docker Image Manifest v2, Schema 2, or the OCI Image Specification.
func (mb *ReferenceManifestBuilder) References() []v1.Descriptor {
	refs := make([]v1.Descriptor, len(mb.Manifest.FSLayers))
	for i := range mb.Manifest.FSLayers {
		layerDigest := mb.Manifest.FSLayers[i].BlobSum
		history := mb.Manifest.History[i]
		ref := Reference{layerDigest, 0, history}
		refs[i] = ref.Descriptor()
	}
	return refs
}

// Reference describes a Manifest v2, schema version 1 dependency.
// An FSLayer associated with a history entry.
//
// Deprecated: Docker Image Manifest v2, Schema 1 is deprecated since 2015.
// Use Docker Image Manifest v2, Schema 2, or the OCI Image Specification.
type Reference struct {
	Digest  digest.Digest
	Size    int64 // if we know it, set it for the descriptor.
	History History
}

// Descriptor describes a reference.
//
// Deprecated: Docker Image Manifest v2, Schema 1 is deprecated since 2015.
// Use Docker Image Manifest v2, Schema 2, or the OCI Image Specification.
func (r Reference) Descriptor() v1.Descriptor {
	return v1.Descriptor{
		MediaType: MediaTypeManifestLayer,
		Digest:    r.Digest,
		Size:      r.Size,
	}
}
