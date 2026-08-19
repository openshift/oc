package kubectlwrappers

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
)

func TestArgsIncludePVOrPVC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "empty", args: nil, want: false},
		{name: "pod by type and name", args: []string{"pod", "foo"}, want: false},
		{name: "pvc short name", args: []string{"pvc", "data"}, want: true},
		{name: "pv short name", args: []string{"pv", "pv-1"}, want: true},
		{name: "persistentvolumeclaim", args: []string{"persistentvolumeclaim", "data"}, want: true},
		{name: "persistentvolumes plural", args: []string{"persistentvolumes", "pv-1"}, want: true},
		{name: "mixed types including pvc", args: []string{"pod,pvc", "foo"}, want: true},
		{name: "resource/name pvc", args: []string{"pvc/data"}, want: true},
		{name: "resource/name pv among pods", args: []string{"pod/foo", "pv/bar"}, want: true},
		{name: "resource/name pods only", args: []string{"pod/foo", "svc/bar"}, want: false},
		{name: "case insensitive", args: []string{"PersistentVolumeClaim", "data"}, want: true},
		{name: "name that looks like pvc", args: []string{"pod", "pvc"}, want: false},
		{name: "qualified kind", args: []string{"persistentvolumeclaims.v1", "data"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := argsIncludePVOrPVC(tt.args); got != tt.want {
				t.Errorf("argsIncludePVOrPVC(%q) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestIsPVOrPVCResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{name: "pv", want: true},
		{name: "pvc", want: true},
		{name: "PersistentVolume", want: true},
		{name: "persistentvolumeclaims", want: true},
		{name: "pod", want: false},
		{name: "service", want: false},
		{name: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isPVOrPVCResource(tt.name); got != tt.want {
				t.Errorf("isPVOrPVCResource(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestFileContainsPVOrPVC(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pvcFile := filepath.Join(dir, "pvc.yaml")
	podFile := filepath.Join(dir, "pod.yaml")
	jsonFile := filepath.Join(dir, "pvc.json")

	if err := os.WriteFile(pvcFile, []byte("apiVersion: v1\nkind: PersistentVolumeClaim\nmetadata:\n  name: data\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(podFile, []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: nginx\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonFile, []byte(`{"apiVersion":"v1","kind":"PersistentVolume","metadata":{"name":"pv1"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "pvc yaml", path: pvcFile, want: true},
		{name: "pod yaml", path: podFile, want: false},
		{name: "pv json", path: jsonFile, want: true},
		{name: "stdin", path: "-", want: false},
		{name: "url", path: "https://example.com/pvc.yaml", want: false},
		{name: "missing", path: filepath.Join(dir, "missing.yaml"), want: false},
		{name: "directory", path: dir, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := fileContainsPVOrPVC(tt.path); got != tt.want {
				t.Errorf("fileContainsPVOrPVC(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestShouldWarn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts storageDeleteConfirmOptions
		want bool
	}{
		{
			name: "tty pvc",
			opts: storageDeleteConfirmOptions{Args: []string{"pvc", "data"}, IsTerminal: true, DryRun: kcmdutil.DryRunNone},
			want: true,
		},
		{
			name: "non-tty pvc",
			opts: storageDeleteConfirmOptions{Args: []string{"pvc", "data"}, IsTerminal: false, DryRun: kcmdutil.DryRunNone},
			want: false,
		},
		{
			name: "tty pod",
			opts: storageDeleteConfirmOptions{Args: []string{"pod", "foo"}, IsTerminal: true, DryRun: kcmdutil.DryRunNone},
			want: false,
		},
		{
			name: "dry-run client",
			opts: storageDeleteConfirmOptions{Args: []string{"pvc", "data"}, IsTerminal: true, DryRun: kcmdutil.DryRunClient},
			want: false,
		},
		{
			name: "raw request",
			opts: storageDeleteConfirmOptions{Args: []string{"pvc", "data"}, IsTerminal: true, Raw: "/api/v1/persistentvolumeclaims/data", DryRun: kcmdutil.DryRunNone},
			want: false,
		},
		{
			name: "explicit interactive false",
			opts: storageDeleteConfirmOptions{Args: []string{"pvc", "data"}, IsTerminal: true, InteractiveChanged: true, Interactive: false, DryRun: kcmdutil.DryRunNone},
			want: false,
		},
		{
			name: "explicit interactive true uses kubectl prompt",
			opts: storageDeleteConfirmOptions{Args: []string{"pvc", "data"}, IsTerminal: true, InteractiveChanged: true, Interactive: true, DryRun: kcmdutil.DryRunNone},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.opts.shouldWarn(); got != tt.want {
				t.Errorf("shouldWarn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfirmStorageDelete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "y", input: "y\n", want: true},
		{name: "Y", input: "Y\n", want: true},
		{name: "n", input: "n\n", want: false},
		{name: "empty", input: "\n", want: false},
		{name: "yes is not accepted", input: "yes\n", want: false},
		{name: "eof", input: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			got := confirmStorageDelete(strings.NewReader(tt.input), &out)
			if got != tt.want {
				t.Errorf("confirmStorageDelete(%q) = %v, want %v", tt.input, got, tt.want)
			}
			if !strings.Contains(out.String(), "Warning:") {
				t.Errorf("prompt %q does not contain the required warning", out.String())
			}
			if !strings.Contains(out.String(), "(y/N)") {
				t.Errorf("prompt %q does not default to no", out.String())
			}
		})
	}
}

func TestRunDeleteWithStorageConfirmationCancel(t *testing.T) {
	t.Parallel()

	var ran bool
	cmd := newTestDeleteCommand()
	in := strings.NewReader("n\n")
	var out, errOut bytes.Buffer
	streams := genericiooptions.IOStreams{In: in, Out: &out, ErrOut: &errOut}

	err := runDeleteWithStorageConfirmation(cmd, []string{"pvc", "data"}, streams, func(*cobra.Command, []string) {
		ran = true
	}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran {
		t.Fatal("wrapped delete ran after the user declined")
	}
	if !strings.Contains(out.String(), "deletion is cancelled") {
		t.Errorf("stdout %q does not report cancellation", out.String())
	}
}

func TestRunDeleteWithStorageConfirmationProceed(t *testing.T) {
	t.Parallel()

	var gotArgs []string
	cmd := newTestDeleteCommand()
	in := strings.NewReader("y\n")
	var out, errOut bytes.Buffer
	streams := genericiooptions.IOStreams{In: in, Out: &out, ErrOut: &errOut}

	err := runDeleteWithStorageConfirmation(cmd, []string{"pvc", "data"}, streams, func(_ *cobra.Command, args []string) {
		gotArgs = append([]string(nil), args...)
	}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff := cmp.Diff([]string{"pvc", "data"}, gotArgs); diff != "" {
		t.Errorf("wrapped delete args mismatch (-want +got):\n%s", diff)
	}
}

func TestRunDeleteWithStorageConfirmationNonStorage(t *testing.T) {
	t.Parallel()

	var ran bool
	cmd := newTestDeleteCommand()
	in := strings.NewReader("")
	var out, errOut bytes.Buffer
	streams := genericiooptions.IOStreams{In: in, Out: &out, ErrOut: &errOut}

	err := runDeleteWithStorageConfirmation(cmd, []string{"pod", "foo"}, streams, func(*cobra.Command, []string) {
		ran = true
	}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Fatal("wrapped delete did not run for a non-storage resource")
	}
	if errOut.Len() != 0 {
		t.Errorf("unexpected warning for pod delete: %q", errOut.String())
	}
}

func TestRunDeleteWithStorageConfirmationNonInteractive(t *testing.T) {
	t.Parallel()

	var ran bool
	cmd := newTestDeleteCommand()
	in := strings.NewReader("")
	var out, errOut bytes.Buffer
	streams := genericiooptions.IOStreams{In: in, Out: &out, ErrOut: &errOut}

	err := runDeleteWithStorageConfirmation(cmd, []string{"pvc", "data"}, streams, func(*cobra.Command, []string) {
		ran = true
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Fatal("wrapped delete did not run for a non-interactive pvc delete")
	}
	if errOut.Len() != 0 {
		t.Errorf("unexpected prompt in a non-interactive session: %q", errOut.String())
	}
}

func newTestDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete"}
	cmd.Flags().String("dry-run", "none", "")
	cmd.Flags().Lookup("dry-run").NoOptDefVal = "unchanged"
	cmd.Flags().String("raw", "", "")
	cmd.Flags().StringSlice("filename", nil, "")
	cmd.Flags().Bool("interactive", false, "")
	return cmd
}
