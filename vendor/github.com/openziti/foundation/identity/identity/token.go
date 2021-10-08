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

package identity

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"sync"
)

type TokenId struct {
	Id    Identity
	Token string
	Data  map[uint32][]byte
}

func (i *TokenId) ClientTLSConfig() *tls.Config {
	if i.Id != nil {
		return i.Id.ClientTLSConfig()
	}
	return nil
}

func (i *TokenId) ServerTLSConfig() *tls.Config {
	if i.Id != nil {
		return i.Id.ServerTLSConfig()
	}
	return nil
}

func NewIdentity(id Identity) *TokenId {
	token := id.Cert().Leaf.Subject.CommonName
	return &TokenId{Id: id, Token: token}
}

func (i *TokenId) ShallowCloneWithNewToken(token string) *TokenId {
	return &TokenId{
		Id:    i.Id,
		Token: token,
	}
}

func LoadServerIdentity(clientCertPath, serverCertPath, keyPath, caCertPath string) (*TokenId, error) {
	idCfg := IdentityConfig{
		Key:        keyPath,
		Cert:       clientCertPath,
		ServerCert: serverCertPath,
		CA:         caCertPath,
	}

	if id, err := LoadIdentity(idCfg); err != nil {
		return nil, err
	} else {
		return NewIdentity(id), nil
	}
}

func LoadClientIdentity(certPath, keyPath, caCertPath string) (*TokenId, error) {
	idCfg := IdentityConfig{
		Key:  keyPath,
		Cert: certPath,
		CA:   caCertPath,
	}

	if id, err := LoadIdentity(idCfg); err != nil {
		return nil, err
	} else {
		return NewIdentity(id), nil
	}
}

func NewClientTokenIdentity(clientCert *x509.Certificate, privateKey crypto.PrivateKey, caCerts []*x509.Certificate) *TokenId {
	pool := x509.NewCertPool()

	for _, ca := range caCerts {
		pool.AddCert(ca)
	}

	id := &ID{
		IdentityConfig: IdentityConfig{},
		certLock:       sync.RWMutex{},
		cert: &tls.Certificate{
			Certificate: [][]byte{
				clientCert.Raw,
			},
			Leaf:       clientCert,
			PrivateKey: privateKey,
		},
		ca: pool,
	}

	return NewIdentity(id)
}
