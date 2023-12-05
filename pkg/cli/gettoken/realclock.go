package gettoken

import (
	"io"
	"time"
)

type Real struct{}

// Now returns the current time.
func (c *Real) Now() time.Time {
	return time.Now()
}

type Writer struct {
	out io.Writer
}
