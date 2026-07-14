//go:build !linux
// +build !linux

package archive

import "github.com/moby/go-archive"

func getWhiteoutConverter(format archive.WhiteoutFormat) tarWhiteoutConverter {
	return nil
}
