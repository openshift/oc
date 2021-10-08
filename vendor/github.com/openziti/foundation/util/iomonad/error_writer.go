package iomonad

import (
	"fmt"
	"github.com/openziti/foundation/util/errorz"
	"io"
)

type ErrorWriter interface {
	errorz.ErrorHolder
	Write([]byte) int
	Print(string)
	Println(string)
	Printf(s string, args ...interface{})
}

func Wrap(writer io.Writer) ErrorWriter {
	return &writerWrapper{
		Writer: writer,
	}
}

type writerWrapper struct {
	errorz.ErrorHolderImpl
	io.Writer
}

func (w *writerWrapper) Print(s string) {
	w.Write([]byte(s))
}

func (w *writerWrapper) Println(s string) {
	w.Write([]byte(s))
	w.Write([]byte("\n"))
}

func (w *writerWrapper) Printf(s string, args ...interface{}) {
	w.Write([]byte(fmt.Sprintf(s, args...)))
}

func (w *writerWrapper) Write(b []byte) int {
	if !w.HasError() {
		n, err := w.Writer.Write(b)
		w.SetError(err)
		return n
	}
	return 0
}
