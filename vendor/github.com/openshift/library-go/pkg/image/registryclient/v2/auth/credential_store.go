package auth

import "net/url"

// CredentialStore is an interface for getting credentials for a given URL.
type CredentialStore interface {
	// Basic returns basic auth for the given URL
	Basic(*url.URL) (string, string)

	// RefreshToken returns a refresh token for the
	// given URL and service
	RefreshToken(*url.URL, string) string

	// SetRefreshToken sets the refresh token if none
	// is provided for the given url and service
	SetRefreshToken(realm *url.URL, service, token string)
}
