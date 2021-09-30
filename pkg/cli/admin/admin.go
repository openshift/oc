package admin

import (
	"fmt"

	"k8s.io/kubectl/pkg/cmd/certificates"
	"k8s.io/kubectl/pkg/cmd/taint"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/cmd/drain"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	ktemplates "k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/cli/admin/buildchain"
	"github.com/openshift/oc/pkg/cli/admin/catalog"
	"github.com/openshift/oc/pkg/cli/admin/createbootstrapprojecttemplate"
	"github.com/openshift/oc/pkg/cli/admin/createerrortemplate"
	"github.com/openshift/oc/pkg/cli/admin/createkubeconfig"
	"github.com/openshift/oc/pkg/cli/admin/createlogintemplate"
	"github.com/openshift/oc/pkg/cli/admin/createproviderselectiontemplate"
	"github.com/openshift/oc/pkg/cli/admin/groups"
	"github.com/openshift/oc/pkg/cli/admin/inspect"
	"github.com/openshift/oc/pkg/cli/admin/migrate"
	migrateetcd "github.com/openshift/oc/pkg/cli/admin/migrate/etcd"
	migrateimages "github.com/openshift/oc/pkg/cli/admin/migrate/images"
	migratehpa "github.com/openshift/oc/pkg/cli/admin/migrate/legacyhpa"
	migratestorage "github.com/openshift/oc/pkg/cli/admin/migrate/storage"
	migratetemplateinstances "github.com/openshift/oc/pkg/cli/admin/migrate/templateinstances"
	"github.com/openshift/oc/pkg/cli/admin/mustgather"
	"github.com/openshift/oc/pkg/cli/admin/network"
	"github.com/openshift/oc/pkg/cli/admin/node"
	"github.com/openshift/oc/pkg/cli/admin/policy"
	"github.com/openshift/oc/pkg/cli/admin/project"
	"github.com/openshift/oc/pkg/cli/admin/prune"
	"github.com/openshift/oc/pkg/cli/admin/release"
	"github.com/openshift/oc/pkg/cli/admin/top"
	"github.com/openshift/oc/pkg/cli/admin/upgrade"
	"github.com/openshift/oc/pkg/cli/admin/verifyimagesignature"
	cmdutil "github.com/openshift/oc/pkg/helpers/cmd"
)

var adminLong = ktemplates.LongDesc(`
	Administrative Commands

	Actions for administering an OpenShift cluster are exposed here.`)

func NewCommandAdmin(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	// Main command
	cmds := &cobra.Command{
		Use:   "adm",
		Short: "Tools for managing a cluster",
		Long:  fmt.Sprintf(adminLong),
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	groups := ktemplates.CommandGroups{
		{
			Message: "Cluster Management:",
			Commands: []*cobra.Command{
				upgrade.New(f, streams),
				top.NewCommandTop(f, streams),
				mustgather.NewMustGatherCommand(f, streams),
				inspect.NewCmdInspect(streams),
			},
		},
		{
			Message: "Node Management:",
			Commands: []*cobra.Command{
				cmdutil.ReplaceCommandName("kubectl", "oc adm", drain.NewCmdDrain(f, streams)),
				cmdutil.ReplaceCommandName("kubectl", "oc adm", ktemplates.Normalize(drain.NewCmdCordon(f, streams))),
				cmdutil.ReplaceCommandName("kubectl", "oc adm", ktemplates.Normalize(drain.NewCmdUncordon(f, streams))),
				cmdutil.ReplaceCommandName("kubectl", "oc adm", ktemplates.Normalize(taint.NewCmdTaint(f, streams))),
				node.NewCmdLogs(f, streams),
			},
		},
		{
			Message: "Security and Policy:",
			Commands: []*cobra.Command{
				project.NewCmdNewProject(f, streams),
				policy.NewCmdPolicy(f, streams),
				groups.NewCmdGroups(f, streams),
				withShortDescription(cmdutil.ReplaceCommandName("kubectl", "oc adm", ktemplates.Normalize(certificates.NewCmdCertificate(f, streams))), "Approve or reject certificate requests"),
				network.NewCmdPodNetwork(f, streams),
			},
		},
		{
			Message: "Maintenance:",
			Commands: []*cobra.Command{
				prune.NewCommandPrune(f, streams),
				migrate.NewCommandMigrate(f, streams,
					// Migration commands
					migrateimages.NewCmdMigrateImageReferences(f, streams),
					migratestorage.NewCmdMigrateAPIStorage(f, streams),
					migrateetcd.NewCmdMigrateTTLs(f, streams),
					migratehpa.NewCmdMigrateLegacyHPA(f, streams),
					migratetemplateinstances.NewCmdMigrateTemplateInstances(f, streams),
				),
			},
		},
		{
			Message: "Configuration:",
			Commands: []*cobra.Command{
				createkubeconfig.NewCommandCreateKubeConfig(streams),

				createbootstrapprojecttemplate.NewCommandCreateBootstrapProjectTemplate(f, streams),

				createlogintemplate.NewCommandCreateLoginTemplate(f, streams),
				createproviderselectiontemplate.NewCommandCreateProviderSelectionTemplate(f, streams),
				createerrortemplate.NewCommandCreateErrorTemplate(f, streams),
			},
		},
	}

	groups.Add(cmds)
	cmdutil.ActsAsRootCommand(cmds, []string{"options"}, groups...)

	cmds.AddCommand(
		release.NewCmd(f, streams),
		buildchain.NewCmdBuildChain(f, streams),
		verifyimagesignature.NewCmdVerifyImageSignature(f, streams),
	)
	catalog.AddCommand(f, streams, cmds)

	return cmds
}

func withShortDescription(cmd *cobra.Command, desc string) *cobra.Command {
	cmd.Short = desc
	return cmd
}
