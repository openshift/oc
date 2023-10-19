package oauth2dev

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
)

// HandleAuthorizationResponseFunc is a function to handle an authorization response.
// It should display the user code and verification URL to the user.
// See https://www.rfc-editor.org/rfc/rfc8628#section-3.3
type HandleAuthorizationResponseFunc func(response AuthorizationResponse)

// GetToken sends an authorization request and then polls the token response.
func GetToken(ctx context.Context, cfg oauth2.Config, h HandleAuthorizationResponseFunc) (*oauth2.Token, error) {
	authorizationResponse, err := RetrieveCode(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("authorization request: %w", err)
	}
	h(*authorizationResponse)
	token, err := PollToken(ctx, cfg, *authorizationResponse)
	if err != nil {
		return nil, fmt.Errorf("poll token: %w", err)
	}
	return token, nil
}
