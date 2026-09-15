package login

import (
	"bytes"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	cliconfig "github.com/openshift/oc/pkg/helpers/kubeconfig"
	"github.com/openshift/oc/pkg/helpers/term"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/printers"
	restclient "k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// parseProxyURL validates a kubeconfig-style proxy URL.
// Supported schemes match client-go's kubeconfig proxy-url validation.
// Errors never include the raw URL, which may contain credentials.
func parseProxyURL(proxyURL string) (*url.URL, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, errors.New("invalid --proxy-url: could not parse")
	}

	switch u.Scheme {
	case "http", "https", "socks5":
	default:
		return nil, fmt.Errorf("invalid --proxy-url: unsupported scheme %q, must be http, https, or socks5", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, errors.New("invalid --proxy-url: host must be specified")
	}
	return u, nil
}

// setClientConfigProxy configures rest.Config.Proxy from a proxy URL string.
// When set, client-go uses this instead of HTTPS_PROXY/HTTP_PROXY, and
// CreateConfig persists it to the kubeconfig cluster's proxy-url field.
func setClientConfigProxy(clientConfig *restclient.Config, proxyURL string) error {
	u, err := parseProxyURL(proxyURL)
	if err != nil {
		return err
	}
	clientConfig.Proxy = http.ProxyURL(u)
	return nil
}

// getMatchingClusters examines the kubeconfig for all clusters that point to the same server
func getMatchingClusters(clientConfig restclient.Config, kubeconfig clientcmdapi.Config) sets.String {
	ret := sets.String{}

	for key, cluster := range kubeconfig.Clusters {
		if (cluster.Server == clientConfig.Host) && (cluster.InsecureSkipTLSVerify == clientConfig.Insecure) && (cluster.CertificateAuthority == clientConfig.CAFile) && (bytes.Compare(cluster.CertificateAuthorityData, clientConfig.CAData) == 0) {
			ret.Insert(key)
		}
	}

	return ret
}

// findCluster returns a cluster matching the host. Prefer the canonical
// nickname CreateConfig would write, so login reuses the same cluster entry
// (and its proxy-url) when multiple stanzas share a server URL.
func findCluster(host string, kubeconfig clientcmdapi.Config) *clientcmdapi.Cluster {
	if nick, err := cliconfig.GetClusterNicknameFromURL(host); err == nil {
		if cluster, ok := kubeconfig.Clusters[nick]; ok && cluster.Server == host {
			return cluster
		}
	}
	for _, cluster := range kubeconfig.Clusters {
		if cluster.Server == host {
			return cluster
		}
	}
	return nil
}

// dialToServer takes the Server URL from the given clientConfig and dials to
// make sure the server is reachable. Note the config received is not mutated.
func dialToServer(clientConfig restclient.Config) error {
	// take a RoundTripper based on the config we already have (TLS, proxies, etc)
	rt, err := restclient.TransportFor(&clientConfig)
	if err != nil {
		return err
	}

	parsedURL, err := url.Parse(clientConfig.Host)
	if err != nil {
		return err
	}

	// Do a HEAD request to serverPathToDial to make sure the server is alive.
	// We don't care about the response, any err != nil is valid for the sake of reachability.
	serverURLToDial := (&url.URL{Scheme: parsedURL.Scheme, Host: parsedURL.Host, Path: "/"}).String()
	req, err := http.NewRequest(http.MethodHead, serverURLToDial, nil)
	if err != nil {
		return err
	}

	res, err := rt.RoundTrip(req)
	if err != nil {
		return err
	}

	// This is to guide a user who passes the console URL instead of server URL
	// See https://bugzilla.redhat.com/show_bug.cgi?id=1704827
	contentType := res.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		return &errInvalidServerURL{}
	}

	defer res.Body.Close()
	return nil
}

func promptForInsecureTLS(reader io.Reader, out io.Writer, reason error) bool {
	var insecureTLSRequestReason string
	if reason != nil {
		switch reason.(type) {
		case x509.UnknownAuthorityError:
			insecureTLSRequestReason = "The server uses a certificate signed by an unknown authority."
		case x509.HostnameError:
			insecureTLSRequestReason = fmt.Sprintf("The server is using a certificate that does not match its hostname: %s", reason.Error())
		case x509.CertificateInvalidError:
			insecureTLSRequestReason = fmt.Sprintf("The server is using an invalid certificate: %s", reason.Error())
		}
	}
	var input bool
	if printers.IsTerminal(reader) {
		if len(insecureTLSRequestReason) > 0 {
			fmt.Fprintln(out, insecureTLSRequestReason)
		}
		fmt.Fprintln(out, "You can bypass the certificate check, but any data you send to the server could be intercepted by others.")
		input = term.PromptForBool(os.Stdin, out, "Use insecure connections? (y/n): ")
		fmt.Fprintln(out)
	}
	return input
}

func hasExistingInsecureCluster(clientConfigToTest restclient.Config, kubeconfig clientcmdapi.Config) bool {
	clientConfigToTest.Insecure = true
	matchingClusters := getMatchingClusters(clientConfigToTest, kubeconfig)
	return len(matchingClusters) > 0
}
