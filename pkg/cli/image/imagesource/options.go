package imagesource

import (
	"context"
	"fmt"
	"net/url"

	"github.com/docker/distribution"
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

// Repository retrieves the appropriate repository implementation and ManifestService for the given typed reference.
func (o *Options) Repository(ctx context.Context, ref TypedImageReference) (distribution.Repository, distribution.ManifestService, error) {
	o.RegistryContext.RepositoryRetriever = &registryContext{o.RegistryContext}
	switch ref.Type {
	case DestinationRegistry:
		repo, err := o.RegistryContext.Repository(ctx, ref.Ref.DockerClientDefaults().RegistryURL(), ref.Ref.RepositoryName(), o.Insecure)
		if err != nil {
			return nil, nil, err
		}
		manifests, err := repo.Manifests(context.TODO())
		if err != nil {
			return nil, nil, fmt.Errorf("unable to get local manifest service: %v", err)
		}
		return repo, manifests, nil
	case DestinationFile:
		driver := &fileDriver{
			BaseDir: o.FileDir,
		}
		repo, err := driver.Repository(ctx, ref.Ref.DockerClientDefaults().RegistryURL(), ref.Ref.RepositoryName(), o.Insecure)
		if err != nil {
			return nil, nil, err
		}
		return repo, nil, nil
	case DestinationS3:
		driver := &s3Driver{
			Creds:    o.RegistryContext.Credentials,
			CopyFrom: o.AttemptS3BucketCopy,
		}
		url := ref.Ref.DockerClientDefaults().RegistryURL()
		repo, err := driver.Repository(ctx, url, ref.Ref.RepositoryName(), o.Insecure)
		if err != nil {
			return nil, nil, err
		}
		return repo, nil, nil
	default:
		return nil, nil, fmt.Errorf("unrecognized image reference type %s", ref.Type)
	}
}

type registryContext struct {
	*registryclient.Context
}

func (c *registryContext) Repository(ctx context.Context, registry *url.URL, repoName string, insecure bool) (distribution.Repository, error) {
	var repo distribution.Repository
	var err error
	if len(c.ImageSources) == 0 {
		repo, err = c.Repository(ctx, registry, repoName, insecure)
		if err != nil {
			return nil, err
		}
	}
	for _, ics := range c.ImageSources {
		repo, err = c.Repository(ctx, ics.RegistryURL(), ics.RepositoryName(), insecure)
		if err != nil {
			continue
		}

		// it would be nice to simply return ManifestService here, as we'll need it, but this will not satifsy library-go's RepositoryRetriever interface
		_, err := repo.Manifests(context.TODO())
		if err != nil {
			err = fmt.Errorf("unable to get local manifest service: %v", err)
			continue
		}
		break
	}
	return repo, nil
}

// ExpandWildcard expands the provided typed reference (which is known to have an expansion)
// to a set of explicit image references.
func (o *Options) ExpandWildcard(ref TypedImageReference) ([]TypedImageReference, error) {
	reSearch, err := buildTagSearchRegexp(ref.Ref.Tag)
	if err != nil {
		return nil, err
	}

	// lookup tags that match the search
	repo, _, err := o.Repository(context.Background(), ref)
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
