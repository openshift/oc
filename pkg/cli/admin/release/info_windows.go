//go:build windows
// +build windows

package release

// There's definitely a Windows-equivalent API here, but this author can't test
// it easily and also has doubts it will be exercised meaningfully in practice,
// so decided to defer on this for now.

func flock(fd int, exclusive bool) error {
	return nil
}

func funlock(fd int) error {
	return nil
}
