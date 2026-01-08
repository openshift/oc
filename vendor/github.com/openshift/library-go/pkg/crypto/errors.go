package crypto

import (
	"crypto/x509"
	"fmt"
	"net"
	"strings"
)

// FormatHostnameError formats hostname errors without calling HostnameError.Error()
// to mitigate CVE-2025-61729 (quadratic runtime from repeated string concatenation with unlimited SANs).
func FormatHostnameError(h x509.HostnameError) string {
	c := h.Certificate
	if c == nil {
		return "x509: cannot validate certificate for " + h.Host
	}

	const maxNamesIncluded = 100

	// Check if host is an IP address
	if ip := net.ParseIP(h.Host); ip != nil {
		if len(c.IPAddresses) == 0 {
			return "x509: cannot validate certificate for " + h.Host + " because it doesn't contain any IP SANs"
		}
		if len(c.IPAddresses) >= maxNamesIncluded {
			return fmt.Sprintf("x509: certificate is valid for %d IP SANs, but none matched %s", len(c.IPAddresses), h.Host)
		}
		var valid strings.Builder
		for i, san := range c.IPAddresses {
			if i > 0 {
				valid.WriteString(", ")
			}
			valid.WriteString(san.String())
		}
		return "x509: certificate is valid for " + valid.String() + ", not " + h.Host
	}

	// DNS name validation
	if len(c.DNSNames) == 0 {
		return "x509: certificate is not valid for any names, but wanted to match " + h.Host
	}
	if len(c.DNSNames) >= maxNamesIncluded {
		return fmt.Sprintf("x509: certificate is valid for %d names, but none matched %s", len(c.DNSNames), h.Host)
	}
	return "x509: certificate is valid for " + strings.Join(c.DNSNames, ", ") + ", not " + h.Host
}
