package ignition

import (
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

// NewCmdIgnition generates the command
func NewCmdIgnition(streams genericclioptions.IOStreams) *cobra.Command {
	// Parent command to which all subcommands are added.
	cmds := &cobra.Command{
		Use:   "ignition",
		Short: "Manage Ignition configuration",
		Run:   cmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	subCommands := []*cobra.Command{
		NewCommandConvert3(streams),
	}
	for _, cmd := range subCommands {
		cmds.AddCommand(cmd)
	}

	return cmds
}
