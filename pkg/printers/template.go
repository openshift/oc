package printers

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kprinters "k8s.io/kubernetes/pkg/printers"

	templatev1 "github.com/openshift/api/template/v1"
)

const templateDescriptionLen = 80

func AddTemplateOpenShiftHandlers(h kprinters.PrintHandler) {
	templateColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Description", Type: "string", Description: "Template description."},
		{Name: "Parameters", Type: "string", Description: templatev1.Template{}.SwaggerDoc()["parameters"]},
		{Name: "Objects", Type: "string", Description: "Number of resources in this template."},
	}
	if err := h.TableHandler(templateColumnDefinitions, printTemplateList); err != nil {
		panic(err)
	}
	if err := h.TableHandler(templateColumnDefinitions, printTemplate); err != nil {
		panic(err)
	}

	templateInstanceColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Template", Type: "string", Description: "Template name to be instantiated."},
	}
	if err := h.TableHandler(templateInstanceColumnDefinitions, printTemplateInstanceList); err != nil {
		panic(err)
	}
	if err := h.TableHandler(templateInstanceColumnDefinitions, printTemplateInstance); err != nil {
		panic(err)
	}

	brokerTemplateInstanceColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Template Instance", Type: "string", Description: "Template instance name."},
	}
	if err := h.TableHandler(brokerTemplateInstanceColumnDefinitions, printBrokerTemplateInstanceList); err != nil {
		panic(err)
	}
	if err := h.TableHandler(brokerTemplateInstanceColumnDefinitions, printBrokerTemplateInstance); err != nil {
		panic(err)
	}
}

func printTemplate(t *templatev1.Template, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: t},
	}

	description := ""
	if t.Annotations != nil {
		description = t.Annotations["description"]
	}
	// Only print the first line of description
	if lines := strings.SplitN(description, "\n", 2); len(lines) > 1 {
		description = lines[0] + "..."
	}
	if len(description) > templateDescriptionLen {
		description = strings.TrimSpace(description[:templateDescriptionLen-3]) + "..."
	}
	empty, generated, total := 0, 0, len(t.Parameters)
	for _, p := range t.Parameters {
		if len(p.Value) > 0 {
			continue
		}
		if len(p.Generate) > 0 {
			generated++
			continue
		}
		empty++
	}
	params := ""
	switch {
	case empty > 0:
		params = fmt.Sprintf("%d (%d blank)", total, empty)
	case generated > 0:
		params = fmt.Sprintf("%d (%d generated)", total, generated)
	default:
		params = fmt.Sprintf("%d (all set)", total)
	}

	name := formatResourceName(options.Kind, t.Name, options.WithKind)

	row.Cells = append(row.Cells, name, description, params, len(t.Objects))

	return []metav1.TableRow{row}, nil
}

func printTemplateList(list *templatev1.TemplateList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printTemplate(&list.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printTemplateInstance(templateInstance *templatev1.TemplateInstance, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: templateInstance},
	}

	name := formatResourceName(options.Kind, templateInstance.Name, options.WithKind)

	row.Cells = append(row.Cells, name, templateInstance.Spec.Template.Name)

	return []metav1.TableRow{row}, nil
}

func printTemplateInstanceList(list *templatev1.TemplateInstanceList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printTemplateInstance(&list.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printBrokerTemplateInstance(brokerTemplateInstance *templatev1.BrokerTemplateInstance, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: brokerTemplateInstance},
	}

	name := formatResourceName(options.Kind, brokerTemplateInstance.Name, options.WithKind)

	row.Cells = append(row.Cells, name, brokerTemplateInstance.Spec.TemplateInstance.Namespace, brokerTemplateInstance.Spec.TemplateInstance.Name)

	return []metav1.TableRow{row}, nil
}

func printBrokerTemplateInstanceList(list *templatev1.BrokerTemplateInstanceList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printBrokerTemplateInstance(&list.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}
