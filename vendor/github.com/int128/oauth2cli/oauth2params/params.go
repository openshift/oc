// Package oauth2params provides the generators of parameters such as state and PKCE.
package oauth2params

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"

	"golang.org/x/oauth2"
)

// NewState returns a state parameter.
// This generates 256 bits of random bytes.
func NewState() (string, error) {
	b, err := random(32)
	if err != nil {
		return "", fmt.Errorf("could not generate a random: %w", err)
	}
	return base64URLEncode(b), nil
}

// PKCE represents a set of PKCE parameters.
// See https://tools.ietf.org/html/rfc7636.
type PKCE struct {
	CodeChallenge       string
	CodeChallengeMethod string
	CodeVerifier        string
}

// AuthCodeOptions returns options for oauth2.Config.AuthCodeURL().
func (pkce *PKCE) AuthCodeOptions() []oauth2.AuthCodeOption {
	return []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_challenge_method", pkce.CodeChallengeMethod),
		oauth2.SetAuthURLParam("code_challenge", pkce.CodeChallenge),
	}
}

// TokenRequestOptions returns options for oauth2.Config.Exchange().
func (pkce *PKCE) TokenRequestOptions() []oauth2.AuthCodeOption {
	return []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_verifier", pkce.CodeVerifier),
	}
}

// NewPKCE returns a PKCE parameter.
// This generates 256 bits of random bytes.
func NewPKCE() (*PKCE, error) {
	b, err := random(32)
	if err != nil {
		return nil, fmt.Errorf("could not generate a random: %w", err)
	}
	s := computeS256(b)
	return &s, nil
}

func computeS256(b []byte) PKCE {
	v := base64URLEncode(b)
	s := sha256.New()
	_, _ = s.Write([]byte(v))
	return PKCE{
		CodeChallenge:       base64URLEncode(s.Sum(nil)),
		CodeChallengeMethod: "S256",
		CodeVerifier:        v,
	}
}

func random(bits int) ([]byte, error) {
	b := make([]byte, bits)
	if err := binary.Read(rand.Reader, binary.LittleEndian, b); err != nil {
		return nil, fmt.Errorf("read error: %w", err)
	}
	return b, nil
}

func base64URLEncode(b []byte) string {
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b)
}
