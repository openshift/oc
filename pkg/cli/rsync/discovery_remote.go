package rsync

import (
	"bufio"
	"bytes"
	"fmt"
	"path"
	"strings"
)

// remoteFileDiscoverer implements fileDiscoverer interface for remote directories.
type remoteFileDiscoverer struct {
	exec executor
}

func newRemoteFileDiscoverer(remoteExec executor) remoteFileDiscoverer {
	return remoteFileDiscoverer{
		exec: remoteExec,
	}
}

func (discoverer remoteFileDiscoverer) DiscoverFiles(basePath string, lastN uint) ([]string, error) {
	// Use find + sort + head to get only the latest N files directly.
	var (
		output    bytes.Buffer
		errOutput bytes.Buffer
	)

	// Sanitize basePath. It must not contain a single quote, otherwise it could break out of
	basePath = strings.ReplaceAll(basePath, "'", "'\\''")

	cmd := []string{"sh", "-c", fmt.Sprintf("find '%s' -maxdepth 1 -type f -printf '%%T@ %%p\\n' | sort -rn | head -n %d", basePath, lastN)}
	if err := discoverer.exec.Execute(cmd, nil, &output, &errOutput); err != nil {
		return nil, fmt.Errorf("failed to execute remote find+sort+head command: %w, stderr: %s", err, errOutput.String())
	}

	// Extract file paths from the output.
	scanner := bufio.NewScanner(&output)
	filenames := make([]string, 0)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			// parts[1] is the full file path. We need filename only.
			fullPath := parts[1]
			filename := path.Base(fullPath)
			filenames = append(filenames, filename)
		} else {
			return nil, fmt.Errorf("failed to parse remote command output line: %s", line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan remote find+sort+head command output: %w", err)
	}
	return filenames, nil
}
