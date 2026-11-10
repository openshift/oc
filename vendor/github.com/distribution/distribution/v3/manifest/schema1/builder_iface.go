package schema1

import (
	"context"

	"github.com/distribution/distribution/v3"
)

// ManifestBuilder is carried locally; upstream removed ManifestBuilder.
type ManifestBuilder interface {
	Build(ctx context.Context) (distribution.Manifest, error)
	References() []distribution.Descriptor
	AppendReference(dependency any) error
}
