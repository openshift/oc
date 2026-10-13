package testdata

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed oc_cli
var embeddedFixtures embed.FS

var fixtureDir string

func init() {
	var err error
	// Create a temporary directory for extracted fixtures
	fixtureDir, err = os.MkdirTemp("", "oc-testdata-fixtures-")
	if err != nil {
		panic(fmt.Sprintf("failed to create fixture directory: %v", err))
	}
}

// FixturePath returns the filesystem path to a fixture file or directory, extracting it from
// embedded files if necessary. The relativePath should be like "testdata/oc_cli/file.yaml" or "oc_cli/file.yaml"
func FixturePath(elem ...string) string {
	relativePath := filepath.Join(elem...)

	// Normalize the path for embed.FS (always use forward slashes, remove testdata/ prefix)
	embedPath := strings.ReplaceAll(relativePath, string(filepath.Separator), "/")
	embedPath = strings.TrimPrefix(embedPath, "testdata/")

	// Target path in temp directory
	targetPath := filepath.Join(fixtureDir, relativePath)

	// Check if already extracted
	if _, err := os.Stat(targetPath); err == nil {
		return targetPath
	}

	// Check if this is a directory or file in embed.FS
	info, err := fs.Stat(embeddedFixtures, embedPath)
	if err != nil {
		panic(fmt.Sprintf("failed to stat embedded path %s: %v", embedPath, err))
	}

	if info.IsDir() {
		// It's a directory - extract all files recursively
		err := fs.WalkDir(embeddedFixtures, embedPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Calculate target path
			relPath := strings.TrimPrefix(path, embedPath)
			relPath = strings.TrimPrefix(relPath, "/")
			target := filepath.Join(targetPath, relPath)

			if d.IsDir() {
				// Create directory
				return os.MkdirAll(target, 0700)
			}

			// Create parent directory
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}

			// Read and write file
			data, err := embeddedFixtures.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, 0644)
		})
		if err != nil {
			panic(fmt.Sprintf("failed to extract directory %s: %v", embedPath, err))
		}
	} else {
		// It's a file
		// Create parent directory
		if err := os.MkdirAll(filepath.Dir(targetPath), 0700); err != nil {
			panic(fmt.Sprintf("failed to create directory for %s: %v", relativePath, err))
		}

		// Read from embedded FS
		data, err := embeddedFixtures.ReadFile(embedPath)
		if err != nil {
			panic(fmt.Sprintf("failed to read embedded file %s: %v", embedPath, err))
		}

		// Write to temp directory
		if err := os.WriteFile(targetPath, data, 0644); err != nil {
			panic(fmt.Sprintf("failed to write fixture file %s: %v", targetPath, err))
		}
	}

	return targetPath
}

// GetFixtureDir returns the temporary directory where fixtures are extracted
func GetFixtureDir() string {
	return fixtureDir
}

// CleanupFixtures removes the temporary fixture directory
func CleanupFixtures() error {
	if fixtureDir != "" {
		return os.RemoveAll(fixtureDir)
	}
	return nil
}

// ListFixtures returns all available fixture paths
func ListFixtures() []string {
	var fixtures []string
	fs.WalkDir(embeddedFixtures, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			fixtures = append(fixtures, path)
		}
		return nil
	})
	return fixtures
}
