package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/templates"
	"k8s.io/kubernetes/pkg/credentialprovider"
)

const CreateDockerConfigSecretRecommendedName = "new-dockercfg"

var (
	createDockercfgLong = templates.LongDesc(`
    Create a new dockercfg secret

    Dockercfg secrets are used to authenticate against container image registries.

    When using the Docker command line to push images, you can authenticate to a given registry by running
    'docker login DOCKER_REGISTRY_SERVER --username=DOCKER_USER --password=DOCKER_PASSWORD --email=DOCKER_EMAIL'.
    That produces a ~/.dockercfg file that is used by subsequent 'docker push' and 'docker pull' commands to
    authenticate to the registry.

    When creating applications, you may have a container image registry that requires authentication.  In order for the
    nodes to pull images on your behalf, they have to have the credentials.  You can provide this information
    by creating a dockercfg secret and attaching it to your service account.`)

	createDockercfgExample = templates.Examples(`
    # Create a new .dockercfg secret:
    %[1]s SECRET --docker-server=DOCKER_REGISTRY_SERVER --docker-username=DOCKER_USER --docker-password=DOCKER_PASSWORD --docker-email=DOCKER_EMAIL

    # Create a new .dockercfg secret from an existing file:
    %[2]s SECRET path/to/.dockercfg

    # Create a new .docker/config.json secret from an existing file:
    %[2]s SECRET .dockerconfigjson=path/to/.docker/config.json

    # To add new secret to 'imagePullSecrets' for the node, or 'secrets' for builds, use:
    %[3]s SERVICE_ACCOUNT`)
)

type CreateDockerConfigOptions struct {
	PrintFlags *genericclioptions.PrintFlags

	Printer printers.ResourcePrinter

	SecretName       string
	RegistryLocation string
	Username         string
	Password         string
	EmailAddress     string

	SecretsInterface corev1client.SecretInterface

	genericclioptions.IOStreams
}

func NewCreateDockerConfigOptions(streams genericclioptions.IOStreams) *CreateDockerConfigOptions {
	return &CreateDockerConfigOptions{
		PrintFlags: genericclioptions.NewPrintFlags("created").WithTypeSetter(scheme.Scheme),
		IOStreams:  streams,
	}
}

// NewCmdCreateDockerConfigSecret creates a command object for making a dockercfg secret
func NewCmdCreateDockerConfigSecret(name, fullName string, f kcmdutil.Factory, streams genericclioptions.IOStreams, newSecretFullName, ocEditFullName string) *cobra.Command {
	o := NewCreateDockerConfigOptions(streams)

	cmd := &cobra.Command{
		Use:        fmt.Sprintf("%s SECRET --docker-server=DOCKER_REGISTRY_SERVER --docker-username=DOCKER_USER --docker-password=DOCKER_PASSWORD --docker-email=DOCKER_EMAIL", name),
		Short:      "Create a new dockercfg secret",
		Long:       createDockercfgLong,
		Example:    fmt.Sprintf(createDockercfgExample, fullName, newSecretFullName, ocEditFullName),
		Deprecated: "use oc create secret",
		Hidden:     true,
		Run: func(c *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())

		},
	}

	cmd.Flags().StringVar(&o.Username, "docker-username", "", "Username for container image registry authentication")
	cmd.Flags().StringVar(&o.Password, "docker-password", "", "Password for container image registry authentication")
	cmd.Flags().StringVar(&o.EmailAddress, "docker-email", "", "Email for container image registry")
	cmd.Flags().StringVar(&o.RegistryLocation, "docker-server", "https://index.docker.io/v1/", "Server location for container image registry")

	o.PrintFlags.AddFlags(cmd)
	return cmd
}

func (o CreateDockerConfigOptions) Run() error {
	secret, err := o.NewDockerSecret()
	if err != nil {
		return err
	}

	// TODO: sweep codebase removing implied --dry-run behavior when -o is specified
	if o.PrintFlags.OutputFormat != nil && len(*o.PrintFlags.OutputFormat) == 0 {
		persistedSecret, err := o.SecretsInterface.Create(secret)
		if err != nil {
			return err
		}

		return o.Printer.PrintObj(persistedSecret, o.Out)
	}

	return o.Printer.PrintObj(secret, o.Out)
}

func (o CreateDockerConfigOptions) NewDockerSecret() (*corev1.Secret, error) {
	dockercfgAuth := credentialprovider.DockerConfigEntry{
		Username: o.Username,
		Password: o.Password,
		Email:    o.EmailAddress,
	}

	dockerCfg := credentialprovider.DockerConfigJson{
		Auths: map[string]credentialprovider.DockerConfigEntry{o.RegistryLocation: dockercfgAuth},
	}

	dockercfgContent, err := json.Marshal(dockerCfg)
	if err != nil {
		return nil, err
	}

	secret := &corev1.Secret{}
	secret.Name = o.SecretName
	secret.Type = corev1.SecretTypeDockerConfigJson
	secret.Data = map[string][]byte{}
	secret.Data[corev1.DockerConfigJsonKey] = dockercfgContent

	return secret, nil
}

func (o *CreateDockerConfigOptions) Complete(f kcmdutil.Factory, args []string) error {
	if len(args) != 1 {
		return errors.New("must have exactly one argument: secret name")
	}
	o.SecretName = args[0]

	config, err := f.ToRESTConfig()
	if err != nil {
		return err
	}

	client, err := corev1client.NewForConfig(config)
	if err != nil {
		return err
	}
	namespace, _, err := f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	o.SecretsInterface = client.Secrets(namespace)

	o.Printer, err = o.PrintFlags.ToPrinter()
	if err != nil {
		return err
	}

	return nil
}

func (o CreateDockerConfigOptions) Validate() error {
	if len(o.SecretName) == 0 {
		return errors.New("secret name must be present")
	}
	if len(o.RegistryLocation) == 0 {
		return errors.New("docker-server must be present")
	}
	if len(o.Username) == 0 {
		return errors.New("docker-username must be present")
	}
	if len(o.Password) == 0 {
		return errors.New("docker-password must be present")
	}
	if len(o.EmailAddress) == 0 {
		return errors.New("docker-email must be present")
	}
	if o.SecretsInterface == nil {
		return errors.New("secrets interface must be present")
	}

	if strings.Contains(o.Username, ":") {
		return fmt.Errorf("username '%v' is illegal because it contains a ':'", o.Username)
	}

	return nil
}
