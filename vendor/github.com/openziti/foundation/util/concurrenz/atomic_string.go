package concurrenz

import (
	"sync/atomic"
)

type AtomicString atomic.Value

func (ab *AtomicString) Set(val string) {
	(*atomic.Value)(ab).Store(val)
}

func (ab *AtomicString) Get() string {
	result := (*atomic.Value)(ab).Load()
	if result, ok := result.(string); ok {
		return result
	}
	return ""
}
