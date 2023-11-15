//go:build !gssapi
// +build !gssapi

package authchallengers

func GSSAPIEnabled() bool {
	return false
}

func NewGSSAPINegotiator(string) Negotiator {
	return newUnsupportedNegotiator("GSSAPI")
}
