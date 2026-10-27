package rsync

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// TestLocalFileDiscoverer tests the local file discovery with temporary files.
func TestLocalFileDiscoverer(t *testing.T) {
	// Create temporary directory with test files.
	tempDir, err := os.MkdirTemp("", "oc-rsync-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test files with different modification times.
	baseTime := time.Now()
	testFiles := []struct {
		name    string
		modTime time.Time
	}{
		{"03_newest.log", baseTime.Add(-1 * time.Minute)},  // newest
		{"02_middle.log", baseTime.Add(-5 * time.Minute)},  // middle
		{"01_oldest.log", baseTime.Add(-10 * time.Minute)}, // oldest
	}

	for _, file := range testFiles {
		// Create the file.
		filePath := filepath.Join(tempDir, file.name)
		f, err := os.Create(filePath)
		if err != nil {
			t.Fatalf("failed to create test file %s: %v", filePath, err)
		}
		f.Close()

		// Set modification time.
		if err := os.Chtimes(filePath, file.modTime, file.modTime); err != nil {
			t.Fatalf("failed to set mtime for %s: %v", filePath, err)
		}
	}

	// Create a subdirectory (which should be ignored).
	subDir := filepath.Join(tempDir, "subdir")
	err = os.Mkdir(subDir, 0755)
	if err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}

	testCases := []struct {
		name          string
		last          uint
		expectedFiles []string
	}{
		{
			name: "limit to 2 files",
			last: 2,
			expectedFiles: []string{
				"03_newest.log",
				"02_middle.log",
			},
		},
		{
			name: "limit higher than available files",
			last: 5,
			expectedFiles: []string{
				"03_newest.log",
				"02_middle.log",
				"01_oldest.log",
			},
		},
		{
			name: "limit to 1 file",
			last: 1,
			expectedFiles: []string{
				"03_newest.log",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			files, err := newLocalFileDiscoverer().DiscoverFiles(tempDir, tc.last)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if !cmp.Equal(files, tc.expectedFiles) {
				t.Errorf("expected files mismatch: \n%s\n", cmp.Diff(tc.expectedFiles, files))
			}
		})
	}
}
