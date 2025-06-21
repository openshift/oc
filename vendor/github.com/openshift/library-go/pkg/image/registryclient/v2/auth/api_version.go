package auth

import (
	"net/http"
	"strings"
)

// APIVersions gets the API versions out of an HTTP response using the provided
// version header as the key for the HTTP header.
func APIVersions(resp *http.Response, versionHeader string) []string {
	var versions []string
	if versionHeader != "" {
		for _, supportedVersions := range resp.Header[http.CanonicalHeaderKey(versionHeader)] {
			for _, version := range strings.Fields(supportedVersions) {
				versions = append(versions, version)
			}
		}
	}
	return versions
}
