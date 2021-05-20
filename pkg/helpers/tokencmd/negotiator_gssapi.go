package tokencmd

import (
	krb5client "github.com/jcmturner/gokrb5/v8/client"
	krb5conf "github.com/jcmturner/gokrb5/v8/config"
	krb5kt "github.com/jcmturner/gokrb5/v8/keytab"
)

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
	// do nothing
	return nil
}

// TODO(lorbus) Make this function more resilient
func (g *gssapiNegotiator) InitSecContext(requestURL string, challengeToken []byte) (tokenToSend []byte, err error) {

	if g.client == nil {
		// TODO confirm support for recursive `include` and `includedir` in conf file
		// TODO Don't hardcode filepath, use env var instead
		config, err := krb5conf.Load("/etc/krb5.conf")
		if err != nil {
			return nil, err
		}

		// TODO add support for using pw instead of kt
		keytab, err := krb5kt.Load("/etc/krb5.keytab")
		if err != nil {
			return nil, err
		}

		// TODO check whether REALM can be safely omitted (2nd param)
		g.client = krb5client.NewWithKeytab(g.principalName, "", keytab, config)

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
		return nil, err
	}

	g.complete = true

	return key.KeyValue, nil
}

func (g *gssapiNegotiator) IsComplete() bool {
	return g.complete
}

func (g *gssapiNegotiator) Release() error {
	// do nothing
	return nil
}
