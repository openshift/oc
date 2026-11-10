// Package client re-exports the registry HTTP client that upstream moved to
// internal/client in distribution v3.
//
// openshift/library-go, openshift/image-registry, openshift/oc and
// openshift-apiserver drive remote registries through this client, and
// keeping a single implementation on the fork avoids each of them carrying a
// copy. Every symbol is a type alias or a thin wrapper that forwards to
// internal/client, so the packages stay in sync with upstream automatically.
//
// Drop this package, together with registry/client/auth,
// registry/client/auth/challenge and registry/client/transport, when the
// consumers stop importing it.
package client

import (
	"net/http"

	"github.com/distribution/distribution/v3"
	"github.com/distribution/distribution/v3/internal/client"
	"github.com/distribution/reference"
	"github.com/opencontainers/go-digest"
)

// ErrNoErrorsInBody is returned when an HTTP response body parses to an empty
// errcode.Errors slice.
var ErrNoErrorsInBody = client.ErrNoErrorsInBody

// UnexpectedHTTPStatusError is returned when an unexpected HTTP status is
// returned when making a registry api call.
type UnexpectedHTTPStatusError = client.UnexpectedHTTPStatusError

// UnexpectedHTTPResponseError is returned when an expected HTTP status code
// is returned, but the content was unexpected and failed to be parsed.
type UnexpectedHTTPResponseError = client.UnexpectedHTTPResponseError

// Registry provides an interface for calling Repositories, which returns a
// catalog of repositories.
type Registry = client.Registry

// HandleHTTPResponseError returns error parsed from HTTP response, if any.
// It returns nil if no error occurred (HTTP status 200-399), or an error for
// unexpected status codes.
func HandleHTTPResponseError(resp *http.Response) error {
	return client.HandleHTTPResponseError(resp)
}

// HandleErrorResponse returns error parsed from HTTP response for an
// unsuccessful HTTP response code (in the range 400 - 499 inclusive).
//
// Deprecated: use [HandleHTTPResponseError].
func HandleErrorResponse(resp *http.Response) error {
	return client.HandleErrorResponse(resp) //nolint:staticcheck // forwards the deprecated upstream function for consumers that still call it.
}

// SuccessStatus returns true if the argument is a successful HTTP response
// code (in the range 200 - 399 inclusive).
//
// Deprecated: use [HandleHTTPResponseError] and check the error.
func SuccessStatus(status int) bool {
	return client.SuccessStatus(status) //nolint:staticcheck // forwards the deprecated upstream function for consumers that still call it.
}

// NewRegistry creates a registry namespace which can be used to get a
// listing of repositories.
func NewRegistry(baseURL string, transport http.RoundTripper) (Registry, error) {
	return client.NewRegistry(baseURL, transport)
}

// NewRepository creates a new Repository for the given repository name and
// base URL.
func NewRepository(name reference.Named, baseURL string, transport http.RoundTripper) (distribution.Repository, error) {
	return client.NewRepository(name, baseURL, transport)
}

// AddEtagToTag allows a client to supply an etag to the tag lookup, which
// results in a 304 Not Modified when the etag matches.
func AddEtagToTag(tag, etag string) distribution.ManifestServiceOption {
	return client.AddEtagToTag(tag, etag)
}

// ReturnContentDigest allows a client to set a the content digest on a
// successful request from the 'Docker-Content-Digest' header.
func ReturnContentDigest(dgst *digest.Digest) distribution.ManifestServiceOption {
	return client.ReturnContentDigest(dgst)
}

// WithMountFrom returns a BlobCreateOption which designates that the blob
// should be mounted from the given canonical reference.
func WithMountFrom(ref reference.Canonical) distribution.BlobCreateOption {
	return client.WithMountFrom(ref)
}
