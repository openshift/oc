package rsync

import (
	"io"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// mockExecutor implements the executor interface for testing.
type mockExecutor struct {
	t               *testing.T
	expectedCommand []string
	output          string
	err             error
}

func (m *mockExecutor) Execute(cmd []string, in io.Reader, out, errOut io.Writer) error {
	if !cmp.Equal(m.expectedCommand, cmd) {
		m.t.Errorf("unexpected remote command invoked: \n%s\n", cmp.Diff(m.expectedCommand, cmd))
	}
	if m.err != nil {
		return m.err
	}
	out.Write([]byte(m.output))
	return nil
}
