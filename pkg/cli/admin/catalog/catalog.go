package catalog

import (
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

var catalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "Tools for managing the OpenShift OLM Catalogs",
	Long: templates.LongDesc(`
			This tool is used to extract and mirror the contents of catalogs for Operator
			Lifecycle Manager.

			The subcommands allow you to mirror catalog content across registries.
			`),
}

type subCommandFunc func(kcmdutil.Factory, genericclioptions.IOStreams) *cobra.Command

// subcommands are added via init in the subcommand files
var subCommands = make([]subCommandFunc, 0)

func AddCommand(f kcmdutil.Factory, streams genericclioptions.IOStreams, cmd *cobra.Command) {
	catalogCmd.Run = kcmdutil.DefaultSubCommandRun(streams.ErrOut)
	for _, c := range subCommands {
		catalogCmd.AddCommand(c(f, streams))
	}
	cmd.AddCommand(catalogCmd)
}
