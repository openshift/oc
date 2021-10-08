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
	"time"
)

type classicDialer struct {
	identity *identity.TokenId
	endpoint transport.Address
	headers  map[int32][]byte
}

func NewClassicDialer(identity *identity.TokenId, endpoint transport.Address, headers map[int32][]byte) UnderlayFactory {
	return &classicDialer{
		identity: identity,
		endpoint: endpoint,
		headers:  headers,
	}
}

func (dialer *classicDialer) Create(timeout time.Duration, tcfg transport.Configuration) (Underlay, error) {
	log := pfxlog.ContextLogger(dialer.endpoint.String())
	log.Debug("started")
	defer log.Debug("exited")

	version := uint32(2)
	tryCount := 0

	for {
		peer, err := dialer.endpoint.Dial("classic", dialer.identity, timeout, tcfg)
		if err != nil {
			return nil, err
		}

		impl := newClassicImpl(peer, version)
		if err := dialer.sendHello(impl); err != nil {
			if tryCount > 0 {
				return nil, err
			} else {
				log.WithError(err).Warnf("error initiating channel with hello")
			}
			tryCount++
			version, _ = getRetryVersion(err)
			log.Warnf("Retrying dial with protocol version %v", version)
			continue
		}
		impl.id = dialer.identity
		return impl, nil
	}
}

func (dialer *classicDialer) sendHello(impl *classicImpl) error {
	log := pfxlog.ContextLogger(impl.Label())
	defer log.Debug("exited")
	log.Debug("started")

	request := NewHello(dialer.identity.Token, dialer.headers)
	request.sequence = HelloSequence
	if err := impl.Tx(request); err != nil {
		_ = impl.peer.Close()
		return err
	}

	response, err := impl.Rx()
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
	impl.headers = response.Headers

	return nil
}
