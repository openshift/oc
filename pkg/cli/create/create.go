package create

import (
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
)

// CreateSubcommandOptions is an options struct to support create subcommands
type CreateSubcommandOptions struct {
	genericclioptions.IOStreams

	// PrintFlags holds options necessary for obtaining a printer
	PrintFlags *genericclioptions.PrintFlags
	// Name of resource being created
	Name             string
	DryRunStrategy   cmdutil.DryRunStrategy
	CreateAnnotation bool

	Namespace        string
	EnforceNamespace bool

	Printer printers.ResourcePrinter
}

func NewCreateSubcommandOptions(ioStreams genericclioptions.IOStreams) *CreateSubcommandOptions {
	return &CreateSubcommandOptions{
		PrintFlags: genericclioptions.NewPrintFlags("created").WithTypeSetter(scheme.Scheme),
		IOStreams:  ioStreams,
	}
}

func (o *CreateSubcommandOptions) AddFlags(cmd *cobra.Command) {
	o.PrintFlags.AddFlags(cmd)
	cmdutil.AddApplyAnnotationVarFlags(cmd, &o.CreateAnnotation)
}

func (o *CreateSubcommandOptions) Complete(f genericclioptions.RESTClientGetter, cmd *cobra.Command, args []string) error {
	name, err := NameFromCommandArgs(cmd, args)
	if err != nil {
		return err
	}
	o.Name = name

	o.Namespace, o.EnforceNamespace, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	o.DryRunStrategy, err = cmdutil.GetDryRunStrategy(cmd)
	if err != nil {
		return err
	}

	cmdutil.PrintFlagsWithDryRunStrategy(o.PrintFlags, o.DryRunStrategy)
	o.Printer, err = o.PrintFlags.ToPrinter()
	if err != nil {
		return err
	}

	return nil
}

// NameFromCommandArgs is a utility function for commands that assume the first argument is a resource name
func NameFromCommandArgs(cmd *cobra.Command, args []string) (string, error) {
	argsLen := cmd.ArgsLenAtDash()
	// ArgsLenAtDash returns -1 when -- was not specified
	if argsLen == -1 {
		argsLen = len(args)
	}
	if argsLen != 1 {
		return "", cmdutil.UsageErrorf(cmd, "exactly one NAME is required, got %d", argsLen)
	}
	return args[0], nil

}
