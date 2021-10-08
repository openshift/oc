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

package impl

import (
	"fmt"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"sync"
	"time"

	"github.com/michaelquigley/pfxlog"
	"github.com/netfoundry/secretstream"
	"github.com/netfoundry/secretstream/kx"
	"github.com/openziti/foundation/channel2"
	"github.com/openziti/foundation/util/concurrenz"
	"github.com/openziti/foundation/util/sequencer"
	"github.com/openziti/sdk-golang/ziti/edge"
	"github.com/pkg/errors"
)

var unsupportedCrypto = errors.New("unsupported crypto")

type ConnType byte

const (
	ConnTypeDial = 1
	ConnTypeBind = 2
)

var _ edge.Conn = &edgeConn{}

type edgeConn struct {
	edge.MsgChannel
	readQ                 sequencer.Sequencer
	leftover              []byte
	msgMux                edge.MsgMux
	hosting               sync.Map
	closed                concurrenz.AtomicBoolean
	readFIN               concurrenz.AtomicBoolean
	sentFIN               concurrenz.AtomicBoolean
	serviceId             string
	sourceIdentity        string
	readDeadline          time.Time
	acceptCompleteHandler *newConnHandler
	connType              ConnType

	crypto   bool
	keyPair  *kx.KeyPair
	rxKey    []byte
	receiver secretstream.Decryptor
	sender   secretstream.Encryptor
	appData  []byte
}

func (conn *edgeConn) Write(data []byte) (int, error) {
	if conn.sentFIN.Get() {
		return 0, errors.New("calling Write() after CloseWrite()")
	}

	if conn.sender != nil {
		cipherData, err := conn.sender.Push(data, secretstream.TagMessage)
		if err != nil {
			return 0, err
		}

		_, err = conn.MsgChannel.Write(cipherData)
		return len(data), err
	} else {
		return conn.MsgChannel.Write(data)
	}
}

var finHeaders = map[int32][]byte{
	edge.FlagsHeader: {edge.FIN, 0, 0, 0},
}

func (conn *edgeConn) CloseWrite() error {
	if conn.sentFIN.CompareAndSwap(false, true) {
		_, err := conn.MsgChannel.WriteTraced(nil, nil, finHeaders)
		return err
	}

	return nil
}

func (conn *edgeConn) Accept(msg *channel2.Message) {
	conn.TraceMsg("Accept", msg)
	switch conn.connType {
	case ConnTypeDial:
		if msg.ContentType == edge.ContentTypeStateClosed {
			conn.sentFIN.Set(true) // if we're not closing until all reads are done, at least prevent more writes
		}

		if err := conn.readQ.PutSequenced(0, msg); err != nil {
			logrus.WithFields(edge.GetLoggerFields(msg)).WithError(err).
				Error("error pushing edge message to sequencer")
		} else {
			logrus.WithFields(edge.GetLoggerFields(msg)).Debugf("received %v bytes (msg type: %v)", len(msg.Body), msg.ContentType)
		}

	case ConnTypeBind:
		if msg.ContentType == edge.ContentTypeDial {
			logrus.WithFields(edge.GetLoggerFields(msg)).Debug("received dial request")
			go conn.newChildConnection(msg)
		}

		if msg.ContentType == edge.ContentTypeStateClosed {
			conn.close(true)
		}
	default:
		logrus.WithFields(edge.GetLoggerFields(msg)).Errorf("invalid connection type: %v", conn.connType)
	}
}

func (conn *edgeConn) IsClosed() bool {
	return conn.closed.Get()
}

func (conn *edgeConn) Network() string {
	return conn.serviceId
}

func (conn *edgeConn) String() string {
	return fmt.Sprintf("zitiConn connId=%v svcId=%v sourceIdentity=%v", conn.Id(), conn.serviceId, conn.sourceIdentity)
}

func (conn *edgeConn) LocalAddr() net.Addr {
	return conn
}

func (conn *edgeConn) RemoteAddr() net.Addr {
	return &edge.Addr{MsgCh: conn.MsgChannel}
}

func (conn *edgeConn) SourceIdentifier() string {
	return conn.sourceIdentity
}

