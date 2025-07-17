// Package oauth2cli provides better user experience on OAuth 2.0 and OpenID Connect (OIDC) on CLI.
// It allows simple and easy user interaction with Authorization Code Grant Flow and a local server.
package oauth2cli

import (
	"context"
	"fmt"
	"net/http"

	"github.com/int128/oauth2cli/oauth2params"
	"golang.org/x/oauth2"
)

var noopMiddleware = func(h http.Handler) http.Handler { return h }

// DefaultLocalServerSuccessHTML is a default response body on authorization success.
const DefaultLocalServerSuccessHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<title>Authorized</title>
	<script>
		window.close()
	</script>
	<style>
		body {
			background-color: #eee;
			margin: 0;
			padding: 0;
			font-family: sans-serif;
		}
		.placeholder {
			margin: 2em;
			padding: 2em;
			background-color: #fff;
			border-radius: 1em;
		}
	</style>
</head>
<body>
	<div class="placeholder">
		<h1>Authorized</h1>
		<p>You can close this window.</p>
	</div>
</body>
</html>
`

// Config represents a config for GetToken.
type Config struct {
	// OAuth2 config.
	// If the RedirectURL field is not set, default to http://localhost with the allocated port and LocalServerCallbackPath.
	// If the RedirectURL field is set, make sure it matches the LocalServerBindAddress and LocalServerCallbackPath.
	OAuth2Config oauth2.Config

	// Options for an authorization request.
	// You can set oauth2.AccessTypeOffline or oauth2.S256ChallengeOption.
	AuthCodeOptions []oauth2.AuthCodeOption

	// Options for a token request.
	// You can set oauth2.VerifierOption.
	TokenRequestOptions []oauth2.AuthCodeOption

	// State parameter in the authorization request.
	// Default to a string of random 32 bytes.
	State string

	// DEPRECATED: Set OAuth2Config.RedirectURL instead.
	RedirectURLHostname string

	// Candidates of hostname and port which the local server binds to.
	// You can set port number to 0 to allocate a free port.
	// If multiple addresses are given, it will try the ports in order.
	// If nil or an empty slice is given, it defaults to "127.0.0.1:0" i.e. a free port.
	LocalServerBindAddress []string

	// A PEM-encoded certificate, and possibly the complete certificate chain.
	// When set, the server will serve TLS traffic using the specified
	// certificates. It's recommended that the public key's SANs contain
	// the loopback addresses - 'localhost', '127.0.0.1' and '::1'
	LocalServerCertFile string

	// A PEM-encoded private key for the certificate.
	// This is required when LocalServerCertFile is set.
	LocalServerKeyFile string

	// Callback path of the local server.
	// If your provider requires a specific path of the redirect URL, set this field.
	// Default to none.
	LocalServerCallbackPath string

	// Response HTML body on authorization completed.
	// Default to DefaultLocalServerSuccessHTML.
	LocalServerSuccessHTML string

	// Middleware for the local server.
	// Default to none.
	LocalServerMiddleware func(h http.Handler) http.Handler

	// A channel to send the local server URL when it is ready.
	// Default to none.
	LocalServerReadyChan chan<- string

	// Redirect URL upon successful login
	SuccessRedirectURL string

	// Redirect URL upon failed login
	FailureRedirectURL string

	// Logger function for debug.
	Logf func(format string, args ...interface{})
}

func (cfg *Config) isLocalServerHTTPS() bool {
	return cfg.LocalServerCertFile != "" && cfg.LocalServerKeyFile != ""
}

func (cfg *Config) validateAndSetDefaults() error {
	if (cfg.LocalServerCertFile != "" && cfg.LocalServerKeyFile == "") ||
		(cfg.LocalServerCertFile == "" && cfg.LocalServerKeyFile != "") {
		return fmt.Errorf("both LocalServerCertFile and LocalServerKeyFile must be set")
	}
	if cfg.State == "" {
		state, err := oauth2params.NewState()
		if err != nil {
			return fmt.Errorf("could not generate a state parameter: %w", err)
		}
		cfg.State = state
	}
	if cfg.LocalServerMiddleware == nil {
		cfg.LocalServerMiddleware = noopMiddleware
	}
	if cfg.LocalServerSuccessHTML == "" {
		cfg.LocalServerSuccessHTML = DefaultLocalServerSuccessHTML
	}
	if (cfg.SuccessRedirectURL != "" && cfg.FailureRedirectURL == "") ||
		(cfg.SuccessRedirectURL == "" && cfg.FailureRedirectURL != "") {
		return fmt.Errorf("when using success and failure redirect URLs, set both URLs")
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...interface{}) {}
	}
	return nil
}

// GetToken performs the Authorization Code Grant Flow and returns a token received from the provider.
// See https://tools.ietf.org/html/rfc6749#section-4.1
//
// This performs the following steps:
//
//  1. Start a local server at the port.
//  2. Open a browser and navigate it to the local server.
//  3. Wait for the user authorization.
//  4. Receive a code via an authorization response (HTTP redirect).
//  5. Exchange the code and a token.
//  6. Return the code.
func GetToken(ctx context.Context, cfg Config) (*oauth2.Token, error) {
	if err := cfg.validateAndSetDefaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	code, err := receiveCodeViaLocalServer(ctx, &cfg)
	if err != nil {
		return nil, fmt.Errorf("authorization error: %w", err)
	}
	cfg.Logf("oauth2cli: exchanging the code and token")
	token, err := cfg.OAuth2Config.Exchange(ctx, code, cfg.TokenRequestOptions...)
	if err != nil {
		return nil, fmt.Errorf("could not exchange the code and token: %w", err)
	}
	return token, nil
}
