//go:generate sh -c "command -v go-bindata >/dev/null 2>&1 || go install github.com/go-bindata/go-bindata/v3/go-bindata@latest"
//go:generate go-bindata -nocompress -nometadata -pkg testdata -o bindata.go -prefix ../.. ../../testdata/...

package testdata

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

var fixtureDir string

func init() {
	var err error
	fixtureDir, err = ioutil.TempDir("", "oc-testdata-fixtures-")
	if err != nil {
		panic(fmt.Sprintf("failed to create fixture directory: %v", err))
	}
}

// FixturePath returns the filesystem path to a fixture file, extracting it from
// embedded bindata if necessary. The relativePath should be like "testdata/oc_cli/file.yaml" or "oc_cli/file.yaml"
func FixturePath(elem ...string) string {
	relativePath := filepath.Join(elem...)

	// bindata prefixes paths with "testdata/", so we need to handle that
	bindataPath := relativePath
	if !strings.HasPrefix(bindataPath, "testdata/") {
		bindataPath = "testdata/" + bindataPath
	}

	// The target path includes the full path as stored in bindata
	// For example: "testdata/oc_cli/file.yaml" -> "testdata/oc_cli/file.yaml" in temp dir
	targetPath := filepath.Join(fixtureDir, bindataPath)

	// Check if already extracted
	if _, err := os.Stat(targetPath); err != nil {
		// File doesn't exist, need to extract it

		// Create parent directory
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			panic(fmt.Sprintf("failed to create directory for %s: %v", relativePath, err))
		}

		// Try to restore the single asset using RestoreAsset
		err := RestoreAsset(fixtureDir, bindataPath)
		if err != nil {
			// If single file fails, try as directory
			err = RestoreAssets(fixtureDir, bindataPath)
			if err != nil {
				panic(fmt.Sprintf("failed to restore asset %s: %v", bindataPath, err))
			}
		}
	}

	// ALWAYS fix permissions, even if file already existed
	// go-bindata creates files with 0000 permissions which are unreadable

	// Fix permissions on all parent directories
	dir := filepath.Dir(targetPath)
	for dir != fixtureDir && len(dir) > len(fixtureDir) {
		os.Chmod(dir, 0755)
		dir = filepath.Dir(dir)
	}

	// Fix the target file/directory permissions
	if info, err := os.Stat(targetPath); err == nil {
		if !info.IsDir() {
			// It's a file, set to 0644 (rw-r--r--)
			if chmodErr := os.Chmod(targetPath, 0644); chmodErr != nil {
				panic(fmt.Sprintf("failed to chmod file %s: %v", targetPath, chmodErr))
			}
		} else {
			// It's a directory, set to 0755 (rwxr-xr-x) and recursively fix all files inside
			if chmodErr := os.Chmod(targetPath, 0755); chmodErr != nil {
				panic(fmt.Sprintf("failed to chmod directory %s: %v", targetPath, chmodErr))
			}
			// Recursively fix permissions for all files and subdirectories
			filepath.Walk(targetPath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() {
					return os.Chmod(path, 0755)
				}
				return os.Chmod(path, 0644)
			})
		}
	} else {
		panic(fmt.Sprintf("file %s does not exist after extraction: %v", targetPath, err))
	}

	// Double-check the file is now readable
	if _, err := os.Open(targetPath); err != nil {
		panic(fmt.Sprintf("file %s exists but cannot be opened: %v", targetPath, err))
	}

	return targetPath
}

// GetFixtureData returns the raw bytes of a fixture without writing to disk
func GetFixtureData(relativePath string) ([]byte, error) {
	bindataPath := relativePath
	if !strings.HasPrefix(bindataPath, "testdata/") {
		bindataPath = "testdata/" + bindataPath
	}
	return Asset(bindataPath)
}

// MustGetFixtureData is like GetFixtureData but panics on error
func MustGetFixtureData(relativePath string) []byte {
	data, err := GetFixtureData(relativePath)
	if err != nil {
		panic(fmt.Sprintf("failed to get fixture data for %s: %v", relativePath, err))
	}
	return data
}

// FixtureExists checks if a fixture is available in bindata
func FixtureExists(relativePath string) bool {
	bindataPath := relativePath
	if !strings.HasPrefix(bindataPath, "testdata/") {
		bindataPath = "testdata/" + bindataPath
	}
	_, err := AssetInfo(bindataPath)
	return err == nil
}

// ListFixtures returns all available fixture paths
func ListFixtures() []string {
	return AssetNames()
}

// CleanupFixtures removes the temporary fixture directory
func CleanupFixtures() error {
	if fixtureDir != "" {
		return os.RemoveAll(fixtureDir)
	}
	return nil
}

// GetFixtureDir returns the temporary directory where fixtures are extracted
func GetFixtureDir() string {
	return fixtureDir
}
