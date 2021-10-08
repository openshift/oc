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

package certtools

import (
	"github.com/openziti/foundation/identity/engines/pkcs11"
	"crypto"
	"fmt"
	"net/url"
)

type Engine interface {
	Id() string
	LoadKey(key *url.URL) (crypto.PrivateKey, error)
}

var engines = map[string]Engine{}

func init() {
	engines[pkcs11.EngineId] = pkcs11.Engine
}

func ListEngines() []string {
	loadEngines()

	res := make([]string, 0, len(engines))
	for k := range engines {
		res = append(res, k)
	}
	return res
}

func LoadEngineKey(engine string, addr *url.URL) (crypto.PrivateKey, error) {
	loadEngines()

	if eng, ok := engines[engine]; ok {
		return eng.LoadKey(addr)
	} else {
		return nil, fmt.Errorf("engine '%s' is not supported", engine)
	}
}
