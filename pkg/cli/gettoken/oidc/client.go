package oidc

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/int128/oauth2cli"
	"golang.org/x/oauth2"
)

// DeviceAuthInfo contains the information needed for the user to complete device code authorization.
type DeviceAuthInfo struct {
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
}

// Authenticator defines the basic functionality to support Auth Code Flow,
// Device Code Flow, and Token Refresh Grant Flow.
type Authenticator interface {
	GetTokenByAuthCode(ctx context.Context, callbackAddress string, localServerReadyChan chan<- string) (string, string, time.Time, error)
	GetTokenByDeviceCode(ctx context.Context, readyChan chan<- DeviceAuthInfo) (string, string, time.Time, error)
	Refresh(ctx context.Context, refreshToken string) (string, string, time.Time, error)
	VerifyToken(ctx context.Context, token *oauth2.Token, nonce string) (string, time.Time, error)
}

// client manages the request/response flow between
// the client and the auth server. It has a capability
// to modify the http client with the given certificate
// authority and pre-defined proxy settings in addition to
// timeout value that can be customizable by the user.
type client struct {
	httpClient   *http.Client
	provider     *gooidc.Provider
	oauth2Config oauth2.Config
	usePKCE      bool
}

func (c *client) GetTokenByAuthCode(ctx context.Context, callbackAddress string, localServerReadyChan chan<- string) (string, string, time.Time, error) {
	state, err := RandomString(32)
	if err != nil {
		return "", "", time.Time{}, err
	}
	nonce, err := RandomString(32)
	if err != nil {
		return "", "", time.Time{}, err
	}

	var p string
	if c.usePKCE {
		p = oauth2.GenerateVerifier()
	}

	if c.httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, c.httpClient)
	}

	config := oauth2cli.Config{
		OAuth2Config:           c.oauth2Config,
		State:                  state,
		AuthCodeOptions:        authorizationRequestOptions(nonce, p),
		TokenRequestOptions:    tokenRequestOptions(p),
		LocalServerBindAddress: []string{callbackAddress},
		LocalServerReadyChan:   localServerReadyChan,
	}
	token, err := oauth2cli.GetToken(ctx, config)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("oauth2 error: %w", err)
	}
	idToken, expiry, err := c.VerifyToken(ctx, token, nonce)
	return idToken, token.RefreshToken, expiry, err
}

// GetTokenByDeviceCode performs the RFC 8628 device authorization grant flow.
// It sends a DeviceAuthInfo to readyChan so the caller can display the verification
// URI and user code, then polls the token endpoint until the user completes authorization.
// For confidential clients, the client secret is included in the device authorization request.
func (c *client) GetTokenByDeviceCode(ctx context.Context, readyChan chan<- DeviceAuthInfo) (string, string, time.Time, error) {
	if c.httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, c.httpClient)
	}

	var deviceAuthOpts []oauth2.AuthCodeOption
	if c.oauth2Config.ClientSecret != "" {
		deviceAuthOpts = append(deviceAuthOpts, oauth2.SetAuthURLParam("client_secret", c.oauth2Config.ClientSecret))
	}

	deviceAuth, err := c.oauth2Config.DeviceAuth(ctx, deviceAuthOpts...)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("device authorization request error: %w", err)
	}

	select {
	case readyChan <- DeviceAuthInfo{
		UserCode:                deviceAuth.UserCode,
		VerificationURI:         deviceAuth.VerificationURI,
		VerificationURIComplete: deviceAuth.VerificationURIComplete,
	}:
	case <-ctx.Done():
		return "", "", time.Time{}, ctx.Err()
	}

	token, err := c.oauth2Config.DeviceAccessToken(ctx, deviceAuth)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("device code token exchange error: %w", err)
	}

	idToken, expiry, err := c.VerifyToken(ctx, token, "")
	return idToken, token.RefreshToken, expiry, err
}

func RandomString(length int) (string, error) {
	bytes := make([]byte, length)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", fmt.Errorf("unable to get random bytes: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func (c *client) Refresh(ctx context.Context, refreshToken string) (string, string, time.Time, error) {
	if c.httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, c.httpClient)
	}

	currentToken := &oauth2.Token{
		Expiry:       time.Now().Add(5 * time.Minute),
		RefreshToken: refreshToken,
	}
	source := c.oauth2Config.TokenSource(ctx, currentToken)
	token, err := source.Token()
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("could not refresh the token: %w", err)
	}
	idToken, expiry, err := c.VerifyToken(ctx, token, "")
	return idToken, refreshToken, expiry, err
}

