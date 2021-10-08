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
	"github.com/openziti/foundation/identity/identity"
	"fmt"
	"github.com/michaelquigley/pfxlog"
	"github.com/openziti/foundation/transport"
	"time"
)

type memoryDialer struct {
	identity *identity.TokenId
	headers  map[int32][]byte
	ctx      *MemoryContext
}

func NewMemoryDialer(identity *identity.TokenId, headers map[int32][]byte, ctx *MemoryContext) UnderlayFactory {
	return &memoryDialer{
		identity: identity,
		headers:  headers,
		ctx:      ctx,
	}
}

func (dialer *memoryDialer) Create(_ time.Duration, _ transport.Configuration) (Underlay, error) {
	log := pfxlog.ContextLogger(fmt.Sprintf("%p", dialer.ctx))
	log.Info("started")
	defer log.Info("exited")

	dialer.ctx.request <- &memoryRequest{
		dialer,
		&Hello{
			IdToken: dialer.identity.Token,
			Headers: dialer.headers,
		},
	}
	return <-dialer.ctx.response, nil
}
