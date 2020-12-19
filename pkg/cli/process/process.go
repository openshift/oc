package process

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"reflect"
	"strings"
	"time"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/generate"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/templates"

	octemplateapi "github.com/openshift/api/template"
	templatev1 "github.com/openshift/api/template/v1"
	templatev1client "github.com/openshift/client-go/template/clientset/versioned/typed/template/v1"
	"github.com/openshift/library-go/pkg/template/generator"
	"github.com/openshift/library-go/pkg/template/templateprocessing"
	cmdutil "github.com/openshift/oc/pkg/helpers/cmd"
	"github.com/openshift/oc/pkg/helpers/describe"
	"github.com/openshift/oc/pkg/helpers/newapp/app"
	"github.com/openshift/oc/pkg/helpers/template/templateprocessorclient"
)

var (
	processLong = templates.LongDesc(`
		Process template into a list of resources specified in filename or stdin

		Templates allow parameterization of resources prior to being sent to the server for creation or
		update. Templates have "parameters", which may either be generated on creation or set by the user,
		as well as metadata describing the template.

		The output of the process command is always a list of one or more resources. You may pipe the
		output to the create command over STDIN (using the '-f -' option) or redirect it to a file.

		Process resolves the template on the server, but you may pass --local to parameterize the template
		locally. When running locally be aware that the version of your client tools will determine what
		template transformations are supported, rather than the server.
	`)

	processExample = templates.Examples(`
		# Convert template.json file into resource list and pass to create
		arvan paas process -f template.json | arvan paas create -f -

		# Process a file locally instead of contacting the server
		arvan paas process -f template.json --local -o yaml

		# Process template while passing a user-defined label
		arvan paas process -f template.json -l name=mytemplate

		# Convert stored template into resource list
		arvan paas process foo

		# Convert stored template into resource list by setting/overriding parameter values
		arvan paas process foo PARM1=VALUE1 PARM2=VALUE2

		# Convert template stored in different namespace into a resource list
		arvan paas process openshift//foo

		# Convert template.json into resource list
		cat template.json | arvan paas process -f -
	`)
)

type ProcessOptions struct {
	PrintFlags *genericclioptions.PrintFlags
	Printer    printers.ResourcePrinter

	usageErrorFn func(string, ...interface{}) error

	outputFormat        string
	labels              string
	filename            string
	local               bool
	raw                 bool
	parameters          bool
	ignoreUnknownParams bool
	templateName        string
	paramFile           []string
	templateParams      []string
	namespace           string
	explicitNamespace   bool
	paramValuesProvided bool

	templateClient    *templatev1client.TemplateV1Client
	templateProcessor func(*templatev1.Template) (*templatev1.Template, error)

	builderFn func() *resource.Builder

	mapper meta.RESTMapper

	genericclioptions.IOStreams
}

func NewProcessOptions(streams genericclioptions.IOStreams) *ProcessOptions {
	printFlags := genericclioptions.NewPrintFlags("processed").WithTypeSetter(scheme.Scheme).WithDefaultOutput("json")
	// disable binding the --template flag so that we can bind our own --template flag with a shorthand (until the shorthand is deprecated)
	printFlags.TemplatePrinterFlags.TemplateArgument = nil

	return &ProcessOptions{
		PrintFlags: printFlags,
		IOStreams:  streams,
	}
}

