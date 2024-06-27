package addnodes

import (
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

func NewCmdAddNodes(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-nodes",
		Short: "Commands for adding new nodes to an OpenShift cluster",
		Long: templates.LongDesc(`
			The subcommands allow you to create an ISO image to be used for adding the desired
			nodes to an OpenShift cluster, and also to monitor the process.
			`),
	}
	cmd.AddCommand(NewCreate(f, streams))
	cmd.AddCommand(NewMonitor(f, streams))
	return cmd
}