func (conn *edgeConn) SetDeadline(t time.Time) error {
	if err := conn.SetReadDeadline(t); err != nil {
		return err
	}
	return conn.SetWriteDeadline(t)
}

func (conn *edgeConn) SetReadDeadline(t time.Time) error {
	conn.readDeadline = t
	return nil
}

func (conn *edgeConn) HandleMuxClose() error {
	conn.close(true)
	return nil
}

func (conn *edgeConn) HandleClose(channel2.Channel) {
	logger := pfxlog.Logger().WithField("connId", conn.Id())
	defer logger.Debug("received HandleClose from underlying channel, marking conn closed")
	conn.readQ.Close()
	conn.closed.Set(true)
	conn.sentFIN.Set(true)
	conn.readFIN.Set(true)
}

func (conn *edgeConn) Connect(session *edge.Session, options *edge.DialOptions) (edge.Conn, error) {
	logger := pfxlog.Logger().WithField("connId", conn.Id())

	var pub []byte
	if conn.crypto {
		pub = conn.keyPair.Public()
	}
	connectRequest := edge.NewConnectMsg(conn.Id(), session.Token, pub, options)
	conn.TraceMsg("connect", connectRequest)
	replyMsg, err := conn.SendAndWaitWithTimeout(connectRequest, options.ConnectTimeout)
	if err != nil {
		logger.Error(err)
		return nil, err
	}

	if replyMsg.ContentType == edge.ContentTypeStateClosed {
		return nil, errors.Errorf("dial failed: %v", string(replyMsg.Body))
	}

	if replyMsg.ContentType != edge.ContentTypeStateConnected {
		return nil, errors.Errorf("unexpected response to connect attempt: %v", replyMsg.ContentType)
	}

	if conn.crypto {
		// There is no race condition where we can receive the other side crypto header
		// because the processing of the crypto header takes place in Conn.Read which
		// can't happen until we return the conn to the user. So as long as we send
		// the header and set rxkey before we return, we should be safe
		method, _ := replyMsg.GetByteHeader(edge.CryptoMethodHeader)
		hostPubKey := replyMsg.Headers[edge.PublicKeyHeader]
		if hostPubKey != nil {
			logger = logger.WithField("session", session.Id)
			logger.Debug("setting up end-to-end encryption")
			if err = conn.establishClientCrypto(conn.keyPair, hostPubKey, edge.CryptoMethod(method)); err != nil {
				logger.WithError(err).Error("crypto failure")
				_ = conn.Close()
				return nil, err
			}
			logger.Debug("client tx encryption setup done")
		} else {
			logger.Warn("connection is not end-to-end-encrypted")
		}
	}
	logger.Debug("connected")

	return conn, nil
}

func (conn *edgeConn) establishClientCrypto(keypair *kx.KeyPair, peerKey []byte, method edge.CryptoMethod) error {
	var err error
	var rx, tx []byte

	if method != edge.CryptoMethodLibsodium {
		return unsupportedCrypto
	}

	if rx, tx, err = keypair.ClientSessionKeys(peerKey); err != nil {
		return errors.Wrap(err, "failed key exchange")
	}

	var txHeader []byte
	if conn.sender, txHeader, err = secretstream.NewEncryptor(tx); err != nil {
		return errors.Wrap(err, "failed to establish crypto stream")
	}

	conn.rxKey = rx

	if _, err = conn.MsgChannel.Write(txHeader); err != nil {
		return errors.Wrap(err, "failed to write crypto header")
	}

	pfxlog.Logger().WithField("connId", conn.Id()).Debug("crypto established")
	return nil
}

func (conn *edgeConn) establishServerCrypto(keypair *kx.KeyPair, peerKey []byte, method edge.CryptoMethod) ([]byte, error) {
	var err error
	var rx, tx []byte

	if method != edge.CryptoMethodLibsodium {
		return nil, unsupportedCrypto
	}
	if rx, tx, err = keypair.ServerSessionKeys(peerKey); err != nil {
		return nil, errors.Wrap(err, "failed key exchange")
	}

	var txHeader []byte
	if conn.sender, txHeader, err = secretstream.NewEncryptor(tx); err != nil {
		return nil, errors.Wrap(err, "failed to establish crypto stream")
	}

	conn.rxKey = rx

	return txHeader, nil
}

