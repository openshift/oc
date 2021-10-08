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
	"fmt"
	"github.com/michaelquigley/pfxlog"
	"github.com/openziti/foundation/identity/identity"
	"github.com/openziti/foundation/transport"
	"time"
)

type memoryListener struct {
	identity *identity.TokenId
	handlers []ConnectionHandler
	ctx      *MemoryContext
	created  chan Underlay
}

func NewMemoryListener(identity *identity.TokenId, ctx *MemoryContext) UnderlayListener {
	return &memoryListener{
		identity: identity,
		ctx:      ctx,
		created:  make(chan Underlay),
	}
}

func (listener *memoryListener) Listen(handlers ...ConnectionHandler) error {
	go listener.listen()
	return nil
}

func (listener *memoryListener) Close() error {
	close(listener.ctx.request)
	close(listener.ctx.response)
	return nil
}

func (listener *memoryListener) Create(_ time.Duration, _ transport.Configuration) (Underlay, error) {
	impl := <-listener.created
	if impl == nil {
		return nil, ListenerClosedError
	}
	return impl, nil
}

func (listener *memoryListener) listen() {
	log := pfxlog.ContextLogger(fmt.Sprintf("%p", listener.ctx))
	log.Info("started")
	defer log.Info("exited")

	for request := range listener.ctx.request {
		if request != nil {
			log.Infof("connecting dialer [%s] and listener [%s]", request.hello.IdToken, listener.identity.Token)

			if connectionId, err := globalRegistry.newConnectionId(); err == nil {
				listenerTx := make(chan *Message)
				dialerTx := make(chan *Message)

				dialerImpl := newMemoryImpl(dialerTx, listenerTx)
				dialerImpl.connectionId = connectionId
				listenerImpl := newMemoryImpl(listenerTx, dialerTx)
				listenerImpl.connectionId = connectionId
				listener.ctx.response <- dialerImpl
				listener.created <- listenerImpl

			} else {
				log.Errorf("unable to allocate connectionId (%s)", err)
			}
		} else {
			return
		}
	}
}
