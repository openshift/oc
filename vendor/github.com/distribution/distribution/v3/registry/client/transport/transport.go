// Package transport re-exports the registry client transport helpers that
// upstream moved to internal/client/transport in distribution v3. See
// package registry/client for the rationale and the removal condition.
//
// DefaultTransportWrapper is deliberately not re-exported: a package-level
// variable cannot be aliased, and a copy would not affect the transports
// built by internal/client.
package transport

import (
	"context"
	"io"
	"net/http"

	"github.com/distribution/distribution/v3/internal/client/transport"
)

// ErrWrongCodeForByteRange is returned if the client sends a request with a
// Range header but the server returns a 2xx or 3xx code other than 206
// Partial Content.
var ErrWrongCodeForByteRange = transport.ErrWrongCodeForByteRange

// RequestModifier represents an object which will do an inplace modification
// of an HTTP request.
type RequestModifier = transport.RequestModifier

// HTTPReadSeeker implements an io.ReadSeeker over an HTTP GET endpoint.
type HTTPReadSeeker = transport.HTTPReadSeeker

// ReadSeekCloser combines io.ReadSeeker with io.Closer.
//
// Deprecated: use [io.ReadSeekCloser].
type ReadSeekCloser = io.ReadSeekCloser

// NewHeaderRequestModifier returns a new RequestModifier which will add the
// given headers to a request.
func NewHeaderRequestModifier(header http.Header) RequestModifier {
	return transport.NewHeaderRequestModifier(header)
}

// NewTransport creates a new transport which will apply modifiers to the
// request on a RoundTrip call.
func NewTransport(base http.RoundTripper, modifiers ...RequestModifier) http.RoundTripper {
	return transport.NewTransport(base, modifiers...)
}

// NewHTTPReadSeeker handles reading from an HTTP endpoint using a GET
// request. If the endpoint supports range requests, Seek is supported.
func NewHTTPReadSeeker(ctx context.Context, client *http.Client, url string, errorHandler func(*http.Response) error) *HTTPReadSeeker {
	return transport.NewHTTPReadSeeker(ctx, client, url, errorHandler)
}
