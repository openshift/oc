package groups

import (
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/cli/admin/groups/new"
	"github.com/openshift/oc/pkg/cli/admin/groups/sync"
	"github.com/openshift/oc/pkg/cli/admin/groups/users"
)

const GroupsRecommendedName = "groups"

var groupLong = templates.LongDesc(`
	Manage groups in your cluster

	Groups are sets of users that can be used when describing policy.
`)

func NewCmdGroups(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	// Parent command to which all subcommands are added.
	cmds := &cobra.Command{
		Use:   "groups",
		Short: "Manage groups",
		Long:  groupLong,
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	cmds.AddCommand(new.NewCmdNewGroup(f, streams))
	cmds.AddCommand(users.NewCmdAddUsers(f, streams))
	cmds.AddCommand(users.NewCmdRemoveUsers(f, streams))
	cmds.AddCommand(sync.NewCmdSync(f, streams))
	cmds.AddCommand(sync.NewCmdPruneGroups("prune", "groups prune", f, streams))

	return cmds
}
