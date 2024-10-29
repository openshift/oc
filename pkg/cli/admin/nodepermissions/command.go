package nodepermissions

import (
	"context"
	"k8s.io/client-go/rest"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	example = templates.Examples(`
		# Wait for all nodes to complete a requested reboot from 'oc adm reboot-machine-config-pool mcp/worker mcp/master'
		oc adm wait-for-node-reboot nodes --all

		# Wait for masters to complete a requested reboot from 'oc adm reboot-machine-config-pool mcp/master'
		oc adm wait-for-node-reboot nodes -l node-role.kubernetes.io/master

		# Wait for masters to complete a specific reboot
		oc adm wait-for-node-reboot nodes -l node-role.kubernetes.io/master --reboot-number=4`)
)

type CheckNodePermissionsOptions struct {
	RESTClientGetter     genericclioptions.RESTClientGetter
	ResourceBuilderFlags *genericclioptions.ResourceBuilderFlags

	RebootNumber int

	genericiooptions.IOStreams
}

func NewCheckNodePermissions(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *CheckNodePermissionsOptions {
	return &CheckNodePermissionsOptions{
		RESTClientGetter: restClientGetter,
		ResourceBuilderFlags: genericclioptions.NewResourceBuilderFlags().
			WithLabelSelector("").
			WithFieldSelector("").
			WithAll(false).
			WithLatest(),

		IOStreams: streams,
	}
}

func NewCmdCheckNodePermissions(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewCheckNodePermissions(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "node-permissions",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Report best effort transitive closure of permissions available from a node."),
		Example:               example,
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
func (o *CheckNodePermissionsOptions) AddFlags(cmd *cobra.Command) {
	o.ResourceBuilderFlags.AddFlags(cmd.Flags())
}

func (o *CheckNodePermissionsOptions) ToRuntime(args []string) (*CheckNodePermissionsRuntime, error) {
	builder := o.ResourceBuilderFlags.ToBuilder(o.RESTClientGetter, args)
	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	ret := &CheckNodePermissionsRuntime{
		ResourceFinder:      builder,
		KubeClient:          kubeClient,
		AnonymousKubeConfig: rest.AnonymousClientConfig(clientConfig),

		IOStreams: o.IOStreams,
	}

	return ret, nil
}
