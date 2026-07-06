package archive

import (
	"archive/tar"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/moby/go-archive"
	"golang.org/x/sys/unix"
)

var errNotSupportedPlatform = errors.New("platform and architecture is not supported")

func lgetxattr(path, attr string) ([]byte, error) {
	dest := make([]byte, 128)
	sz, errno := unix.Lgetxattr(path, attr, dest)
	for errors.Is(errno, unix.ERANGE) {
		sz, errno = unix.Lgetxattr(path, attr, []byte{})
		if errno != nil {
			return nil, errno
		}
		dest = make([]byte, sz)
		sz, errno = unix.Lgetxattr(path, attr, dest)
	}
	if errors.Is(errno, unix.ENODATA) {
		return nil, nil
	}
	if errno != nil {
		return nil, errno
	}
	return dest[:sz], nil
}

func lsetxattr(path, attr string, data []byte, flags int) error {
	return unix.Lsetxattr(path, attr, data, flags)
}

func lutimesNano(path string, ts []syscall.Timespec) error {
	uts := []unix.Timespec{
		unix.NsecToTimespec(syscall.TimespecToNsec(ts[0])),
		unix.NsecToTimespec(syscall.TimespecToNsec(ts[1])),
	}
	err := unix.UtimesNanoAt(unix.AT_FDCWD, path, uts, unix.AT_SYMLINK_NOFOLLOW)
	if err != nil && !errors.Is(err, unix.ENOSYS) {
		return err
	}
	return nil
}

func getWhiteoutConverter(format archive.WhiteoutFormat) tarWhiteoutConverter {
	if format == archive.OverlayWhiteoutFormat {
		return overlayWhiteoutConverter{}
	}
	return nil
}

type overlayWhiteoutConverter struct{}

func (overlayWhiteoutConverter) ConvertWrite(hdr *tar.Header, path string, fi os.FileInfo) (wo *tar.Header, err error) {
	// convert whiteouts to AUFS format
	if fi.Mode()&os.ModeCharDevice != 0 && hdr.Devmajor == 0 && hdr.Devminor == 0 {
		// we just rename the file and make it normal
		dir, filename := filepath.Split(hdr.Name)
		hdr.Name = filepath.Join(dir, archive.WhiteoutPrefix+filename)
		hdr.Mode = 0600
		hdr.Typeflag = tar.TypeReg
		hdr.Size = 0
	}

	if fi.Mode()&os.ModeDir != 0 {
		// convert opaque dirs to AUFS format by writing an empty file with the prefix
		opaque, err := lgetxattr(path, "trusted.overlay.opaque")
		if err != nil {
			return nil, err
		}
		if len(opaque) == 1 && opaque[0] == 'y' {
			if hdr.Xattrs != nil {
				delete(hdr.Xattrs, "trusted.overlay.opaque")
			}

			// create a header for the whiteout file
			// it should inherit some properties from the parent, but be a regular file
			wo = &tar.Header{
				Typeflag:   tar.TypeReg,
				Mode:       hdr.Mode & int64(os.ModePerm),
				Name:       filepath.Join(hdr.Name, archive.WhiteoutOpaqueDir),
				Size:       0,
				Uid:        hdr.Uid,
				Uname:      hdr.Uname,
				Gid:        hdr.Gid,
				Gname:      hdr.Gname,
				AccessTime: hdr.AccessTime,
				ChangeTime: hdr.ChangeTime,
			}
		}
	}

	return
}

func (overlayWhiteoutConverter) ConvertRead(hdr *tar.Header, path string) (bool, error) {
	base := filepath.Base(path)
	dir := filepath.Dir(path)

	// if a directory is marked as opaque by the AUFS special file, we need to translate that to overlay
	if base == archive.WhiteoutOpaqueDir {
		err := unix.Setxattr(dir, "trusted.overlay.opaque", []byte{'y'}, 0)
		// don't write the file itself
		return false, err
	}

	// if a file was deleted and we are using overlay, we need to create a character device
	if strings.HasPrefix(base, archive.WhiteoutPrefix) {
		originalBase := base[len(archive.WhiteoutPrefix):]
		originalPath := filepath.Join(dir, originalBase)

		if err := unix.Mknod(originalPath, unix.S_IFCHR, 0); err != nil {
			return false, err
		}
		if err := os.Chown(originalPath, hdr.Uid, hdr.Gid); err != nil {
			return false, err
		}

		// don't write the file itself
		return false, nil
	}

	return true, nil
}
