package dockercredentials

import (
	"net/url"
	"sync"

	"github.com/distribution/distribution/v3/registry/client/auth"
	"github.com/openshift/library-go/pkg/image/registryclient"
	"go.podman.io/image/v5/docker/reference"
	containertypes "go.podman.io/image/v5/types"
)

// NewCredentialStoreFactory returns an entity capable of creating a CredentialStore
func NewCredentialStoreFactory(path string) (registryclient.CredentialStoreFactory, error) {
	authResolver, err := NewAuthResolver(path)
	if err != nil {
		return nil, err
	}
	return &credentialStoreFactory{authResolver}, nil
}

type credentialStoreFactory struct {
	authResolver *AuthResolver
}

func (c *credentialStoreFactory) CredentialStoreFor(image string) auth.CredentialStore {
	nocreds := registryclient.NoCredentials
	if c.authResolver == nil {
		return nocreds
	}

	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return nocreds
	}

	authCfg, err := c.authResolver.findAuthentication(ref, reference.Domain(ref))
	if err != nil {
		return nocreds
	}

	return NewDynamicCredentialStore(&containertypes.DockerAuthConfig{
		Username:      authCfg.Username,
		Password:      authCfg.Password,
		IdentityToken: authCfg.IdentityToken,
	})
}

func NewDynamicCredentialStore(auth *containertypes.DockerAuthConfig) auth.CredentialStore {
	return &DynamicCredentialStore{authConfig: auth}
}

type DynamicCredentialStore struct {
	authConfig *containertypes.DockerAuthConfig
	mutex      sync.Mutex
}

func (dcs *DynamicCredentialStore) Basic(*url.URL) (string, string) {
	if dcs.authConfig == nil {
		return "", ""
	}
	dcs.mutex.Lock()
	defer dcs.mutex.Unlock()

	return dcs.authConfig.Username, dcs.authConfig.Password
}

func (dcs *DynamicCredentialStore) RefreshToken(*url.URL, string) string {
	if dcs.authConfig == nil {
		return ""
	}
	dcs.mutex.Lock()
	defer dcs.mutex.Unlock()

	return dcs.authConfig.IdentityToken
}

func (dcs *DynamicCredentialStore) SetRefreshToken(u *url.URL, service, token string) {
	if dcs.authConfig != nil {
		dcs.mutex.Lock()
		defer dcs.mutex.Unlock()

		dcs.authConfig.IdentityToken = token
	}
}
