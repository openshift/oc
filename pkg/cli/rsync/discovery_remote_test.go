package rsync

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestRemoteFileDiscoverer tests the remote file discovery with mocked executor.
func TestRemoteFileDiscoverer(t *testing.T) {
	testCases := []struct {
		name            string
		basePath        string
		last            uint
		mockOutput      string
		mockError       error
		expectedCommand []string
		expectedFiles   []string
		expectedError   bool
	}{
		{
			name:     "successful discovery with 3 files",
			basePath: "/test/path",
			last:     3,
			mockOutput: `12345.123 /test/path/03_newest.log
12345.456 /test/path/02_middle.log
12345.789 /test/path/01_oldest.log`,
			expectedFiles: []string{
				"03_newest.log",
				"02_middle.log",
				"01_oldest.log",
			},
		},
		{
			name:     "discovery with fewer files than limit",
			basePath: "/test/path",
			last:     5,
			mockOutput: `12345.123 /test/path/01_file.log
12345.456 /test/path/02_file.log`,
			expectedFiles: []string{
				"01_file.log",
				"02_file.log",
			},
		},
		{
			name:          "empty directory",
			basePath:      "/test/empty",
			last:          3,
			mockOutput:    "",
			expectedFiles: []string{},
		},
		{
			name:     "escape base path containing a single quote",
			basePath: `/test'path`,
			last:     3,
			expectedCommand: []string{
				"sh", "-c",
				`find '/test'\''path' -maxdepth 1 -type f -printf '%T@ %p\n' | sort -rn | head -n 3`,
			},
			expectedFiles: []string{},
		},
		{
			name:          "command execution error",
			basePath:      "/test/error",
			last:          3,
			mockError:     fmt.Errorf("command failed"),
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			expectedCommand := tc.expectedCommand
			if expectedCommand == nil {
				expectedCommand = []string{
					"sh", "-c",
					`find '` + tc.basePath + `' -maxdepth 1 -type f -printf '%T@ %p\n' | sort -rn | head -n ` + strconv.Itoa(int(tc.last)),
				}
			}

			executor := &mockExecutor{
				t:               t,
				expectedCommand: expectedCommand,
				output:          tc.mockOutput,
				err:             tc.mockError,
			}

			files, err := newRemoteFileDiscoverer(executor).DiscoverFiles(tc.basePath, tc.last)
			if tc.expectedError {
				if err == nil {
					t.Fatal("expected error but got none")
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !cmp.Equal(files, tc.expectedFiles) {
				t.Errorf("expected files mismatch: \n%s\n", cmp.Diff(tc.expectedFiles, files))
			}
		})
	}
}
