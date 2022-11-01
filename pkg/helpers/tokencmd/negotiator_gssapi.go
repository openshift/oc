//go:build gssapi
// +build gssapi

package tokencmd

import (
	"os"

	krb5client "github.com/jcmturner/gokrb5/v8/client"
	krb5conf "github.com/jcmturner/gokrb5/v8/config"
	krb5kt "github.com/jcmturner/gokrb5/v8/keytab"
)

const (
	krb5Config       = "/etc/krb5.conf"
	krb5ConfigEnvKey = "KRB5_CONFIG"
	krb5Keytab       = "/etc/krb5.keytab"
	krb5KeytabEnvKey = "KRB5_CLIENT_KTNAME"
)

func GSSAPIEnabled() bool {
	return true
}

type gssapiNegotiator struct {
	// principalName contains the name of the principal desired by the user, if specified.
	principalName string

	// track whether the last response from InitSecContext was GSS_S_COMPLETE
	complete bool

	client *krb5client.Client
}

func NewGSSAPINegotiator(principalName string) Negotiator {
	return &gssapiNegotiator{principalName: principalName}
}

func (g *gssapiNegotiator) Load() error {
	return nil
}

func (g *gssapiNegotiator) InitSecContext(requestURL string, challengeToken []byte) (tokenToSend []byte, err error) {
	if g.client == nil {
		krb5ConfLocation := krb5Config
		if os.Getenv(krb5ConfigEnvKey) != "" {
			krb5ConfLocation = os.Getenv(krb5ConfigEnvKey)
		}

		config, err := krb5conf.Load(krb5ConfLocation)
		if err != nil {
			return nil, err
		}

		krb5KeytabLocation := krb5Keytab
		if os.Getenv(krb5KeytabEnvKey) != "" {
			krb5KeytabLocation = os.Getenv(krb5KeytabEnvKey)
		}
		keytab, err := krb5kt.Load(krb5KeytabLocation)
		if err != nil {
			return nil, err
		}

		g.client = krb5client.NewWithKeytab(g.principalName, config.LibDefaults.DefaultRealm, keytab, config)

		err = g.client.Login()
		if err != nil {
			return nil, err
		}
	}

	servicePrincipleName, err := getServiceName('/', requestURL)
	if err != nil {
		return nil, err
	}

	_, key, err := g.client.GetServiceTicket(servicePrincipleName)
	if err != nil {
		g.client = nil
		g.complete = false
		return nil, err
	}

	g.complete = true
	return key.KeyValue, nil
}

func (g *gssapiNegotiator) IsComplete() bool {
	return g.complete
}

func (g *gssapiNegotiator) Release() error {
	return nil
}
