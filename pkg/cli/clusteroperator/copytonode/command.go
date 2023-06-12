package copytonode

import (
	"github.com/openshift/oc/pkg/cli/clusteroperator/pernodepod"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	copyToNodeLong = templates.LongDesc(`
		Copies file from the host to the specified nodes.

		Experimental: This command is under active development and may change without notice.
	`)

	copyToNodeExample = templates.Examples(`
		# copy a new bootstrap kubeconfig file to every node
		oc adm clusteroperators copy-to-node --copy=new-bootstrap-kubeconfig=/etc/kubernetes/kubeconfig

		# copy a new bootstrap kubeconfig file to masters
		oc adm clusteroperators copy-to-node --copy=new-bootstrap-kubeconfig=/etc/kubernetes/kubeconfig -l node-role.kubernetes.io/master`)
)

type CopyToNodeOptions struct {
	PerNodePodOptions *pernodepod.PerNodePodOptions

	// FileSources to derive the secret from (optional)
	FileSources []string

	genericclioptions.IOStreams
}

func NewRestartKubelet(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *CopyToNodeOptions {
	return &CopyToNodeOptions{
		PerNodePodOptions: pernodepod.NewPerNodePodOptions(
			"openshift-copy-to-node-",
			restClientGetter,
			streams,
		),

		IOStreams: streams,
	}
}

func NewCmdCopyToNode(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewRestartKubelet(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "copy-to-node",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Copies specified files to the node."),
		Long:                  copyToNodeLong,
		Example:               copyToNodeExample,
		Run: func(cmd *cobra.Command, args []string) {
			ctx, cancel := pernodepod.SignalContext()
			defer cancel()

			r, err := o.ToRuntime(args)
			cmdutil.CheckErr(err)
			cmdutil.CheckErr(r.Run(ctx))
		},
	}

	o.AddFlags(cmd)

	return cmd
}

// AddFlags registers flags for a cli
func (o *CopyToNodeOptions) AddFlags(cmd *cobra.Command) {
	o.PerNodePodOptions.AddFlags(cmd)

	cmd.Flags().StringSliceVar(&o.FileSources, "copy", o.FileSources, "<source-path>=<node-destination>.  Specifying a directory will iterate each named file in the directory, non-recursive (PR welcome) that is a valid secret key.")

}

func (o *CopyToNodeOptions) ToRuntime(args []string) (*CopyToNodeRuntime, error) {
	perNodePodRuntime, err := o.PerNodePodOptions.ToRuntime(args)
	if err != nil {
		return nil, err
	}
	return &CopyToNodeRuntime{
		PerNodePodRuntime: perNodePodRuntime,
		FileSources:       o.FileSources,
	}, nil
}
