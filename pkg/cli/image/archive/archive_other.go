//go:build !linux
// +build !linux

package archive

import (
	"errors"
	"syscall"

	"github.com/moby/go-archive"
)

var errNotSupportedPlatform = errors.New("platform and architecture is not supported")

func getWhiteoutConverter(format archive.WhiteoutFormat) tarWhiteoutConverter {
	return nil
}

func lsetxattr(path, attr string, data []byte, flags int) error {
	return errNotSupportedPlatform
}

func lutimesNano(path string, ts []syscall.Timespec) error {
	return errNotSupportedPlatform
}
