package nodeimage

import (
	"context"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

func NewCreate(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewCreateOptions(streams)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an ISO image for booting the nodes to be added to the target cluster",
		Long: templates.LongDesc(`
			<TODO>
		`),
		Example: templates.Examples(`
			<TODO>
		`),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run(cmd.Context()))
		},
	}
	_ = cmd.Flags()
	return cmd

}

func NewCreateOptions(streams genericiooptions.IOStreams) *CreateOptions {
	return &CreateOptions{
		IOStreams: streams,
	}
}

type CreateOptions struct {
	genericiooptions.IOStreams
}

func (o *CreateOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	return nil
}

func (o *CreateOptions) Validate() error {
	return nil
}

func (o *CreateOptions) Run(ctx context.Context) error {
	return nil
}
