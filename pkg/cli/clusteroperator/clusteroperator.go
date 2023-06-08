package clusteroperator

import (
	"fmt"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	ktemplates "k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/cli/clusteroperator/waitforstable"
)

var clusterOperatorsLong = ktemplates.LongDesc(`
	OCP ClusterOperator Commands
	Actions for managing OpenShift cluster operators.`)

func NewCommandClusterOperators(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	// Main command
	cmds := &cobra.Command{
		Use:   "clusteroperator",
		Short: "Tools for managing clusteroperators",
		Long:  fmt.Sprintf(clusterOperatorsLong),
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	cmds.AddCommand(
		waitforstable.NewCmdWaitForStableClusterOperators(f, streams),
	)

	return cmds
}
