package image

import (
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	ktemplates "k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/cli/image/append"
	"github.com/openshift/oc/pkg/cli/image/extract"
	"github.com/openshift/oc/pkg/cli/image/info"
	"github.com/openshift/oc/pkg/cli/image/mirror"
)

var (
	imageLong = ktemplates.LongDesc(`
		Manage images on OpenShift

		These commands help you manage images on OpenShift.`)
)

// NewCmdImage exposes commands for modifying images.
func NewCmdImage(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	image := &cobra.Command{
		Use:   "image COMMAND",
		Short: "Useful commands for managing images",
		Long:  imageLong,
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	groups := ktemplates.CommandGroups{
		{
			Message: "View or copy images:",
			Commands: []*cobra.Command{
				info.NewInfo(f, streams),
				mirror.NewCmdMirrorImage(streams),
			},
		},
		{
			Message: "Advanced commands:",
			Commands: []*cobra.Command{
				append.NewCmdAppendImage(streams),
				extract.NewExtract(streams),
			},
		},
	}
	groups.Add(image)
	return image
}
