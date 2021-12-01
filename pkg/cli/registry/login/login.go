package login

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	dockerconfig "github.com/containers/image/v5/pkg/docker/config"
	containertypes "github.com/containers/image/v5/types"
	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/homedir"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	imageclient "github.com/openshift/client-go/image/clientset/versioned"
	"github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/library-go/pkg/image/registryclient"
	"github.com/openshift/oc/pkg/helpers/image"
)

var (
	desc = templates.LongDesc(`
		Log in to the OpenShift integrated registry.

		This logs your local Docker client into the OpenShift integrated registry using the
		external registry name (if configured by your administrator). You may also log in
		using a service account if you have access to its credentials. If you are logged in
		to the server using a client certificate the command will report an error because
		container registries do not generally allow client certificates.

		As an advanced option you may specify the credentials to login with using --auth-basic
		with USER:PASSWORD. This may not be used with the -z flag.

		You may specify an alternate file to write credentials to with --to instead of
		.docker/config.json in your home directory. If you pass --to=- the file will be
		written to standard output.

		To detect the registry hostname the client will attempt to find an image stream in
		the current namespace or the openshift namespace and use the status fields that
		indicate the registry hostnames. If no image stream is found or if you do not have
		permission to view image streams you will have to pass the --registry flag with the
		desired host name.

		You may also pass the --registry flag to login to the integrated registry but with a
		custom DNS name, or to an external registry. Note that in absence of --auth-basic=USER:PASSWORD,
		the authentication token from the connected kubeconfig file will be recorded as the auth entry
		in the credentials file (defaults to Docker config.json) for the passed registry value.

		Experimental: This command is under active development and may change without notice.`)

	example = templates.Examples(`
		# Log in to the integrated registry
		oc registry login

		# Log in as the default service account in the current namespace
		oc registry login -z default

		# Log in to different registry using BASIC auth credentials
		oc registry login --registry quay.io/myregistry --auth-basic=USER:PASS
	`)
)

type Credentials struct {
	Auth     []byte `json:"auth"`
	Username string `json:"-"`
	Password string `json:"-"`
}

func newCredentials(username, password string) Credentials {
	return Credentials{
		Username: username,
		Password: password,
		Auth:     []byte(fmt.Sprintf("%s:%s", username, password)),
	}
}

func (c Credentials) Empty() bool {
	return len(c.Auth) == 0
}

type LoginOptions struct {
	ConfigFile  string
	Credentials Credentials
	HostPort    string
	SkipCheck   bool
	Insecure    bool

	AuthBasic      string
	ServiceAccount string

	genericclioptions.IOStreams
}

func NewRegistryLoginOptions(streams genericclioptions.IOStreams) *LoginOptions {
	return &LoginOptions{
		IOStreams: streams,
	}
}