func (conn *edgeConn) Listen(session *edge.Session, service *edge.Service, options *edge.ListenOptions) (edge.Listener, error) {
	logger := pfxlog.Logger().
		WithField("connId", conn.Id()).
		WithField("service", service.Name).
		WithField("session", session.Token)

	listener := &edgeListener{
		baseListener: baseListener{
			service: service,
			acceptC: make(chan edge.Conn, 10),
			errorC:  make(chan error, 1),
		},
		token:       session.Token,
		edgeChan:    conn,
		manualStart: options.ManualStart,
	}
	logger.Debug("adding listener for session")
	conn.hosting.Store(session.Token, listener)

	success := false
	defer func() {
		if !success {
			logger.Debug("removing listener for session")
			conn.hosting.Delete(session.Token)
		}
	}()

	logger.Debug("sending bind request to edge router")
	var pub []byte
	if conn.crypto {
		pub = conn.keyPair.Public()
	}
	bindRequest := edge.NewBindMsg(conn.Id(), session.Token, pub, options)
	conn.TraceMsg("listen", bindRequest)
	replyMsg, err := conn.SendAndWaitWithTimeout(bindRequest, 5*time.Second)
	if err != nil {
		logger.WithError(err).Error("failed to bind")
		return nil, err
	}

	if replyMsg.ContentType == edge.ContentTypeStateClosed {
		msg := string(replyMsg.Body)
		logger.Errorf("bind request resulted in disconnect. msg: (%v)", msg)
		return nil, errors.Errorf("attempt to use closed connection: %v", msg)
	}

	if replyMsg.ContentType != edge.ContentTypeStateConnected {
		logger.Errorf("unexpected response to connect attempt: %v", replyMsg.ContentType)
		return nil, errors.Errorf("unexpected response to connect attempt: %v", replyMsg.ContentType)
	}

	success = true
	logger.Debug("connected")

	return listener, nil
}

func (conn *edgeConn) Read(p []byte) (int, error) {
	log := pfxlog.Logger().WithField("connId", conn.Id())
	if conn.closed.Get() {
		return 0, io.EOF
	}

	log.Tracef("read buffer = %d bytes", cap(p))
	if len(conn.leftover) > 0 {
		log.Tracef("found %d leftover bytes", len(conn.leftover))
		n := copy(p, conn.leftover)
		conn.leftover = conn.leftover[n:]
		return n, nil
	}

	for {
		if conn.readFIN.Get() {
			return 0, io.EOF
		}

		next, err := conn.readQ.GetNextWithDeadline(conn.readDeadline)
		if err == sequencer.ErrClosed {
			log.Debug("sequencer closed, closing connection")
			conn.closed.Set(true)
			return 0, io.EOF
		} else if err != nil {
			log.Debugf("unexepcted sequencer err (%v)", err)
			return 0, err
		}

		msg := next.(*channel2.Message)

		flags, _ := msg.GetUint32Header(edge.FlagsHeader)
		if flags&edge.FIN != 0 {
			conn.readFIN.Set(true)
		}

		switch msg.ContentType {

		case edge.ContentTypeStateClosed:
			log.Debug("received ConnState_CLOSED message, closing connection")
			conn.close(true)
			continue

		case edge.ContentTypeData:
			d := msg.Body
			log.Tracef("got buffer from sequencer %d bytes", len(d))
			if len(d) == 0 && conn.readFIN.Get() {
				return 0, io.EOF
			}

			// first data message should contain crypto header
			if conn.rxKey != nil {
				if len(d) != secretstream.StreamHeaderBytes {
					return 0, errors.Errorf("failed to receive crypto header bytes: read[%d]", len(d))
				}
				conn.receiver, err = secretstream.NewDecryptor(conn.rxKey, d)
				conn.rxKey = nil
				continue
			}

			if conn.receiver != nil {
				d, _, err = conn.receiver.Pull(d)
				if err != nil {
					log.WithFields(edge.GetLoggerFields(msg)).Errorf("crypto failed on msg of size=%v, headers=%+v err=(%v)", len(msg.Body), msg.Headers, err)
					return 0, err
				}
			}
			if len(d) <= cap(p) {
				log.Debugf("reading %v bytes", len(d))
				return copy(p, d), nil
			}
			conn.leftover = d[cap(p):]
			log.Tracef("saving %d bytes for leftover", len(conn.leftover))
			log.Debugf("reading %v bytes", len(p))
			return copy(p, d), nil

		default:
			log.WithField("type", msg.ContentType).Error("unexpected message")
		}
	}
}

