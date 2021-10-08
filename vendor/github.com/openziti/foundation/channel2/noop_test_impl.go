package channel2

import (
	"crypto/x509"
	"github.com/openziti/foundation/identity/identity"
	"time"
)

type NoopTestChannel struct {
}

func (ch *NoopTestChannel) StartRx() {
}

func (ch *NoopTestChannel) Id() *identity.TokenId {
	panic("implement Id()")
}

func (ch *NoopTestChannel) LogicalName() string {
	panic("implement LogicalName()")
}

func (ch *NoopTestChannel) ConnectionId() string {
	panic("implement ConnectionId()")
}

func (ch *NoopTestChannel) Certificates() []*x509.Certificate {
	panic("implement Certificates()")
}

func (ch *NoopTestChannel) Label() string {
	return "testchannel"
}

func (ch *NoopTestChannel) SetLogicalName(string) {
	panic("implement SetLogicalName")
}

func (ch *NoopTestChannel) Bind(BindHandler) error {
	panic("implement Bind")
}

func (ch *NoopTestChannel) AddPeekHandler(PeekHandler) {
	panic("implement AddPeekHandler")
}

func (ch *NoopTestChannel) AddTransformHandler(TransformHandler) {
	panic("implement AddTransformHandler")
}

func (ch *NoopTestChannel) AddReceiveHandler(ReceiveHandler) {
	panic("implement AddReceiveHandler")
}

func (ch *NoopTestChannel) AddErrorHandler(ErrorHandler) {
	panic("implement me")
}

func (ch *NoopTestChannel) AddCloseHandler(CloseHandler) {
	panic("implement AddErrorHandler")
}

func (ch *NoopTestChannel) SetUserData(interface{}) {
	panic("implement SetUserData")
}

func (ch *NoopTestChannel) GetUserData() interface{} {
	panic("implement GetUserData")
}

func (ch *NoopTestChannel) Send(*Message) error {
	return nil
}

func (ch *NoopTestChannel) SendWithPriority(*Message, Priority) error {
	return nil
}

func (ch *NoopTestChannel) SendAndSync(m *Message) (chan error, error) {
	return ch.SendAndSyncWithPriority(m, Standard)
}

func (ch *NoopTestChannel) SendAndSyncWithPriority(*Message, Priority) (chan error, error) {
	result := make(chan error, 1)
	result <- nil
	return result, nil
}

func (ch *NoopTestChannel) SendWithTimeout(*Message, time.Duration) error {
	return nil
}

func (ch *NoopTestChannel) SendPrioritizedWithTimeout(*Message, Priority, time.Duration) error {
	return nil
}

func (ch *NoopTestChannel) SendAndWaitWithTimeout(*Message, time.Duration) (*Message, error) {
	panic("implement SendAndWaitWithTimeout")
}

func (ch *NoopTestChannel) SendPrioritizedAndWaitWithTimeout(*Message, Priority, time.Duration) (*Message, error) {
	panic("implement SendPrioritizedAndWaitWithTimeout")
}

func (ch *NoopTestChannel) SendAndWait(*Message) (chan *Message, error) {
	panic("implement SendAndWait")
}

func (ch *NoopTestChannel) SendAndWaitWithPriority(*Message, Priority) (chan *Message, error) {
	panic("implement SendAndWaitWithPriority")
}

func (ch *NoopTestChannel) SendForReply(TypedMessage, time.Duration) (*Message, error) {
	panic("implement SendForReply")
}

func (ch *NoopTestChannel) SendForReplyAndDecode(TypedMessage, time.Duration, TypedMessage) error {
	return nil
}

func (ch *NoopTestChannel) Close() error {
	panic("implement Close")
}

func (ch *NoopTestChannel) IsClosed() bool {
	panic("implement IsClosed")
}

func (ch *NoopTestChannel) Underlay() Underlay {
	panic("implement Underlay")
}

func (ch *NoopTestChannel) GetTimeSinceLastRead() time.Duration {
	return 0
}
