package serviceaccounts

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	watchtools "k8s.io/client-go/tools/watch"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/generate"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/helpers/term"
)

const (
	NewServiceAccountTokenRecommendedName = "new-token"

	newServiceAccountTokenShort = `Generate a new token for a service account.`

	newServiceAccountTokenUsage = `%s SA-NAME`
)

var (
	newServiceAccountTokenLong = templates.LongDesc(`
		Generate a new token for a service account.

		Service account API tokens are used by service accounts to authenticate to the API.
		This command will generate a new token, which could be used to compartmentalize service
		account actions by executing them with distinct tokens, to rotate out pre-existing token
		on the service account, or for use by an external client. If a label is provided, it will
		be applied to any created token so that tokens created with this command can be idenitifed.
	`)

	newServiceAccountTokenExamples = templates.Examples(`
		# Generate a new token for service account 'default'
		arvan paas serviceaccounts new-token 'default'

		# Generate a new token for service account 'default' and apply
		# labels 'foo' and 'bar' to the new token for identification
		arvan paas serviceaccounts new-token 'default' --labels foo=foo-value,bar=bar-value
	`)
)

type ServiceAccountTokenOptions struct {
	SAName        string
	SAClient      corev1client.ServiceAccountInterface
	SecretsClient corev1client.SecretInterface

	Labels map[string]string

	Timeout time.Duration

	genericclioptions.IOStreams
}

func NewServiceAccountTokenOptions(streams genericclioptions.IOStreams) *ServiceAccountTokenOptions {
	return &ServiceAccountTokenOptions{
		IOStreams: streams,
		Labels:    map[string]string{},
	}
}

func NewCommandNewServiceAccountToken(f cmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	options := NewServiceAccountTokenOptions(streams)

	var requestedLabels string
	newServiceAccountTokenCommand := &cobra.Command{
		Use:     "new-token",
		Short:   newServiceAccountTokenShort,
		Long:    newServiceAccountTokenLong,
		Example: newServiceAccountTokenExamples,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(options.Complete(args, requestedLabels, f, cmd))
			cmdutil.CheckErr(options.Validate())
			cmdutil.CheckErr(options.Run())
		},
	}

	newServiceAccountTokenCommand.Flags().DurationVar(&options.Timeout, "timeout", 30*time.Second, "the maximum time allowed to generate a token")
	newServiceAccountTokenCommand.Flags().StringVarP(&requestedLabels, "labels", "l", "", "labels to set in all resources for this application, given as a comma-delimited list of key-value pairs")
	return newServiceAccountTokenCommand
}

func (o *ServiceAccountTokenOptions) Complete(args []string, requestedLabels string, f cmdutil.Factory, cmd *cobra.Command) error {
	if len(args) != 1 {
		return cmdutil.UsageErrorf(cmd, fmt.Sprintf("expected one service account name as an argument, got %q", args))
	}

	o.SAName = args[0]

	if len(requestedLabels) > 0 {
		labels, err := generate.ParseLabels(requestedLabels)
		if err != nil {
			return cmdutil.UsageErrorf(cmd, err.Error())
		}
		o.Labels = labels
	}

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
		return fmt.Errorf("could not retrieve default namespace: %v", err)
	}

	o.SAClient = client.ServiceAccounts(namespace)
	o.SecretsClient = client.Secrets(namespace)
	return nil
}

func (o *ServiceAccountTokenOptions) Validate() error {
	if o.SAName == "" {
		return errors.New("service account name cannot be empty")
	}

	if o.SAClient == nil || o.SecretsClient == nil {
		return errors.New("API clients must not be nil in order to create a new service account token")
	}

	if o.Timeout <= 0 {
		return errors.New("a positive amount of time must be allotted for the timeout")
	}

	if o.Out == nil || o.ErrOut == nil {
		return errors.New("cannot proceed if output or error writers are nil")
	}

	return nil
}

// Run creates a new token secret, waits for the service account token controller to fulfill it, then adds the token to the service account
func (o *ServiceAccountTokenOptions) Run() error {
	serviceAccount, err := o.SAClient.Get(context.TODO(), o.SAName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: GetTokenSecretNamePrefix(serviceAccount.Name),
			Namespace:    serviceAccount.Namespace,
			Labels:       o.Labels,
			Annotations: map[string]string{
				corev1.ServiceAccountNameKey: serviceAccount.Name,
			},
		},
		Type: corev1.SecretTypeServiceAccountToken,
		Data: map[string][]byte{},
	}

	persistedToken, err := o.SecretsClient.Create(context.TODO(), tokenSecret, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	// we need to wait for the service account token controller to make the new token valid
	tokenSecret, err = waitForToken(persistedToken, serviceAccount, o.Timeout, o.SecretsClient)
	if err != nil {
		return err
	}

	token, exists := tokenSecret.Data[corev1.ServiceAccountTokenKey]
	if !exists {
		return fmt.Errorf("service account token %q did not contain token data", tokenSecret.Name)
	}

	fmt.Fprintf(o.Out, string(token))
	if term.IsTerminalWriter(o.Out) {
		// pretty-print for a TTY
		fmt.Fprintf(o.Out, "\n")
	}
	return nil
}

// waitForToken uses `cmd.Until` to wait for the service account controller to fulfill the token request
func waitForToken(token *corev1.Secret, serviceAccount *corev1.ServiceAccount, timeout time.Duration, client corev1client.SecretInterface) (*corev1.Secret, error) {
	// there is no provided rounding function, so we use Round(x) === Floor(x + 0.5)
	timeoutSeconds := int64(math.Floor(timeout.Seconds() + 0.5))

	options := metav1.ListOptions{
		FieldSelector:   fields.SelectorFromSet(fields.Set(map[string]string{"metadata.name": token.Name})).String(),
		Watch:           true,
		ResourceVersion: token.ResourceVersion,
		TimeoutSeconds:  &timeoutSeconds,
	}

	watcher, err := client.Watch(context.TODO(), options)
	if err != nil {
		return nil, fmt.Errorf("could not begin watch for token: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	event, err := watchtools.UntilWithoutRetry(ctx, watcher, func(event watch.Event) (bool, error) {
		if event.Type == watch.Error {
			return false, fmt.Errorf("encountered error while watching for token: %v", event.Object)
		}

		eventToken, ok := event.Object.(*corev1.Secret)
		if !ok {
			return false, nil
		}

		if eventToken.Name != token.Name {
			return false, nil
		}

		switch event.Type {
		case watch.Modified:
			if isServiceAccountToken(eventToken, serviceAccount) {
				return true, nil
			}
		case watch.Deleted:
			return false, errors.New("token was deleted before fulfillment by service account token controller")
		case watch.Added:
			return false, errors.New("unxepected action: token was added after initial creation")
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return event.Object.(*corev1.Secret), nil
}
