package rsync

import (
	"errors"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestNewTarStrategy_FileDiscovery tests the specific file discovery logic in NewTarStrategy.
func TestNewTarStrategy_FileDiscovery(t *testing.T) {
	testCases := []struct {
		name             string
		originalIncludes []string
		discoveredFiles  []string
		discoveryError   error
		expectedIncludes []string
	}{
		{
			name:             "discovery finds files - replaces original includes",
			originalIncludes: []string{"*.log", "*.txt"},
			discoveredFiles:  []string{"newest.log", "middle.log", "oldest.log"},
			expectedIncludes: []string{"newest.log", "middle.log", "oldest.log"},
		},
		{
			name:             "discovery finds no files - keeps original includes",
			originalIncludes: []string{"*.log", "*.txt"},
			discoveredFiles:  []string{},
			expectedIncludes: []string{"*.log", "*.txt"},
		},
		{
			name:             "discovery fails - keeps original includes",
			originalIncludes: []string{"*.log", "*.txt"},
			discoveryError:   errors.New("command failed"),
			expectedIncludes: []string{"*.log", "*.txt"},
		},
		{
			name:             "no original includes but discovery finds files",
			originalIncludes: []string{},
			discoveredFiles:  []string{"file1.txt", "file2.txt"},
			expectedIncludes: []string{"file1.txt", "file2.txt"},
		},
		{
			name:             "no original includes and no discovery",
			originalIncludes: []string{},
			discoveredFiles:  []string{},
			expectedIncludes: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Init the strategy.
			options := &RsyncOptions{
				RsyncInclude: tc.originalIncludes,
				Last:         3, // Enable file discovery
				Source:       &PathSpec{PodName: "test-pod", Path: "/test/path"},
				Destination:  &PathSpec{Path: "/local/path"},
				fileDiscovery: &mockFileDiscoverer{
					files: tc.discoveredFiles,
					err:   tc.discoveryError,
				},
			}

			strategy := NewTarStrategy(options).(*tarStrategy)

			// Verify the result matches expectations.
			sort.Strings(strategy.Includes)
			sort.Strings(tc.expectedIncludes)
			if !cmp.Equal(strategy.Includes, tc.expectedIncludes) {
				t.Errorf("expected includes mismatch: \n%s\n",
					cmp.Diff(tc.expectedIncludes, strategy.Includes))
			}
		})
	}
}
