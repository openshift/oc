package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/templates"

	oauthv1 "github.com/openshift/api/oauth/v1"
	oauthclient "github.com/openshift/client-go/oauth/clientset/versioned"
	oauthv1client "github.com/openshift/client-go/oauth/clientset/versioned/typed/oauth/v1"
	userclient "github.com/openshift/client-go/user/clientset/versioned"
	userv1 "github.com/openshift/client-go/user/clientset/versioned/typed/user/v1"

	cmdutil "github.com/openshift/oc/pkg/helpers/cmd"
)

const RecommendedName = "oauth"

var oauthLong = templates.LongDesc(`
	Manage OAuth resources on the cluster

	These commands assist in tasks related to the OAuth configuration of the cluster. Note that some
	clusters may be configured to use an external OAuth provider that will disable these actions.

	To see more information on the configuration, use the 'get' and 'describe' commands on the following
	resources: 'oauthclient' and 'oauthaccesstokens'.`)

// NewCmd implements the OpenShift cli oauth command
func NewCmd(name, fullName string, f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	// Parent command to which all subcommands are added.
	cmds := &cobra.Command{
		Use:   name,
		Short: "Manage OAuth actions",
		Long:  oauthLong,
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	groups := templates.CommandGroups{
		{
			Message: "Advanced:",
			Commands: []*cobra.Command{
				NewCmdCreateToken(CreateTokenRecommendedName, fullName+" "+CreateTokenRecommendedName, f, streams),
			},
		},
	}
	groups.Add(cmds)
	cmdutil.ActsAsRootCommand(cmds, []string{"options"}, groups...)

	return cmds
}

const (
	CreateTokenRecommendedName = "create-token"
)

var (
	createTokenExample = templates.Examples(`
		# Create a token for the current user
		%[1]s
		
		# Create a token for another user that expires in one minute
		%[1]s --duration=1m --user=alice`)
)

type CreateTokenOptions struct {
	PrintFlags     *genericclioptions.PrintFlags
	ToPrinter      func(string) (printers.ResourcePrinter, error)
	DryRunStrategy kcmdutil.DryRunStrategy
	Output         string

	OAuthClientInterface      oauthv1client.OAuthClientInterface
	OAuthAccessTokenInterface oauthv1client.OAuthAccessTokenInterface
	UserInterface             userv1.UserInterface

	Duration        time.Duration
	UserName        string
	UserUID         string
	OAuthClientName string
	UserScopes      []string
	RoleScope       string
	RoleAll         bool
	RedirectURI     string

	genericclioptions.IOStreams
}

func NewCreateTokenOptions(streams genericclioptions.IOStreams) *CreateTokenOptions {
	return &CreateTokenOptions{
		PrintFlags: genericclioptions.NewPrintFlags("added to").WithTypeSetter(scheme.Scheme),
		IOStreams:  streams,
	}
}

func NewCmdCreateToken(name, fullName string, f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewCreateTokenOptions(streams)
	o.OAuthClientName = "explicit-token-grant"
	o.UserName = "~"
	o.Duration = time.Hour
	cmd := &cobra.Command{
		Use:   name,
		Short: "Create a new OAuth access token",
		Long: templates.LongDesc(`
			Create an OAuth access token

			This command will create an OAuth access token with the requested scopes and expiration time for the
			current user or a requested user. The actions for creating a token require cluster admin and will
			be assigned to an OAuth client called 'explicit-token-grant' which will not be usable from normal
			OAuth flows.

			The scopes that may be assigned to a token are:
			
			  * user:info - retrieve user name and uid from the OAuth server
			  * user:full - act as the user
			  * role:<cluster_role_name>:* - actions allowed by the provided cluster role in any namespace
			  * role:<cluster_role_name>:<namespace> - actions allowed by the provided cluster role in the provided

			The token will be printed on success and may be used via 'oc login' or the '--token=' argument.

			Note that this command will have no effect if the OAuth component has been disabled via configuration.
			`),
		Example: fmt.Sprintf(createTokenExample, fullName),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run())
		},
	}
	cmd.Flags().StringVar(&o.UserName, "user", o.UserName, "The name of the user to create a token for. Defaults to the current user.")
	cmd.Flags().StringVar(&o.RoleScope, "role", o.RoleScope, "Sets the scope to the provided cluster role name in the current namespace.")
	cmd.Flags().BoolVar(&o.RoleAll, "role-all", o.RoleAll, "Sets the role scope over all namespaces.")
	cmd.Flags().StringSliceVar(&o.UserScopes, "scopes", o.UserScopes, "The scopes to be assigned to the user.")
	cmd.Flags().DurationVar(&o.Duration, "duration", o.Duration, "How long before this token expires.")

	kcmdutil.AddDryRunFlag(cmd)
	o.PrintFlags.AddFlags(cmd)
	return cmd
}

