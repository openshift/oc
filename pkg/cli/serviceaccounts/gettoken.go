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
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/helpers/term"
)

const (
	getServiceAccountTokenShort = `Get a token assigned to a service account.`
)

var (
	getServiceAccountTokenLong = templates.LongDesc(`
		Get a token assigned to a service account.

		If the service account has multiple tokens, the first token found will be returned.

		Service account API tokens are used by service accounts to authenticate to the API.
		Client actions using a service account token will be executed as if the service account
		itself were making the actions.
	`)

	getServiceAccountTokenExamples = templates.Examples(`
		# Get the service account token from service account 'default'
		arvan paas serviceaccounts get-token 'default'
	`)
)

type GetServiceAccountTokenOptions struct {
	SAName        string
	SAClient      corev1client.ServiceAccountInterface
	SecretsClient corev1client.SecretInterface

	genericclioptions.IOStreams
}

func NewGetServiceAccountTokenOptions(streams genericclioptions.IOStreams) *GetServiceAccountTokenOptions {
	return &GetServiceAccountTokenOptions{
		IOStreams: streams,
	}
}

func NewCommandGetServiceAccountToken(f cmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	options := NewGetServiceAccountTokenOptions(streams)

	getServiceAccountTokenCommand := &cobra.Command{
		Use:     "get-token NAME",
		Short:   getServiceAccountTokenShort,
		Long:    getServiceAccountTokenLong,
		Example: getServiceAccountTokenExamples,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(options.Complete(args, f, cmd))
			cmdutil.CheckErr(options.Validate())
			cmdutil.CheckErr(options.Run())
		},
	}

	return getServiceAccountTokenCommand
}

func (o *GetServiceAccountTokenOptions) Complete(args []string, f cmdutil.Factory, cmd *cobra.Command) error {
	if len(args) != 1 {
		return cmdutil.UsageErrorf(cmd, fmt.Sprintf("expected one service account name as an argument, got %q", args))
	}

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

	o.SAClient = client.ServiceAccounts(namespace)
	o.SecretsClient = client.Secrets(namespace)
	return nil
}

func (o *GetServiceAccountTokenOptions) Validate() error {
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

func (o *GetServiceAccountTokenOptions) Run() error {
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

			fmt.Fprintf(o.Out, string(token))
			if term.IsTerminalWriter(o.Out) {
				// pretty-print for a TTY
				fmt.Fprintf(o.Out, "\n")
			}
			return nil
		}
	}
	return fmt.Errorf("could not find a service account token for service account %q", serviceAccount.Name)
}
