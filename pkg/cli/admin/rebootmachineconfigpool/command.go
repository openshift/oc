package rebootmachineconfigpool

import (
	"context"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/dynamic"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	rebootMachineConfigPoolLong = templates.LongDesc(`
		Reboot the specified machine config pool by modifying an appropriate MachineConfig.

		Does not wait for the reboot to complete, only initiates it.  This command will honor paused pools.
		Degraded, failed, or otherwise not healthy nodes will not restart.

		Experimental: This command is under active development and may change without notice.
	`)

	rebootMachineConfigPoolExample = templates.Examples(`
		# Reboot all MachineConfigPools
		oc adm reboot-machine-config-pool mcp/worker mcp/master

		# Reboot all MachineConfigPools that inherit from worker.  This include all custom MachineConfigPools and infra.
		oc adm reboot-machine-config-pool mcp/worker

		# Reboot masters
		oc adm reboot-machine-config-pool mcp/master`)
)

type RebootMachineConfigPoolOptions struct {
	RESTClientGetter     genericclioptions.RESTClientGetter
	PrintFlags           *genericclioptions.PrintFlags
	ResourceBuilderFlags *genericclioptions.ResourceBuilderFlags

	// TODO push this into genericclioptions
	DryRun bool

	genericiooptions.IOStreams
}

func NewRebootMachineConfigPoolOptions(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *RebootMachineConfigPoolOptions {
	return &RebootMachineConfigPoolOptions{
		RESTClientGetter: restClientGetter,
		PrintFlags:       genericclioptions.NewPrintFlags("rolling reboot initiated"),
		ResourceBuilderFlags: genericclioptions.NewResourceBuilderFlags().
			WithLatest(),

		IOStreams: streams,
	}
}

func NewCmdRebootMachineConfigPool(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewRebootMachineConfigPoolOptions(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "reboot-machine-config-pool",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Initiate reboot of the specified MachineConfigPool"),
		Long:                  rebootMachineConfigPoolLong,
		Example:               rebootMachineConfigPoolExample,
		Run: func(cmd *cobra.Command, args []string) {
			r, err := o.ToRuntime(args)

			cmdutil.CheckErr(err)
			cmdutil.CheckErr(r.Run(context.Background()))
		},
	}

	o.AddFlags(cmd)

	return cmd
}

// AddFlags registers flags for a cli
func (o *RebootMachineConfigPoolOptions) AddFlags(cmd *cobra.Command) {
	o.PrintFlags.AddFlags(cmd)
	o.ResourceBuilderFlags.AddFlags(cmd.Flags())

	cmd.Flags().BoolVar(&o.DryRun, "dry-run", o.DryRun, "Set to true to use server-side dry run.")
}

func (o *RebootMachineConfigPoolOptions) ToRuntime(args []string) (*RebootMachineConfigPoolRuntime, error) {
	printer, err := o.PrintFlags.ToPrinter()
	if err != nil {
		return nil, err
	}

	builder := o.ResourceBuilderFlags.ToBuilder(o.RESTClientGetter, args)
	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	ret := &RebootMachineConfigPoolRuntime{
		ResourceFinder: builder,
		DynamicClient:  dynamicClient,

		dryRun: o.DryRun,

		Printer:   printer,
		IOStreams: o.IOStreams,
	}

	return ret, nil
}
