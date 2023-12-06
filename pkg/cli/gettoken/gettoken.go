package gettoken

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/int128/kubelogin/pkg/infrastructure/logger"
	"github.com/int128/kubelogin/pkg/jwt"
	"github.com/int128/kubelogin/pkg/oidc"
	"github.com/int128/kubelogin/pkg/oidc/client"
	"github.com/int128/kubelogin/pkg/tlsclientconfig"
	"github.com/int128/kubelogin/pkg/tlsclientconfig/loader"
	"github.com/int128/kubelogin/pkg/usecases/authentication/authcode"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/util/homedir"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	getTokenLong    = templates.LongDesc(``)
	getTokenExample = templates.Examples(``)

	defaultCallbackAddress = "localhost:8000"
)

type GetTokenOptions struct {
	IssuerURL      string
	ClientID       string
	ExtraScopes    []string
	BindAdress     string
	CACertFilename string
	InsecureTLS    bool

	tokenCacheRepo *Repository
	credWriter     *Writer
	credLogger     logger.Interface
	realClock      *Real

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
		Use:     "get-token",
		Short:   "get-token",
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
	cmd.Flags().StringVar(&o.BindAdress, "oidc-bind-address", o.BindAdress, "Bind address for callback. Defaults to localhost:8000")

	return cmd
}

func (o *GetTokenOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	o.CACertFilename = kcmdutil.GetFlagString(cmd, "certificate-authority")
	o.InsecureTLS = kcmdutil.GetFlagBool(cmd, "insecure-skip-tls-verify")

	o.tokenCacheRepo = &Repository{}

	o.credWriter = &Writer{
		out: o.Out,
	}

	o.credLogger = NewLogger(o.IOStreams)

	o.realClock = &Real{}

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
	provider := oidc.Provider{
		IssuerURL:   o.IssuerURL,
		ClientID:    o.ClientID,
		ExtraScopes: o.ExtraScopes,
		UsePKCE:     true,
	}

	tokenCacheKey := TokenKey{
		IssuerURL:      o.IssuerURL,
		ClientID:       o.ClientID,
		ExtraScopes:    o.ExtraScopes,
		CACertFilename: o.CACertFilename,
		SkipTLSVerify:  o.InsecureTLS,
	}

	cachedTokenSet, _ := o.tokenCacheRepo.FindByKey(filepath.Join(homedir.HomeDir(), ".kube"), tokenCacheKey)
	alreadyValid, idToken, refreshToken, err := o.getToken(context.TODO(), cachedTokenSet, provider)
	if err != nil {
		return err
	}

	idTokenClaims, err := jwt.DecodeWithoutVerify(idToken)
	if err != nil {
		return fmt.Errorf("you got an invalid token: %w", err)
	}

	if !alreadyValid {
		if err := o.tokenCacheRepo.Save(filepath.Join(homedir.HomeDir(), ".kube"), tokenCacheKey, TokenSet{
			IDToken:      idToken,
			RefreshToken: refreshToken,
		}); err != nil {
			return fmt.Errorf("could not write the token cache: %w", err)
		}
	}
	if err := o.credWriter.Write(idToken, idTokenClaims.Expiry); err != nil {
		return fmt.Errorf("could not write the token to client-go: %w", err)
	}
	return nil
}

func (o *GetTokenOptions) getToken(ctx context.Context, cache *TokenSet, provider oidc.Provider) (bool, string, string, error) {
	if cache != nil {
		claims, err := jwt.DecodeWithoutVerify(cache.IDToken)
		if err != nil {
			return false, "", "", fmt.Errorf("invalid token cache (you may need to remove): %w", err)
		}
		if !claims.IsExpired(o.realClock) {
			return true, cache.IDToken, cache.RefreshToken, nil
		}
	}

	oidcClientFactory := &client.Factory{
		Loader: loader.Loader{},
		Clock:  o.realClock,
		Logger: o.credLogger,
	}

	oidcClient, err := oidcClientFactory.New(ctx, provider, tlsclientconfig.Config{
		SkipTLSVerify:  o.InsecureTLS,
		CACertFilename: []string{o.CACertFilename},
	})
	if err != nil {
		return false, "", "", fmt.Errorf("oidc error: %w", err)
	}

	if cache != nil && cache.RefreshToken != "" {
		tokenSet, err := oidcClient.Refresh(ctx, cache.RefreshToken)
		if err == nil {
			return false, tokenSet.IDToken, tokenSet.RefreshToken, nil
		}
	}

	authCode := &authcode.Browser{
		Browser: NewBrowser(o.IOStreams),
		Logger:  o.credLogger,
	}

	tokenSet, err := authCode.Do(ctx, &authcode.BrowserOption{
		SkipOpenBrowser:       true,
		BindAddress:           []string{o.BindAdress},
		AuthenticationTimeout: 30 * time.Second,
	}, oidcClient)
	if err != nil {
		return false, "", "", err
	}
	return false, tokenSet.IDToken, tokenSet.RefreshToken, nil
}
