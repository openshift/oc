package prune

import (
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	groups "github.com/openshift/oc/pkg/cli/admin/groups/sync"
	"github.com/openshift/oc/pkg/cli/admin/prune/auth"
	"github.com/openshift/oc/pkg/cli/admin/prune/builds"
	"github.com/openshift/oc/pkg/cli/admin/prune/deployments"
	"github.com/openshift/oc/pkg/cli/admin/prune/images"
	"github.com/openshift/oc/pkg/cli/admin/prune/imagestream"
)

var pruneLong = templates.LongDesc(`
	Remove older versions of resources from the server

	The commands here allow administrators to manage the older versions of resources on
	the system by removing them.`)

func NewCommandPrune(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	// Parent command to which all subcommands are added.
	cmds := &cobra.Command{
		Use:   "prune",
		Short: "Remove older versions of resources from the server",
		Long:  pruneLong,
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	cmds.AddCommand(builds.NewCmdPruneBuilds(f, streams))
	cmds.AddCommand(deployments.NewCmdPruneDeployments(f, streams))
	cmds.AddCommand(images.NewCmdPruneImages(f, streams))
	cmds.AddCommand(imagestream.NewCmd(f, streams))
	cmds.AddCommand(groups.NewCmdPruneGroups("groups", "prune groups", f, streams))
	cmds.AddCommand(auth.NewCmdPruneAuth(f, streams))
	return cmds
}
