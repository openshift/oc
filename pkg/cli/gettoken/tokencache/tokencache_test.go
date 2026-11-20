package tokencache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestComputeFilename(t *testing.T) {
	tests := []struct {
		name      string
		k1        Key
		k2        Key
		wantEqual bool
	}{
		{
			name:      "same keys without scopes produce same filename",
			k1:        Key{IssuerURL: "https://issuer.example.com", ClientID: "my-client"},
			k2:        Key{IssuerURL: "https://issuer.example.com", ClientID: "my-client"},
			wantEqual: true,
		},
		{
			name:      "identical sorted scopes produce same filename",
			k1:        Key{IssuerURL: "https://issuer.example.com", ClientID: "my-client", ExtraScopes: []string{"email", "profile"}},
			k2:        Key{IssuerURL: "https://issuer.example.com", ClientID: "my-client", ExtraScopes: []string{"email", "profile"}},
			wantEqual: true,
		},
		{
			name:      "presence of scopes changes filename",
			k1:        Key{IssuerURL: "https://issuer.example.com", ClientID: "my-client"},
			k2:        Key{IssuerURL: "https://issuer.example.com", ClientID: "my-client", ExtraScopes: []string{"email", "profile"}},
			wantEqual: false,
		},
		{
			name:      "differently ordered scopes produce same filename",
			k1:        Key{IssuerURL: "https://issuer.example.com", ClientID: "my-client", ExtraScopes: []string{"email", "profile"}},
			k2:        Key{IssuerURL: "https://issuer.example.com", ClientID: "my-client", ExtraScopes: []string{"profile", "email"}},
			wantEqual: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f1, err := computeFilename(tt.k1)
			if err != nil {
				t.Fatalf("computeFilename(k1) error: %v", err)
			}
			f2, err := computeFilename(tt.k2)
			if err != nil {
				t.Fatalf("computeFilename(k2) error: %v", err)
			}

			if tt.wantEqual && f1 != f2 {
				t.Errorf("expected same filename, got %s and %s", f1, f2)
			}
			if !tt.wantEqual && f1 == f2 {
				t.Errorf("expected different filenames, got %s for both", f1)
			}
		})
	}
}

func TestSaveAndFindByKey(t *testing.T) {
	dir := t.TempDir()
	repo := &Repository{}

	key := Key{
		IssuerURL:   "https://issuer.example.com",
		ClientID:    "my-client",
		ExtraScopes: []string{"email", "profile"},
	}
	want := Set{
		IDToken:      "test-id-token",
		RefreshToken: "test-refresh-token",
	}

	if err := repo.Save(dir, key, want); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	got, err := repo.FindByKey(dir, key)
	if err != nil {
		t.Fatalf("FindByKey() error: %v", err)
	}

	if diff := cmp.Diff(&want, got); diff != "" {
		t.Errorf("FindByKey() mismatch (-want +got):\n%s", diff)
	}
}

func TestFindByKey_DifferentScopesMiss(t *testing.T) {
	dir := t.TempDir()
	repo := &Repository{}

	key := Key{
		IssuerURL:   "https://issuer.example.com",
		ClientID:    "my-client",
		ExtraScopes: []string{"email"},
	}
	if err := repo.Save(dir, key, Set{IDToken: "token1", RefreshToken: "refresh1"}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	differentKey := Key{
		IssuerURL:   "https://issuer.example.com",
		ClientID:    "my-client",
		ExtraScopes: []string{"email", "profile"},
	}
	_, err := repo.FindByKey(dir, differentKey)
	if err == nil {
		t.Fatal("expected FindByKey to fail for a key with different scopes, but it succeeded")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected file-not-found error, got: %v", err)
	}
}

func TestSave_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	repo := &Repository{}

	key := Key{IssuerURL: "https://issuer.example.com", ClientID: "my-client"}
	if err := repo.Save(dir, key, Set{IDToken: "tok"}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	filename, err := computeFilename(key)
	if err != nil {
		t.Fatalf("computeFilename() error: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, filename))
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected file permission 0600, got %o", perm)
	}
}
