package login

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/browser"

	projectv1typedclient "github.com/openshift/client-go/project/clientset/versioned/typed/project/v1"
	"github.com/openshift/library-go/pkg/oauth/tokenrequest"
	"github.com/openshift/library-go/pkg/oauth/tokenrequest/challengehandlers"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/apis/clientauthentication"
	restclient "k8s.io/client-go/rest"
	kclientcmd "k8s.io/client-go/tools/clientcmd"
	kclientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"

	occhallengers "github.com/openshift/oc/pkg/helpers/authchallengers"
	ocerrors "github.com/openshift/oc/pkg/helpers/errors"
	cliconfig "github.com/openshift/oc/pkg/helpers/kubeconfig"
	"github.com/openshift/oc/pkg/helpers/motd"
	"github.com/openshift/oc/pkg/helpers/project"
	loginutil "github.com/openshift/oc/pkg/helpers/project"
	"github.com/openshift/oc/pkg/helpers/term"
	"github.com/openshift/oc/pkg/version"
)

const defaultClusterURL = "https://localhost:8443"

const projectsItemsSuppressThreshold = 50

type ExecPluginType string

const (
	OCOIDC ExecPluginType = "oc-oidc"
)

// LoginOptions is a helper for the login and setup process, gathers all information required for a
// successful login and eventual update of config files.
// Depending on the Reader present it can be interactive, asking for terminal input in
// case of any missing information.
// Notice that some methods mutate this object so it should not be reused. The Config
// provided as a pointer will also mutate (handle new auth tokens, etc).
type LoginOptions struct {
	Server      string
	CAFile      string
	InsecureTLS bool

	// flags and printing helpers
	Username     string
	Password     string
	Project      string
	WebLogin     bool
	CallbackPort int32

	// infra
	StartingKubeConfig *kclientcmdapi.Config
	DefaultNamespace   string
	Config             *restclient.Config

	// cert data to be used when authenticating
	CertFile string
	KeyFile  string

	OIDCExecPluginType string
	OIDCClientID       string
	OIDCClientSecret   string
	OIDCExtraScopes    []string
	OIDCIssuerURL      string
	OIDCCAFile         string

	Token string

	PathOptions *kclientcmd.PathOptions

	CommandName    string
	RequestTimeout time.Duration

	genericiooptions.IOStreams
}

type passwordPrompter func(r io.Reader, w io.Writer, format string, a ...interface{}) string

func (p passwordPrompter) PromptForPassword(r io.Reader, w io.Writer, format string, a ...interface{}) string {
	return p(r, w, format, a...)
}

func NewLoginOptions(streams genericiooptions.IOStreams) *LoginOptions {
	return &LoginOptions{
		IOStreams:   streams,
		CommandName: "oc",
	}
}

// Gather all required information in a comprehensive order.
func (o *LoginOptions) GatherInfo() error {
	defer o.prepareAndDisplayMOTD()

	if err := o.gatherAuthInfo(); err != nil {
		return err
	}
	if err := o.gatherProjectInfo(); err != nil {
		return err
	}

	return nil
}

