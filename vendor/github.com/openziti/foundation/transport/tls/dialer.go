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

package tls

import (
	"crypto/tls"
	"github.com/michaelquigley/pfxlog"
	"github.com/openziti/foundation/identity/identity"
	"github.com/openziti/foundation/transport"
	"net"
	"time"
)

func Dial(destination, name string, i *identity.TokenId, timeout time.Duration) (transport.Connection, error) {
	log := pfxlog.Logger()

	socket, err := tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", destination, i.ClientTLSConfig())
	if err != nil {
		return nil, err
	}

	log.Debugf("server provided [%d] certificates", len(socket.ConnectionState().PeerCertificates))

	return &Connection{
		detail: &transport.ConnectionDetail{
			Address: "tls:" + destination,
			InBound: false,
			Name:    name,
		},
		socket: socket,
	}, nil
}
