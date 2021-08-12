package policy

import (
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"

	adminpolicy "github.com/openshift/oc/pkg/cli/admin/policy"
)

func NewCmdPolicy(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	// Parent command to which all subcommands are added.
	cmds := &cobra.Command{
		Use:   "policy",
		Short: "Manage authorization policy",
		Long:  `Manage authorization policy`,
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	cmds.AddCommand(adminpolicy.NewCmdWhoCan(f, streams))
	cmds.AddCommand(adminpolicy.NewCmdSccSubjectReview(f, streams))
	cmds.AddCommand(adminpolicy.NewCmdSccReview(f, streams))

	cmds.AddCommand(adminpolicy.NewCmdAddRoleToUser(f, streams))
	cmds.AddCommand(adminpolicy.NewCmdRemoveRoleFromUser(f, streams))
	cmds.AddCommand(adminpolicy.NewCmdRemoveUserFromProject(f, streams))
	cmds.AddCommand(adminpolicy.NewCmdAddRoleToGroup(f, streams))
	cmds.AddCommand(adminpolicy.NewCmdRemoveRoleFromGroup(f, streams))
	cmds.AddCommand(adminpolicy.NewCmdRemoveGroupFromProject(f, streams))

	return cmds
}
