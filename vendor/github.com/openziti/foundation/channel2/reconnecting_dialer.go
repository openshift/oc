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
	"errors"
	"fmt"
	"github.com/michaelquigley/pfxlog"
	"github.com/openziti/foundation/identity/identity"
	"github.com/openziti/foundation/transport"
	"sync"
	"time"
)

type reconnectingDialer struct {
	identity         *identity.TokenId
	endpoint         transport.Address
	headers          map[int32][]byte
	tcfg             transport.Configuration
	reconnectLock    sync.Mutex
	reconnectHandler func()
}

func NewReconnectingDialer(identity *identity.TokenId, endpoint transport.Address, headers map[int32][]byte) UnderlayFactory {
	return &reconnectingDialer{
		identity: identity,
		endpoint: endpoint,
		headers:  headers,
	}
}

func NewReconnectingDialerWithHandler(identity *identity.TokenId, endpoint transport.Address, headers map[int32][]byte, reconnectHandler func()) UnderlayFactory {
	return &reconnectingDialer{
		identity:         identity,
		endpoint:         endpoint,
		headers:          headers,
		reconnectHandler: reconnectHandler,
	}
}

func (dialer *reconnectingDialer) Create(timeout time.Duration, tcfg transport.Configuration) (Underlay, error) {
	log := pfxlog.ContextLogger(dialer.endpoint.String())
	log.Debug("started")
	defer log.Debug("exited")

	dialer.tcfg = tcfg

	version := uint32(2)

	peer, err := dialer.endpoint.Dial("reconnecting", dialer.identity, timeout, tcfg)
	if err != nil {
		return nil, err
	}

	impl := newReconnectingImpl(peer, dialer, timeout)
	impl.setProtocolVersion(version)

	if err := dialer.sendHello(impl); err != nil {
		_ = peer.Close()
		// If we bump channel2 protocol and need to handle multiple versions,
		// we'll need to reintroduce version handling code here
		// version, _ = getRetryVersion(err)
		return nil, err
	}

	return impl, nil
}

func (dialer *reconnectingDialer) Reconnect(impl *reconnectingImpl) error {
	log := pfxlog.ContextLogger(impl.Label() + " @" + dialer.endpoint.String())
	log.Debug("starting")
	defer log.Debug("exiting")

	dialer.reconnectLock.Lock()
	defer dialer.reconnectLock.Unlock()

	if err := impl.pingInstance(); err != nil {
		log.Errorf("unable to ping (%s)", err)
		for i := 0; true; i++ {
			peer, err := dialer.endpoint.Dial("reconnecting", dialer.identity, impl.timeout, dialer.tcfg)
			if err == nil {
				impl.peer = peer
				if err := dialer.sendHello(impl); err == nil {
					if dialer.reconnectHandler != nil {
						dialer.reconnectHandler()
					}
					return nil
				} else {
					if version, ok := getRetryVersion(err); ok {
						impl.setProtocolVersion(version)
					}
					log.Errorf("hello attempt [#%d] failed (%s)", i+1, err)
					time.Sleep(5 * time.Second)
				}

			} else {
				log.Errorf("reconnection attempt [#%d] failed (%s)", i+1, err)
				time.Sleep(5 * time.Second)
			}
		}
	}
	return nil
}

func (dialer *reconnectingDialer) sendHello(impl *reconnectingImpl) error {
	log := pfxlog.ContextLogger(impl.Label())
	defer log.Debug("exited")
	log.Debug("started")

	request := NewHello(dialer.identity.Token, dialer.headers)
	request.sequence = HelloSequence
	if impl.connectionId != "" {
		request.Headers[ConnectionIdHeader] = []byte(impl.connectionId)
		log.Debugf("adding connectionId header [%s]", impl.connectionId)
	}
	if err := impl.tx(request); err != nil {
		_ = impl.peer.Close()
		return err
	}

	response, err := impl.rx()
	if err != nil {
		return err
	}
	if !response.IsReplyingTo(request.sequence) || response.ContentType != ContentTypeResultType {
		return fmt.Errorf("channel synchronization error, expected %v, got %v", request.sequence, response.ReplyFor())
	}
	result := UnmarshalResult(response)
	if !result.Success {
		return errors.New(result.Message)
	}
	impl.connectionId = string(response.Headers[ConnectionIdHeader])

	return nil
}