// getClientConfig returns back the current clientConfig as we know it.  If there is no clientConfig, it builds one with enough information
// to talk to a server.  This may involve user prompts.  This method is not threadsafe.
func (o *LoginOptions) getClientConfig() (*restclient.Config, error) {
	if o.Config != nil {
		return o.Config, nil
	}

	if len(o.Server) == 0 {
		// we need to have a server to talk to
		if printers.IsTerminal(o.In) {
			for !o.serverProvided() {
				defaultServer := defaultClusterURL
				promptMsg := fmt.Sprintf("Server [%s]: ", defaultServer)
				o.Server = term.PromptForStringWithDefault(o.In, o.Out, defaultServer, promptMsg)
			}
		}
	}

	clientConfig := &restclient.Config{}

	// ensure clientConfig has timeout option
	if o.RequestTimeout > 0 {
		clientConfig.Timeout = o.RequestTimeout
	}

	// normalize the provided server to a format expected by config
	serverNormalized, err := cliconfig.NormalizeServerURL(o.Server)
	if err != nil {
		return nil, err
	}
	o.Server = serverNormalized
	clientConfig.Host = o.Server
	clientConfig.Insecure = o.InsecureTLS

	if !o.InsecureTLS {
		// use specified CA or find existing CA
		if len(o.CAFile) > 0 {
			clientConfig.CAFile = o.CAFile
			clientConfig.CAData = nil
		} else if caFile, caData, ok := findExistingClientCA(clientConfig.Host, *o.StartingKubeConfig); ok {
			clientConfig.CAFile = caFile
			clientConfig.CAData = caData
		}
	}

	// try to TCP connect to the server to make sure it's reachable, and discover
	// about the need of certificates or insecure TLS
	if err := dialToServer(*clientConfig); err != nil {
		// In go 1.20 and upwards versions, x509 errors in the switch statement
		// are wrapped in tls.CertificateVerificationError.
		var cerr *tls.CertificateVerificationError
		if errors.As(err, &cerr) {
			err = cerr.Unwrap()
		}

		switch err.(type) {
		// certificate authority unknown, check or prompt if we want an insecure
		// connection or if we already have a cluster stanza that tells us to
		// connect to this particular server insecurely
		case x509.UnknownAuthorityError, x509.HostnameError, x509.CertificateInvalidError:
			if o.InsecureTLS ||
				hasExistingInsecureCluster(*clientConfig, *o.StartingKubeConfig) ||
				promptForInsecureTLS(o.In, o.Out, err) {
				clientConfig.Insecure = true
				clientConfig.CAFile = ""
				clientConfig.CAData = nil
				// dialToServer was called above but in case of user choosing insecure,
				// need to call again for invalidServerURL check
				if err := dialToServer(*clientConfig); err != nil {
					return nil, err
				}
			} else {
				return nil, getPrettyErrorForServer(err, o.Server)
			}
		// TLS record header errors, like oversized record which usually means
		// the server only supports "http"
		case tls.RecordHeaderError:
			return nil, getPrettyErrorForServer(err, o.Server)
		default:
			if _, ok := err.(*net.OpError); ok {
				return nil, fmt.Errorf("%v - verify you have provided the correct host and port and that the server is currently running.", err)
			}
			return nil, err
		}

	}

	o.Config = clientConfig

	return o.Config, nil
}

