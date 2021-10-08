package cowslice

import (
	"reflect"
	"sync"
	"sync/atomic"
)

type CowSlice struct {
	value atomic.Value
	lock  sync.Mutex
}

func NewCowSlice(initialValue interface{}) *CowSlice {
	result := &CowSlice{}
	result.value.Store(initialValue)
	return result
}

func (slice *CowSlice) Value() interface{} {
	return slice.value.Load()
}

func Append(registry *CowSlice, listener interface{}) {
	registry.lock.Lock()
	defer registry.lock.Unlock()

	currentSlice := registry.value.Load()
	newSlice := reflect.Append(reflect.ValueOf(currentSlice), reflect.ValueOf(listener))
	registry.value.Store(newSlice.Interface())
}

func Delete(registry *CowSlice, listener interface{}) {
	registry.lock.Lock()
	defer registry.lock.Unlock()

	currentSlice := registry.value.Load()
	t := reflect.TypeOf(currentSlice)
	val := reflect.ValueOf(currentSlice)

	cap := val.Len() - 1

	if cap < 0 {
		cap = 0
	}

	newSlice := reflect.MakeSlice(t, 0, cap)
	for i := 0; i < val.Len(); i++ {
		next := val.Index(i)
		if next.Interface() != listener {
			newSlice = reflect.Append(newSlice, next)
		}
	}
	registry.value.Store(newSlice.Interface())
}
