package admin

import (
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/kubectl/pkg/cmd/certificates"
	"k8s.io/kubectl/pkg/cmd/drain"
	"k8s.io/kubectl/pkg/cmd/taint"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	ktemplates "k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/cli/admin/buildchain"
	"github.com/openshift/oc/pkg/cli/admin/catalog"
	"github.com/openshift/oc/pkg/cli/admin/copytonode"
	"github.com/openshift/oc/pkg/cli/admin/createbootstrapprojecttemplate"
	"github.com/openshift/oc/pkg/cli/admin/createerrortemplate"
	"github.com/openshift/oc/pkg/cli/admin/createlogintemplate"
	"github.com/openshift/oc/pkg/cli/admin/createproviderselectiontemplate"
	"github.com/openshift/oc/pkg/cli/admin/groups"
	"github.com/openshift/oc/pkg/cli/admin/inspect"
	"github.com/openshift/oc/pkg/cli/admin/inspectalerts"
	"github.com/openshift/oc/pkg/cli/admin/migrate"
	migrateteicsp "github.com/openshift/oc/pkg/cli/admin/migrate/icsp"
	migratetemplateinstances "github.com/openshift/oc/pkg/cli/admin/migrate/templateinstances"
	"github.com/openshift/oc/pkg/cli/admin/mustgather"
	"github.com/openshift/oc/pkg/cli/admin/network"
	"github.com/openshift/oc/pkg/cli/admin/node"
	"github.com/openshift/oc/pkg/cli/admin/nodeimage"
	"github.com/openshift/oc/pkg/cli/admin/ocpcertificates"
	"github.com/openshift/oc/pkg/cli/admin/policy"
	"github.com/openshift/oc/pkg/cli/admin/project"
	"github.com/openshift/oc/pkg/cli/admin/prune"
	"github.com/openshift/oc/pkg/cli/admin/rebootmachineconfigpool"
	"github.com/openshift/oc/pkg/cli/admin/release"
	"github.com/openshift/oc/pkg/cli/admin/restartkubelet"
	"github.com/openshift/oc/pkg/cli/admin/top"
	"github.com/openshift/oc/pkg/cli/admin/upgrade"
	"github.com/openshift/oc/pkg/cli/admin/verifyimagesignature"
	"github.com/openshift/oc/pkg/cli/admin/waitfornodereboot"
	"github.com/openshift/oc/pkg/cli/admin/waitforstable"
	cmdutil "github.com/openshift/oc/pkg/helpers/cmd"
)

var adminLong = ktemplates.LongDesc(`
	Administrative Commands

	Actions for administering an OpenShift cluster are exposed here.`)

// inspectAlertsFeatureGate is an environment variable used to gate the inclusion of the inspect-alerts subcommand.
const inspectAlertsFeatureGate = "OC_ENABLE_CMD_INSPECT_ALERTS"

func NewCommandAdmin(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	// Main command
	cmds := &cobra.Command{
		Use:   "adm",
		Short: "Tools for managing a cluster",
		Long:  adminLong,
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	clusterManagement := []*cobra.Command{
		upgrade.New(f, streams),
		top.NewCommandTop(f, streams),
		mustgather.NewMustGatherCommand(f, streams),
		ocpcertificates.NewCommandOCPCertificates(f, streams),
		waitforstable.NewCmdWaitForStableClusterOperators(f, streams),
		inspect.NewCmdInspect(streams),
	}

	if kcmdutil.FeatureGate(inspectAlertsFeatureGate).IsEnabled() {
		clusterManagement = append(clusterManagement, inspectalerts.New(f, streams))
	}

	groups := ktemplates.CommandGroups{
		{
			Message:  "Cluster Management:",
			Commands: clusterManagement,
		},
		{
			Message: "Node Management:",
			Commands: []*cobra.Command{
				cmdutil.ReplaceCommandName("kubectl", "oc adm", drain.NewCmdDrain(f, streams)),
				cmdutil.ReplaceCommandName("kubectl", "oc adm", ktemplates.Normalize(drain.NewCmdCordon(f, streams))),
				cmdutil.ReplaceCommandName("kubectl", "oc adm", ktemplates.Normalize(drain.NewCmdUncordon(f, streams))),
				cmdutil.ReplaceCommandName("kubectl", "oc adm", ktemplates.Normalize(taint.NewCmdTaint(f, streams))),
				node.NewCmdLogs(f, streams),
				restartkubelet.NewCmdRestartKubelet(f, streams),
				copytonode.NewCmdCopyToNode(f, streams),
				rebootmachineconfigpool.NewCmdRebootMachineConfigPool(f, streams),
				waitfornodereboot.NewCmdWaitForNodeReboot(f, streams),
				nodeimage.NewCmdNodeImage(f, streams),
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
					migratetemplateinstances.NewCmdMigrateTemplateInstances(f, streams),
					migrateteicsp.NewCmdMigrateICSP(f, streams),
				),
			},
		},
		{
			Message: "Configuration:",
			Commands: []*cobra.Command{
				createbootstrapprojecttemplate.NewCommandCreateBootstrapProjectTemplate(f, streams),

				createlogintemplate.NewCommandCreateLoginTemplate(f, streams),
				createproviderselectiontemplate.NewCommandCreateProviderSelectionTemplate(f, streams),
				createerrortemplate.NewCommandCreateErrorTemplate(f, streams),
			},
		},
	}

	groups.Add(cmds)

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
