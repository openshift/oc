package gettoken

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog/v2"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/cli/gettoken/credwriter"
	"github.com/openshift/oc/pkg/cli/gettoken/oidc"
	"github.com/openshift/oc/pkg/cli/gettoken/tokencache"
)

var (
	getTokenLong = templates.LongDesc(`
	Experimental: This command is under development and may change without notice.
	Built-in Credential Exec plugin of the oc.

	It supports Auth Code, Auth Code + PKCE in addition to refresh token.
	get-token caches the ID token and Refresh token after the auth code flow is
	successfully completed and once ID token expires, command tries to get the
	new token by using the refresh token flow. Although it is optional, command
	also supports getting client secret to behave as an confidential client.
`)
	getTokenExample = templates.Examples(`
	# Starts an auth code flow to the issuer URL with the client ID and the given extra scopes
	oc get-token --client-id=client-id --issuer-url=test.issuer.url --extra-scopes=email,profile

	# Starts an auth code flow to the issuer URL with a different callback address
	oc get-token --client-id=client-id --issuer-url=test.issuer.url --callback-address=127.0.0.1:8343
`)
)

const defaultCallbackAddress = "127.0.0.1:0"

type GetTokenOptions struct {
	IssuerURL       string
	ClientID        string
	ClientSecret    string
	ExtraScopes     []string
	CallbackAdress  string
	CACertFilename  string
	InsecureTLS     bool
	AutoOpenBrowser bool

	authenticator         oidc.Authenticator
	tokenCache            *tokencache.Repository
	credWriter            *credwriter.Writer
	tokenCacheDir         string
	authenticationTimeout time.Duration

	genericiooptions.IOStreams
}

func NewGetTokenOptions(streams genericiooptions.IOStreams) *GetTokenOptions {
	return &GetTokenOptions{
		IOStreams:             streams,
		CallbackAdress:        defaultCallbackAddress,
		authenticationTimeout: 5 * time.Minute,
	}
}