func (c *client) VerifyToken(ctx context.Context, token *oauth2.Token, nonce string) (string, time.Time, error) {
	idToken, ok := token.Extra("id_token").(string)
	if !ok {
		return "", time.Time{}, fmt.Errorf("id_token is missing in the token response: %#v", token)
	}
	verifier := c.provider.Verifier(&gooidc.Config{ClientID: c.oauth2Config.ClientID})
	verifiedIDToken, err := verifier.Verify(ctx, idToken)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("could not verify the ID token: %w", err)
	}
	if nonce != "" && nonce != verifiedIDToken.Nonce {
		return "", time.Time{}, fmt.Errorf("nonce did not match (wants %s but got %s)", nonce, verifiedIDToken.Nonce)
	}

	return idToken, verifiedIDToken.Expiry, nil
}

func authorizationRequestOptions(nonce string, pkce string) []oauth2.AuthCodeOption {
	o := []oauth2.AuthCodeOption{
		oauth2.AccessTypeOffline,
		gooidc.Nonce(nonce),
	}
	if pkce != "" {
		o = append(o, oauth2.S256ChallengeOption(pkce))
	}
	return o
}

func tokenRequestOptions(p string) (o []oauth2.AuthCodeOption) {
	if p != "" {
		o = append(o, oauth2.VerifierOption(p))
	}
	return
}

// Provider represents an OIDC provider.
type Provider struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string   // optional
	ExtraScopes  []string // optional
	UsePKCE      bool     // optional
}

func NewAuthenticator(ctx context.Context, p *Provider, cacertdata string, cacertfile string, insecureTLS bool) (Authenticator, error) {
	rawTLSClientConfig, err := loadTLSConfig(cacertdata, cacertfile, insecureTLS)
	if err != nil {
		return nil, fmt.Errorf("could not load the TLS client config: %w", err)
	}
	baseTransport := &http.Transport{
		TLSClientConfig: rawTLSClientConfig,
		Proxy:           http.ProxyFromEnvironment,
	}
	httpClient := &http.Client{
		Transport: baseTransport,
	}

	ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)
	provider, err := gooidc.NewProvider(ctx, p.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery error: %w", err)
	}
	supportedPKCEMethods, err := extractSupportedPKCEMethods(provider)
	if err != nil {
		return nil, fmt.Errorf("could not determine supported PKCE methods: %w", err)
	}

	usePKCE := p.UsePKCE
	if !usePKCE {
		for _, m := range supportedPKCEMethods {
			if m == "S256" {
				usePKCE = true
				break
			}
		}
	}
	endpoint := provider.Endpoint()
	if p.ClientSecret == "" {
		endpoint.AuthStyle = oauth2.AuthStyleInParams
	}

	return &client{
		httpClient: httpClient,
		provider:   provider,
		oauth2Config: oauth2.Config{
			Endpoint:     endpoint,
			ClientID:     p.ClientID,
			ClientSecret: p.ClientSecret,
			Scopes:       append(p.ExtraScopes, gooidc.ScopeOpenID),
		},
		usePKCE: usePKCE,
	}, nil
}

func extractSupportedPKCEMethods(provider *gooidc.Provider) ([]string, error) {
	var d struct {
		CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
	}
	if err := provider.Claims(&d); err != nil {
		return nil, fmt.Errorf("invalid discovery document: %w", err)
	}
	return d.CodeChallengeMethodsSupported, nil
}

func loadTLSConfig(cacertdata, cacertfile string, insecureTLS bool) (*tls.Config, error) {
	rootCAs := x509.NewCertPool()

	if cacertfile != "" {
		crt, err := os.ReadFile(cacertfile)
		if err != nil {
			return nil, fmt.Errorf("could not load certificate authority passed in --certificate-authority")
		}
		if !rootCAs.AppendCertsFromPEM(crt) {
			return nil, fmt.Errorf("invalid CA certificate passed in --certificate-authority")
		}
	}

	if cacertdata != "" {
		b, err := base64.StdEncoding.DecodeString(cacertdata)
		if err != nil {
			return nil, fmt.Errorf("could not load certificate authority passed in --certificate-authority")
		}
		if !rootCAs.AppendCertsFromPEM(b) {
			return nil, fmt.Errorf("invalid CA certificate passed in --certificate-authority")
		}
	}

	if rootCAs.Equal(x509.NewCertPool()) {
		// if empty, use the host's root CA set
		rootCAs = nil
	}
	return &tls.Config{
		RootCAs:            rootCAs,
		InsecureSkipVerify: insecureTLS,
	}, nil
}
