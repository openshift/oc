package waitfornodereboot

import (
	"context"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/dynamic"
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

type WaitForNodeRebootOptions struct {
	RESTClientGetter     genericclioptions.RESTClientGetter
	ResourceBuilderFlags *genericclioptions.ResourceBuilderFlags

	RebootNumber int

	genericiooptions.IOStreams
}

func NewWaitForNodeReboot(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *WaitForNodeRebootOptions {
	return &WaitForNodeRebootOptions{
		RESTClientGetter: restClientGetter,
		ResourceBuilderFlags: genericclioptions.NewResourceBuilderFlags().
			WithLabelSelector("").
			WithFieldSelector("").
			WithAll(false).
			WithLatest(),

		IOStreams: streams,
	}
}

func NewCmdWaitForNodeReboot(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewWaitForNodeReboot(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "wait-for-node-reboot",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Wait for nodes to reboot after running `oc adm reboot-machine-config-pool`"),
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
func (o *WaitForNodeRebootOptions) AddFlags(cmd *cobra.Command) {
	o.ResourceBuilderFlags.AddFlags(cmd.Flags())

	cmd.Flags().IntVar(&o.RebootNumber, "reboot-number", o.RebootNumber, "If unset, the current reboot numbers are used. If specified, any node at or beyond that reboot number is considered complete.")
}

func (o *WaitForNodeRebootOptions) ToRuntime(args []string) (*WaitForNodeRebootRuntime, error) {

	builder := o.ResourceBuilderFlags.ToBuilder(o.RESTClientGetter, args)
	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	ret := &WaitForNodeRebootRuntime{
		ResourceFinder: builder,
		KubeClient:     kubeClient,
		DynamicClient:  dynamicClient,

		RebootNumber: o.RebootNumber,

		IOStreams: o.IOStreams,
	}

	return ret, nil
}
