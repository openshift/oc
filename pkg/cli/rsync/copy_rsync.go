package rsync

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	cmdutil "github.com/openshift/oc/pkg/helpers/cmd"
)

// rsyncStrategy implements the rsync copy strategy
// The rsync strategy calls the local rsync command directly and passes the OpenShift
// CLI rsh command as the remote shell command for rsync. It requires that rsync be
// present in both the client machine and the remote container.
type rsyncStrategy struct {
	Flags          []string
	RshCommand     string
	LocalExecutor  executor
	RemoteExecutor executor
	podChecker     podChecker

	Last          uint
	fileDiscovery fileDiscoverer
}

// DefaultRsyncRemoteShellToUse generates an command to create a remote shell.
func DefaultRsyncRemoteShellToUse(cmd *cobra.Command) string {
	// find the rsh command in the direct command path
	rshCmd := cmdutil.SiblingOrNiblingCommand(cmd, "rsh")
	// do not add local flags, unless also rsh flags to the command
	localFlags := sets.NewString()
	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		localFlags.Insert(flag.Name)
	})
	// flag.Name represents what was present on the CLI, so the excluded list needs
	// to have both short and long versions of flags
	excludeFlags := localFlags.Difference(sets.NewString("container", "c", "no-tty", "T", "shell", "tty", "t"))
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		if excludeFlags.Has(flag.Name) {
			return
		}
		if flag.Name == flag.Shorthand {
			rshCmd = append(rshCmd, fmt.Sprintf("-%s=%s", flag.Name, flag.Value.String()))
		} else {
			rshCmd = append(rshCmd, fmt.Sprintf("--%s=%s", flag.Name, flag.Value.String()))
		}
	})
	return strings.Join(rsyncEscapeCommand(rshCmd), " ")
}

// NewRsyncStrategy returns a copy strategy that uses rsync.
func NewRsyncStrategy(o *RsyncOptions) CopyStrategy {
	klog.V(4).Infof("Rsh command: %s", o.RshCmd)

	// The blocking-io flag is used to resolve a sync issue when
	// copying from the pod to the local machine
	flags := []string{"--blocking-io"}
	flags = append(flags, rsyncDefaultFlags...)
	flags = append(flags, rsyncFlagsFromOptions(o)...)

	podName := o.Source.PodName
	if o.Source.Local() {
		podName = o.Destination.PodName
	}

	return &rsyncStrategy{
		Flags:          flags,
		RshCommand:     o.RshCmd,
		RemoteExecutor: newRemoteExecutor(o),
		LocalExecutor:  newLocalExecutor(),
		podChecker:     podAPIChecker{o.Client, o.Namespace, podName, o.ContainerName, o.Quiet, o.ErrOut},
		Last:           o.Last,
		fileDiscovery:  o.fileDiscovery,
	}
}

func (r *rsyncStrategy) Copy(source, destination *PathSpec, out, errOut io.Writer) error {
	klog.V(3).Infof("Copying files with rsync")

	// In case --last is specified, discover the right files and pass them to rsync as an explicit list.
	var (
		in  io.Reader
		dst = destination.RsyncPath()
	)
	if r.Last > 0 {
		filenames, err := r.fileDiscovery.DiscoverFiles(source.Path, r.Last)
		if err != nil {
			klog.Infof("Warning: failed to apply --last filtering: %v", err)
		} else {
			var b bytes.Buffer
			for _, filename := range filenames {
				b.WriteString(filename)
				b.WriteRune('\n')
			}
			in = &b

			// Make dst compatible with what rsync does without --last.
			dst = filepath.Join(dst, filepath.Base(source.Path))

			klog.V(3).Infof("Applied --last=%d to rsync strategy: using %d files", r.Last, len(filenames))
		}
	}

	cmd := append([]string{"rsync"}, r.Flags...)
	if in != nil {
		cmd = append(cmd, "--files-from", "-")
	}
	cmd = append(cmd, "-e", r.RshCommand, source.RsyncPath(), dst)
	errBuf := &bytes.Buffer{}
	err := r.LocalExecutor.Execute(cmd, in, out, errBuf)
	if isExitError(err) {
		// Check if pod exists
		if podCheckErr := r.podChecker.CheckPod(); podCheckErr != nil {
			return podCheckErr
		}
		// Determine whether rsync is present in the pod container
		testRsyncErr := checkRsync(r.RemoteExecutor)
		if testRsyncErr != nil {
			return strategySetupError("rsync not available in container")
		}
	}
	io.Copy(errOut, errBuf)
	return err
}

func (r *rsyncStrategy) Validate() error {
	errs := []error{}
	if len(r.RshCommand) == 0 {
		errs = append(errs, errors.New("rsh command must be provided"))
	}
	if r.LocalExecutor == nil {
		errs = append(errs, errors.New("local executor must not be nil"))
	}
	if r.RemoteExecutor == nil {
		errs = append(errs, errors.New("remote executor must not be nil"))
	}
	if len(errs) > 0 {
		return kerrors.NewAggregate(errs)
	}
	return nil
}

// rsyncEscapeCommand wraps each element of the command in double quotes
// if it contains any of the following: single quote, double quote, or space
// It also replaces every pre-existing double quote character in the element with a pair.
// Example: " -> ""
func rsyncEscapeCommand(command []string) []string {
	var escapedCommand []string
	for _, val := range command {
		needsQuoted := strings.ContainsAny(val, `'" `)
		if needsQuoted {
			val = strings.Replace(val, `"`, `""`, -1)
			val = `"` + val + `"`
		}
		escapedCommand = append(escapedCommand, val)
	}
	return escapedCommand
}

func (r *rsyncStrategy) String() string {
	return "rsync"
}
