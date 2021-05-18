package gendocs

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/printers"
)

type Examples []*unstructured.Unstructured

func (x Examples) Len() int      { return len(x) }
func (x Examples) Swap(i, j int) { x[i], x[j] = x[j], x[i] }
func (x Examples) Less(i, j int) bool {
	xi, _ := x[i].Object["fullName"].(string)
	xj, _ := x[j].Object["fullName"].(string)
	return xi < xj
}

func GenDocs(cmd *cobra.Command, filename string, admin bool) error {
	out := new(bytes.Buffer)
	templatePath := "hack/clibyexample/template"
	if admin {
		templatePath = "hack/clibyexample/template-admin"
	}
	templateFile, err := filepath.Abs(templatePath)
	if err != nil {
		return err
	}
	template, err := ioutil.ReadFile(templateFile)
	if err != nil {
		return err
	}

	output := &unstructured.UnstructuredList{}
	output.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("List"))

	examples := extractExamples(cmd, admin)
	for i := range examples {
		output.Items = append(output.Items, *examples[i])
	}

	printer, err := printers.NewGoTemplatePrinter(template)
	if err != nil {
		return err
	}
	err = printer.PrintObj(output, out)
	if err != nil {
		return err
	}

	outFile, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer outFile.Close()

	_, err = outFile.Write(out.Bytes())
	if err != nil {
		return err
	}
	return nil
}

func extractExamples(cmd *cobra.Command, admin bool) Examples {
	objs := Examples{}
	for _, c := range cmd.Commands() {
		if len(c.Deprecated) > 0 {
			continue
		}
		if strings.HasPrefix(c.CommandPath(), "oc adm") && !admin {
			continue
		} else if !strings.HasPrefix(c.CommandPath(), "oc adm") && admin {
			continue
		} else {
			objs = append(objs, extractExamples(c, admin)...)
		}
	}
	if cmd.HasExample() {
		o := &unstructured.Unstructured{
			Object: make(map[string]interface{}),
		}
		o.Object["name"] = cmd.Name()
		o.Object["fullName"] = cmd.CommandPath()
		o.Object["description"] = cmd.Short
		o.Object["examples"] = cmd.Example
		objs = append(objs, o)
	}
	sort.Sort(objs)
	return objs
}
