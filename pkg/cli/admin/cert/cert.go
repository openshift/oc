package cert

import (
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

const CertRecommendedName = "ca"

// NewCmdCert implements the OpenShift cli ca command
func NewCmdCert(streams genericclioptions.IOStreams) *cobra.Command {
	// Parent command to which all subcommands are added.
	cmds := &cobra.Command{
		Use:        "ca",
		Long:       "Manage certificates and keys",
		Short:      "",
		Run:        cmdutil.DefaultSubCommandRun(streams.ErrOut),
		Deprecated: "and will be removed in the future version",
		Hidden:     true,
	}

	subCommands := []*cobra.Command{
		NewCommandEncrypt(streams),
		NewCommandDecrypt(streams),
	}

	for _, cmd := range subCommands {
		// Unsetting Short description will not show this command in help
		cmd.Short = ""
		cmd.Deprecated = "and will be removed in the future version"
		cmds.AddCommand(cmd)
	}

	return cmds
}
