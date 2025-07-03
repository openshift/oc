package nodeimage

import (
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

// NewCmdNodeImage exposes the commands to add nodes to an existing cluster.
func NewCmdNodeImage(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node-image",
		Short: "Add nodes to an existing cluster",
		Long: templates.LongDesc(`
			The subcommands allow you to create an ISO image to be used for adding the desired
			nodes to an OpenShift cluster, and also to monitor the process.
			`),
	}
	cmd.AddCommand(NewCreate(f, streams))
	cmd.AddCommand(NewMonitor(f, streams))
	return cmd
}