// Negotiate a bearer token with the auth server, or try to reuse one based on the
// information already present. In case of any missing information, ask for user input
// (usually username and password, interactive depending on the Reader).
func (o *LoginOptions) gatherAuthInfo() error {
	directClientConfig, err := o.getClientConfig()
	if err != nil {
		return err
	}

	if directClientConfig.Insecure || o.InsecureTLS {
		fmt.Fprintf(o.Out, "WARNING: Using insecure TLS client config. Setting this option is not supported!\n\n")
	}

	// make a copy and use it to avoid mutating the original
	t := *directClientConfig
	clientConfig := &t

	// if a token were explicitly provided, try to use it
	if o.tokenProvided() {
		clientConfig.BearerToken = o.Token
		me, err := project.WhoAmI(clientConfig)
		if err != nil {
			if kerrors.IsUnauthorized(err) {
				return fmt.Errorf("The token provided is invalid or expired.\n\n")
			}
			return err
		}
		o.Username = me.Name
		o.Config = clientConfig

		fmt.Fprintf(o.Out, "Logged into %q as %q using the token provided.\n\n", o.Config.Host, o.Username)
		return nil
	}

	// if a username was provided try to make use of it, but if a password were provided we force a token
	// request which will return a proper response code for that given password
	if o.usernameProvided() && !o.passwordProvided() {
		// search all valid contexts with matching server stanzas to see if we have a matching user stanza
		kubeconfig := *o.StartingKubeConfig
		matchingClusters := getMatchingClusters(*clientConfig, kubeconfig)

		for key, context := range o.StartingKubeConfig.Contexts {
			if matchingClusters.Has(context.Cluster) {
				clientcmdConfig := kclientcmd.NewDefaultClientConfig(kubeconfig, &kclientcmd.ConfigOverrides{CurrentContext: key})
				if kubeconfigClientConfig, err := clientcmdConfig.ClientConfig(); err == nil {
					if me, err := project.WhoAmI(kubeconfigClientConfig); err == nil && (o.Username == me.Name) {
						clientConfig.BearerToken = kubeconfigClientConfig.BearerToken
						clientConfig.CertFile = kubeconfigClientConfig.CertFile
						clientConfig.CertData = kubeconfigClientConfig.CertData
						clientConfig.KeyFile = kubeconfigClientConfig.KeyFile
						clientConfig.KeyData = kubeconfigClientConfig.KeyData

						o.Config = clientConfig

						fmt.Fprintf(o.Out, "Logged into %q as %q using existing credentials.\n\n", o.Config.Host, o.Username)

						return nil
					}
				}
			}
		}
	}

	if o.OIDCExecPluginType == string(OCOIDC) {
		execProvider, err := o.prepareBuiltinExecPlugin()
		if err != nil {
			return err
		}

		clientConfig.ExecProvider = execProvider
		me, err := project.WhoAmI(clientConfig)
		if err != nil {
			return err
		}

		o.Username = me.Name
		o.Config = clientConfig
		fmt.Fprintf(o.Out, "Logged into %q as %q from an external oidc issuer.\n\n", o.Config.Host, o.Username)
		return nil
	}

	// if kubeconfig doesn't already have a matching user stanza...
	clientConfig.BearerToken = ""
	clientConfig.CertData = []byte{}
	clientConfig.KeyData = []byte{}
	clientConfig.CertFile = o.CertFile
	clientConfig.KeyFile = o.KeyFile

	var token string
	if o.WebLogin {
		loginURLHandler := func(u *url.URL) error {
			loginURL := u.String()
			fmt.Fprintf(o.Out, "Opening login URL in the default browser: %s\n", loginURL)
			return browser.OpenURL(loginURL)
		}
		token, err = tokenrequest.RequestTokenWithLocalCallback(o.Config, loginURLHandler, int(o.CallbackPort))
	} else {
		token, err = tokenrequest.RequestTokenWithChallengeHandlers(o.Config, o.getAuthChallengeHandler())
	}
	if err != nil {
		return err
	}

	clientConfig.BearerToken = token

	me, err := project.WhoAmI(clientConfig)
	if err != nil {
		return err
	}
	o.Username = me.Name
	o.Config = clientConfig
	fmt.Fprint(o.Out, "Login successful.\n\n")

	return nil
}

// prepareBuiltinExecPlugin sets up the ExecConfig correctly
// with the given values
func (o *LoginOptions) prepareBuiltinExecPlugin() (*kclientcmdapi.ExecConfig, error) {
	execProvider := &kclientcmdapi.ExecConfig{
		APIVersion: clientauthentication.GroupName + "/v1",
		Command:    "oc",
		Args: []string{
			"get-token",
			fmt.Sprintf("--issuer-url=%s", o.OIDCIssuerURL),
			fmt.Sprintf("--client-id=%s", o.OIDCClientID),
			fmt.Sprintf("--callback-address=127.0.0.1:%d", o.CallbackPort),
		},
		InstallHint:     "Please be sure that oc is defined in $PATH to be executed as credentials exec plugin",
		InteractiveMode: kclientcmdapi.IfAvailableExecInteractiveMode,
	}

	if len(o.OIDCExtraScopes) > 0 {
		execProvider.Args = append(execProvider.Args, fmt.Sprintf("--extra-scopes=%s", strings.Join(o.OIDCExtraScopes, ",")))
	}

	if o.OIDCClientSecret != "" {
		execProvider.Args = append(execProvider.Args, fmt.Sprintf("--client-secret=%s", o.OIDCClientSecret))
	}

	if o.InsecureTLS {
		execProvider.Args = append(execProvider.Args, "--insecure-skip-tls-verify")
	}

	if len(o.OIDCCAFile) > 0 {
		execProvider.Args = append(execProvider.Args, fmt.Sprintf("--certificate-authority=%s", o.OIDCCAFile))
	}

	return execProvider, nil
}

