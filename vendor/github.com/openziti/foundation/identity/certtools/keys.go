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
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net/url"
	"strconv"
	"strings"
)

var CURVES = map[string]elliptic.Curve{}

var curves = []elliptic.Curve{
	elliptic.P224(),
	elliptic.P256(),
	elliptic.P384(),
	elliptic.P521(),
}

func init() {
	for _, c := range curves {
		CURVES[c.Params().Name] = c
	}
}

func GetKey(eng *url.URL, file, newkey string) (crypto.PrivateKey, error) {
	if eng != nil {
		var engine = eng.Scheme
		return LoadEngineKey(engine, eng)
	}

	if newkey != "" {
		key, err := generateKey(newkey)
		if err != nil {
			return nil, err
		}

		if err := SavePrivateKey(key, file); err != nil {
			return nil, err
		}

		return key, nil
	}

	if file != "" {
		if pemBytes, err := ioutil.ReadFile(file); err != nil {
			return nil, err
		} else {
			return LoadPrivateKey(pemBytes)
		}
	}

	return nil, fmt.Errorf("no key mechanism specified")
}

func SavePrivateKey(key crypto.PrivateKey, file string) error {
	var der []byte
	var t string
	if rsaK, ok := key.(*rsa.PrivateKey); ok {
		t = "RSA PRIVATE KEY"
		der = x509.MarshalPKCS1PrivateKey(rsaK)
	} else if ecK, ok := key.(*ecdsa.PrivateKey); ok {
		t = "EC PRIVATE KEY"
		der, _ = x509.MarshalECPrivateKey(ecK)
	} else {
		return fmt.Errorf("Unsupported key type")
	}

	keyPem := &pem.Block{Type: t, Bytes: der}

	return ioutil.WriteFile(file, pem.EncodeToMemory(keyPem), 0600)
}

func LoadPrivateKey(pemBytes []byte) (crypto.PrivateKey, error) {

	var keyBlock *pem.Block
	for len(pemBytes) > 0 {
		keyBlock, pemBytes = pem.Decode(pemBytes)
		switch keyBlock.Type {
		case "EC PRIVATE KEY":
			return x509.ParseECPrivateKey(keyBlock.Bytes)
		case "RSA PRIVATE KEY":
			return x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
		case "PRIVATE KEY":
			return x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
		}
	}

	return nil, fmt.Errorf("no key found")
}

func SupportedCurves() []string {
	names := make([]string, 0, len(curves))
	for _, c := range curves {
		names = append(names, c.Params().Name)
	}
	return names
}

func generateKey(spec string) (crypto.PrivateKey, error) {
	specs := strings.Split(spec, ":")

	switch specs[0] {
	case "rsa":
		if bits, err := strconv.Atoi(specs[1]); err != nil {
			return nil, err
		} else {
			return rsa.GenerateKey(rand.Reader, bits)
		}
	case "ec":
		if c, ok := CURVES[specs[1]]; !ok {
			return nil, fmt.Errorf("ECurve '%s' not found", specs[1])
		} else {
			return ecdsa.GenerateKey(c, rand.Reader)
		}
	default:
		return nil, fmt.Errorf("unsupported key spec '%s'", specs[0])
	}
}