// NewCmdProcess implements the OpenShift cli process command
func NewCmdProcess(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewProcessOptions(streams)

	cmd := &cobra.Command{
		Use:     "process (TEMPLATE | -f FILENAME) [-p=KEY=VALUE]",
		Short:   "Process a template into list of resources",
		Long:    processLong,
		Example: processExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate(cmd))
			kcmdutil.CheckErr(o.RunProcess())
		},
	}

	o.PrintFlags.AddFlags(cmd)
	// edit --output flag description to mention "describe" as an acceptable output format
	// TODO: add custom PrintFlags printer that does this ^
	if f := cmd.Flag("output"); f != nil {
		f.Usage = "Output format. One of: json|yaml|name|describe|go-template-file|templatefile|template|go-template|jsonpath|jsonpath-file."
	}

	// point to the original memory address shared between the jsonpath and go-template printer's TemplateArgument field
	o.PrintFlags.TemplatePrinterFlags.TemplateArgument = o.PrintFlags.TemplatePrinterFlags.JSONPathPrintFlags.TemplateArgument
	cmd.Flags().StringVar(o.PrintFlags.TemplatePrinterFlags.TemplateArgument, "template", *o.PrintFlags.TemplatePrinterFlags.TemplateArgument, "Template string or path to template file to use when -o=go-template, -o=go-template-file. The template format is golang templates [http://golang.org/pkg/text/template/#pkg-overview].")
	cmd.MarkFlagFilename("template")

	cmd.Flags().StringVarP(&o.filename, "filename", "f", o.filename, "Filename or URL to file to read a template")
	cmd.MarkFlagFilename("filename", "yaml", "yml", "json")
	cmd.Flags().StringArrayVarP(&o.templateParams, "param", "p", o.templateParams, "Specify a key-value pair (eg. -p FOO=BAR) to set/override a parameter value in the template.")
	cmd.Flags().StringArrayVar(&o.paramFile, "param-file", o.paramFile, "File containing template parameter values to set/override in the template.")
	cmd.MarkFlagFilename("param-file")
	cmd.Flags().BoolVar(&o.ignoreUnknownParams, "ignore-unknown-parameters", o.ignoreUnknownParams, "If true, will not stop processing if a provided parameter does not exist in the template.")
	cmd.Flags().BoolVarP(&o.local, "local", "", o.local, "If true process the template locally instead of contacting the server.")
	cmd.Flags().BoolVarP(&o.parameters, "parameters", "", o.parameters, "If true, do not process but only print available parameters")
	cmd.Flags().StringVarP(&o.labels, "labels", "l", o.labels, "Label to set in all resources for this template")

	cmd.Flags().BoolVar(&o.raw, "raw", o.raw, "If true, output the processed template instead of the template's objects. Implied by -o describe")

	return cmd
}

// processPrinter can handle printing the "describe" outputFormat
type processPrinter struct {
	printFlags   *genericclioptions.PrintFlags
	outputFormat string
}

func (p *processPrinter) PrintObj(obj runtime.Object, out io.Writer) error {
	if p.outputFormat == "describe" {
		templateObj, ok := obj.(*templatev1.Template)
		if !ok {
			return fmt.Errorf("attempt to describe a non-template object of type %T", obj)
		}

		s, err := (&describe.TemplateDescriber{
			MetadataAccessor: meta.NewAccessor(),
			ObjectTyper:      scheme.Scheme,
			ObjectDescriber:  nil,
		}).DescribeTemplate(templateObj)

		if err != nil {
			return fmt.Errorf("error describing %q: %v\n", templateObj.Name, err)
		}

		fmt.Fprintf(out, s)
		return nil
	}

	printer, err := p.printFlags.ToPrinter()
	if err != nil {
		return err
	}

	return printer.PrintObj(obj, out)
}

func (o *ProcessOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	o.outputFormat = kcmdutil.GetFlagString(cmd, "output")

	o.Printer = &processPrinter{
		printFlags:   o.PrintFlags,
		outputFormat: o.outputFormat,
	}

	o.usageErrorFn = func(format string, args ...interface{}) error {
		return kcmdutil.UsageErrorf(cmd, format, args)
	}

	o.paramValuesProvided = cmd.Flag("param").Changed

	templateName, templateParams := "", []string{}
	for _, s := range args {
		isValue := strings.Contains(s, "=")
		switch {
		case isValue:
			templateParams = append(templateParams, s)
		case !isValue && len(templateName) == 0:
			templateName = s
		case !isValue && len(templateName) > 0:
			return kcmdutil.UsageErrorf(cmd, "template name must be specified only once: %s", s)
		}
	}

	o.templateName = templateName

	o.templateParams = append(o.templateParams, templateParams...)
	if o.paramValuesProvided {
		cmdutil.WarnAboutCommaSeparation(o.ErrOut, o.templateParams, "--param")
	}

	var err error
	o.namespace, o.explicitNamespace, err = f.ToRawKubeConfigLoader().Namespace()
	// we only need to fail on namespace acquisition if we're actually taking action.  Otherwise the namespace can be enforced later
	if err != nil && !o.local {
		return err
	}

	o.builderFn = f.NewBuilder

	o.templateProcessor = processTemplateLocally
	if !o.local {
		clientConfig, err := f.ToRESTConfig()
		if err != nil {
			return err
		}

		o.templateClient, err = templatev1client.NewForConfig(clientConfig)
		if err != nil {
			return err
		}

		templateProcessorClient := templateprocessorclient.NewTemplateProcessorClient(o.templateClient.RESTClient(), o.namespace)

		o.templateProcessor = func(template *templatev1.Template) (*templatev1.Template, error) {
			t, err := templateProcessorClient.Process(template)
			if err != nil {
				if err, ok := err.(*errors.StatusError); ok && err.ErrStatus.Details != nil {
					errstr := "unable to process template\n"
					for _, cause := range err.ErrStatus.Details.Causes {
						errstr += fmt.Sprintf("  %s\n", cause.Message)
					}

					// if no error causes found, fallback to returning original
					// error message received from the server
					if len(err.ErrStatus.Details.Causes) == 0 {
						errstr += fmt.Sprintf("  %v\n", err)
					}

					return nil, fmt.Errorf(errstr)
				}

				return nil, fmt.Errorf("unable to process template: %v\n", err)
			}

			return t, nil
		}
	}

	return nil
}

