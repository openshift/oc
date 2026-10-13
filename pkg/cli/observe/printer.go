package observe

import (
	"bytes"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/kubectl/pkg/scheme"
)

type printerWrapper struct {
	printer printers.ResourcePrinter
}

func (p printerWrapper) PrintObj(obj runtime.Object) ([]string, []byte, error) {
	data, err := runtime.Encode(scheme.DefaultJSONEncoder(), obj)
	if err != nil {
		return nil, nil, err
	}

	out := bytes.Buffer{}
	if err := p.printer.PrintObj(obj, &out); err != nil {
		return nil, nil, err
	}
	return []string{out.String()}, data, nil
}

type newlineTrailingWriter struct {
	w        io.Writer
	openLine bool
}

func (w *newlineTrailingWriter) Write(data []byte) (int, error) {
	if len(data) > 0 && data[len(data)-1] != '\n' {
		w.openLine = true
	}
	return w.w.Write(data)
}

func (w *newlineTrailingWriter) Flush() error {
	if w.openLine {
		w.openLine = false
		_, err := fmt.Fprintln(w.w)
		return err
	}
	return nil
}
