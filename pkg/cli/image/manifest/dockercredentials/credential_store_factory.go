package dockercredentials

import (
	"github.com/docker/distribution/registry/client/auth"

	"github.com/openshift/library-go/pkg/image/registryclient"
)

// NewCredentialStoreFactory returns an entity capable of creating a CredentialStore
func NewCredentialStoreFactory(path string) (registryclient.CredentialStoreFactory, error) {
	// test ability to load the registry config
	if _, err := NewCredentialStore(path); err != nil {
		return nil, err
	}
	return &credentialStoreFactory{path: path}, nil
}

type credentialStoreFactory struct {
	path string
}

func (c *credentialStoreFactory) CredentialStoreFor(image string) (creds auth.CredentialStore) {
	// error noticed in NewCredentialStoreFactory
	store, _ := NewCredentialStore(c.path)
	return store
}
