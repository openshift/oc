package restartkubelet

import (
	"fmt"
	"strings"

	"github.com/openshift/oc/pkg/cli/clusteroperator/pernodepod"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	regenerateSignersLong = templates.LongDesc(`
		Regenerate certificates provided by an OCP v4 cluster.
		
		This command does not wait for changes to be acknowledged by the cluster.
		Some may take a very long time to roll out into a cluster, with different operators and operands involved for each.

		Experimental: This command is under active development and may change without notice.
	`)

	regenerateSignersExample = templates.Examples(`
		# Restart all the nodes,  10% at a time
		oc adm clusteroperators restart-kubelet nodes --all

		# Restart all the nodes,  20 nodes at a time
		oc adm clusteroperators restart-kubelet nodes --all --parallelism=20

		# Restart all the nodes,  15% at a time
		oc adm clusteroperators restart-kubelet nodes --all --parallelism=15%

		# Restart all the masters at the same time
		oc adm clusteroperators restart-kubelet nodes -l node-role.kubernetes.io/master --parallelism=100%`)
)

type RestartKubeletOptions struct {
	PerNodePodOptions *pernodepod.PerNodePodOptions

	CommandWhileKubeletIsOff string
	Directive                string

	genericclioptions.IOStreams
}

func NewRestartKubelet(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *RestartKubeletOptions {
	return &RestartKubeletOptions{
		PerNodePodOptions: pernodepod.NewPerNodePodOptions(
			"openshift-restart-kubelet-",
			"restarted kubelet",
			restClientGetter,
			streams,
		),

		IOStreams: streams,
	}
}

func NewCmdRestartKubelet(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewRestartKubelet(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "restart-kubelet",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Restarts kubelet on the specified nodes"),
		Long:                  regenerateSignersLong,
		Example:               regenerateSignersExample,
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
func (o *RestartKubeletOptions) AddFlags(cmd *cobra.Command) {
	o.PerNodePodOptions.AddFlags(cmd)

	cmd.Flags().StringVar(&o.CommandWhileKubeletIsOff, "command", o.CommandWhileKubeletIsOff, "command to run after the kubelet stops, before the kubelet starts.")
	cmd.Flags().StringVar(&o.Directive, "directive", o.Directive, "run a well-known command while restarting kubelets: RemoveKubeletKubeconfig")
}

func (o *RestartKubeletOptions) ToRuntime(args []string) (*RestartKubeletRuntime, error) {
	if len(o.CommandWhileKubeletIsOff) > 0 && len(o.Directive) > 0 {
		return nil, fmt.Errorf("only one of --command and --directive can be set")
	}
	commandWhileKubeletIsOff := o.CommandWhileKubeletIsOff
	switch o.Directive {
	case "RemoveKubeletKubeconfig":
		commandWhileKubeletIsOff = "rm -f /host-root/var/lib/kubelet/kubeconfig"
	default:
		return nil, fmt.Errorf("unknown directive %q, known directives: %v", o.Directive, strings.Join([]string{"RemoveKubeletKubeconfig"}, ", "))
	}

	perNodePodRuntime, err := o.PerNodePodOptions.ToRuntime(args)
	if err != nil {
		return nil, err
	}
	return &RestartKubeletRuntime{
		PerNodePodRuntime:        perNodePodRuntime,
		CommandWhileKubeletIsOff: commandWhileKubeletIsOff,
	}, nil
}
