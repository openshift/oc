package gettoken

import (
	"time"
)

type Real struct{}

// Now returns the current time.
func (c *Real) Now() time.Time {
	return time.Now()
}
