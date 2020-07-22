package serviceaccounts

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	kclientcmd "k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

const (
	createKubeconfigShort = `Generate a kubeconfig file for a service account`
)

var (
	createKubeconfigLong = templates.LongDesc(`
		Generate a kubeconfig file that will utilize this service account.

		The kubeconfig file will reference the service account token and use the current server,
		namespace, and cluster contact info. If the service account has multiple tokens, the
		first token found will be returned. The generated file will be output to STDOUT.

		Service account API tokens are used by service accounts to authenticate to the API.
		Client actions using a service account token will be executed as if the service account
		itself were making the actions.
	`)

	createKubeconfigExamples = templates.Examples(`
		# Create a kubeconfig file for service account 'default'
		oc serviceaccounts create-kubeconfig 'default' > default.kubeconfig
	`)
)

type CreateKubeconfigOptions struct {
	SAName           string
	SAClient         corev1client.ServiceAccountInterface
	SecretsClient    corev1client.SecretInterface
	RawConfig        clientcmdapi.Config
	ContextNamespace string
	Context          string

	genericclioptions.IOStreams
}

func NewCreateKubeconfigOptions(streams genericclioptions.IOStreams) *CreateKubeconfigOptions {
	return &CreateKubeconfigOptions{
		IOStreams: streams,
	}
}

func NewCommandCreateKubeconfig(f cmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	options := NewCreateKubeconfigOptions(streams)

	cmd := &cobra.Command{
		Use:     "create-kubeconfig NAME",
		Short:   createKubeconfigShort,
		Long:    createKubeconfigLong,
		Example: createKubeconfigExamples,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(options.Complete(args, f, cmd))
			cmdutil.CheckErr(options.Validate())
			cmdutil.CheckErr(options.Run())
		},
	}
	cmd.Flags().StringVar(&options.ContextNamespace, "with-namespace", "", "Namespace for this context in .kubeconfig.")
	return cmd
}

func (o *CreateKubeconfigOptions) Complete(args []string, f cmdutil.Factory, cmd *cobra.Command) error {
	if len(args) != 1 {
		return cmdutil.UsageErrorf(cmd, fmt.Sprintf("expected one service account name as an argument, got %q", args))
	}
	context, err := cmd.Flags().GetString("context")
	if err != nil {
		return fmt.Errorf("unable to get value for --context flag: %v", err)
	}

	o.Context = context

	o.SAName = args[0]

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	client, err := corev1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	namespace, _, err := f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	o.RawConfig, err = f.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return err
	}

	if len(o.ContextNamespace) == 0 {
		o.ContextNamespace = namespace
	}

	o.SAClient = client.ServiceAccounts(namespace)
	o.SecretsClient = client.Secrets(namespace)
	return nil
}

func (o *CreateKubeconfigOptions) Validate() error {
	if o.SAName == "" {
		return errors.New("service account name cannot be empty")
	}

	if o.SAClient == nil || o.SecretsClient == nil {
		return errors.New("API clients must not be nil in order to create a new service account token")
	}

	if o.Out == nil || o.ErrOut == nil {
		return errors.New("cannot proceed if output or error writers are nil")
	}

	return nil
}

func (o *CreateKubeconfigOptions) Run() error {
	serviceAccount, err := o.SAClient.Get(context.TODO(), o.SAName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for _, reference := range serviceAccount.Secrets {
		secret, err := o.SecretsClient.Get(context.TODO(), reference.Name, metav1.GetOptions{})
		if err != nil {
			continue
		}

		if isServiceAccountToken(secret, serviceAccount) {
			token, exists := secret.Data[corev1.ServiceAccountTokenKey]
			if !exists {
				return fmt.Errorf("service account token %q for service account %q did not contain token data", secret.Name, serviceAccount.Name)
			}

			// It's assumed o.RawConfig is only read here since MinifyConfig modifies
			// the list of contexts and reduces it to a single context given by CurrentContext
			// field. Thus, it's safe to modify cfg.CurrentContext here.
			cfg := &o.RawConfig
			if o.Context != "" {
				cfg.CurrentContext = o.Context
			}

			if err := clientcmdapi.MinifyConfig(cfg); err != nil {
				return fmt.Errorf("invalid configuration, unable to create new config file: %v", err)
			}

			ctx := cfg.Contexts[cfg.CurrentContext]
			ctx.Namespace = o.ContextNamespace
			// rename the current context
			cfg.CurrentContext = o.SAName
			cfg.Contexts = map[string]*clientcmdapi.Context{
				cfg.CurrentContext: ctx,
			}
			// use the server name
			ctx.AuthInfo = o.SAName
			cfg.AuthInfos = map[string]*clientcmdapi.AuthInfo{
				ctx.AuthInfo: {
					Token: string(token),
				},
			}
			out, err := kclientcmd.Write(*cfg)
			if err != nil {
				return err
			}
			fmt.Fprintf(o.Out, string(out))
			return nil
		}
	}
	return fmt.Errorf("could not find a service account token for service account %q", serviceAccount.Name)
}
