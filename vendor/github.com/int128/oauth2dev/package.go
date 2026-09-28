// Package oauth2dev is an implementation of OAuth 2.0 Device Authorization Grant,
// described in RFC 8628 https://www.rfc-editor.org/rfc/rfc8628.
package oauth2dev

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"
)

func contextHTTPClient(ctx context.Context) *http.Client {
	if ctx == nil {
		return http.DefaultClient
	}
	v, ok := ctx.Value(oauth2.HTTPClient).(*http.Client)
	if !ok {
		return http.DefaultClient
	}
	return v
}
