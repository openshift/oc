package kubeletbootstrapkubeconfig

import (
	"context"
	"fmt"

	configv1client "github.com/openshift/client-go/config/clientset/versioned"

	"github.com/openshift/oc/pkg/cli/config/refreshcabundle"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

type NewKubeletBootstrapKubeconfigOptions struct {
	RESTClientGetter genericclioptions.RESTClientGetter

	genericiooptions.IOStreams
}

var (
	newKubeletBootstrapKubeconfigExample = templates.Examples(`
		# Generate a new kubelet bootstrap kubeconfig
		oc config new-kubelet-bootstrap-kubeconfig`)
)

func NewNewKubeletBootstrapKubeconfigOptions(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *NewKubeletBootstrapKubeconfigOptions {
	return &NewKubeletBootstrapKubeconfigOptions{
		RESTClientGetter: restClientGetter,
		IOStreams:        streams,
	}
}

func NewCmdNewKubeletBootstrapKubeconfig(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewNewKubeletBootstrapKubeconfigOptions(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "new-kubelet-bootstrap-kubeconfig",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Generate, make the server trust, and display a new kubelet /etc/kubernetes/kubeconfig"),
		Long:                  i18n.T("Generate, make the server trust, and display a new kubelet /etc/kubernetes/kubeconfig."),
		Example:               newKubeletBootstrapKubeconfigExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete())
			cmdutil.CheckErr(o.Validate(args))
			r, err := o.ToRuntime()
			cmdutil.CheckErr(err)
			cmdutil.CheckErr(r.Run(context.TODO()))

		},
	}

	o.AddFlags(cmd)

	return cmd
}

func (o *NewKubeletBootstrapKubeconfigOptions) AddFlags(cmd *cobra.Command) {
}

func (o *NewKubeletBootstrapKubeconfigOptions) Complete() error {

	return nil
}

func (o NewKubeletBootstrapKubeconfigOptions) Validate(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("no arguments allowed")
	}

	return nil
}

func (o *NewKubeletBootstrapKubeconfigOptions) ToRuntime() (*NewKubeletBootstrapKubeconfigRuntime, error) {
	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}
	configClient, err := configv1client.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	ret := &NewKubeletBootstrapKubeconfigRuntime{
		KubeClient:   kubeClient,
		ConfigClient: configClient,
		IOStreams:    o.IOStreams,
	}

	return ret, nil
}

type NewKubeletBootstrapKubeconfigRuntime struct {
	KubeClient   kubernetes.Interface
	ConfigClient configv1client.Interface

	genericiooptions.IOStreams
}

func (r *NewKubeletBootstrapKubeconfigRuntime) Run(ctx context.Context) error {
	infrastructure, err := r.ConfigClient.ConfigV1().Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("unable to read infrastructure: %w", err)
	}

	bootstrapTokenSecret, err := r.KubeClient.CoreV1().Secrets("openshift-machine-config-operator").Get(ctx, "node-bootstrapper-token", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("unable to read bootstrap token: %w", err)
	}
	token := bootstrapTokenSecret.Data["token"]
	if len(token) == 0 {
		return fmt.Errorf("token is missing from the secret: %w", err)
	}

	serverCABundle, err := refreshcabundle.GetCABundleToTrustKubeAPIServer(ctx, r.KubeClient)
	if err != nil {
		return fmt.Errorf("unable to get the CA bundle from the cluster: %w", err)
	}

	// keep in sync with https://github.com/openshift/machine-config-operator/blob/3d84f653e08d760d446442ddc80c3da21d8d7e59/pkg/server/cluster_server.go#L167
	newConfig := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"cluster": {
				Server:                   infrastructure.Status.APIServerInternalURL,
				CertificateAuthorityData: []byte(serverCABundle),
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"bootstrap": {
				Token: string(token),
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"bootstrap": {
				Cluster:  "cluster",
				AuthInfo: "bootstrap",
			},
		},
		CurrentContext: "bootstrap",
	}
	newKubeletBootstrapConfig, err := clientcmd.Write(newConfig)
	if err != nil {
		return fmt.Errorf("unable to serialize new kubeconfig: %w", err)
	}

	fmt.Fprintln(r.Out, string(newKubeletBootstrapConfig))

	return nil
}
