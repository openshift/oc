package waitfornoderestart

import (
	"context"

	"k8s.io/client-go/dynamic"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	removeOldTrustLong = templates.LongDesc(`
		Prune CA certificate bundles supplied by the platform and stored in ConfigMaps
		throughout the cluster.

		This command does not wait for changes to be acknowledged by the cluster.
		Some may take a very long time to roll out into a cluster, with different operators and operands involved for each.

		Experimental: This command is under active development and may change without notice.
	`)

	removeOldTrustExample = templates.Examples(`
		# Wait for all nodes to complete a requested reboot
		oc adm ocp-certificates wait-for-node-restart nodes --all

		# Wait for masters to complete a reboot
		oc adm wait-for-node-restart nodes -l node-role.kubernetes.io/master

		# Wait for masters to complete a specific reboot
		oc adm wait-for-node-restart nodes -l node-role.kubernetes.io/master --reboot-number=4
	`)
)

type WaitForNodeRestartOptions struct {
	RESTClientGetter     genericclioptions.RESTClientGetter
	ResourceBuilderFlags *genericclioptions.ResourceBuilderFlags

	RebootNumber int

	genericclioptions.IOStreams
}

func NewWaitForNodeRestart(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *WaitForNodeRestartOptions {
	return &WaitForNodeRestartOptions{
		RESTClientGetter: restClientGetter,
		ResourceBuilderFlags: genericclioptions.NewResourceBuilderFlags().
			WithLabelSelector("").
			WithFieldSelector("").
			WithAll(false).
			WithLatest(),

		IOStreams: streams,
	}
}

func NewCmdWaitForNodeRestart(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewWaitForNodeRestart(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "wait-for-node-reboot",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Remove old CAs from ConfigMaps representing platform trust bundles in an OpenShift cluster"),
		Long:                  removeOldTrustLong,
		Example:               removeOldTrustExample,
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
func (o *WaitForNodeRestartOptions) AddFlags(cmd *cobra.Command) {
	o.ResourceBuilderFlags.AddFlags(cmd.Flags())

	cmd.Flags().IntVar(&o.RebootNumber, "reboot-number", o.RebootNumber, "If unset, the current reboot numbers are used. If specified, any node at or beyond that reboot number is considered complete.")
}

func (o *WaitForNodeRestartOptions) ToRuntime(args []string) (*WaitForNodeRestartRuntime, error) {

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

	ret := &WaitForNodeRestartRuntime{
		ResourceFinder: builder,
		KubeClient:     kubeClient,
		DynamicClient:  dynamicClient,

		RebootNumber: o.RebootNumber,

		IOStreams: o.IOStreams,
	}

	return ret, nil
}
