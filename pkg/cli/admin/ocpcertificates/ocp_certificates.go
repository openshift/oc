package ocpcertificates

import (
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	ktemplates "k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/cli/admin/ocpcertificates/certregen"
	"github.com/openshift/oc/pkg/cli/admin/ocpcertificates/monitorregeneration"
	"github.com/openshift/oc/pkg/cli/admin/ocpcertificates/regeneratemco"
	"github.com/openshift/oc/pkg/cli/admin/ocpcertificates/trustpurge"
)

var ocpCertificatesLong = ktemplates.LongDesc(`
	OCP Certificate Commands

	Actions for managing OpenShift platform certificates are exposed here.`)

func NewCommandOCPCertificates(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	// Main command
	cmds := &cobra.Command{
		Use:   "ocp-certificates",
		Short: "Tools for managing a cluster's certificates",
		Long:  ocpCertificatesLong,
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	cmds.AddCommand(
		certregen.NewCmdRegenerateTopLevel(f, streams),
		certregen.NewCmdRegenerateLeaves(f, streams),
		regeneratemco.NewCmdRegenerateTopLevel(f, streams),
		regeneratemco.NewCmdUpdateUserData(f, streams),
		monitorregeneration.NewCmdMonitorCertificates(f, streams),
		trustpurge.NewCmdRemoveOldTrust(f, streams),
	)

	return cmds
}
