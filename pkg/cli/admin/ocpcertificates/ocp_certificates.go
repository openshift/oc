package ocpcertificates

import (
	"fmt"

	"github.com/openshift/oc/pkg/cli/admin/ocpcertificates/monitorregeneration"

	"github.com/openshift/oc/pkg/cli/admin/ocpcertificates/certregen"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	ktemplates "k8s.io/kubectl/pkg/util/templates"
)

var ocpCertificatesLong = ktemplates.LongDesc(`
	OCP Certificate Commands

	Actions for managing OpenShift platform certificates are exposed here.`)

func NewCommandOCPCertificates(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	// Main command
	cmds := &cobra.Command{
		Use:   "ocp-certificates",
		Short: "Tools for managing a cluster's certificates",
		Long:  fmt.Sprintf(ocpCertificatesLong),
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	cmds.AddCommand(
		certregen.NewCmdRegenerateTopLevel(f, streams),
		certregen.NewCmdRegenerateLeaves(f, streams),
		monitorregeneration.NewCmdMonitorCertificates(f, streams),
	)

	return cmds
}
