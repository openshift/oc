//go:build !windows
// +build !windows

package authchallengers

import (
	"io"

	"github.com/openshift/oc/pkg/version"
)

func SSPIEnabled() bool {
	return false
}

func NewSSPINegotiator(string, string, string, io.Reader, version.ServerVersionRetriever) Negotiator {
	return newUnsupportedNegotiator("SSPI")
}
