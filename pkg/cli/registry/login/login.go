package login

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/homedir"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	imageclient "github.com/openshift/client-go/image/clientset/versioned"
	"github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/library-go/pkg/image/registryclient"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var (
	desc = templates.LongDesc(`
		Login to the OpenShift integrated registry.

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
		desired hostname.

		Experimental: This command is under active development and may change without notice.`)

	example = templates.Examples(`
		# Log in to the integrated registry
		arvan paas registry login

		# Log in as the default service account in the current namespace
		arvan paas registry login -z default

		# Log in to a different registry using BASIC auth credentials
		arvan paas registry login --registry quay.io --auth-basic=USER:PASS
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
	ConfigFile      string
	Credentials     Credentials
	HostPort        string
	SkipCheck       bool
	Insecure        bool
	CreateDirectory bool

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
		Short:   "Login to the integrated registry",
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
	flag.StringVarP(&o.ConfigFile, "registry-config", "a", o.ConfigFile, "The location of the Docker config.json your credentials will be stored in.")
	flag.StringVar(&o.ConfigFile, "to", o.ConfigFile, "The location of the Docker config.json your credentials will be stored in.")
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
		home := homedir.HomeDir()
		o.ConfigFile = filepath.Join(home, ".docker", "config.json")
		o.CreateDirectory = true
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
	if len(o.ConfigFile) == 0 {
		return fmt.Errorf("Specify a file to write credentials to with --to")
	}
	return nil
}

func (o *LoginOptions) Run() error {
	if !o.SkipCheck {
		ctx := apirequest.NewContext()
		creds := registryclient.NewBasicCredentials()
		url := &url.URL{Host: o.HostPort}
		creds.Add(url, o.Credentials.Username, o.Credentials.Password)
		insecureRT, err := rest.TransportFor(&rest.Config{TLSClientConfig: rest.TLSClientConfig{Insecure: true}})
		if err != nil {
			return err
		}
		c := registryclient.NewContext(http.DefaultTransport, insecureRT).WithCredentials(creds)
		if _, err := c.Repository(ctx, url, "does_not_exist", o.Insecure); err != nil {
			return fmt.Errorf("unable to check your credentials - pass --skip-check to bypass this error: %v", err)
		}
	}

	filename := o.ConfigFile
	var contents []byte
	if filename != "-" {
		data, err := ioutil.ReadFile(filename)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		contents = data
	}
	if len(contents) == 0 {
		contents = []byte("{}")
	}

	cfg := make(map[string]interface{})
	if err := json.Unmarshal(contents, &cfg); err != nil {
		return fmt.Errorf("unable to parse existing credentials %s: %v", filename, err)
	}

	obj, ok := cfg["auths"]
	if !ok {
		obj = make(map[string]interface{})
		cfg["auths"] = obj
	}
	auths, ok := obj.(map[string]interface{})
	if !ok {
		return fmt.Errorf("the specified config file %s does not appear to be in the correct Docker config.json format: have 'auths' key of type %T", filename, obj)
	}
	auths[o.HostPort] = map[string]interface{}{
		"auth": o.Credentials.Auth,
	}
	contents, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if o.ConfigFile == "-" {
		fmt.Fprintln(o.Out, string(contents))
		return nil
	}

	if o.CreateDirectory {
		dir := filepath.Dir(filename)
		if err := os.MkdirAll(dir, 0700); err != nil {
			klog.V(2).Infof("Unable to create nested directories: %v", err)
		}
	}

	f, err := os.OpenFile(filename, os.O_TRUNC|os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, bytes.NewReader(contents)); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Fprintf(o.Out, "Saved credentials for %s\n", o.HostPort)
	return nil
}
