package api

import (
	"github.com/openziti/foundation/identity/identity"
	"github.com/openziti/sdk-golang/ziti/config"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"net/url"
	"os"
	"sync"
)

func NewLazyClient(config *config.Config, initCallback func(Client) error) Client {
	return &lazyClient{
		config:       config,
		initComplete: initCallback,
	}
}

type lazyClient struct {
	RestClient
	config       *config.Config
	initDone     sync.Once
	initComplete func(Client) error
	id           identity.Identity
}

func (client *lazyClient) GetIdentity() identity.Identity {
	return client.id
}

func (client *lazyClient) ensureConfigPresent() error {
	if client.config != nil {
		return nil
	}

	const configEnvVarName = "ZITI_SDK_CONFIG"
	// If configEnvVarName is set, try to use it.
	// The calling application may override this by calling NewContextWithConfig
	confFile := os.Getenv(configEnvVarName)

	if confFile == "" {
		return errors.Errorf("unable to configure ziti as config environment variable %v not populated", configEnvVarName)
	}

	logrus.Infof("loading Ziti configuration from %s", confFile)
	cfg, err := config.NewFromFile(confFile)
	if err != nil {
		return errors.Errorf("error loading config file specified by ${%s}: %v", configEnvVarName, err)
	}
	client.config = cfg
	return nil
}

func (client *lazyClient) Initialize() error {
	var err error
	client.initDone.Do(func() {
		err = client.load()
	})
	return err
}

func (client *lazyClient) load() error {
	err := client.ensureConfigPresent()
	if err != nil {
		return err
	}

	zitiUrl, _ := url.Parse(client.config.ZtAPI)

	client.id, err = identity.LoadIdentity(client.config.ID)
	if err != nil {
		return err
	}
	client.RestClient, err = NewClient(zitiUrl, client.id.ClientTLSConfig(), client.config.ConfigTypes)
	if err != nil {
		return err
	}
	return client.initComplete(client)
}
