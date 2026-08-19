package kubectlwrappers

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/kubectl/pkg/cmd/delete"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	cmdutil "github.com/openshift/oc/pkg/helpers/cmd"
	"github.com/openshift/oc/pkg/helpers/term"
)

const storageDeleteWarning = `Warning: Deleting a PV or PVC may result in application downtime, data loss or workload failure if it is mounted by any active workload.
Are you sure you want to proceed? (y/N): `

// pvOrPVCKindRE matches YAML/JSON kind fields for PersistentVolume and PersistentVolumeClaim.
var pvOrPVCKindRE = regexp.MustCompile(`(?i)kind["']?\s*:\s*["']?PersistentVolume(Claim)?["']?`)

// NewCmdDelete is a wrapper for the Kubernetes cli delete command.
//
// When deleting persistent volumes or persistent volume claims from a terminal,
// oc prints a warning and requires explicit confirmation (RFE-8872). Scripts and
// other non-interactive sessions are unchanged. Pass --interactive=false to skip
// the prompt in a terminal.
func NewCmdDelete(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(delete.NewCmdDelete(f, streams)))
	cmd.Long = strings.Trim(cmd.Long+"\n\n"+templates.LongDesc(`
		When deleting persistent volumes (pv) or persistent volume claims (pvc) from a terminal,
		oc warns that the operation may disrupt workloads or cause data loss and asks for confirmation.
		The default answer is no. Confirmation is skipped for non-interactive sessions (for example CI
		scripts) and when --interactive=false is set.`), " \t\n")

	originalRun := cmd.Run
	cmd.Run = func(c *cobra.Command, args []string) {
		kcmdutil.CheckErr(runDeleteWithStorageConfirmation(c, args, streams, originalRun, term.IsTerminalReader(streams.In)))
	}
	return cmd
}

func runDeleteWithStorageConfirmation(cmd *cobra.Command, args []string, streams genericiooptions.IOStreams, originalRun func(*cobra.Command, []string), isTerminal bool) error {
	opts, err := storageDeleteConfirmOptionsFromCmd(cmd, args, isTerminal)
	if err != nil {
		// Invalid flags such as --dry-run are reported by the wrapped command.
		originalRun(cmd, args)
		return nil
	}

	if opts.shouldWarn() {
		if !confirmStorageDelete(streams.In, streams.ErrOut) {
			fmt.Fprintf(streams.Out, "deletion is cancelled\n")
			return nil
		}
	}

	originalRun(cmd, args)
	return nil
}

type storageDeleteConfirmOptions struct {
	Args               []string
	Filenames          []string
	Interactive        bool
	InteractiveChanged bool
	DryRun             kcmdutil.DryRunStrategy
	Raw                string
	IsTerminal         bool
}

func storageDeleteConfirmOptionsFromCmd(cmd *cobra.Command, args []string, isTerminal bool) (storageDeleteConfirmOptions, error) {
	opts := storageDeleteConfirmOptions{
		Args:       args,
		IsTerminal: isTerminal,
	}

	dryRun, err := kcmdutil.GetDryRunStrategy(cmd)
	if err != nil {
		return opts, err
	}
	opts.DryRun = dryRun

	if cmd.Flags().Lookup("raw") != nil {
		opts.Raw, _ = cmd.Flags().GetString("raw")
	}
	if cmd.Flags().Lookup("filename") != nil {
		opts.Filenames, _ = cmd.Flags().GetStringSlice("filename")
	}
	if f := cmd.Flags().Lookup("interactive"); f != nil {
		opts.InteractiveChanged = f.Changed
		opts.Interactive, _ = cmd.Flags().GetBool("interactive")
	}

	return opts, nil
}

func (o storageDeleteConfirmOptions) shouldWarn() bool {
	if !o.IsTerminal {
		return false
	}
	if o.Raw != "" {
		return false
	}
	if o.DryRun != kcmdutil.DryRunNone {
		return false
	}
	if o.InteractiveChanged {
		// Honor an explicit --interactive setting: kubectl prompts when true,
		// and the user opted out when false.
		return false
	}
	return argsOrFilesIncludePVOrPVC(o.Args, o.Filenames)
}

func argsOrFilesIncludePVOrPVC(args, filenames []string) bool {
	if argsIncludePVOrPVC(args) {
		return true
	}
	for _, filename := range filenames {
		if fileContainsPVOrPVC(filename) {
			return true
		}
	}
	return false
}

func argsIncludePVOrPVC(args []string) bool {
	if len(args) == 0 {
		return false
	}

	hasResourceName := false
	for _, arg := range args {
		if strings.Contains(arg, "/") {
			hasResourceName = true
			break
		}
	}

	if hasResourceName {
		for _, arg := range args {
			for _, part := range strings.Split(arg, ",") {
				resource := strings.TrimSpace(part)
				if i := strings.Index(resource, "/"); i >= 0 {
					resource = resource[:i]
				}
				if isPVOrPVCResource(resource) {
					return true
				}
			}
		}
		return false
	}

	for _, resource := range strings.Split(args[0], ",") {
		if isPVOrPVCResource(resource) {
			return true
		}
	}
	return false
}

func isPVOrPVCResource(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if i := strings.Index(name, "."); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "pv", "persistentvolume", "persistentvolumes",
		"pvc", "persistentvolumeclaim", "persistentvolumeclaims":
		return true
	default:
		return false
	}
}

func fileContainsPVOrPVC(path string) bool {
	if path == "" || path == "-" {
		return false
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return false
	}

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}

	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return false
	}
	return pvOrPVCKindRE.Match(data)
}

func confirmStorageDelete(in io.Reader, out io.Writer) bool {
	fmt.Fprint(out, storageDeleteWarning)
	var input string
	if _, err := fmt.Fscanln(in, &input); err != nil {
		return false
	}
	return strings.EqualFold(input, "y")
}
