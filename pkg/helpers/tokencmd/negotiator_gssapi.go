//go:build gssapi
// +build gssapi

package tokencmd

import (
	"fmt"
	"os"

	krb5client "github.com/jcmturner/gokrb5/v8/client"
	krb5conf "github.com/jcmturner/gokrb5/v8/config"
	krb5kt "github.com/jcmturner/gokrb5/v8/keytab"
	krb5spnego "github.com/jcmturner/gokrb5/v8/spnego"
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
	g.complete = false

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

	if challengeToken != nil {
		var aprep krb5spnego.KRB5Token
		if err := aprep.Unmarshal(challengeToken); err != nil {
			return nil, err
		}

		if aprep.IsKRBError() {
			return nil, fmt.Errorf("received Kerberos error")
		}

		if !aprep.IsAPRep() {
			return nil, fmt.Errorf("didn't receive an AP_REP")
		}

		return nil, nil
	}

	servicePrincipleName, err := getServiceName('/', requestURL)
	if err != nil {
		return nil, err
	}

	tkt, key, err := g.client.GetServiceTicket(servicePrincipleName)
	if err != nil {
		g.client = nil
		return nil, err
	}

	negTokenInit, err := krb5spnego.NewNegTokenInitKRB5(g.client, tkt, key)
	if err != nil {
		g.client = nil
		return nil, err
	}

	// Marshal init negotiation token.
	initTokenBytes, err := negTokenInit.Marshal()
	if err != nil {
		g.client = nil
		return nil, err
	}

	g.complete = true
	return initTokenBytes, nil
}

func (g *gssapiNegotiator) IsComplete() bool {
	return g.complete
}

func (g *gssapiNegotiator) Release() error {
	return nil
}
