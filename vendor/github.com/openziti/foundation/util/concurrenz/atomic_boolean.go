/*
	Copyright NetFoundry, Inc.

	Licensed under the Apache License, Version 2.0 (the "License");
	you may not use this file except in compliance with the License.
	You may obtain a copy of the License at

	https://www.apache.org/licenses/LICENSE-2.0

	Unless required by applicable law or agreed to in writing, software
	distributed under the License is distributed on an "AS IS" BASIS,
	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
	See the License for the specific language governing permissions and
	limitations under the License.
*/

package concurrenz

import (
	"errors"
	"sync/atomic"
	"time"
)

type AtomicBoolean int32

func (ab *AtomicBoolean) Set(val bool) {
	atomic.StoreInt32((*int32)(ab), boolToInt(val))
}

func (ab *AtomicBoolean) Get() bool {
	return atomic.LoadInt32((*int32)(ab)) == 1
}

// GetUnsafe returns the value if you are sure you are getting from the same thread as the last set
// This is only useful if you only set from one goroutine and are only using Get to sync access across
// other threads. GetUnsafe can then be used from the Set goroutine
func (ab *AtomicBoolean) GetUnsafe() bool {
	return *ab == 1
}

// CompareAndSwap sets the given value only if the current value is equal to expected. return true if the swap was made
func (ab *AtomicBoolean) CompareAndSwap(expected, val bool) bool {
	return atomic.CompareAndSwapInt32((*int32)(ab), boolToInt(expected), boolToInt(val))
}

func (ab *AtomicBoolean) WaitForState(val bool, timeout time.Duration, pollInterval time.Duration) error {
	deadline := time.Now().Add(timeout)
	for ab.Get() != val {
		if deadline.After(time.Now()) {
			time.Sleep(pollInterval)
		} else {
			return errors.New("timed out waiting for condition")
		}
	}
	return nil
}

func boolToInt(val bool) int32 {
	if val {
		return 1
	}
	return 0
}