func (o *LoginOptions) getAuthChallengeHandler() challengehandlers.ChallengeHandler {
	var challengeHandlers []challengehandlers.ChallengeHandler
	var webConsoleURL string

	serverVersionDetector := version.NewServerVersionRetriever(o.Config)
	serverVersion, err := serverVersionDetector.RetrieveServerVersion()
	// this feature was introduced in Openshift 4.11 which should correspond to 1.24
	if err == nil && serverVersion.MajorNumber >= 1 && serverVersion.MinorNumber >= 24 {
		webConsoleURL = o.Config.Host + "/console"
	}

	if occhallengers.GSSAPIEnabled() {
		klog.V(6).Info("GSSAPI Enabled")
		challengeHandlers = append(challengeHandlers,
			occhallengers.NewNegotiateChallengeHandler(occhallengers.NewGSSAPINegotiator(o.Username)),
		)
	}

	if occhallengers.SSPIEnabled() {
		klog.V(6).Info("SSPI Enabled")
		challengeHandlers = append(challengeHandlers,
			occhallengers.NewNegotiateChallengeHandler(occhallengers.NewSSPINegotiator(o.Username, o.Password, o.Config.Host, webConsoleURL, o.In)))
	}

	challengeHandlers = append(challengeHandlers,
		challengehandlers.NewMultiHandler(
			challengehandlers.NewBasicChallengeHandler(
				o.Config.Host, webConsoleURL,
				o.In, o.Out,
				passwordPrompter(term.PromptForPasswordString),
				o.Username, o.Password),
		))

	var handler challengehandlers.ChallengeHandler
	if len(challengeHandlers) == 1 {
		handler = challengeHandlers[0]
	} else {
		handler = challengehandlers.NewMultiHandler(challengeHandlers...)
	}

	return handler
}

// Discover the projects available for the established session and take one to use. It
// fails in case of no existing projects, and print out useful information in case of
// multiple projects.
// Requires o.Username to be set.
func (o *LoginOptions) gatherProjectInfo() error {
	projectClient, err := projectv1typedclient.NewForConfig(o.Config)
	if err != nil {
		return err
	}

	projectsList, err := projectClient.Projects().List(context.TODO(), metav1.ListOptions{})
	// if we're running on kube (or likely kube), just set it to "default"
	if err != nil {
		if !kerrors.IsNotFound(err) && !kerrors.IsForbidden(err) {
			fmt.Fprintf(o.Out, "WARNING: Failed to list projects: %v\n", err)
		}
		fmt.Fprintf(o.Out, "Using \"default\" namespace.  You can switch namespaces with:\n\n %s project <projectname>\n", o.CommandName)
		o.Project = "default"
		return nil
	}

	projectsItems := projectsList.Items
	projects := sets.String{}
	for _, project := range projectsItems {
		projects.Insert(project.Name)
	}

	if len(o.DefaultNamespace) > 0 && !projects.Has(o.DefaultNamespace) {
		// Attempt a direct get of our current project in case it hasn't appeared in the list yet
		if currentProject, err := projectClient.Projects().Get(context.TODO(), o.DefaultNamespace, metav1.GetOptions{}); err == nil {
			// If we get it successfully, add it to the list
			projectsItems = append(projectsItems, *currentProject)
			projects.Insert(currentProject.Name)
		}
	}

	switch len(projectsItems) {
	case 0:
		canRequest, err := loginutil.CanRequestProjects(o.Config, o.DefaultNamespace)
		if err != nil {
			return err
		}
		msg := ocerrors.NoProjectsExistMessage(canRequest)
		fmt.Fprintf(o.Out, msg)
		o.Project = ""

	case 1:
		o.Project = projectsItems[0].Name
		fmt.Fprintf(o.Out, "You have one project on this server: %q\n\n", o.Project)
		fmt.Fprintf(o.Out, "Using project %q.\n", o.Project)

	default:
		namespace := o.DefaultNamespace
		if !projects.Has(namespace) {
			if namespace != metav1.NamespaceDefault && projects.Has(metav1.NamespaceDefault) {
				namespace = metav1.NamespaceDefault
			} else {
				namespace = projects.List()[0]
			}
		}

		current, err := projectClient.Projects().Get(context.TODO(), namespace, metav1.GetOptions{})
		if err != nil && !kerrors.IsNotFound(err) && !kerrors.IsForbidden(err) {
			return err
		}
		o.Project = current.Name

		// Suppress project listing if the number of projects available to the user is greater than the threshold. Prevents unnecessarily noisy logins on clusters with large numbers of projects
		if len(projectsItems) > projectsItemsSuppressThreshold {
			fmt.Fprintf(o.Out, "You have access to %d projects, the list has been suppressed. You can list all projects with '%s projects'\n\n", len(projectsItems), o.CommandName)
		} else {
			fmt.Fprintf(o.Out, "You have access to the following projects and can switch between them with '%s project <projectname>':\n\n", o.CommandName)
			for _, p := range projects.List() {
				if o.Project == p {
					fmt.Fprintf(o.Out, "  * %s\n", p)
				} else {
					fmt.Fprintf(o.Out, "    %s\n", p)
				}
			}
			fmt.Fprintln(o.Out)
		}
		fmt.Fprintf(o.Out, "Using project %q.\n", o.Project)
	}

	return nil
}

