package login

import (
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/cobra"

	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kclientcmd "k8s.io/client-go/tools/clientcmd"
	kclientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
	"k8s.io/kubectl/pkg/util/term"

	"github.com/openshift/library-go/pkg/oauth/tokenrequest"

	"github.com/openshift/oc/pkg/helpers/flagtypes"
	"github.com/openshift/oc/pkg/helpers/oidc"
)

var (
	loginLong = templates.LongDesc(`
		Log in to your server and save login for subsequent use.

		First-time users of the client should run this command to connect to a server,
		establish an authenticated session, and save connection to the configuration file. The
		default configuration will be saved to your home directory under
		".kube/config".

		The information required to login -- like username and password, a session token, or
		the server details -- can be provided through flags. If not provided, the command will
		prompt for user input as needed. It is also possible to login through a web browser by
		providing the respective flag.
	`)

	loginExample = templates.Examples(`
		# Log in interactively
		oc login --username=myuser

		# Log in to the given server with the given certificate authority file
		oc login localhost:8443 --certificate-authority=/path/to/cert.crt

		# Log in to the given server with the given credentials (will not prompt interactively)
		oc login localhost:8443 --username=myuser --password=mypass

		# Log in to the given server through a browser
		oc login localhost:8443 --web --callback-port 8280

		# Log in to the given server uses an external OIDC issuer for authentication
		oc login localhost:8443 --external-auth-type=oidc --external-client-id=client-id --external-extra-args="--grant-type=authcode"

		# Log in to the given server uses Azure as an external issuer for authentication
		oc login localhost:8443 --external-auth-type=azure --external-client-id=client-id --external-extra-args="--tenant-id=user-tenant-id"
	`)
)

// NewCmdLogin implements the OpenShift cli login command
func NewCmdLogin(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewLoginOptions(streams)
	cmds := &cobra.Command{
		Use:     "login [URL]",
		Short:   "Log in to a server",
		Long:    loginLong,
		Example: loginExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate(cmd, kcmdutil.GetFlagString(cmd, "server"), args))

			if err := o.Run(); kapierrors.IsUnauthorized(err) {
				if err, isStatusErr := err.(*kapierrors.StatusError); isStatusErr {
					if err.Status().Message != tokenrequest.BasicAuthNoUsernameMessage {
						fmt.Fprintln(streams.Out, "Login failed (401 Unauthorized)")
						fmt.Fprintln(streams.Out, "Verify you have provided the correct credentials.")
					}
					if details := err.Status().Details; details != nil {
						for _, cause := range details.Causes {
							fmt.Fprintln(streams.Out, cause.Message)
						}
					}
				}

				os.Exit(1)

			} else {
				kcmdutil.CheckErr(err)
			}
		},
	}

	// Login is the only command that can negotiate a session token against the auth server using basic auth
	cmds.Flags().StringVarP(&o.Username, "username", "u", o.Username, "Username for server")
	cmds.Flags().StringVarP(&o.Password, "password", "p", o.Password, "Password for server")

	cmds.Flags().BoolVarP(&o.WebLogin, "web", "w", o.WebLogin, "Login with web browser. Starts a local HTTP callback server to perform the OAuth2 Authorization Code Grant flow. Use with caution on multi-user systems, as the server's port will be open to all users.")
	cmds.Flags().Int32VarP(&o.CallbackPort, "callback-port", "c", o.CallbackPort, "Port for the callback server when using --web. Defaults to a random open port")

	cmds.Flags().StringVar(&o.OIDCAuthType, "external-auth-type", o.OIDCAuthType, "Experimental: Specify the authentication type for external issuers to choose which plugin will be used for authentication. Valid values are oidc and azure.")
	cmds.Flags().StringArrayVar(&o.OIDCExtraArgs, "external-extra-args", o.OIDCExtraArgs, "Experimental: Set extra arguments or overwrite the default ones for external OIDC plugins.")
	cmds.Flags().StringVar(&o.OIDCClientID, "external-client-id", o.OIDCClientID, "Experimental: OIDC client id to be used for authentication. Required if external OIDC is used.")

	return cmds
}