// New logs you in to a container image registry locally.
func NewRegistryLoginCmd(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewRegistryLoginOptions(streams)

	cmd := &cobra.Command{
		Use:     "login ",
		Short:   "Log in to the integrated registry",
		Long:    desc,
		Example: example,
		Run: func(c *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}

	flag := cmd.Flags()
	flag.StringVar(&o.AuthBasic, "auth-basic", o.AuthBasic, "Provide credentials in the form 'user:password' to authenticate (advanced)")
	// TODO: fix priority and deprecation notice in 4.12
	flag.StringVarP(&o.ConfigFile, "registry-config", "a", o.ConfigFile, "The location of the file your credentials will be stored in. Alternatively REGISTRY_AUTH_FILE env variable can be also specified. Defaults to ~/.docker/config.json. Default can be changed via REGISTRY_AUTH_PREFERENCE env variable to docker (current default - deprecated) or podman (prioritizes podman credentials over docker).")
	flag.StringVar(&o.ConfigFile, "to", o.ConfigFile, "The location of the file your credentials will be stored in. Alternatively REGISTRY_AUTH_FILE env variable can be also specified. Default is Docker config.json (deprecated). Default can be changed via REGISTRY_AUTH_PREFERENCE env variable to docker or podman.")
	flag.StringVarP(&o.ServiceAccount, "service-account", "z", o.ServiceAccount, "Log in as the specified service account name in the specified namespace.")
	flag.StringVar(&o.HostPort, "registry", o.HostPort, "An alternate domain name and port to use for the registry, defaults to the cluster's configured external hostname.")
	flag.BoolVar(&o.SkipCheck, "skip-check", o.SkipCheck, "Skip checking the credentials against the registry.")
	flag.BoolVar(&o.Insecure, "insecure", o.Insecure, "Bypass HTTPS certificate verification when checking the registry login.")

	return cmd
}

func (o *LoginOptions) Complete(f kcmdutil.Factory, args []string) error {
	credentials := 0
	if len(o.ServiceAccount) > 0 {
		credentials++
	}
	if len(o.AuthBasic) > 0 {
		credentials++
	}
	if credentials > 1 {
		return fmt.Errorf("You may only specify a single authentication input as -z or --auth-basic")
	}

	cfg, err := f.ToRESTConfig()
	if err != nil {
		return err
	}

	switch {
	case len(o.ServiceAccount) > 0:
		ns, _, err := f.ToRawKubeConfigLoader().Namespace()
		if err != nil {
			return err
		}
		client, err := clientset.NewForConfig(cfg)
		if err != nil {
			return err
		}
		sa, err := client.CoreV1().ServiceAccounts(ns).Get(context.TODO(), o.ServiceAccount, metav1.GetOptions{})
		if err != nil {
			if kerrors.IsNotFound(err) {
				return fmt.Errorf("the service account %s does not exist in namespace %s", o.ServiceAccount, ns)
			}
			return err
		}
		var lastErr error
		for _, ref := range sa.Secrets {
			secret, err := client.CoreV1().Secrets(ns).Get(context.TODO(), ref.Name, metav1.GetOptions{})
			if err != nil {
				lastErr = err
				continue
			}
			if secret.Type != corev1.SecretTypeServiceAccountToken {
				continue
			}
			token := secret.Data[corev1.ServiceAccountTokenKey]
			if len(token) == 0 {
				continue
			}
			o.Credentials = newCredentials(fmt.Sprintf("system-serviceaccount-%s-%s", ns, o.ServiceAccount), string(token))
			break
		}
		if o.Credentials.Empty() {
			if lastErr != nil {
				if kerrors.IsForbidden(lastErr) {
					return fmt.Errorf("you do not have permission to view service account secrets in namespace %s", ns)
				}
				return err
			}
			return fmt.Errorf("the service account %s had no valid secrets associated with it", o.ServiceAccount)
		}
	case len(o.AuthBasic) > 0:
		parts := strings.SplitN(o.AuthBasic, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("--auth-basic must be in the form user:password")
		}
		o.Credentials = newCredentials(parts[0], parts[1])
	default:
		if len(cfg.BearerToken) == 0 {
			return fmt.Errorf("no token is currently in use for this session")
		}
		o.Credentials = newCredentials("user", cfg.BearerToken)
	}

	if len(o.HostPort) == 0 {
		client, err := imageclient.NewForConfig(cfg)
		if err != nil {
			return err
		}
		ns, _, err := f.ToRawKubeConfigLoader().Namespace()
		if err != nil {
			return err
		}

		registry, internal, err := findPublicHostname(client, ns, "openshift")
		if err != nil {
			return err
		}
		if len(registry) > 0 {
			if ref, err := reference.Parse(registry); err == nil {
				o.HostPort = ref.Registry
				if internal {
					fmt.Fprintf(o.ErrOut, "info: Using internal registry hostname %s\n", o.HostPort)
				} else {
					fmt.Fprintf(o.ErrOut, "info: Using registry public hostname %s\n", o.HostPort)
				}
			}
		}
	}

	if len(o.ConfigFile) == 0 {
		if authFile := os.Getenv("REGISTRY_AUTH_FILE"); authFile != "" {
			o.ConfigFile = authFile
		} else if image.GetRegistryAuthConfigPreference() == image.DockerPreference {
			home := homedir.HomeDir()
			o.ConfigFile = filepath.Join(home, ".docker", "config.json")
		}
	}

	return nil
}

func findPublicHostname(client *imageclient.Clientset, namespaces ...string) (name string, internal bool, err error) {
	for _, ns := range namespaces {
		imageStreams, err := client.ImageV1().ImageStreams(ns).List(context.TODO(), metav1.ListOptions{})
		if kerrors.IsUnauthorized(err) {
			return "", false, err
		}
		if err != nil || len(imageStreams.Items) == 0 {
			continue
		}
		is := imageStreams.Items[0]
		if len(is.Status.PublicDockerImageRepository) > 0 {
			return is.Status.PublicDockerImageRepository, false, nil
		}
		return is.Status.DockerImageRepository, true, nil
	}
	return "", false, nil
}

func (o *LoginOptions) Validate() error {
	if len(o.HostPort) == 0 {
		return fmt.Errorf("The public hostname of the integrated registry could not be determined. Please specify one with --registry.")
	}
	if o.Credentials.Empty() {
		return fmt.Errorf("Unable to determine registry credentials, please specify a service account or log into the cluster.")
	}
	return nil
}

func (o *LoginOptions) Run() error {
	if !o.SkipCheck {
		ctx := apirequest.NewContext()
		creds := registryclient.NewBasicCredentials()
		hostPortURL := &url.URL{Host: o.HostPort}
		creds.Add(hostPortURL, o.Credentials.Username, o.Credentials.Password)
		insecureRT, err := rest.TransportFor(&rest.Config{TLSClientConfig: rest.TLSClientConfig{Insecure: true}, UserAgent: rest.DefaultKubernetesUserAgent()})
		if err != nil {
			return err
		}
		c := registryclient.NewContext(http.DefaultTransport, insecureRT).WithCredentials(creds)
		if _, err := c.Repository(ctx, hostPortURL, "does_not_exist", o.Insecure); err != nil {
			return fmt.Errorf("unable to check your credentials - pass --skip-check to bypass this error: %v", err)
		}
	}

	ctx := &containertypes.SystemContext{AuthFilePath: o.ConfigFile}
	if err := dockerconfig.SetAuthentication(ctx, o.HostPort, o.Credentials.Username, o.Credentials.Password); err != nil {
		return err
	}

	fmt.Fprintf(o.Out, "Saved credentials for %s\n", o.HostPort)
	return nil
}
