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
	"github.com/michaelquigley/pfxlog"
	"github.com/netfoundry/secretstream/kx"
	"github.com/openziti/foundation/channel2"
	"github.com/openziti/foundation/util/sequencer"
	"github.com/openziti/sdk-golang/ziti/edge"
)

type RouterConnOwner interface {
	OnClose(factory edge.RouterConn)
}

type routerConn struct {
	routerName string
	key        string
	ch         channel2.Channel
	msgMux     edge.MsgMux
	owner      RouterConnOwner
}

func (conn *routerConn) Key() string {
	return conn.key
}

func (conn *routerConn) GetRouterName() string {
	return conn.routerName
}

func (conn *routerConn) HandleClose(channel2.Channel) {
	if conn.owner != nil {
		conn.owner.OnClose(conn)
	}
}

func NewEdgeConnFactory(routerName, key string, ch channel2.Channel, owner RouterConnOwner) edge.RouterConn {
	connFactory := &routerConn{
		key:        key,
		routerName: routerName,
		ch:         ch,
		msgMux:     edge.NewCowMapMsgMux(),
		owner:      owner,
	}

	ch.AddReceiveHandler(&edge.FunctionReceiveAdapter{
		Type:    edge.ContentTypeDial,
		Handler: connFactory.msgMux.HandleReceive,
	})

	ch.AddReceiveHandler(&edge.FunctionReceiveAdapter{
		Type:    edge.ContentTypeStateClosed,
		Handler: connFactory.msgMux.HandleReceive,
	})

	// Since data is the common message type, it gets to be dispatched directly
	ch.AddReceiveHandler(connFactory.msgMux)
	ch.AddCloseHandler(connFactory.msgMux)
	ch.AddCloseHandler(connFactory)

	return connFactory
}

func (conn *routerConn) NewConn(service *edge.Service, connType ConnType) *edgeConn {
	id := conn.msgMux.GetNextId()

	edgeCh := &edgeConn{
		MsgChannel: *edge.NewEdgeMsgChannel(conn.ch, id),
		readQ:      sequencer.NewNoopSequencer(4),
		msgMux:     conn.msgMux,
		serviceId:  service.Name,
		connType:   connType,
	}

	var err error
	if service.Encryption {
		if edgeCh.keyPair, err = kx.NewKeyPair(); err == nil {
			edgeCh.crypto = true
		} else {
			pfxlog.Logger().Errorf("unable to setup encryption for edgeConn[%s] %v", service.Name, err)
		}
	}

	err = conn.msgMux.AddMsgSink(edgeCh) // duplicate errors only happen on the server side, since client controls ids
	if err != nil {
		pfxlog.Logger().Warnf("error adding message sink %s[%d]: %v", service.Name, id, err)
	}
	return edgeCh
}

func (conn *routerConn) Connect(service *edge.Service, session *edge.Session, options *edge.DialOptions) (edge.Conn, error) {
	ec := conn.NewConn(service, ConnTypeDial)
	dialConn, err := ec.Connect(session, options)
	if err != nil {
		if err2 := ec.Close(); err2 != nil {
			pfxlog.Logger().Errorf("failed to cleanup connection for service '%v' (%v)", service.Name, err2)
		}
	}
	return dialConn, err
}

func (conn *routerConn) Listen(service *edge.Service, session *edge.Session, options *edge.ListenOptions) (edge.Listener, error) {
	ec := conn.NewConn(service, ConnTypeBind)
	listener, err := ec.Listen(session, service, options)
	if err != nil {
		if err2 := ec.Close(); err2 != nil {
			pfxlog.Logger().Errorf("failed to cleanup listenet for service '%v' (%v)", service.Name, err2)
		}
	}
	return listener, err
}

func (conn *routerConn) Close() error {
	return conn.ch.Close()
}

func (conn *routerConn) IsClosed() bool {
	return conn.ch.IsClosed()
}
