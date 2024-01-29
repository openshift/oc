package oidc

import (
	"testing"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

func TestAuthorizationRequestOptions(t *testing.T) {
	oauthCodeOptions := authorizationRequestOptions("test-nonce", "test-pkce")
	expectedOptions := []oauth2.AuthCodeOption{
		oauth2.AccessTypeOffline,
		gooidc.Nonce("test-nonce"),
		oauth2.S256ChallengeOption("test-pkce"),
	}
	if len(oauthCodeOptions) != len(expectedOptions) {
		t.Errorf("unexpected oauth code options length %d expected %d", len(oauthCodeOptions), len(expectedOptions))
	}
}
