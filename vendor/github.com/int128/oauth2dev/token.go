package oauth2dev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// tokenResponse represents the successful response type,
// described in https://www.rfc-editor.org/rfc/rfc6749#section-5.1
type tokenResponse struct {
	// AccessToken is the token that authorizes and authenticates
	// the requests.
	AccessToken string `json:"access_token"`

	// TokenType is the type of token.
	// The Type method returns either this or "Bearer", the default.
	TokenType string `json:"token_type,omitempty"`

	// RefreshToken is a token that's used by the application
	// (as opposed to the user) to refresh the access token
	// if it expires.
	RefreshToken string `json:"refresh_token,omitempty"`

	// The lifetime in seconds of the access token.
	ExpiresIn int `json:"expires_in,omitempty"`

	// Raw optionally contains extra metadata from the server
	// when updating a token.
	Raw interface{}
}

func (tr tokenResponse) Expiry() time.Time {
	return time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
}

// Token returns the corresponding oauth2.Token
func (tr tokenResponse) Token() *oauth2.Token {
	return (&oauth2.Token{
		AccessToken:  tr.AccessToken,
		TokenType:    tr.TokenType,
		RefreshToken: tr.RefreshToken,
		Expiry:       tr.Expiry(),
	}).WithExtra(tr.Raw)
}

// TokenErrorResponse represents an error response,
// described in https://www.rfc-editor.org/rfc/rfc6749#section-5.2
// and https://www.rfc-editor.org/rfc/rfc8628#section-3.5
type TokenErrorResponse struct {
	StatusCode       int    `json:"-"`
	ErrorCode        string `json:"error,omitempty"`
	ErrorDescription string `json:"error_description,omitempty"`
	ErrorURI         string `json:"error_uri,omitempty"`
}

func (err TokenErrorResponse) Error() string {
	return fmt.Sprintf("token error response %d %s (%s)", err.StatusCode, err.ErrorCode, err.ErrorDescription)
}

// Error codes of token response,
// described in https://www.rfc-editor.org/rfc/rfc8628#section-3.5
const (
	TokenErrorAuthorizationPending = "authorization_pending"
	TokenErrorSlowDown             = "slow_down"
	TokenErrorAccessDenied         = "access_denied"
	TokenErrorExpiredToken         = "expired_token"
)

// PollToken tries a token request and waits until it receives a token response.
// It polls by the interval described in https://www.rfc-editor.org/rfc/rfc8628#section-3.5.
// When the context is done, this function immediately returns the context error.
func PollToken(ctx context.Context, cfg oauth2.Config, ar AuthorizationResponse) (*oauth2.Token, error) {
	interval := ar.IntervalDuration()
	for {
		tokenResponse, err := RetrieveToken(ctx, cfg, ar.DeviceCode)
		if err != nil {
			var eresp TokenErrorResponse
			if errors.As(err, &eresp) {
				if eresp.ErrorCode == TokenErrorAuthorizationPending {
					// the client MUST wait at least the number of seconds specified by
					// the "interval" parameter of the device authorization response
					// https://www.rfc-editor.org/rfc/rfc8628#section-3.5
					select {
					case <-time.After(interval):
						continue
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				if eresp.ErrorCode == TokenErrorSlowDown {
					// the interval MUST be increased by 5 seconds for this and all subsequent requests.
					// https://www.rfc-editor.org/rfc/rfc8628#section-3.5
					interval += 5 * time.Second
					select {
					case <-time.After(interval):
						continue
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
			}
			return nil, fmt.Errorf("token request: %w", err)
		}
		return tokenResponse, nil
	}
}

// RetrieveToken sends a token request to the endpoint.
// If it received a successful response, it returns the oauth2.Token.
// If it received an error response JSON, it returns an TokenErrorResponse.
// Otherwise, it returns an error wrapped with the cause.
func RetrieveToken(ctx context.Context, cfg oauth2.Config, deviceCode string) (*oauth2.Token, error) {
	// Device Access Token Request,
	// described in https://www.rfc-editor.org/rfc/rfc8628#section-3.4
	params := url.Values{
		"client_id":   {cfg.ClientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	if cfg.ClientSecret != "" {
		params.Set("client_secret", cfg.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.Endpoint.TokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("unable to create an authorization request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	hc := contextHTTPClient(ctx)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to send an authorization request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		var eresp TokenErrorResponse
		eresp.StatusCode = resp.StatusCode
		d := json.NewDecoder(bytes.NewReader(b))
		if err := d.Decode(&eresp); err != nil {
			return nil, fmt.Errorf("token error response (status: %d, payload: %s)", resp.StatusCode, string(b))
		}
		return nil, eresp
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body: %w", err)
	}
	var tresp tokenResponse
	if err := json.Unmarshal(body, &tresp); err != nil {
		return nil, fmt.Errorf("unable to parse the authorization response: %w", err)
	}
	tresp.Raw = make(map[string]interface{})
	if err := json.Unmarshal(body, &tresp.Raw); err != nil {
		return nil, fmt.Errorf("unable to parse the raw authorization response: %w", err)
	}
	return tresp.Token(), nil
}
