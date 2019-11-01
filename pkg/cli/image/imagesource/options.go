package imagesource

import (
	"context"
	"fmt"

	"github.com/docker/distribution"
	"github.com/openshift/library-go/pkg/image/registryclient"
)

type Options struct {
	FileDir             string
	Insecure            bool
	AttemptS3BucketCopy []string
	RegistryContext     *registryclient.Context
}

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
