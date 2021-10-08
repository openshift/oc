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

package channel2

import (
	"crypto/x509"
)

// Binding is used to add handlers to Channel.
//
// NOTE: It is intended that the Add* methods are used at initial channel setup, and not invoked on an in-service
// Channel. This API may change in the future to enforce those semantics programmatically.
//
type Binding interface {
	Bind(h BindHandler) error
	AddPeekHandler(h PeekHandler)
	AddTransformHandler(h TransformHandler)
	AddReceiveHandler(h ReceiveHandler)
	AddErrorHandler(h ErrorHandler)
	AddCloseHandler(h CloseHandler)
	SetUserData(data interface{})
	GetUserData() interface{}
}

type BindHandler interface {
	BindChannel(ch Channel) error
}

type ConnectionHandler interface {
	HandleConnection(hello *Hello, certificates []*x509.Certificate) error
}

type PeekHandler interface {
	Connect(ch Channel, remoteAddress string)
	Rx(m *Message, ch Channel)
	Tx(m *Message, ch Channel)
	Close(ch Channel)
}

type TransformHandler interface {
	Rx(m *Message, ch Channel)
	Tx(m *Message, ch Channel)
}

type ReceiveHandler interface {
	ContentType() int32
	HandleReceive(m *Message, ch Channel)
}

type ErrorHandler interface {
	HandleError(err error, ch Channel)
}

type CloseHandler interface {
	HandleClose(ch Channel)
}