func NewCmdGetToken(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewGetTokenOptions(streams)

	cmd := &cobra.Command{
		Use:     "get-token --oidc-client-id=CLIENT_ID --oidc-issuer-url=ISSUER_URL",
		Short:   "Experimental: Get token from external OIDC issuer as credentials exec plugin",
		Long:    getTokenLong,
		Example: getTokenExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().StringVar(&o.IssuerURL, "issuer-url", o.IssuerURL, "Issuer URL of the external OIDC provider")
	cmd.Flags().StringVar(&o.ClientID, "client-id", o.ClientID, "Client ID of the user managed by the external OIDC provider")
	cmd.Flags().StringVar(&o.ClientSecret, "client-secret", o.ClientSecret, "Client Secret of the user managed by the external OIDC provider. Optional.")
	cmd.Flags().StringSliceVar(&o.ExtraScopes, "extra-scopes", o.ExtraScopes, "Extra scopes for the auth request to the external OIDC provider. Optional.")
	cmd.Flags().StringVar(&o.CallbackAdress, "callback-address", o.CallbackAdress, "Callback address where external OIDC issuer redirects to after flow is completed. Defaults to 127.0.0.1:0 to pick a random port.")
	cmd.Flags().BoolVar(&o.AutoOpenBrowser, "auto-open-browser", o.AutoOpenBrowser, "Specify browser is automatically opened or not.")

	return cmd
}

func (o *GetTokenOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	o.CACertFilename = kcmdutil.GetFlagString(cmd, "certificate-authority")
	o.InsecureTLS = kcmdutil.GetFlagBool(cmd, "insecure-skip-tls-verify")

	provider := &oidc.Provider{
		IssuerURL:    o.IssuerURL,
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
		ExtraScopes:  o.ExtraScopes,
		UsePKCE:      true,
	}

	authenticator, err := oidc.NewAuthenticator(context.Background(), provider, "", o.CACertFilename, o.InsecureTLS)
	if err != nil {
		return fmt.Errorf("oidc authenticator error: %w", err)
	}

	o.authenticator = authenticator
	o.tokenCache = &tokencache.Repository{}
	o.credWriter = credwriter.NewWriter(o.IOStreams)

	o.tokenCacheDir = filepath.Join(homedir.HomeDir(), ".kube", "cache", "oc")
	if kcd := os.Getenv("KUBECACHEDIR"); kcd != "" {
		o.tokenCacheDir = filepath.Join(kcd, "oc")
	}

	return nil
}

func (o *GetTokenOptions) Validate() error {
	if o.IssuerURL == "" {
		return fmt.Errorf("--issuer-url is required")
	}
	if o.ClientID == "" {
		return fmt.Errorf("--client-id is required")
	}

	return nil
}

// Run starts the authentication flow with a caching and refreshing capability.
// If refresh token is found, it tries to use it to get a valid id token from
// external OIDC issuer. If not, it forces user to log in.
func (o *GetTokenOptions) Run() error {
	tokenCacheKey := tokencache.Key{
		IssuerURL: o.IssuerURL,
		ClientID:  o.ClientID,
	}

	// Ignoring the error because if there is any error occurred
	// other than the missed cache, it will be captured while writing the token.
	tokenSet, err := o.tokenCache.FindByKey(o.tokenCacheDir, tokenCacheKey)
	if err != nil {
		// If we get the file not found error, this can be the first time
		// user authenticates and we can continue authentication.
		// If we get any other error, we should return this by short cutting.
		if !os.IsNotExist(err) {
			return err
		}
	}
	alreadyValid, idToken, refreshToken, expiry, err := o.getToken(context.Background(), tokenSet)
	if err != nil {
		return err
	}

	if !alreadyValid {
		err = o.tokenCache.Save(o.tokenCacheDir, tokenCacheKey, tokencache.Set{
			IDToken:      idToken,
			RefreshToken: refreshToken,
		})
		if err != nil {
			return fmt.Errorf("failed to write to token cache")
		}
	}

	if err := o.credWriter.Write(idToken, expiry); err != nil {
		return fmt.Errorf("failed to write the token to client-go: %w", err)
	}
	return nil
}

// getToken checks the id token in the passed cache object and it returns if it is not expired.
// If the cached token is expired, it checks first the refresh token's existence.
// If the refresh token is present, it tries to get the id token by using the refresh token
// in a token refresh flow. If none of the above steps succeeds, it triggers a new auth code
// token process.
func (o *GetTokenOptions) getToken(ctx context.Context, cache *tokencache.Set) (bool, string, string, time.Time, error) {
	if cache == nil {
		idToken, refreshToken, expiry, err := o.doAuthCode(ctx)
		return false, idToken, refreshToken, expiry, err
	}

	if cache.IDToken != "" {
		extra := make(map[string]interface{})
		extra["id_token"] = cache.IDToken
		t := &oauth2.Token{}
		t = t.WithExtra(extra)
		_, expiry, err := o.authenticator.VerifyToken(ctx, t, "")
		if err == nil {
			return true, cache.IDToken, cache.RefreshToken, expiry, nil
		}
		tokenExpiredError := &gooidc.TokenExpiredError{}
		if !errors.As(err, &tokenExpiredError) {
			return false, "", "", time.Time{}, err
		}
	}

	if cache.RefreshToken != "" {
		idToken, refreshToken, expiry, err := o.authenticator.Refresh(ctx, cache.RefreshToken)
		if err != nil {
			klog.V(2).Infof("refreshing token failed: %v, we'll attempt to do the auth code grant flow", err)
		} else {
			return false, idToken, refreshToken, expiry, nil
		}
	}

	idToken, refreshToken, expiry, err := o.doAuthCode(ctx)
	return false, idToken, refreshToken, expiry, err
}

// doAuthCode does the auth code flow with PKCE(if the issuer supports it).
func (o *GetTokenOptions) doAuthCode(ctx context.Context) (string, string, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, o.authenticationTimeout)
	defer cancel()
	readyChan := make(chan string, 1)
	var idToken, refreshToken string
	var expiry time.Time
	var eg errgroup.Group
	eg.Go(func() error {
		select {
		case url, ok := <-readyChan:
			if !ok {
				return nil
			}

			if !o.AutoOpenBrowser {
				// We are writing this to ErrOut instead of Out because Out is listened by client-go to get token.
				fmt.Fprintf(o.IOStreams.ErrOut, "Please visit the following URL in your browser: %s\n", url)
				return nil
			}

			err := browser.OpenURL(url)
			if err != nil {
				// We are writing this to ErrOut instead of Out because Out is listened by client-go to get token.
				fmt.Fprintf(o.IOStreams.ErrOut, "error: could not open the browser: %s\n\nlease visit the following URL in your browser manually: %s", err, url)
			}
			return nil
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for the local server: %w", ctx.Err())
		}
	})
	eg.Go(func() error {
		defer close(readyChan)
		var authErr error
		idToken, refreshToken, expiry, authErr = o.authenticator.GetTokenByAuthCode(ctx, o.CallbackAdress, readyChan)
		if authErr != nil {
			return fmt.Errorf("authorization code flow error: %w", authErr)
		}
		return nil
	})
	if err := eg.Wait(); err != nil {
		return "", "", time.Time{}, fmt.Errorf("authentication error: %w", err)
	}
	return idToken, refreshToken, expiry, nil
}
