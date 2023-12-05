package gettoken

import (
	"context"
	"fmt"
	"path/filepath"

	"k8s.io/client-go/util/homedir"

	"github.com/spf13/cobra"

	"github.com/int128/kubelogin/pkg/credentialplugin/writer"
	"github.com/int128/kubelogin/pkg/infrastructure/logger"
	"github.com/int128/kubelogin/pkg/infrastructure/mutex"
	"github.com/int128/kubelogin/pkg/oidc"
	"github.com/int128/kubelogin/pkg/tlsclientconfig"
	"github.com/int128/kubelogin/pkg/tokencache/repository"
	"github.com/int128/kubelogin/pkg/usecases/authentication"
	"github.com/int128/kubelogin/pkg/usecases/authentication/authcode"
	"github.com/int128/kubelogin/pkg/usecases/credentialplugin"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	getTokenLong    = templates.LongDesc(``)
	getTokenExample = templates.Examples(``)

	defaultCallbackAddress = "127.0.0.1:8000"
)

type GetTokenOptions struct {
	IssuerURL      string
	ClientID       string
	ExtraScopes    []string
	BindAdress     string
	CACertFilename string
	InsecureTLS    bool

	tokenCacheRepo     repository.Interface
	credWriter         writer.Interface
	credLogger         logger.Interface
	credAuthentication authentication.Interface

	genericiooptions.IOStreams
}

func NewGetTokenOptions(streams genericiooptions.IOStreams) *GetTokenOptions {
	return &GetTokenOptions{
		IOStreams:  streams,
		BindAdress: defaultCallbackAddress,
	}
}

func NewCmdGetToken(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewGetTokenOptions(streams)

	cmd := &cobra.Command{
		Use:     "",
		Short:   "",
		Long:    getTokenLong,
		Example: getTokenExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().StringVar(&o.IssuerURL, "oidc-issuer-url", o.IssuerURL, "Issuer URL of the external OIDC provider")
	cmd.Flags().StringVar(&o.ClientID, "oidc-client-id", o.ClientID, "Client ID of the provider")
	cmd.Flags().StringArrayVar(&o.ExtraScopes, "oidc-extra-scope", o.ExtraScopes, "Scopes to request to the OIDC provider")
	cmd.Flags().StringVar(&o.BindAdress, "oidc-bind-address", o.BindAdress, "Bind address for callback. Defaults to 127.0.0.1:8000")

	return cmd
}

func (o *GetTokenOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	o.CACertFilename = kcmdutil.GetFlagString(cmd, "certificate-authority")
	o.InsecureTLS = kcmdutil.GetFlagBool(cmd, "insecure-skip-tls-verify")

	o.tokenCacheRepo = &repository.Repository{}

	o.credWriter = &writer.Writer{
		Stdout: o.Out,
	}

	o.credLogger = logger.New()

	o.credAuthentication = &authentication.Authentication{
		ClientFactory:   nil,
		Logger:          o.credLogger,
		Clock:           nil,
		AuthCodeBrowser: nil,
	}

	return nil
}

func (o *GetTokenOptions) Validate() error {
	if o.IssuerURL == "" {
		return fmt.Errorf("--oidc-issuer-url is required")
	}
	if o.ClientID == "" {
		return fmt.Errorf("--oidc-client-id is required")
	}

	return nil
}

func (o *GetTokenOptions) Run() error {
	credInput := credentialplugin.Input{
		Provider: oidc.Provider{
			IssuerURL:   o.IssuerURL,
			ClientID:    o.ClientID,
			ExtraScopes: o.ExtraScopes,
			UsePKCE:     true,
		},
		TokenCacheDir: filepath.Join(homedir.HomeDir(), ".kube"),
		GrantOptionSet: authentication.GrantOptionSet{
			AuthCodeBrowserOption: &authcode.BrowserOption{
				BindAddress:     []string{o.BindAdress},
				SkipOpenBrowser: true,
			},
		},
		TLSClientConfig: tlsclientconfig.Config{
			CACertFilename: []string{o.CACertFilename},
			SkipTLSVerify:  o.InsecureTLS,
		},
	}

	credExec := &credentialplugin.GetToken{
		Authentication:       o.credAuthentication,
		TokenCacheRepository: o.tokenCacheRepo,
		Writer:               o.credWriter,
		Mutex: &mutex.Mutex{
			Logger: o.credLogger,
		},
		Logger: o.credLogger,
	}
	if err := credExec.Do(context.TODO(), credInput); err != nil {
		return fmt.Errorf("get-token credential exec plugin error %v", err)
	}

	return nil
}
