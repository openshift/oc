package release

import (
	"context"

	"k8s.io/klog"

	"github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/library-go/pkg/image/registryclient"
	"github.com/openshift/oc/pkg/cli/image/imagesource"
	imagemanifest "github.com/openshift/oc/pkg/cli/image/manifest"
)

func verifyImageExists(fromContext *registryclient.Context, fileDir string, insecure bool, include imagemanifest.FilterFunc, ref reference.DockerImageReference) bool {
	from := imagesource.TypedImageReference{Type: imagesource.DestinationRegistry, Ref: ref}
	ctx := context.Background()
	fromOptions := &imagesource.Options{
		FileDir:         fileDir,
		Insecure:        insecure,
		RegistryContext: fromContext,
	}

	repo, err := fromOptions.Repository(ctx, from)
	if err != nil {
		klog.V(2).Infof("unable to connect to image repository %s: %v", from.String(), err)
		return false
	}
	_, _, err = imagemanifest.FirstManifest(ctx, from.Ref, repo, include)
	if err != nil {
		if imagemanifest.IsImageNotFound(err) {
			return false
		}
		klog.V(2).Infof("unable to read image %s: %v", from.String(), err)
		return false
	}
	return true
}