func (o *ProcessOptions) Validate(cmd *cobra.Command) error {
	if o.parameters {
		for _, flag := range []string{"param", "labels", "output", "output-version", "raw", "template"} {
			if f := cmd.Flags().Lookup(flag); f != nil && f.Changed {
				return kcmdutil.UsageErrorf(cmd, "The --parameters flag does not process the template, can't be used with --%v", flag)
			}
		}
	}

	if len(o.templateName) > 0 && o.local {
		return kcmdutil.UsageErrorf(cmd, "You may only specify a local template file via -f when running this command with --local")
	}

	return nil
}

// RunProcess contains all the necessary functionality for the OpenShift cli process command
func (o *ProcessOptions) RunProcess() error {
	duplicatedKeys := sets.NewString()
	params, paramErr := app.ParseAndCombineEnvironment(o.templateParams, o.paramFile, o.In, func(key, file string) error {
		if file == "" {
			duplicatedKeys.Insert(key)
		} else {
			fmt.Fprintf(o.ErrOut, "warning: Template parameter %q already defined, ignoring value from file %q\n", key, file)
		}
		return nil
	})
	if len(duplicatedKeys) != 0 {
		return o.usageErrorFn(fmt.Sprintf("The following parameters were provided more than once: %s", strings.Join(duplicatedKeys.List(), ", ")))
	}

	if len(o.templateName) == 0 && len(o.filename) == 0 {
		return o.usageErrorFn("Must pass a filename or name of stored template")
	}

	var infos []*resource.Info

	// When templateName is not empty, then we fetch the template from the
	// server, otherwise we require to set the `-f` parameter.
	if len(o.templateName) > 0 && !o.local {
		if o.templateClient == nil {
			return fmt.Errorf("attempt to fetch template from server with nil template client")
		}

		var (
			storedTemplate, rs string
			sourceNamespace    string
			ok                 bool
		)
		sourceNamespace, rs, storedTemplate, ok = parseNamespaceResourceName(o.templateName, o.namespace)
		if !ok {
			return fmt.Errorf("invalid argument %q", o.templateName)
		}
		if len(rs) > 0 && (rs != "template" && rs != "templates") {
			return fmt.Errorf("unable to process invalid resource %q", rs)
		}
		if len(storedTemplate) == 0 {
			return fmt.Errorf("invalid value syntax %q", o.templateName)
		}

		templateObj, err := o.templateClient.Templates(sourceNamespace).Get(context.TODO(), storedTemplate, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return fmt.Errorf("template %q could not be found", storedTemplate)
			}
			return err
		}
		templateObj.CreationTimestamp = metav1.Now()
		infos = append(infos, &resource.Info{Object: templateObj})
	} else {
		var err error
		infos, err = o.builderFn().
			WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
			LocalParam(o.local).
			FilenameParam(o.explicitNamespace, &resource.FilenameOptions{Recursive: false, Filenames: []string{o.filename}}).
			Do().
			Infos()
		if err != nil {
			return fmt.Errorf("failed to read input object (not a Template?): %v", err)
		}
	}

	if len(infos) > 1 {
		// in order to run validation on the input given to us by a user, we only support the processing
		// of one template in a list. For instance, we want to be able to fail when a user does not give
		// a parameter that the template wants or when they give a parameter the template doesn't need,
		// as this may indicate that they have mis-used `oc process`. This is much less complicated when
		// we process at most one template.
		fmt.Fprintf(o.Out, "%d input templates found, but only the first will be processed\n", len(infos))
	}

	obj, ok := infos[0].Object.(*templatev1.Template)
	if !ok {
		sourceName := o.filename
		if len(o.templateName) > 0 {
			sourceName = o.namespace + "/" + o.templateName
		}
		return fmt.Errorf("unable to parse %q, not a valid Template but %s\n", sourceName, reflect.TypeOf(infos[0].Object))
	}

	// If 'parameters' flag is set it does not do processing but only print
	// the template parameters to console for inspection.
	if o.parameters {
		return describe.PrintTemplateParameters(obj.Parameters, o.Out)
	}

	if label := o.labels; len(label) > 0 {
		lbl, err := generate.ParseLabels(label)
		if err != nil {
			return fmt.Errorf("error parsing labels: %v\n", err)
		}
		if obj.ObjectLabels == nil {
			obj.ObjectLabels = make(map[string]string)
		}
		for key, value := range lbl {
			obj.ObjectLabels[key] = value
		}
	}

	// Raise parameter parsing errors here after we had chance to return UsageErrors first
	if paramErr != nil {
		return paramErr
	}
	if errs := injectUserVars(params, obj, o.ignoreUnknownParams); errs != nil {
		return kerrors.NewAggregate(errs)
	}

	resultObj := obj
	resultObj, err := o.templateProcessor(obj)
	if err != nil {
		return err
	}

	if o.outputFormat == "describe" {
		return o.Printer.PrintObj(resultObj, o.Out)
	}

	// the name printer does not accept object lists, so re-use
	// the print loop used for --raw printing instead.
	if o.outputFormat == "name" || o.raw {
		for _, obj := range resultObj.Objects {
			objToPrint := obj.Object

			if objToPrint == nil {
				converted, err := runtime.Decode(unstructured.UnstructuredJSONScheme, obj.Raw)
				if err != nil {
					return err
				}

				objToPrint = converted
			}

			if err := o.Printer.PrintObj(objToPrint, o.Out); err != nil {
				return err
			}
		}

		return nil
	}

	return o.Printer.PrintObj(&corev1.List{
		ListMeta: metav1.ListMeta{},
		Items:    resultObj.Objects,
	}, o.Out)
}

