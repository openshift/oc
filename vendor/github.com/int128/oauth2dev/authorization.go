package oauth2dev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// AuthorizationResponse represents Device Authorization Response,
// described in https://www.rfc-editor.org/rfc/rfc8628#section-3.2
type AuthorizationResponse struct {
	// The device verification code.
	DeviceCode string `json:"device_code,omitempty"`

	// The end-user verification code.
	UserCode string `json:"user_code,omitempty"`

	// The end-user verification URI on the authorization server.
	VerificationURI string `json:"verification_uri,omitempty"`

	// A verification URI that includes the "user_code" (or
	// other information with the same function as the "user_code"),
	// which is designed for non-textual transmission.
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`

	// The end-user verification URI on the authorization server.
	// Some implementations return this field instead of verification_uri,
	// such as https://developers.google.com/identity/protocols/oauth2/limited-input-device.
	VerificationURL string `json:"verification_url,omitempty"`

	// The lifetime in seconds of the "device_code" and "user_code".
	ExpiresIn int `json:"expires_in,omitempty"`

	// The minimum amount of time in seconds that the client
	// SHOULD wait between polling requests to the token endpoint.
	// If no value is provided, clients MUST use 5 as the default.
	Interval int `json:"interval,omitempty"`
}

func (ar AuthorizationResponse) IntervalDuration() time.Duration {
	return time.Duration(ar.Interval) * time.Second
}

// URL returns either of VerificationURIComplete, VerificationURI or VerificationURL.
func (ar AuthorizationResponse) URL() string {
	if ar.VerificationURIComplete != "" {
		return ar.VerificationURIComplete
	}
	if ar.VerificationURI != "" {
		return ar.VerificationURI
	}
	if ar.VerificationURL != "" {
		return ar.VerificationURL
	}
	return ""
}

// AuthorizationErrorResponse represents the error response,
// described in https://www.rfc-editor.org/rfc/rfc6749#section-5.2
type AuthorizationErrorResponse struct {
	StatusCode       int    `json:"-"`
	ErrorCode        string `json:"error,omitempty"`
	ErrorDescription string `json:"error_description,omitempty"`
	ErrorURI         string `json:"error_uri,omitempty"`
}

func (err AuthorizationErrorResponse) Error() string {
	return fmt.Sprintf("authorization error response %d, %s (%s)", err.StatusCode, err.ErrorCode, err.ErrorDescription)
}

// RetrieveCode sends an authorization request to the authorization endpoint.
// If it received a successful response, it returns the AuthorizationResponse.
// If it received an error response JSON, it returns an AuthorizationErrorResponse.
// Otherwise, it returns an error wrapped with the cause.
func RetrieveCode(ctx context.Context, cfg oauth2.Config) (*AuthorizationResponse, error) {
	// Device Authorization Request,
	// described in https://www.rfc-editor.org/rfc/rfc8628#section-3.1
	params := url.Values{"client_id": {cfg.ClientID}}
	if len(cfg.Scopes) > 0 {
		params.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	if cfg.ClientSecret != "" {
		params.Set("client_secret", cfg.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.Endpoint.AuthURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("unable to create a request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	hc := contextHTTPClient(ctx)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to send the request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		var eresp AuthorizationErrorResponse
		eresp.StatusCode = resp.StatusCode
		d := json.NewDecoder(bytes.NewReader(b))
		if err := d.Decode(&eresp); err != nil {
			return nil, fmt.Errorf("authorization error response (status: %d, payload: %s)", resp.StatusCode, string(b))
		}
		return nil, eresp
	}

	d := json.NewDecoder(resp.Body)
	var aresp AuthorizationResponse
	if err := d.Decode(&aresp); err != nil {
		return nil, fmt.Errorf("invalid response: %w", err)
	}
	if aresp.Interval == 0 {
		// If no value is provided, clients MUST use 5 as the default.
		// https://www.rfc-editor.org/rfc/rfc8628#section-3.2
		aresp.Interval = 5
	}
	return &aresp, nil
}
