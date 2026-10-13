package dockercredentials

import (
	"testing"

	"github.com/containers/image/v5/docker/reference"
	containertypes "github.com/containers/image/v5/types"
)

func Test_AuthResolver(t *testing.T) {
	fn := func(host string, entry containertypes.DockerAuthConfig) AuthResolver {
		return AuthResolver{
			map[string]containertypes.DockerAuthConfig{
				host: entry,
			},
		}
	}
	def := containertypes.DockerAuthConfig{
		Username: "local_user",
		Password: "local_pass",
	}

	type testInput struct {
		authResolver AuthResolver
		image        string
	}
	tests := []struct {
		name     string
		input    testInput
		user     string
		password string
	}{
		{name: "docker/docker,", input: testInput{authResolver: fn("index.docker.io", def), image: "docker/docker"}, user: def.Username, password: def.Password},
		{name: "library/debian", input: testInput{authResolver: fn("docker.io", def), image: "library/debian"}, user: def.Username, password: def.Password},
		{name: "debian", input: testInput{authResolver: fn("index.docker.io", def), image: "debian"}, user: def.Username, password: def.Password},
		{name: "debian:v1.0", input: testInput{authResolver: fn("index.docker.io", def), image: "debian:v1.0"}, user: def.Username, password: def.Password},
		{name: "docker.io/docker/docker,", input: testInput{authResolver: fn("index.docker.io", def), image: "docker.io/docker/docker"}, user: def.Username, password: def.Password},
		{name: "docker.io/library/debian", input: testInput{authResolver: fn("index.docker.io", def), image: "docker.io/library/debian"}, user: def.Username, password: def.Password},
		{name: "docker.io/library/debian:latest", input: testInput{authResolver: fn("index.docker.io", def), image: "docker.io/library/debian:latest"}, user: def.Username, password: def.Password},
		{name: "registry-1.docker.io/library/debian", input: testInput{authResolver: fn("index.docker.io", def), image: "registry-1.docker.io/library/debian"}, user: def.Username, password: def.Password},
		{name: "index.docker.io/debian", input: testInput{authResolver: fn("index.docker.io", def), image: "index.docker.io/debian"}, user: def.Username, password: def.Password},
		{name: "alternative conf format debian", input: testInput{authResolver: fn("registry-1.docker.io", def), image: "debian"}, user: def.Username, password: def.Password},
		{name: "alternative conf format docker.io/library/debian", input: testInput{authResolver: fn("registry-1.docker.io", def), image: "docker.io/library/debian"}, user: def.Username, password: def.Password},
		{name: "alternative conf format registry-1.docker.io/library/debian", input: testInput{authResolver: fn("registry-1.docker.io", def), image: "registry-1.docker.io/library/debian"}, user: def.Username, password: def.Password},
		{name: "alternative conf format index.docker.io/debian", input: testInput{authResolver: fn("registry-1.docker.io", def), image: "index.docker.io/debian"}, user: def.Username, password: def.Password},
		{name: "old conf format debian ,", input: testInput{authResolver: fn("docker.io", def), image: "debian"}, user: def.Username, password: def.Password},
		{name: "old conf format docker.io/library/debian", input: testInput{authResolver: fn("docker.io", def), image: "docker.io/library/debian"}, user: def.Username, password: def.Password},
		{name: "old conf format registry-1.docker.io/library/debian", input: testInput{authResolver: fn("docker.io", def), image: "registry-1.docker.io/library/debian"}, user: def.Username, password: def.Password},
		{name: "old conf format index.docker.io/debian", input: testInput{authResolver: fn("docker.io", def), image: "index.docker.io/debian"}, user: def.Username, password: def.Password},
		{name: "localhost:5000/docker/docker,", input: testInput{authResolver: fn("localhost:5000", def), image: "localhost:5000/docker/docker"}, user: def.Username, password: def.Password},
		{name: "localhost:5000/docker/docker:latest,", input: testInput{authResolver: fn("localhost:5000", def), image: "localhost:5000/docker/docker:latest"}, user: def.Username, password: def.Password},
		{name: "127.0.0.1:5000/library/debian", input: testInput{authResolver: fn("127.0.0.1:5000", def), image: "127.0.0.1:5000/library/debian"}, user: def.Username, password: def.Password},
		{name: "127.0.0.1:5000/debian", input: testInput{authResolver: fn("127.0.0.1:5000", def), image: "127.0.0.1:5000/debian"}, user: def.Username, password: def.Password},
		{name: "localhost:5000/docker/docker,", input: testInput{authResolver: fn("localhost:5000", def), image: "localhost:5000/docker/docker"}, user: def.Username, password: def.Password},
		{name: "https localhost/library/debian", input: testInput{authResolver: fn("https://localhost", def), image: "localhost/library/debian"}, user: def.Username, password: def.Password},
		{name: "http localhost/library/debian", input: testInput{authResolver: fn("http://localhost", def), image: "localhost/library/debian"}, user: def.Username, password: def.Password},
		{name: "exact https localhost/library/debian", input: testInput{authResolver: fn("localhost:443", def), image: "localhost:443/library/debian"}, user: def.Username, password: def.Password},
		{name: "exact http localhost:80/library/debian", input: testInput{authResolver: fn("localhost:80", def), image: "localhost:80/library/debian"}, user: def.Username, password: def.Password},

		// this is not allowed by the credential keyring, but should be
		{name: "exact http localhost:80", input: testInput{authResolver: fn("http://localhost", def), image: "localhost:80/library/debian"}, user: "", password: ""},
		{name: "exact https localhost:443", input: testInput{authResolver: fn("localhost:443", def), image: "localhost/library/debian"}, user: "", password: ""},

		// these should not be allowed
		{name: "host is for port 80 only", input: testInput{authResolver: fn("localhost:80", def), image: "localhost/library/debian"}, user: "", password: ""},
		{name: "host is for port 443 only", input: testInput{authResolver: fn("localhost:443", def), image: "localhost/library/debian"}, user: "", password: ""},
		{name: "exact http", input: testInput{authResolver: fn("localhost", def), image: "localhost:80/library/debian"}, user: "", password: ""},
		{name: "exact https", input: testInput{authResolver: fn("localhost", def), image: "localhost:443/library/debian"}, user: "", password: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := reference.ParseNormalizedNamed(tt.input.image)
			if err != nil {
				t.Errorf("unexpected ParseNamed error: %v", err)
			}
			authEntry, err := tt.input.authResolver.findAuthentication(ref, reference.Domain(ref))
			if err != nil {
				t.Errorf("unexpected findAuthentication error: %v", err)
			}
			if authEntry.Username != tt.user {
				t.Errorf("BasicFromKeyring() user = %v, actual = %v", authEntry.Username, tt.user)
			}
			if authEntry.Password != tt.password {
				t.Errorf("BasicFromKeyring() password = %v, actual = %v", authEntry.Password, tt.password)
			}
		})
	}
}
