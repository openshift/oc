package createbootstrapprojecttemplate

import (
	"errors"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/templates"
)

type CreateBootstrapProjectTemplateOptions struct {
	PrintFlags *genericclioptions.PrintFlags

	Name string
	Args []string

	Printer printers.ResourcePrinter

	genericclioptions.IOStreams
}

var createBootstrapProjectTemplateExample = templates.Examples(`
	# Output a bootstrap project template in YAML format to stdout
	oc adm create-bootstrap-project-template -o yaml
`)

func NewCreateBootstrapProjectTemplateOptions(streams genericclioptions.IOStreams) *CreateBootstrapProjectTemplateOptions {
	return &CreateBootstrapProjectTemplateOptions{
		PrintFlags: genericclioptions.NewPrintFlags("created").WithTypeSetter(scheme.Scheme).WithDefaultOutput("json"),
		Name:       DefaultTemplateName,
		IOStreams:  streams,
	}
}

func NewCommandCreateBootstrapProjectTemplate(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewCreateBootstrapProjectTemplateOptions(streams)
	cmd := &cobra.Command{
		Use:     "create-bootstrap-project-template",
		Short:   "Create a bootstrap project template",
		Example: createBootstrapProjectTemplateExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().StringVar(&o.Name, "name", o.Name, "The name of the template to output.")
	o.PrintFlags.AddFlags(cmd)

	return cmd
}

func (o *CreateBootstrapProjectTemplateOptions) Complete(args []string) error {
	o.Args = args
	var err error
	o.Printer, err = o.PrintFlags.ToPrinter()
	if err != nil {
		return err
	}
	return nil
}

func (o *CreateBootstrapProjectTemplateOptions) Validate() error {
	if len(o.Args) != 0 {
		return errors.New("no arguments are supported")
	}
	if len(o.Name) == 0 {
		return errors.New("--name must be provided")
	}

	return nil
}

func (o *CreateBootstrapProjectTemplateOptions) Run() error {
	template := DefaultTemplate()
	template.Name = o.Name

	return o.Printer.PrintObj(template, o.Out)
}
