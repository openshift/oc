package imagesource

import (
	"runtime"
	"testing"
)

func TestGenerateDigestPath(t *testing.T) {
	path := generateDigestPath("sha256:123456", "mnt", "mirror")
	desiredPath := "mnt/mirror/sha256:123456"

	if runtime.GOOS == "windows" {
		desiredPath = "mnt\\mirror\\sha256-123456"
	}

	if path != desiredPath {
		t.Errorf("path %s does not equal desired path %s", path, desiredPath)
	}
}
