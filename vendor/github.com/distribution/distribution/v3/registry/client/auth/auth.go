// Package auth re-exports the registry client authentication handlers that
// upstream moved to internal/client/auth in distribution v3. See package
// registry/client for the rationale and the removal condition.
package auth

import (
	"net/http"

	"github.com/distribution/distribution/v3/internal/client/auth"
	"github.com/distribution/distribution/v3/registry/client/auth/challenge"
	"github.com/distribution/distribution/v3/registry/client/transport"
)

var (
	// ErrNoBasicAuthCredentials is returned if a request can't be authorized
	// with basic auth due to lack of credentials.
	ErrNoBasicAuthCredentials = auth.ErrNoBasicAuthCredentials

	// ErrNoToken is returned if a request is successful but the body does not
	// contain an authorization token.
	ErrNoToken = auth.ErrNoToken
)

// APIVersion represents a version of an API including its type and version
// number.
type APIVersion = auth.APIVersion

// AuthenticationHandler is an interface for authorizing a request from
// params from a "WWW-Authenicate" header for a single scheme.
type AuthenticationHandler = auth.AuthenticationHandler

// CredentialStore is an interface for getting credentials for a given URL.
type CredentialStore = auth.CredentialStore

// Scope is a type which is serializable to a string using the allow scope
// grammar from the token authentication specification.
type Scope = auth.Scope

// RepositoryScope represents a token scope for access to a repository.
type RepositoryScope = auth.RepositoryScope

// RegistryScope represents a token scope for access to resources in the
// registry.
type RegistryScope = auth.RegistryScope

// Logger defines the injectable logging interface, used on TokenHandlers.
type Logger = auth.Logger

// TokenHandlerOptions is used to configure a new token handler.
type TokenHandlerOptions = auth.TokenHandlerOptions

// APIVersions gets the API versions out of an HTTP response using the
// provided version header as the key for the HTTP header.
func APIVersions(resp *http.Response, versionHeader string) []APIVersion {
	return auth.APIVersions(resp, versionHeader)
}

// ParseAPIVersion parses an API version string into an APIVersion. Format
// (Expected, but not enforced): <type>/<version>.
func ParseAPIVersion(versionStr string) APIVersion {
	return auth.ParseAPIVersion(versionStr)
}

// NewAuthorizer creates an authorizer which can handle multiple
// authentication schemes. The handlers are tried in order, the higher
// priority authentication methods should be first.
func NewAuthorizer(manager challenge.Manager, handlers ...AuthenticationHandler) transport.RequestModifier {
	return auth.NewAuthorizer(manager, handlers...)
}

// NewTokenHandler creates a new AuthenticationHandler which supports
// fetching tokens from a remote token server.
func NewTokenHandler(transport http.RoundTripper, creds CredentialStore, scope string, actions ...string) AuthenticationHandler {
	return auth.NewTokenHandler(transport, creds, scope, actions...)
}

// NewTokenHandlerWithOptions creates a new token handler using the provided
// options structure.
func NewTokenHandlerWithOptions(options TokenHandlerOptions) AuthenticationHandler {
	return auth.NewTokenHandlerWithOptions(options)
}

// NewBasicHandler creates a new authentication handler which adds basic
// authentication credentials to a request.
func NewBasicHandler(creds CredentialStore) AuthenticationHandler {
	return auth.NewBasicHandler(creds)
}
