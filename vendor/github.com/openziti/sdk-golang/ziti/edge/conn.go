/*
	Copyright 2019 NetFoundry, Inc.

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

package edge

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/michaelquigley/pfxlog"
	"github.com/openziti/foundation/channel2"
	"github.com/openziti/foundation/transport"
	"github.com/openziti/foundation/transport/tls"
	"github.com/openziti/foundation/util/sequence"
	"github.com/pkg/errors"
)

type addrParser struct {
	p tls.AddressParser
}

func (ap addrParser) Parse(addressString string) (transport.Address, error) {
	return ap.p.Parse(strings.Replace(addressString, "/", "", -1))
}

func init() {
	transport.AddAddressParser(new(addrParser))
}

type RouterClient interface {
	Connect(service *Service, session *Session, options *DialOptions) (Conn, error)
	Listen(service *Service, session *Session, options *ListenOptions) (Listener, error)
}

type RouterConn interface {
	io.Closer
	RouterClient
	IsClosed() bool
	Key() string
	GetRouterName() string
}

type Identifiable interface {
	Id() uint32
}

type Listener interface {
	net.Listener
	AcceptEdge() (Conn, error)
	IsClosed() bool
	UpdateCost(cost uint16) error
	UpdatePrecedence(precedence Precedence) error
	UpdateCostAndPrecedence(cost uint16, precedence Precedence) error
	SendHealthEvent(pass bool) error
}

type SessionListener interface {
	Listener
	GetCurrentSession() *Session
	SetConnectionChangeHandler(func(conn []Listener))
	SetErrorEventHandler(func(error))
	GetErrorEventHandler() func(error)
}

type CloseWriter interface {
	CloseWrite() error
}

type ServiceConn interface {
	net.Conn
	CloseWriter
	IsClosed() bool
	GetAppData() []byte
	SourceIdentifier() string
}

type Conn interface {
	ServiceConn
	Identifiable
	CompleteAcceptSuccess() error
	CompleteAcceptFailed(err error)
}

type MsgChannel struct {
	channel2.Channel
	id            uint32
	msgIdSeq      *sequence.Sequence
	writeDeadline time.Time
	trace         bool
}

func NewEdgeMsgChannel(ch channel2.Channel, connId uint32) *MsgChannel {
	traceEnabled := strings.EqualFold("true", os.Getenv("ZITI_TRACE_ENABLED"))
	if traceEnabled {
		pfxlog.Logger().Info("Ziti message tracing ENABLED")
	}

	return &MsgChannel{
		Channel:  ch,
		id:       connId,
		msgIdSeq: sequence.NewSequence(),
		trace:    traceEnabled,
	}
}

func (ec *MsgChannel) Id() uint32 {
	return ec.id
}

func (ec *MsgChannel) NextMsgId() uint32 {
	return ec.msgIdSeq.Next()
}

func (ec *MsgChannel) SetWriteDeadline(t time.Time) error {
	ec.writeDeadline = t
	return nil
}

func (ec *MsgChannel) Write(data []byte) (n int, err error) {
	return ec.WriteTraced(data, nil, nil)
}

func (ec *MsgChannel) WriteTraced(data []byte, msgUUID []byte, hdrs map[int32][]byte) (int, error) {
	copyBuf := make([]byte, len(data))
	copy(copyBuf, data)

	msg := NewDataMsg(ec.id, ec.msgIdSeq.Next(), copyBuf)
	if msgUUID != nil {
		msg.Headers[UUIDHeader] = msgUUID
	}

	for k, v := range hdrs {
		msg.Headers[k] = v
	}
	ec.TraceMsg("write", msg)
	pfxlog.Logger().WithFields(GetLoggerFields(msg)).Debugf("writing %v bytes", len(copyBuf))

	// NOTE: We need to wait for the buffer to be on the wire before returning. The Writer contract
	//       states that buffers are not allowed be retained, and if we have it queued asynchronously
	//       it is retained and we can cause data corruption
	var err error
	if ec.writeDeadline.IsZero() {
		err = ec.Channel.Send(msg)
	} else {
		err = ec.Channel.SendWithTimeout(msg, time.Until(ec.writeDeadline))
	}

	if err != nil {
		return 0, err
	}

	return len(data), nil
}

func (ec *MsgChannel) SendState(msg *channel2.Message) error {
	msg.PutUint32Header(SeqHeader, ec.msgIdSeq.Next())
	ec.TraceMsg("SendState", msg)
	syncC, err := ec.SendAndSyncWithPriority(msg, channel2.Standard)
	if err != nil {
		return err
	}

	select {
	case err = <-syncC:
		return err
	case <-time.After(time.Second * 5):
		return errors.New("timed out waiting for close message send to complete")
	}
}

func (ec *MsgChannel) TraceMsg(source string, msg *channel2.Message) {
	msgUUID, found := msg.Headers[UUIDHeader]
	if ec.trace && !found {
		newUUID, err := uuid.NewRandom()
		if err == nil {
			msgUUID = newUUID[:]
			msg.Headers[UUIDHeader] = msgUUID
		} else {
			pfxlog.Logger().WithField("connId", ec.id).WithError(err).Infof("failed to create trace uuid")
		}
	}

	if msgUUID != nil {
		pfxlog.Logger().WithFields(GetLoggerFields(msg)).WithField("source", source).Debug("tracing message")
	}
}

type ConnOptions interface {
	GetConnectTimeout() time.Duration
}

type DialOptions struct {
	ConnectTimeout time.Duration
	Identity       string
	CallerId       string
	AppData        []byte
}

func (d DialOptions) GetConnectTimeout() time.Duration {
	return d.ConnectTimeout
}

type ListenOptions struct {
	Cost                  uint16
	Precedence            Precedence
	ConnectTimeout        time.Duration
	MaxConnections        int
	Identity              string
	IdentitySecret        string
	BindUsingEdgeIdentity bool
	ManualStart           bool
}

func (options *ListenOptions) GetConnectTimeout() time.Duration {
	return options.ConnectTimeout
}

func (options *ListenOptions) String() string {
	return fmt.Sprintf("[ListenOptions cost=%v, max-connections=%v]", options.Cost, options.MaxConnections)
}
