package dockercredentials

import (
	"github.com/openshift/library-go/pkg/image/registryclient"
	"testing"

	"github.com/openshift/oc/pkg/helpers/image/credentialprovider"
)

func Test_CredentialStoreFactory(t *testing.T) {
	emptyStore := credentialStoreFactory{}

	// test nil AuthResolver
	if credentials := emptyStore.CredentialStoreFor("localhost/library/debian"); credentials != registryclient.NoCredentials {
		t.Fatalf("Expected no credentials: got %#v", credentials)
	}

	invalidImages := []string{
		"https://github.com/docker/docker",
		"docker/Docker",
		"-docker",
		"-docker/docker",
		"-docker.io/docker/docker",
		"docker///docker",
		"docker.io/docker/Docker",
		"docker.io/docker///docker",
		"1a3f5e7d9c1b3a5f7e9d1c3b5a7f9e1d3c5b7a9f1e3d5d7c9b1a3f5e7d9c1b3a",
	}

	for _, image := range invalidImages {
		store := credentialStoreFactory{
			authResolver: &AuthResolver{
				authConfigs: map[string]credentialprovider.DockerConfigEntry{
					image: {Username: "local_user", Password: "local_pass"},
				},
			},
		}

		if credentials := store.CredentialStoreFor(image); credentials != registryclient.NoCredentials {
			t.Fatalf("Expected no credentials: got %#v", credentials)
		}
	}
}