func (conn *edgeConn) Close() error {
	conn.close(false)
	return nil
}

func (conn *edgeConn) close(closedByRemote bool) {
	// everything in here should be safe to execute concurrently from outside the muxer loop, with
	// the exception of the remove from mux call
	if !conn.closed.CompareAndSwap(false, true) {
		return
	}
	conn.readFIN.Set(true)
	conn.sentFIN.Set(true)

	log := pfxlog.Logger().WithField("connId", conn.Id())
	log.Debug("close: begin")
	defer log.Debug("close: end")

	if !closedByRemote {
		msg := edge.NewStateClosedMsg(conn.Id(), "")
		if err := conn.SendState(msg); err != nil {
			log.WithError(err).Error("failed to send close message")
		}
	}

	conn.readQ.Close()
	conn.msgMux.RemoveMsgSink(conn) // if we switch back to ChMsgMux will need to be done async again, otherwise we may deadlock

	conn.hosting.Range(func(key, value interface{}) bool {
		listener := value.(*edgeListener)
		if err := listener.Close(); err != nil {
			log.WithError(err).Errorf("failed to close listener for service %v", listener.service.Name)
		}
		return true
	})
}

func (conn *edgeConn) getListener(token string) (*edgeListener, bool) {
	if val, found := conn.hosting.Load(token); found {
		listener, ok := val.(*edgeListener)
		return listener, ok
	}
	return nil, false
}

func (conn *edgeConn) newChildConnection(message *channel2.Message) {
	token := string(message.Body)
	logger := pfxlog.Logger().WithField("connId", conn.Id()).WithField("token", token)
	logger.Debug("looking up listener")
	listener, found := conn.getListener(token)
	if !found {
		logger.Warn("listener not found")
		reply := edge.NewDialFailedMsg(conn.Id(), "invalid token")
		reply.ReplyTo(message)
		if err := conn.SendPrioritizedWithTimeout(reply, channel2.Highest, time.Second*5); err != nil {
			logger.WithError(err).Error("failed to send reply to dial request")
		}
		return
	}

	logger.Debug("listener found. checking for router provided connection id")

	id, routerProvidedConnId := message.GetUint32Header(edge.RouterProvidedConnId)
	if routerProvidedConnId {
		logger.Debugf("using router provided connection id %v", id)
	} else {
		id = conn.msgMux.GetNextId()
		logger.Debugf("listener found. generating id for new connection: %v", id)
	}

	sourceIdentity, _ := message.GetStringHeader(edge.CallerIdHeader)

	edgeCh := &edgeConn{
		MsgChannel:     *edge.NewEdgeMsgChannel(conn.Channel, id),
		readQ:          sequencer.NewNoopSequencer(4),
		msgMux:         conn.msgMux,
		sourceIdentity: sourceIdentity,
		crypto:         conn.crypto,
		appData:        message.Headers[edge.AppDataHeader],
		connType:       ConnTypeDial,
	}

	newConnLogger := pfxlog.Logger().
		WithField("connId", id).
		WithField("parentConnId", conn.Id()).
		WithField("token", token)

	err := conn.msgMux.AddMsgSink(edgeCh) // duplicate errors only happen on the server side, since client controls ids
	if err != nil {
		newConnLogger.WithError(err).Error("invalid conn id, already in use")
		reply := edge.NewDialFailedMsg(conn.Id(), err.Error())
		reply.ReplyTo(message)
		if err := conn.SendPrioritizedWithTimeout(reply, channel2.Highest, time.Second*5); err != nil {
			logger.WithError(err).Error("failed to send reply to dial request")
		}
		return
	}

	var txHeader []byte
	if edgeCh.crypto {
		newConnLogger.Debug("setting up crypto")
		clientKey := message.Headers[edge.PublicKeyHeader]
		method, _ := message.GetByteHeader(edge.CryptoMethodHeader)

		if clientKey != nil {
			if txHeader, err = edgeCh.establishServerCrypto(conn.keyPair, clientKey, edge.CryptoMethod(method)); err != nil {
				logger.WithError(err).Error("failed to establish crypto session")
			}
		} else {
			newConnLogger.Warnf("client did not send its key. connection is not end-to-end encrypted")
		}
	}

	if err != nil {
		newConnLogger.WithError(err).Error("Failed to establish connection")
		reply := edge.NewDialFailedMsg(conn.Id(), err.Error())
		reply.ReplyTo(message)
		if err := conn.SendPrioritizedWithTimeout(reply, channel2.Highest, time.Second*5); err != nil {
			logger.WithError(err).Error("Failed to send reply to dial request")
		}
		return
	}

	connHandler := &newConnHandler{
		conn:                 conn,
		edgeCh:               edgeCh,
		message:              message,
		txHeader:             txHeader,
		routerProvidedConnId: routerProvidedConnId,
	}

	if listener.manualStart {
		edgeCh.acceptCompleteHandler = connHandler
	} else if err := connHandler.dialSucceeded(); err != nil {
		return
	}

	listener.acceptC <- edgeCh
}

