package addnodes

import (
	"context"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

func NewMonitor(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewMonitorOptions(streams)
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Monitor the process of adding new nodes to an OpenShift cluster",
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

func NewMonitorOptions(streams genericiooptions.IOStreams) *MonitorOptions {
	return &MonitorOptions{
		IOStreams: streams,
	}
}

type MonitorOptions struct {
	genericiooptions.IOStreams
}

func (o *MonitorOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	return nil
}

func (o *MonitorOptions) Validate() error {
	return nil
}

func (o *MonitorOptions) Run(ctx context.Context) error {
	return nil
}
