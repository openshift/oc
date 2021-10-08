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

package sequencer

import (
	"github.com/emirpasic/gods/trees/btree"
	"github.com/emirpasic/gods/utils"
	"github.com/openziti/foundation/util/concurrenz"
	"github.com/pkg/errors"
	"time"
)

func NewSingleWriterSeq(maxOutOfOrder uint32) Sequencer {
	return &singleWriterBtreeSeq{
		maxOutOfOrder: int(maxOutOfOrder),
		ch:            make(chan interface{}, 16),
		tree:          btree.NewWith(4, utils.UInt32Comparator),
		nextSeq:       1,
		closeNotify:   make(chan struct{}),
	}
}

// singleWriterBtreeSeq is a single write, multi reader capable sequencer
type singleWriterBtreeSeq struct {
	maxOutOfOrder int
	ch            chan interface{}
	tree          *btree.Tree
	nextSeq       uint32
	closed        concurrenz.AtomicBoolean
	closeNotify   chan struct{}
}

func (seq *singleWriterBtreeSeq) PutSequenced(itemSeq uint32, val interface{}) error {
	if seq.closed.Get() {
		return ErrClosed
	}
	if seq.nextSeq == itemSeq {
		if err := seq.enqueue(val); err != nil {
			return err
		}
		for !seq.tree.Empty() {
			nextKey := seq.tree.LeftKey().(uint32)
			if seq.nextSeq != nextKey {
				return nil
			}
			nextVal := seq.tree.LeftValue()
			seq.tree.Remove(nextKey)
			if err := seq.enqueue(nextVal); err != nil {
				return err
			}
		}
	} else if seq.tree.Size() < seq.maxOutOfOrder {
		seq.tree.Put(itemSeq, val)
	} else {
		return errors.Errorf("exceeded max out of order entries: %v", seq.maxOutOfOrder)
	}
	return nil
}

func (seq *singleWriterBtreeSeq) enqueue(val interface{}) error {
	select {
	case seq.ch <- val:
		seq.nextSeq++
		return nil
	case <-seq.closeNotify:
		return ErrClosed
	}
}

func (seq *singleWriterBtreeSeq) GetNext() interface{} {
	select {
	case val := <-seq.ch:
		return val
	case <-seq.closeNotify:
		// If we're closed, return any buffered values, otherwise return nil
		select {
		case val := <-seq.ch:
			return val
		default:
			return nil
		}
	}
}

func (seq *singleWriterBtreeSeq) GetNextWithDeadline(t time.Time) (interface{}, error) {
	if t.IsZero() {
		result := seq.GetNext()
		if result == nil {
			return nil, ErrClosed
		}
		return result, nil
	}

	select {
	case v := <-seq.ch:
		return v, nil
	case <-seq.closeNotify:
		select {
		case val := <-seq.ch:
			return val, nil
		default:
			return nil, ErrClosed
		}
	case <-time.After(time.Until(t)):
		return nil, ErrTimedOut
	}
}

// Close should be called if a non-producer threads wants to notify the producer that it should stop producing
func (seq *singleWriterBtreeSeq) Close() {
	if seq.closed.CompareAndSwap(false, true) {
		close(seq.closeNotify)
	}
}