func (conn *edgeConn) GetAppData() []byte {
	return conn.appData
}

func (conn *edgeConn) CompleteAcceptSuccess() error {
	if conn.acceptCompleteHandler != nil {
		result := conn.acceptCompleteHandler.dialSucceeded()
		conn.acceptCompleteHandler = nil
		return result
	}
	return nil
}

func (conn *edgeConn) CompleteAcceptFailed(err error) {
	if conn.acceptCompleteHandler != nil {
		conn.acceptCompleteHandler.dialFailed(err)
		conn.acceptCompleteHandler = nil
	}
}

type newConnHandler struct {
	conn                 *edgeConn
	edgeCh               *edgeConn
	message              *channel2.Message
	txHeader             []byte
	routerProvidedConnId bool
}

func (self *newConnHandler) dialFailed(err error) {
	token := string(self.message.Body)
	logger := pfxlog.Logger().WithField("connId", self.conn.Id()).WithField("token", token)

	newConnLogger := pfxlog.Logger().
		WithField("connId", self.edgeCh.Id()).
		WithField("parentConnId", self.conn.Id()).
		WithField("token", token)

	newConnLogger.WithError(err).Error("Failed to establish connection")
	reply := edge.NewDialFailedMsg(self.conn.Id(), err.Error())
	reply.ReplyTo(self.message)
	if err := self.conn.SendPrioritizedWithTimeout(reply, channel2.Highest, time.Second*5); err != nil {
		logger.WithError(err).Error("Failed to send reply to dial request")
	}
}

func (self *newConnHandler) dialSucceeded() error {
	token := string(self.message.Body)
	logger := pfxlog.Logger().WithField("connId", self.conn.Id()).WithField("token", token)

	newConnLogger := pfxlog.Logger().
		WithField("connId", self.edgeCh.Id()).
		WithField("parentConnId", self.conn.Id()).
		WithField("token", token)

	newConnLogger.Debug("new connection established")

	reply := edge.NewDialSuccessMsg(self.conn.Id(), self.edgeCh.Id())
	reply.ReplyTo(self.message)

	if !self.routerProvidedConnId {
		startMsg, err := self.conn.SendPrioritizedAndWaitWithTimeout(reply, channel2.Highest, time.Second*5)
		if err != nil {
			logger.WithError(err).Error("Failed to send reply to dial request")
			return err
		}

		if startMsg.ContentType != edge.ContentTypeStateConnected {
			logger.Errorf("failed to receive start after dial. got %v", startMsg)
			return errors.Errorf("failed to receive start after dial. got %v", startMsg)
		}
	} else if err := self.conn.SendPrioritizedWithTimeout(reply, channel2.Highest, time.Second*5); err != nil {
		logger.WithError(err).Error("Failed to send reply to dial request")
		return err
	}

	if self.txHeader != nil {
		newConnLogger.Debug("sending crypto header")
		if _, err := self.edgeCh.MsgChannel.Write(self.txHeader); err != nil {
			newConnLogger.WithError(err).Error("failed to write crypto header")
			return err
		}
		newConnLogger.Debug("tx crypto established")
	}
	return nil
}