// Prepare the kubernetes clientset in order to get the MOTD and
// potentially display it.
func (o *LoginOptions) prepareAndDisplayMOTD() error {
	// At this point, the client was unable to create a config, which is a
	// pre-requisite. So Lets skip this, since we can't even reach the
	// apiserver.
	if o.Config == nil {
		return nil
	}
	clientset, err := kubernetes.NewForConfig(o.Config)
	if err != nil {
		return err
	}
	return motd.DisplayMOTD(clientset.CoreV1(), o.Out)
}

// Save all the information present in this helper to a config file. An explicit config
// file path can be provided, if not use the established conventions about config
// loading rules. Will create a new config file if one can't be found at all. Will only
// succeed if all required info is present.
func (o *LoginOptions) SaveConfig() (bool, error) {
	if len(o.Username) == 0 {
		return false, fmt.Errorf("Insufficient data to merge configuration.")
	}

	globalExistedBefore := true
	if _, err := os.Stat(o.PathOptions.GlobalFile); os.IsNotExist(err) {
		globalExistedBefore = false
	}

	newConfig, err := cliconfig.CreateConfig(o.Project, o.Username, o.Config)
	if err != nil {
		return false, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}
	baseDir, err := kclientcmdapi.MakeAbs(filepath.Dir(o.PathOptions.GetDefaultFilename()), cwd)
	if err != nil {
		return false, err
	}
	if err := cliconfig.RelativizeClientConfigPaths(newConfig, baseDir); err != nil {
		return false, err
	}

	configToWrite, err := cliconfig.MergeConfig(*o.StartingKubeConfig, *newConfig)
	if err != nil {
		return false, err
	}

	if err := kclientcmd.ModifyConfig(o.PathOptions, *configToWrite, true); err != nil {
		if !os.IsPermission(err) {
			return false, err
		}

		out := &bytes.Buffer{}
		fmt.Fprintf(out, ocerrors.ErrKubeConfigNotWriteable(o.PathOptions.GetDefaultFilename(), o.PathOptions.IsExplicitFile(), err).Error())
		return false, fmt.Errorf("%v", out)
	}

	created := false
	if _, err := os.Stat(o.PathOptions.GlobalFile); err == nil {
		created = created || !globalExistedBefore
	}

	return created, nil
}

func (o *LoginOptions) usernameProvided() bool {
	return len(o.Username) > 0
}

func (o *LoginOptions) passwordProvided() bool {
	return len(o.Password) > 0
}

func (o *LoginOptions) serverProvided() bool {
	return (len(o.Server) > 0)
}

func (o *LoginOptions) tokenProvided() bool {
	return len(o.Token) > 0
}
