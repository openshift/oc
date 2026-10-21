package observe

import (
	"bytes"
	"testing"

	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func TestDumpMetricsDoesNotPanic(t *testing.T) {
	out := &bytes.Buffer{}
	o := &ObserveOptions{
		IOStreams:          genericiooptions.IOStreams{Out: out},
		printMetricsOnExit: true,
	}

	o.dumpMetrics()

	if out.Len() == 0 {
		t.Fatal("expected dumpMetrics to write metrics output")
	}
}
