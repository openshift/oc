//go:build !windows
// +build !windows

package release

import "syscall"

func flock(fd int, exclusive bool) error {
	flag := syscall.LOCK_SH
	if exclusive {
		flag = syscall.LOCK_EX
	}
	if err := syscall.Flock(fd, flag); err != nil {
		return err
	}
	return nil
}

func funlock(fd int) error {
	if err := syscall.Flock(fd, syscall.LOCK_UN); err != nil {
		return err
	}
	return nil
}
