package logout

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	restclient "k8s.io/client-go/rest"
	kclientcmd "k8s.io/client-go/tools/clientcmd"
	kclientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	oauthv1client "github.com/openshift/client-go/oauth/clientset/versioned/typed/oauth/v1"
	"github.com/openshift/oc/pkg/helpers/project"
)

const sha256Prefix = "sha256~"

type LogoutOptions struct {
	StartingKubeConfig *kclientcmdapi.Config
	Config             *restclient.Config

	PathOptions *kclientcmd.PathOptions

	genericclioptions.IOStreams
}

var (
	logoutLong = templates.LongDesc(`
		Log out of the active session out by clearing saved tokens

		An authentication token is stored in the config file after login - this command will delete
		that token on the server, and then remove the token from the configuration file.

		If you are using an alternative authentication method like Kerberos or client certificates,
		your ticket or client certificate will not be removed from the current system since these
		are typically managed by other programs. Instead, you can delete your config file to remove
		the local copy of that certificate or the record of your server login.

		After logging out, if you want to log back into the server use 'arvan paas login'.
	`)

	logoutExample = templates.Examples(`
		# Logout
		arvan paas logout
	`)
)

func NewLogoutOptions(streams genericclioptions.IOStreams) *LogoutOptions {
	return &LogoutOptions{
		IOStreams: streams,
	}
}

// NewCmdLogout implements the OpenShift cli logout command
func NewCmdLogout(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewLogoutOptions(streams)
	cmds := &cobra.Command{
		Use:     "logout",
		Short:   "End the current server session",
		Long:    logoutLong,
		Example: logoutExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate(args))
			kcmdutil.CheckErr(o.RunLogout())
		},
	}

	// TODO: support --all which performs the same logic on all users in your config file.

	return cmds
}

func (o *LogoutOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	kubeconfig, err := f.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return err
	}
	o.StartingKubeConfig = &kubeconfig

	o.Config, err = f.ToRESTConfig()
	if err != nil {
		return err
	}

	o.PathOptions = kclientcmd.NewDefaultPathOptions()
	// we need to set explicit path if one was specified, since NewDefaultPathOptions doesn't do it for us
	o.PathOptions.LoadingRules.ExplicitPath = kcmdutil.GetFlagString(cmd, kclientcmd.RecommendedConfigPathFlag)

	return nil
}

func (o LogoutOptions) Validate(args []string) error {
	if len(args) > 0 {
		return errors.New("No arguments are allowed")
	}

	if o.StartingKubeConfig == nil {
		return errors.New("Must have a config file already created")
	}

	if len(o.Config.BearerToken) == 0 {
		return errors.New("You must have a token in order to logout.")
	}

	return nil
}

func (o LogoutOptions) RunLogout() error {
	token := o.Config.BearerToken
	tokenName := o.Config.BearerToken

	client, err := oauthv1client.NewForConfig(o.Config)
	if err != nil {
		return err
	}

	userInfo, err := project.WhoAmI(o.Config)
	if err != nil {
		return err
	}

	if strings.HasPrefix(tokenName, sha256Prefix) {
		tokenName = tokenToObjectName(tokenName)
	}

	if err := client.OAuthAccessTokens().Delete(context.TODO(), tokenName, metav1.DeleteOptions{}); err != nil {
		klog.V(1).Infof("%v", err)
	}

	configErr := deleteTokenFromConfig(*o.StartingKubeConfig, o.PathOptions, token)
	if configErr == nil {
		klog.V(1).Infof("Removed token from your local configuration.")

		// only return error instead of successful message if removing token from client
		// config fails. Any error that occurs deleting token using api is logged above.
		fmt.Fprintf(o.Out, "Logged %q out on %q\n", userInfo.Name, o.Config.Host)
	}

	return configErr
}

func deleteTokenFromConfig(config kclientcmdapi.Config, pathOptions *kclientcmd.PathOptions, bearerToken string) error {
	for key, value := range config.AuthInfos {
		if value.Token == bearerToken {
			value.Token = ""
			config.AuthInfos[key] = value
			// don't break, its possible that more than one user stanza has the same token.
		}
	}
	return kclientcmd.ModifyConfig(pathOptions, config, true)
}

// tokenToObjectName returns the oauthaccesstokens object name for the given raw token,
// i.e. the sha256 hash prefixed with "sha256~".
func tokenToObjectName(token string) string {
	name := strings.TrimPrefix(token, sha256Prefix)
	h := sha256.Sum256([]byte(name))
	return sha256Prefix + base64.RawURLEncoding.EncodeToString(h[0:])
}
