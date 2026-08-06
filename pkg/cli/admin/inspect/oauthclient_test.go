package inspect

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	oauthv1 "github.com/openshift/api/oauth/v1"
)

func TestElideOAuthClient(t *testing.T) {
	tests := []struct {
		name     string
		input    oauthv1.OAuthClient
		expected oauthv1.OAuthClient
	}{
		{
			name:     "empty client is unchanged",
			input:    oauthv1.OAuthClient{},
			expected: oauthv1.OAuthClient{},
		},
		{
			name: "secret is replaced with length stub",
			input: oauthv1.OAuthClient{
				Secret: "supersecret",
			},
			expected: oauthv1.OAuthClient{
				Secret: "11 bytes long",
			},
		},
		{
			name: "additional secrets are replaced with length stubs",
			input: oauthv1.OAuthClient{
				AdditionalSecrets: []string{"abc", "abcdefgh"},
			},
			expected: oauthv1.OAuthClient{
				AdditionalSecrets: []string{"3 bytes long", "8 bytes long"},
			},
		},
		{
			name: "both secret and additional secrets are elided",
			input: oauthv1.OAuthClient{
				Secret:            "topsecret",
				AdditionalSecrets: []string{"additionalsecret"},
			},
			expected: oauthv1.OAuthClient{
				Secret:            "9 bytes long",
				AdditionalSecrets: []string{"16 bytes long"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			elideOAuthClient(&tt.input)
			if diff := cmp.Diff(tt.expected, tt.input); diff != "" {
				t.Errorf("elideOAuthClient() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
