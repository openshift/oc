package imagesource

import (
	"context"
	"errors"
	"net/http"

	"github.com/distribution/distribution/v3"

	"github.com/openshift/library-go/pkg/image/registryclient/v2"
)

func NewDryRun(ref TypedImageReference) (distribution.Repository, error) {
	return registryclient.NewContext(dryRunRoundTripper, dryRunRoundTripper).
		Repository(context.Background(), ref.Ref.RegistryURL(), ref.Ref.RepositoryName(), false)
}

var dryRunRoundTripper = errorRoundTripper{errors.New("dry-run repository is not available")}

type errorRoundTripper struct {
	err error
}

func (rt errorRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, rt.err
}
