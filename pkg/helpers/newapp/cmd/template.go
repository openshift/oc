package cmd

import (
	"fmt"
	"io"
	"strings"

	templatev1 "github.com/openshift/api/template/v1"
	"github.com/openshift/library-go/pkg/template/templateprocessing"
	"github.com/openshift/oc/pkg/helpers/newapp/app"
	"github.com/openshift/oc/pkg/helpers/template/templateprocessorclient"
)

// TransformTemplateV1 processes a template with the provided parameters, returning an error if transformation fails.
func TransformTemplate(tpl *templatev1.Template, templateProcessor templateprocessorclient.TemplateProcessorInterface, namespace string, parameters map[string]string, ignoreUnknownParameters bool) (*templatev1.Template, error) {
	// only set values that match what's expected by the template.
	for k, value := range parameters {
		v := templateprocessing.GetParameterByName(tpl, k)
		if v != nil {
			v.Value = value
			v.Generate = ""
		} else if !ignoreUnknownParameters {
			return nil, fmt.Errorf("unexpected parameter name %q", k)
		}
	}

	name := localOrRemoteName(tpl.ObjectMeta, namespace)

	// transform the template
	result, err := templateProcessor.Process(tpl)
	if err != nil {
		return nil, fmt.Errorf("error processing template %q: %v", name, err)
	}

	return result, nil
}

func formatString(out io.Writer, tab, s string) {
	labelVals := strings.Split(strings.TrimSuffix(s, "\n"), "\n")

	for _, lval := range labelVals {
		fmt.Fprintf(out, "%s%s\n", tab, lval)
	}
}

// DescribeGeneratedTemplate writes a description of the provided template to out.
func DescribeGeneratedTemplate(out io.Writer, input string, result *templatev1.Template, baseNamespace string) {
	qualifiedName := localOrRemoteName(result.ObjectMeta, baseNamespace)
	if len(input) > 0 && result.ObjectMeta.Name != input {
		fmt.Fprintf(out, "--> Deploying template %q for %q to project %s\n", qualifiedName, input, baseNamespace)
	} else {
		fmt.Fprintf(out, "--> Deploying template %q to project %s\n", qualifiedName, baseNamespace)
	}
	fmt.Fprintln(out)

	name := displayName(result.ObjectMeta)
	message := result.Message
	description := result.Annotations["description"]

	// If there is a message or description
	if len(message) > 0 || len(description) > 0 {
		fmt.Fprintf(out, "     %s\n", name)
		fmt.Fprintf(out, "     ---------\n")
		if len(description) > 0 {
			formatString(out, "     ", description)
			fmt.Fprintln(out)
		}
		if len(message) > 0 {
			formatString(out, "     ", message)
			fmt.Fprintln(out)
		}
	}

	if warnings := result.Annotations[app.GenerationWarningAnnotation]; len(warnings) > 0 {
		delete(result.Annotations, app.GenerationWarningAnnotation)
		fmt.Fprintln(out)
		lines := strings.Split("warning: "+warnings, "\n")
		for _, line := range lines {
			fmt.Fprintf(out, "    %s\n", line)
		}
		fmt.Fprintln(out)
	}

	if len(result.Parameters) > 0 {
		fmt.Fprintf(out, "     * With parameters:\n")
		for _, p := range result.Parameters {
			name := p.DisplayName
			if len(name) == 0 {
				name = p.Name
			}
			var generated string
			if len(p.Generate) > 0 {
				generated = " # generated"
			}
			fmt.Fprintf(out, "        * %s=%s%s\n", name, p.Value, generated)
		}
		fmt.Fprintln(out)
	}
}
