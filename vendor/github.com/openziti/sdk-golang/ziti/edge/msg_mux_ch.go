package edge

import (
	"github.com/michaelquigley/pfxlog"
	"github.com/openziti/foundation/channel2"
	"github.com/openziti/foundation/util/concurrenz"
	"github.com/pkg/errors"
	"math"
	"sync/atomic"
	"time"
)

func NewChMsgMux() *ChMsgMux {
	mux := &ChMsgMux{
		eventC:  make(chan MuxEvent),
		chanMap: make(map[uint32]MsgSink),
		maxId:   (math.MaxUint32 / 2) - 1,
	}

	mux.running.Set(true)
	go mux.handleEvents()
	return mux
}

type ChMsgMux struct {
	closed  concurrenz.AtomicBoolean
	running concurrenz.AtomicBoolean
	eventC  chan MuxEvent
	chanMap map[uint32]MsgSink
	nextId  uint32
	minId   uint32
	maxId   uint32
}

func (mux *ChMsgMux) GetNextId() uint32 {
	nextId := atomic.AddUint32(&mux.nextId, 1)
	if nextId > mux.maxId {
		atomic.StoreUint32(&mux.nextId, mux.minId)
		nextId = atomic.AddUint32(&mux.nextId, 1)
	}
	return nextId
}

func (mux *ChMsgMux) ContentType() int32 {
	return ContentTypeData
}

func (mux *ChMsgMux) HandleReceive(msg *channel2.Message, _ channel2.Channel) {
	if event, err := UnmarshalMsgEvent(msg); err != nil {
		pfxlog.Logger().WithError(err).Errorf("error unmarshaling edge message headers. content type: %v", msg.ContentType)
	} else {
		mux.eventC <- event
	}
}

func (mux *ChMsgMux) AddMsgSink(sink MsgSink) error {
	if !mux.closed.Get() {
		event := &muxAddSinkEvent{sink: sink, doneC: make(chan error)}
		mux.eventC <- event
		err, ok := <-event.doneC // wait for event to be done processing
		if ok && err != nil {
			return err
		}
		pfxlog.Logger().WithField("connId", sink.Id()).Debug("added to msg mux")
	}
	return nil
}

func (mux *ChMsgMux) RemoveMsgSink(sink MsgSink) {
	mux.RemoveMsgSinkById(sink.Id())
}

func (mux *ChMsgMux) RemoveMsgSinkById(sinkId uint32) {
	log := pfxlog.Logger().WithField("connId", sinkId)
	if mux.closed.Get() {
		log.Debug("mux closed, sink already removed or being removed")
	} else {
		log.Debug("queuing sink for removal from message mux")
		event := &muxRemoveSinkEvent{sinkId: sinkId}
		mux.eventC <- event
	}
}

func (mux *ChMsgMux) Close() {
	if !mux.closed.Get() {
		mux.eventC <- &muxCloseEvent{}
	}
}

func (mux *ChMsgMux) Event(event MuxEvent) {
	if !mux.closed.Get() {
		mux.eventC <- event
	}
}

func (mux *ChMsgMux) IsClosed() bool {
	return mux.closed.Get()
}

func (mux *ChMsgMux) HandleClose(_ channel2.Channel) {
	mux.Close()
}

func (mux *ChMsgMux) handleEvents() {
	defer mux.running.Set(false)
	for event := range mux.eventC {
		event.Handle(mux)
		if mux.closed.GetUnsafe() {
			return
		}
	}
}

func (mux *ChMsgMux) ExecuteClose() {
	mux.closed.Set(true)
	for _, val := range mux.chanMap {
		if err := val.HandleMuxClose(); err != nil {
			pfxlog.Logger().
				WithField("sinkId", val.Id()).
				WithError(err).
				Error("error while closing message sink")
		}
	}

	// make sure that anything trying to deliver events is freed
	for {
		select {
		case <-mux.eventC: // drop event
		case <-time.After(time.Millisecond * 100):
			close(mux.eventC)
			return
		}
	}
}

type MuxEvent interface {
	Handle(mux *ChMsgMux)
}

// muxAddSinkEvent handles adding a new message sink to the mux
type muxAddSinkEvent struct {
	sink  MsgSink
	doneC chan error
}

func (event *muxAddSinkEvent) Handle(mux *ChMsgMux) {
	defer close(event.doneC)
	if _, found := mux.chanMap[event.sink.Id()]; found {
		event.doneC <- errors.Errorf("message sink with id %v already exists", event.sink.Id())
	} else {
		mux.chanMap[event.sink.Id()] = event.sink
		pfxlog.Logger().
			WithField("connId", event.sink.Id()).
			Debugf("Added sink to mux. Current sink count: %v", len(mux.chanMap))
	}
}

// muxRemoveSinkEvent handles removing a closed message sink from the mux
type muxRemoveSinkEvent struct {
	sinkId uint32
}

func (event *muxRemoveSinkEvent) Handle(mux *ChMsgMux) {
	delete(mux.chanMap, event.sinkId)
	pfxlog.Logger().WithField("connId", event.sinkId).Debug("removed from msg mux")
}

func (event *MsgEvent) Handle(mux *ChMsgMux) {
	logger := pfxlog.Logger().
		WithField("seq", event.Seq).
		WithField("connId", event.ConnId)

	logger.Debugf("dispatching %v", ContentTypeNames[event.Msg.ContentType])

	if sink, found := mux.chanMap[event.ConnId]; found {
		sink.Accept(event.Msg)
	} else {
		logger.Debug("unable to dispatch msg received for unknown edge conn id")
	}
}

// muxCloseEvent handles closing the message multiplexer and all associated sinks
type muxCloseEvent struct{}

func (event *muxCloseEvent) Handle(mux *ChMsgMux) {
	mux.ExecuteClose()
}
