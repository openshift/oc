// Package oauth2params provides the generators of parameters such as state and PKCE.
package oauth2params

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
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
