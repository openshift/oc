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

package transport

import (
	"errors"
	"fmt"
	"github.com/openziti/foundation/identity/identity"
	"io"
	"time"
)

type Configuration map[interface{}]interface{}

// Address implements the functionality provided by a generic "address".
//
type Address interface {
	Dial(name string, i *identity.TokenId, timeout time.Duration, tcfg Configuration) (Connection, error)
	Listen(name string, i *identity.TokenId, incoming chan Connection, tcfg Configuration) (io.Closer, error)
	MustListen(name string, i *identity.TokenId, incoming chan Connection, tcfg Configuration) io.Closer
	String() string
}

// AddressParser implements the functionality provided by an "address parser".
//
type AddressParser interface {
	Parse(addressString string) (Address, error)
}

// AddAddressParser adds an AddressParser to the globally-configured address parsers.
//
func AddAddressParser(addressParser AddressParser) {
	for _, e := range addressParsers {
		if addressParser == e {
			return
		}
	}
	addressParsers = append(addressParsers, addressParser)
}

// ParseAddress uses the globally-configured AddressParser instances to parse an address.
//
func ParseAddress(addressString string) (Address, error) {
	if addressParsers == nil || len(addressParsers) < 1 {
		return nil, errors.New("no configured address parsers")
	}
	for _, addressParser := range addressParsers {
		address, err := addressParser.Parse(addressString)
		if err == nil {
			return address, nil
		}
	}
	return nil, fmt.Errorf("address (%v) not parsed", addressString)
}

// The globally-configured address parsers.
//
var addressParsers = make([]AddressParser, 0)
