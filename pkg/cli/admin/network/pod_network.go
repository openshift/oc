package network

import (
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	podNetworkLong = templates.LongDesc(`
		Manage pod network in the cluster

		This command provides common pod network operations for administrators.`)
)

func NewCmdPodNetwork(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	// Parent command to which all subcommands are added.
	cmds := &cobra.Command{
		Use:   "pod-network",
		Short: "Manage pod network",
		Long:  podNetworkLong,
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	cmds.AddCommand(NewCmdJoinProjectsNetwork(f, streams))
	cmds.AddCommand(NewCmdMakeGlobalProjectsNetwork(f, streams))
	cmds.AddCommand(NewCmdIsolateProjectsNetwork(f, streams))
	return cmds
}
