package monitorregeneration

import (
	"context"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"

	configclient "github.com/openshift/client-go/config/clientset/versioned"
)

var (
	monitorCertificatesLong = templates.LongDesc(`
		Watch the platform certificates in the cluster.

		Experimental: This command is under active development and may change without notice.
	`)

	monitorCertificatesExample = templates.Examples(`
		# Watch platform certificates
		oc adm ocp-certificates monitor-certificates
	`)
)

type MonitorCertificatesOptions struct {
	RESTClientGetter genericclioptions.RESTClientGetter

	genericiooptions.IOStreams
}

func NewMonitorCertificatesOptions(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *MonitorCertificatesOptions {
	return &MonitorCertificatesOptions{
		RESTClientGetter: restClientGetter,

		IOStreams: streams,
	}
}

func NewCmdMonitorCertificates(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewMonitorCertificatesOptions(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "monitor-certificates",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Watch platform certificates"),
		Long:                  monitorCertificatesLong,
		Example:               monitorCertificatesExample,
		Run: func(cmd *cobra.Command, args []string) {
			r, err := o.ToRuntime(args)
			cmdutil.CheckErr(err)
			cmdutil.CheckErr(r.Run(context.Background()))
		},
	}

	o.AddFlags(cmd)

	return cmd
}

// AddFlags registers flags for a cli
func (o *MonitorCertificatesOptions) AddFlags(cmd *cobra.Command) {
}

func (o *MonitorCertificatesOptions) ToRuntime(args []string) (*MonitorCertificatesRuntime, error) {
	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}
	configClient, err := configclient.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	ret := &MonitorCertificatesRuntime{
		KubeClient:                  kubeClient,
		ConfigClient:                configClient,
		IOStreams:                   o.IOStreams,
		interestingConfigMaps:       newNamespacedCache(),
		interestingSecrets:          newNamespacedCache(),
		interestingClusterOperators: newUnnamespacedCache(),
	}

	return ret, nil
}
