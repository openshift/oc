//go:build gssapi
// +build gssapi

package tokencmd

import (
	"fmt"
	"os"
	"os/user"
	"strings"

	krb5client "github.com/jcmturner/gokrb5/v8/client"
	krb5conf "github.com/jcmturner/gokrb5/v8/config"
	krb5credentials "github.com/jcmturner/gokrb5/v8/credentials"
	krb5crypto "github.com/jcmturner/gokrb5/v8/crypto"
	krb5flags "github.com/jcmturner/gokrb5/v8/iana/flags"
	krb5kt "github.com/jcmturner/gokrb5/v8/keytab"
	krb5messages "github.com/jcmturner/gokrb5/v8/messages"
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

		useKeytab := true
		krb5KeytabLocation := krb5Keytab
		if os.Getenv(krb5KeytabEnvKey) != "" {
			krb5KeytabLocation = os.Getenv(krb5KeytabEnvKey)
		}

		if g.principalName == "" {
			if _, err := os.Stat(krb5KeytabLocation); err != nil {
				useKeytab = false
			}
		}

		if useKeytab {
			keytab, err := krb5kt.Load(krb5KeytabLocation)
			if err != nil {
				return nil, err
			}

			g.client = krb5client.NewWithKeytab(g.principalName, config.LibDefaults.DefaultRealm, keytab, config)
		} else {
			u, err := user.Current()
			if err != nil {
				return nil, err
			}

			path := "/tmp/krb5cc_" + u.Uid

			if env, ok := os.LookupEnv("KRB5CCNAME"); ok && strings.HasPrefix(env, "FILE:") {
				path = strings.SplitN(env, ":", 2)[1]
			}

			cache, err := krb5credentials.LoadCCache(path)
			if err != nil {
				return nil, err
			}

			g.client, err = krb5client.NewFromCCache(cache, config)
			if err != nil {
				return nil, err
			}
		}

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

		g.complete = true
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

	apreq, err := krb5spnego.NewKRB5TokenAPREQ(g.client, tkt, key, nil, []int{krb5flags.APOptionMutualRequired})
	if err != nil {
		return nil, err
	}

	if err = apreq.APReq.DecryptAuthenticator(key); err != nil {
		return nil, err
	}

	etype, err := krb5crypto.GetEtype(key.KeyType)
	if err != nil {
		return nil, err
	}

	if err = apreq.APReq.Authenticator.GenerateSeqNumberAndSubKey(key.KeyType, etype.GetKeyByteSize()); err != nil {
		return nil, err
	}

	if apreq.APReq, err = krb5messages.NewAPReq(tkt, key, apreq.APReq.Authenticator); err != nil {
		return nil, err
	}

	b, err := apreq.Marshal()
	if err != nil {
		return nil, err
	}

	g.complete = true
	return b, nil
}

func (g *gssapiNegotiator) IsComplete() bool {
	return g.complete
}

func (g *gssapiNegotiator) Release() error {
	return nil
}
