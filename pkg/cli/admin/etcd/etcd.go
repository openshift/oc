package etcd

import (
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
)

// NewCmdEtcd implements the OpenShift cli etcd command
func NewCmdEtcd(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	// Parent command to which all subcommands are added.
	cmds := &cobra.Command{
		Use:   "etcd",
		Long:  "Manage etcd cluster",
		Short: "",
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	subCommands := []*cobra.Command{
		NewCommandListMembers(f, streams),
	}
	for _, cmd := range subCommands {
		cmds.AddCommand(cmd)
	}

	return cmds
}
