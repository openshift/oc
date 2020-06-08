package serviceaccounts

import (
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

const ServiceAccountsRecommendedName = "serviceaccounts"

var serviceAccountsLong = templates.LongDesc(`Manage service accounts in your project

Service accounts allow system components to access the API.`)

const (
	serviceAccountsShort = `Manage service accounts in your project`
)

func NewCmdServiceAccounts(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	cmds := &cobra.Command{
		Use:     "serviceaccounts",
		Short:   serviceAccountsShort,
		Long:    serviceAccountsLong,
		Aliases: []string{"sa"},
		Run:     kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	cmds.AddCommand(NewCommandCreateKubeconfig(f, streams))
	cmds.AddCommand(NewCommandGetServiceAccountToken(f, streams))
	cmds.AddCommand(NewCommandNewServiceAccountToken(f, streams))

	return cmds
}
