package whoami

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	v1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	authenticationv1client "k8s.io/client-go/kubernetes/typed/authentication/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	userv1 "github.com/openshift/api/user/v1"
	userv1typedclient "github.com/openshift/client-go/user/clientset/versioned/typed/user/v1"
	"sigs.k8s.io/yaml"
)

const (
	openShiftConfigManagedNamespaceName = "openshift-config-managed"
	consolePublicConfigMap              = "console-public"
)

var whoamiLong = templates.LongDesc(`
	Show information about the current session

	The default options for this command will return the currently authenticated user name
	or an empty string.  Other flags support returning the currently used token or the
	user context.`)

var whoamiExample = templates.Examples(`
	# Display the currently authenticated user
	oc whoami
`)

type WhoAmIOptions struct {
	UserInterface userv1typedclient.UserV1Interface
	AuthV1Client  authenticationv1client.AuthenticationV1Interface

	ClientConfig *rest.Config
	KubeClient   kubernetes.Interface
	RawConfig    api.Config

	ShowToken      bool
	ShowContext    bool
	ShowServer     bool
	ShowConsoleUrl bool
	ShowGroups     bool
	Output         string

	genericiooptions.IOStreams
}

func NewWhoAmIOptions(streams genericiooptions.IOStreams) *WhoAmIOptions {
	return &WhoAmIOptions{
		IOStreams: streams,
	}
}

func NewCmdWhoAmI(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewWhoAmIOptions(streams)

	cmd := &cobra.Command{
		Use:     "whoami",
		Short:   "Return information about the current session",
		Long:    whoamiLong,
		Example: whoamiExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().BoolVarP(&o.ShowToken, "show-token", "t", o.ShowToken, "Print the token the current session is using. This will return an error if you are using a different form of authentication.")
	cmd.Flags().BoolVarP(&o.ShowContext, "show-context", "c", o.ShowContext, "Print the current user context name")
	cmd.Flags().BoolVar(&o.ShowServer, "show-server", o.ShowServer, "If true, print the current server's REST API URL")
	cmd.Flags().BoolVar(&o.ShowConsoleUrl, "show-console", o.ShowConsoleUrl, "If true, print the current server's web console URL")
	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output, "One of 'yaml' or 'json'.")

	return cmd
}

func (o WhoAmIOptions) WhoAmI() (*userv1.User, error) {
	res, err := o.AuthV1Client.SelfSubjectReviews().Create(context.TODO(), &v1.SelfSubjectReview{}, metav1.CreateOptions{})
	if err == nil {
		me := &userv1.User{
			ObjectMeta: metav1.ObjectMeta{
				Name: res.Status.UserInfo.Username,
			},
			Groups: res.Status.UserInfo.Groups,
		}
		fmt.Fprintf(o.Out, "%s\n", me.Name)
		return me, nil
	} else {
		klog.V(2).Infof("selfsubjectreview request error %v, falling back to user object", err)
	}

	me, err := o.UserInterface.Users().Get(context.TODO(), "~", metav1.GetOptions{})
	if err == nil {
		fmt.Fprintf(o.Out, "%s\n", me.Name)
	}

	return me, err
}

func (o *WhoAmIOptions) Complete(f kcmdutil.Factory) error {
	var err error

	o.ClientConfig, err = f.ToRESTConfig()
	if err != nil {
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(o.ClientConfig)
	if err != nil {
		return err
	}
	o.KubeClient = kubeClient

	o.RawConfig, err = f.ToRawKubeConfigLoader().RawConfig()
	return err
}

func (o *WhoAmIOptions) Validate() error {
	if o.ShowToken && len(o.ClientConfig.BearerToken) == 0 {
		return fmt.Errorf("no token is currently in use for this session")
	}
	if o.ShowContext && len(o.RawConfig.CurrentContext) == 0 {
		return fmt.Errorf("no context has been set")
	}

	return nil
}

func (o *WhoAmIOptions) getWebConsoleUrl() (string, error) {
	consolePublicConfig, err := o.KubeClient.CoreV1().ConfigMaps(openShiftConfigManagedNamespaceName).Get(context.TODO(), consolePublicConfigMap, metav1.GetOptions{})
	// This means the command was run against 3.x server
	if errors.IsNotFound(err) {
		return o.ClientConfig.Host, nil
	}
	if err != nil {
		return "", fmt.Errorf("unable to determine console location: %v", err)
	}

	consoleUrl, exists := consolePublicConfig.Data["consoleURL"]
	if !exists {
		return "", fmt.Errorf("unable to determine console location from the cluster")
	}
	return consoleUrl, nil
}

func (o *WhoAmIOptions) Run() error {
	var err error
	o.UserInterface, err = userv1typedclient.NewForConfig(o.ClientConfig)
	if err != nil {
		return err
	}

	switch {
	case o.ShowToken:
		fmt.Fprintf(o.Out, "%s\n", o.ClientConfig.BearerToken)
		return nil
	case o.ShowContext:
		fmt.Fprintf(o.Out, "%s\n", o.RawConfig.CurrentContext)
		return nil
	case o.ShowServer:
		fmt.Fprintf(o.Out, "%s\n", o.ClientConfig.Host)
		return nil
	case o.ShowConsoleUrl:
		consoleUrl, err := o.getWebConsoleUrl()
		if err != nil {
			return err
		}
		fmt.Fprintf(o.Out, "%s\n", consoleUrl)
		return nil
	case o.Output == "yaml":
		u, err := o.UserInterface.Users().Get(context.TODO(), "~", metav1.GetOptions{})
		u.SetGroupVersionKind(userv1.GroupVersion.WithKind("User"))
		if err != nil {
			return err
		}

		y, err := yaml.Marshal(u)
		if err != nil {
			return err
		}
		fmt.Fprintf(o.Out, "%s\n", string(y))
		return nil
	case o.Output == "json":
		u, err := o.UserInterface.Users().Get(context.TODO(), "~", metav1.GetOptions{})
		u.SetGroupVersionKind(userv1.GroupVersion.WithKind("User"))
		if err != nil {
			return err
		}
		j, err := json.MarshalIndent(u, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintf(o.Out, "%s\n", string(j))
		return nil
	}

	o.AuthV1Client, err = authenticationv1client.NewForConfig(o.ClientConfig)
	if err != nil {
		return err
	}

	_, err = o.WhoAmI()

	return err
}
