package imagesource

import (
	"context"
	"fmt"

	"github.com/docker/distribution"
	imagereference "github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/library-go/pkg/image/registryclient"
	"k8s.io/klog/v2"
)

// Options contains inputs necessary to build a repository implementation for a reference.
type Options struct {
	FileDir             string
	Insecure            bool
	AttemptS3BucketCopy []string
	RegistryContext     *registryclient.Context
}

// RepositoryWithAlternateRef retrieves the appropriate repository implementation for the given typed reference.
// In case of DestinationRegistry type, also returns preferred DockerImageReference that may have been updated with alternative image sources
// from ImageContentSourcePolicy objects.
func (o *Options) RepositoryWithAlternateRef(ctx context.Context, ref TypedImageReference) (distribution.Repository, imagereference.DockerImageReference, error) {
	switch ref.Type {
	case DestinationRegistry:
		return o.RegistryContext.RepositoryWithAlternateReference(ctx, ref.Ref, o.Insecure)
	default:
		return nil, ref.Ref, fmt.Errorf("unexpected type for call to RepositoryWithAlternateRef %s, call Repository instead", ref.Type)
	}
}

// Repository retrieves the appropriate repository implementation for the given typed reference.
func (o *Options) Repository(ctx context.Context, ref TypedImageReference) (distribution.Repository, error) {
	switch ref.Type {
	case DestinationRegistry:
		return o.RegistryContext.Repository(ctx, ref.Ref.DockerClientDefaults().RegistryURL(), ref.Ref.RepositoryName(), o.Insecure)
	case DestinationFile:
		driver := &fileDriver{
			BaseDir: o.FileDir,
		}
		return driver.Repository(ctx, ref.Ref.DockerClientDefaults().RegistryURL(), ref.Ref.RepositoryName(), o.Insecure)
	case DestinationS3:
		driver := &s3Driver{
			Creds:    o.RegistryContext.Credentials,
			CopyFrom: o.AttemptS3BucketCopy,
		}
		url := ref.Ref.DockerClientDefaults().RegistryURL()
		return driver.Repository(ctx, url, ref.Ref.RepositoryName(), o.Insecure)
	default:
		return nil, fmt.Errorf("unrecognized image reference type %s", ref.Type)
	}
}

// ExpandWildcard expands the provided typed reference (which is known to have an expansion)
// to a set of explicit image references.
func (o *Options) ExpandWildcard(ref TypedImageReference) ([]TypedImageReference, error) {
	reSearch, err := buildTagSearchRegexp(ref.Ref.Tag)
	if err != nil {
		return nil, err
	}

	// lookup tags that match the search
	repo, err := o.Repository(context.Background(), ref)
	if err != nil {
		return nil, err
	}
	tags, err := repo.Tags(context.Background()).All(context.Background())
	if err != nil {
		return nil, err
	}
	klog.V(5).Infof("Search for %q (%s) found: %v", ref.Ref.Tag, reSearch.String(), tags)
	refs := make([]TypedImageReference, 0, len(tags))
	for _, tag := range tags {
		if !reSearch.MatchString(tag) {
			continue
		}
		copied := ref
		copied.Ref.Tag = tag
		refs = append(refs, copied)
	}
	return refs, nil
}