func (o *CreateTokenOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	var err error
	o.DryRunStrategy, err = kcmdutil.GetDryRunStrategy(cmd)
	if err != nil {
		return err
	}
	o.Output = kcmdutil.GetFlagString(cmd, "output")

	o.ToPrinter = func(message string) (printers.ResourcePrinter, error) {
		o.PrintFlags.NamePrintFlags.Operation = message
		kcmdutil.PrintFlagsWithDryRunStrategy(o.PrintFlags, o.DryRunStrategy)
		return o.PrintFlags.ToPrinter()
	}

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	userClient, err := userclient.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	oauthClient, err := oauthclient.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	o.UserInterface = userClient.UserV1().Users()
	o.OAuthAccessTokenInterface = oauthClient.OauthV1().OAuthAccessTokens()
	o.OAuthClientInterface = oauthClient.OauthV1().OAuthClients()

	if len(o.UserUID) == 0 {
		userName := o.UserName
		if len(userName) == 0 {
			userName = "~"
		}
		user, err := o.UserInterface.Get(context.Background(), userName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if len(user.UID) == 0 {
			return fmt.Errorf("user %s must not be a virtual user (e.g. system:admin) - create a user first", user.Name)
		}
		o.UserName = user.Name
		o.UserUID = string(user.UID)
	}

	if len(o.RoleScope) > 0 {
		if o.RoleAll {
			o.UserScopes = append(o.UserScopes, fmt.Sprintf("role:%s:*", o.RoleScope))
		} else {
			ns, _, err := f.ToRawKubeConfigLoader().Namespace()
			if err != nil {
				return err
			}
			o.UserScopes = append(o.UserScopes, fmt.Sprintf("role:%s:%s", o.RoleScope, ns))
		}
	}
	if len(o.UserScopes) == 0 {
		o.UserScopes = append(o.UserScopes, "user:full")
	}

	// use of this redirectURI prohibits normal login
	o.RedirectURI = "https://localhost:8443/oauth/token/implicit"

	return nil
}

func (o *CreateTokenOptions) Run() error {
	if o.DryRunStrategy != kcmdutil.DryRunClient {
		client := &oauthv1.OAuthClient{
			ObjectMeta:  metav1.ObjectMeta{Name: o.OAuthClientName},
			GrantMethod: oauthv1.GrantHandlerAuto,
		}
		if _, err := o.OAuthClientInterface.Create(context.Background(), client, metav1.CreateOptions{}); err != nil {
			if !errors.IsAlreadyExists(err) {
				return err
			}
		}
	}

	tokenBytes := make([]byte, 64)
	if _, err := io.ReadAtLeast(rand.Reader, tokenBytes, 64); err != nil {
		return err
	}
	accessToken := base64.RawURLEncoding.EncodeToString(tokenBytes)

	token := &oauthv1.OAuthAccessToken{
		ObjectMeta:  metav1.ObjectMeta{Name: accessToken},
		ClientName:  o.OAuthClientName,
		UserName:    o.UserName,
		UserUID:     o.UserUID,
		Scopes:      o.UserScopes,
		RedirectURI: o.RedirectURI,
		ExpiresIn:   int64(math.Ceil(o.Duration.Seconds())),
	}
	if o.DryRunStrategy == kcmdutil.DryRunClient {
		p, err := o.ToPrinter("creating")
		if err != nil {
			return err
		}

		return p.PrintObj(token, o.Out)
	}
	if _, err := o.OAuthAccessTokenInterface.Create(context.Background(), token, metav1.CreateOptions{}); err != nil {
		return err
	}

	_, err := fmt.Fprintf(o.Out, "%s\n", token.Name)
	return err
}
