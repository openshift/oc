package sequencer

import (
	"github.com/openziti/foundation/util/concurrenz"
	"time"
)

func NewNoopSequencer(channelDepth int) Sequencer {
	return &noopSeq{
		ch:          make(chan interface{}, channelDepth),
		closeNotify: make(chan struct{}),
	}
}

type noopSeq struct {
	ch          chan interface{}
	closeNotify chan struct{}
	closed      concurrenz.AtomicBoolean
}

func (seq *noopSeq) PutSequenced(_ uint32, event interface{}) error {
	select {
	case seq.ch <- event:
		return nil
	case <-seq.closeNotify:
		return ErrClosed
	}
}

func (seq *noopSeq) GetNext() interface{} {
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

func (seq *noopSeq) GetNextWithDeadline(t time.Time) (interface{}, error) {
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

func (seq *noopSeq) Close() {
	if seq.closed.CompareAndSwap(false, true) {
		close(seq.closeNotify)
	}
}
