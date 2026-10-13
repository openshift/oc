package registry

import (
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	ktemplates "k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/cli/registry/info"
	"github.com/openshift/oc/pkg/cli/registry/login"
)

var (
	imageLong = ktemplates.LongDesc(`
		Manage the integrated registry on OpenShift

		These commands help you work with an integrated OpenShift registry.`)
)

// NewCmd exposes commands for working with the registry.
func NewCmd(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	image := &cobra.Command{
		Use:   "registry COMMAND",
		Short: "Commands for working with the registry",
		Long:  imageLong,
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	groups := ktemplates.CommandGroups{
		{
			Message: "Advanced commands:",
			Commands: []*cobra.Command{
				info.NewRegistryInfoCmd(f, streams),
				login.NewRegistryLoginCmd(f, streams),
			},
		},
	}
	groups.Add(image)
	return image
}
