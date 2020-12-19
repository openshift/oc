package create

import (
	"fmt"

	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/templates"

	routev1client "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
)

var (
	routeLong = templates.LongDesc(`
		Expose containers externally via secured routes

		Three types of secured routes are supported: edge, passthrough, and reencrypt.
		If you wish to create unsecured routes, see "arvan paas expose -h"
	`)
)

// NewCmdCreateRoute is a macro command to create a secured route.
func NewCmdCreateRoute(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "route",
		Short: "Expose containers externally via secured routes",
		Long:  routeLong,
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	cmd.AddCommand(NewCmdCreateEdgeRoute(f, streams))
	cmd.AddCommand(NewCmdCreatePassthroughRoute(f, streams))
	cmd.AddCommand(NewCmdCreateReencryptRoute(f, streams))

	return cmd
}

// CreateRouteSubcommandOptions is an options struct to support create subcommands
type CreateRouteSubcommandOptions struct {
	// PrintFlags holds options necessary for obtaining a printer
	PrintFlags *genericclioptions.PrintFlags
	// Name of resource being created
	Name        string
	ServiceName string

	DryRunStrategy kcmdutil.DryRunStrategy

	Namespace        string
	EnforceNamespace bool
	CreateAnnotation bool

	Mapper meta.RESTMapper

	Printer printers.ResourcePrinter

	Client     routev1client.RoutesGetter
	CoreClient corev1client.CoreV1Interface

	genericclioptions.IOStreams
}

func NewCreateRouteSubcommandOptions(ioStreams genericclioptions.IOStreams) *CreateRouteSubcommandOptions {
	return &CreateRouteSubcommandOptions{
		PrintFlags: genericclioptions.NewPrintFlags("created").WithTypeSetter(scheme.Scheme),
		IOStreams:  ioStreams,
	}
}

func (o *CreateRouteSubcommandOptions) AddFlags(cmd *cobra.Command) {
	o.PrintFlags.AddFlags(cmd)
	cmdutil.AddApplyAnnotationVarFlags(cmd, &o.CreateAnnotation)
}

func (o *CreateRouteSubcommandOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	var err error
	o.Name, err = resolveRouteName(args)
	if err != nil {
		return err
	}

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}

	o.CoreClient, err = corev1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	o.Client, err = routev1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	o.Mapper, err = f.ToRESTMapper()
	if err != nil {
		return err
	}
	o.Namespace, o.EnforceNamespace, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	o.CreateAnnotation = cmdutil.GetFlagBool(cmd, cmdutil.ApplyAnnotationsFlag)

	o.DryRunStrategy, err = kcmdutil.GetDryRunStrategy(cmd)
	if err != nil {
		return err
	}
	kcmdutil.PrintFlagsWithDryRunStrategy(o.PrintFlags, o.DryRunStrategy)
	o.Printer, err = o.PrintFlags.ToPrinter()
	if err != nil {
		return err
	}

	return nil
}

func resolveRouteName(args []string) (string, error) {
	switch len(args) {
	case 0:
	case 1:
		return args[0], nil
	default:
		return "", fmt.Errorf("multiple names provided. Please specify at most one")
	}
	return "", nil
}