// injectUserVars injects user specified variables into the Template
func injectUserVars(values app.Environment, t *templatev1.Template, ignoreUnknownParameters bool) []error {
	var errors []error
	for param, val := range values {
		v := templateprocessing.GetParameterByName(t, param)
		if v != nil {
			v.Value = val
			v.Generate = ""
		} else if !ignoreUnknownParameters {
			errors = append(errors, fmt.Errorf("unknown parameter name %q\n", param))
		}
	}
	return errors
}

// processTemplateLocally applies the same logic that a remote call would make but makes no
// connection to the server.
func processTemplateLocally(tpl *templatev1.Template) (*templatev1.Template, error) {
	processor := templateprocessing.NewProcessor(map[string]generator.Generator{
		"expression": generator.NewExpressionValueGenerator(rand.New(rand.NewSource(time.Now().UnixNano()))),
	})
	if errs := processor.Process(tpl); len(errs) > 0 {
		return nil, errors.NewInvalid(octemplateapi.Kind("Template"), tpl.Name, errs)
	}

	return tpl, nil
}

// parseNamespaceResourceName parses the value and returns namespace, resource and the
// value (resource name) itself. The valid syntax is:
// oc process mytemplate - implicit namespace (current), implicit resource (template)
// oc process template/mytemplate - implicit namespace (current), explicit resource
// oc process ns/template/mytemplate - explicit namespace, explicit resource
// oc process ns//mytemplate - explicit namespace, implicit resource (template)
func parseNamespaceResourceName(v, defaultNamespace string) (ns, resource, name string, ok bool) {
	parts := strings.Split(strings.TrimSpace(v), "/")
	switch len(parts) {
	case 3:
		return parts[0], parts[1], parts[2], true
	case 2:
		return defaultNamespace, parts[0], parts[1], true
	case 1:
		return defaultNamespace, "", parts[0], true
	}
	return "", "", "", false
}
