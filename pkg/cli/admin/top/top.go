package top

import (
	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/cmd/top"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	toppvc "github.com/openshift/oc/pkg/cli/admin/toppvc"
	cmdutil "github.com/openshift/oc/pkg/helpers/cmd"
)

const (
	TopRecommendedName = "top"
)

var topLong = templates.LongDesc(`
	Show usage statistics of resources on the server

	This command analyzes resources managed by the platform and presents current
	usage statistics.`)

func NewCommandTop(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	// Parent command to which all subcommands are added.
	cmds := &cobra.Command{
		Use:   "top",
		Short: "Show usage statistics of resources on the server",
		Long:  topLong,
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	cmdTopNode := cmdutil.ReplaceCommandName("kubectl", "oc adm", top.NewCmdTopNode(f, nil, streams))
	cmdTopPod := cmdutil.ReplaceCommandName("kubectl", "oc adm", top.NewCmdTopPod(f, nil, streams))

	cmds.AddCommand(NewCmdTopImages(f, streams))
	cmds.AddCommand(NewCmdTopImageStreams(f, streams))
	cmds.AddCommand(toppvc.NewCmdTopPersistentVolumeClaims(f, streams))
	cmdTopNode.Long = templates.LongDesc(cmdTopNode.Long)
	cmdTopNode.Example = templates.Examples(cmdTopNode.Example)
	cmdTopPod.Long = templates.LongDesc(cmdTopPod.Long)
	cmdTopPod.Example = templates.Examples(cmdTopPod.Example)
	cmds.AddCommand(cmdTopNode)
	cmds.AddCommand(cmdTopPod)
	return cmds
}
