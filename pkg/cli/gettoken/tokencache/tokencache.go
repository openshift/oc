package tokencache

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Set stores the id token and the refresh token
// decoded from the cache.
type Set struct {
	IDToken      string
	RefreshToken string
}

// Key stores the all fields that generate a unique cache key
// to return the correct tokens. It determines the level of
// singularity
type Key struct {
	IssuerURL string
	ClientID  string
}

// tokenCacheEntity is a internal structure for the in-memory token
// caching mechanism. Since this is not exported, this allows us to store different
// data structure.
type tokenCacheEntity struct {
	IDToken      string `json:"id_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type Repository struct{}

// FindByKey finds the key from the given cache directory and
// key and returns id token and refresh token
func (r *Repository) FindByKey(dir string, key Key) (*Set, error) {
	filename, err := computeFilename(key)
	if err != nil {
		return nil, fmt.Errorf("could not compute the key: %w", err)
	}
	p := filepath.Join(dir, filename)
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	var e tokenCacheEntity
	if err := d.Decode(&e); err != nil {
		os.Remove(p)
		return nil, fmt.Errorf("invalid json file %s: %w", p, err)
	}
	return &Set{
		IDToken:      e.IDToken,
		RefreshToken: e.RefreshToken,
	}, nil
}

// Save saves the id token and refresh token into the given
// directory with the key
func (r *Repository) Save(dir string, key Key, tokenSet Set) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("could not create directory %s: %w", dir, err)
	}
	filename, err := computeFilename(key)
	if err != nil {
		return fmt.Errorf("could not compute the key: %w", err)
	}
	p := filepath.Join(dir, filename)
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("could not create file %s: %w", p, err)
	}
	defer f.Close()
	e := tokenCacheEntity{
		IDToken:      tokenSet.IDToken,
		RefreshToken: tokenSet.RefreshToken,
	}
	if err := json.NewEncoder(f).Encode(&e); err != nil {
		return fmt.Errorf("json encode error: %w", err)
	}
	return nil
}

func computeFilename(key Key) (string, error) {
	s := sha256.New()
	e := gob.NewEncoder(s)
	if err := e.Encode(&key); err != nil {
		return "", fmt.Errorf("could not encode the key: %w", err)
	}
	h := hex.EncodeToString(s.Sum(nil))
	return h, nil
}
