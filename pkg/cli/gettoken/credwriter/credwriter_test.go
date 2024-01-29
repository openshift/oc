package credwriter

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func TestCredWriter(t *testing.T) {
	streams, _, out, _ := genericiooptions.NewTestIOStreams()
	writer := NewWriter(streams)
	tm, err := time.Parse(time.DateTime, "2006-01-02 15:04:05")
	if err != nil {
		t.Errorf("unexpected time parsing error %v", err)
	}
	err = writer.Write("test", tm)
	if err != nil {
		t.Errorf("unexpected credentials exec plugin write error %v", err)
	}
	expected := `{"kind":"ExecCredential","apiVersion":"client.authentication.k8s.io/v1","spec":{"interactive":false},"status":{"expirationTimestamp":"2006-01-02T15:04:05Z","token":"test"}}`
	expected = fmt.Sprintf("%s\n", expected)
	if out.String() != expected {
		t.Errorf("unexpected credentials exec writer output %s expected %s", out.String(), expected)
	}
}