func (o *LoginOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	kubeconfig, err := f.ToRawKubeConfigLoader().RawConfig()
	o.StartingKubeConfig = &kubeconfig
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// build a valid object to use if we failed on a non-existent file
		o.StartingKubeConfig = kclientcmdapi.NewConfig()
	}

	unparsedTimeout := kcmdutil.GetFlagString(cmd, "request-timeout")
	timeout, err := kclientcmd.ParseTimeout(unparsedTimeout)
	if err != nil {
		return err
	}
	o.RequestTimeout = timeout

	parsedDefaultClusterURL, err := url.Parse(defaultClusterURL)
	if err != nil {
		return err
	}
	addr := flagtypes.Addr{Value: parsedDefaultClusterURL.Host, DefaultScheme: parsedDefaultClusterURL.Scheme, AllowPrefix: true}.Default()

	if serverFlag := kcmdutil.GetFlagString(cmd, "server"); len(serverFlag) > 0 {
		if err := addr.Set(serverFlag); err != nil {
			return err
		}
		o.Server = addr.String()

	} else if len(args) == 1 {
		if err := addr.Set(args[0]); err != nil {
			return err
		}
		o.Server = addr.String()

	} else if len(o.Server) == 0 {
		if defaultContext, defaultContextExists := o.StartingKubeConfig.Contexts[o.StartingKubeConfig.CurrentContext]; defaultContextExists {
			if cluster, exists := o.StartingKubeConfig.Clusters[defaultContext.Cluster]; exists {
				o.Server = cluster.Server
			}
		}
	}

	o.CertFile = kcmdutil.GetFlagString(cmd, "client-certificate")
	o.KeyFile = kcmdutil.GetFlagString(cmd, "client-key")

	o.CAFile = kcmdutil.GetFlagString(cmd, "certificate-authority")
	o.InsecureTLS = kcmdutil.GetFlagBool(cmd, "insecure-skip-tls-verify")
	o.Token = kcmdutil.GetFlagString(cmd, "token")

	o.OIDCAuthType = kcmdutil.GetFlagString(cmd, "external-auth-type")
	o.OIDCClientID = kcmdutil.GetFlagString(cmd, "external-client-id")
	o.OIDCExtraArgs = kcmdutil.GetFlagStringArray(cmd, "external-extra-args")

	o.DefaultNamespace, _, _ = f.ToRawKubeConfigLoader().Namespace()

	o.PathOptions = kclientcmd.NewDefaultPathOptions()
	// we need to set explicit path if one was specified, since NewDefaultPathOptions doesn't do it for us
	o.PathOptions.LoadingRules.ExplicitPath = kcmdutil.GetFlagString(cmd, kclientcmd.RecommendedConfigPathFlag)

	return nil
}

func (o LoginOptions) Validate(cmd *cobra.Command, serverFlag string, args []string) error {
	if len(args) > 1 {
		return errors.New("Only the server URL may be specified as an argument")
	}

	if (len(serverFlag) > 0) && (len(args) == 1) {
		return errors.New("--server and passing the server URL as an argument are mutually exclusive")
	}

	if (len(o.Server) == 0) && !term.IsTerminal(o.In) {
		return errors.New("A server URL must be specified")
	}

	if len(o.Username) > 0 && len(o.Token) > 0 {
		return errors.New("--token and --username are mutually exclusive")
	}

	if o.StartingKubeConfig == nil {
		return errors.New("Must have a config file already created")
	}

	if o.WebLogin && (o.Username != "" || o.Password != "" || o.Token != "") {
		return errors.New("--web cannot be used along with --username, --password or --token")
	}

	if len(o.OIDCClientID) == 0 && (len(o.OIDCAuthType) > 0 || len(o.OIDCExtraArgs) > 0) {
		return errors.New("--external-auth-type, --external-extra-args can only be used for external issuers and --external-client-id is required")
	}

	if len(o.OIDCClientID) > 0 {
		if len(o.OIDCAuthType) == 0 {
			return errors.New("auth type should be set and accepted values are oidc and azure")
		}

		if o.OIDCAuthType == AzureAuthType {
			found, _ := oidc.ExternalOIDCExtraArgIsSet(o.OIDCExtraArgs, "--tenant-id")
			if !found {
				return errors.New("--tenant-id in --external-extra-args is required for Azure")
			}
		}

		if len(o.Username) > 0 {
			return errors.New("--oidc-client-id and --username are mutually exclusive")
		}

		if len(o.Token) > 0 {
			return errors.New("--oidc-client-id and --token are mutually exclusive")
		}
	}

	if o.CallbackPort != 0 && !o.WebLogin {
		return errors.New("--callback-port can only be specified along with --web")
	}

	return nil
}

// Run contains all the necessary functionality for the OpenShift cli login command
func (o LoginOptions) Run() error {
	if err := o.GatherInfo(); err != nil {
		return err
	}

	newFileCreated, err := o.SaveConfig()
	if err != nil {
		return err
	}

	if newFileCreated {
		fmt.Fprintf(o.Out, "Welcome! See 'oc help' to get started.\n")
	}
	return nil
}
