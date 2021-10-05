package rsh

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/cmd/exec"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util"
	"k8s.io/kubectl/pkg/util/templates"
	"k8s.io/kubectl/pkg/util/term"
)

const (
	DefaultShell         = "/bin/sh"
	defaultPodRshTimeout = 60 * time.Second
)

var (
	rshUsageStr    = "rsh [-c CONTAINER] [flags] (POD | TYPE/NAME) COMMAND [args...]"
	rshUsageErrStr = fmt.Sprintf("expected '%s'.\nPOD or TYPE/NAME is a required argument for the rsh command", rshUsageStr)

	rshLong = templates.LongDesc(`
		Open a remote shell session to a container.

		This command will attempt to start a shell session in a pod for the specified resource.
		It works with pods, deployment configs, deployments, jobs, daemon sets, replication controllers
		and replica sets.
		Any of the aforementioned resources (apart from pods) will be resolved to a ready pod.
		It will default to the first container if none is specified, and will attempt to use
		'/bin/sh' as the default shell. You may pass any flags supported by this command before
		the resource name, and an optional command after the resource name, which will be executed
		instead of a login shell. A TTY will be automatically allocated if standard input is
		interactive - use -t and -T to override. A TERM variable is sent to the environment where
		the shell (or command) will be executed. By default its value is the same as the TERM
		variable from the local environment; if not set, 'xterm' is used.

		Note, some containers may not include a shell - use 'oc exec' if you need to run commands
		directly.`)

	rshExample = templates.Examples(`
		# Open a shell session on the first container in pod 'foo'
		oc rsh foo

		# Open a shell session on the first container in pod 'foo' and namespace 'bar'
		# (Note that oc client specific arguments must come before the resource name and its arguments)
		oc rsh -n bar foo

		# Run the command 'cat /etc/resolv.conf' inside pod 'foo'
		oc rsh foo cat /etc/resolv.conf

		# See the configuration of your internal registry
		oc rsh dc/docker-registry cat config.yml

		# Open a shell session on the container named 'index' inside a pod of your job
		oc rsh -c index job/sheduled
	`)
)

// RshOptions declare the arguments accepted by the Rsh command
type RshOptions struct {
	ForceTTY   bool
	DisableTTY bool
	Executable string
	*exec.ExecOptions
}

func NewRshOptions(streams genericclioptions.IOStreams) *RshOptions {
	return &RshOptions{
		ForceTTY:   false,
		DisableTTY: false,
		Executable: DefaultShell,
		ExecOptions: &exec.ExecOptions{
			StreamOptions: exec.StreamOptions{
				IOStreams: streams,
				TTY:       true,
				Stdin:     true,
			},

			Executor: &exec.DefaultRemoteExecutor{},
		},
	}
}

// NewCmdRsh returns a command that attempts to open a shell session to the server.
func NewCmdRsh(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewRshOptions(streams)
	cmd := &cobra.Command{
		Use:                   rshUsageStr,
		DisableFlagsInUseLine: true,
		Short:                 "Start a shell session in a container",
		Long:                  rshLong,
		Example:               rshExample,
		ValidArgsFunction:     util.PodResourceNameAndContainerCompletionFunc(f),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}
	kcmdutil.AddPodRunningTimeoutFlag(cmd, defaultPodRshTimeout)
	kcmdutil.AddJsonFilenameFlag(cmd.Flags(), &o.FilenameOptions.Filenames, "to use to rsh into the resource")
	cmd.Flags().BoolVarP(&o.ForceTTY, "tty", "t", o.ForceTTY, "Force a pseudo-terminal to be allocated")
	cmd.Flags().BoolVarP(&o.DisableTTY, "no-tty", "T", o.DisableTTY, "Disable pseudo-terminal allocation")
	cmd.Flags().StringVar(&o.Executable, "shell", o.Executable, "Path to the shell command")
	cmd.Flags().StringVarP(&o.ContainerName, "container", "c", o.ContainerName, "Container name; defaults to first container")
	// For consistencty with rsh API (https://linux.die.net/man/1/rsh) we don't
	// allow '--' and we need this flag enabled explicitly, otherwise two things
	// will break:
	// 1. '--' will start to work and we don't want that, although when specifying resource
	//    using -f we still need to ensure it's not showing up.
	// 2. this stops parsing when it encounters first non-flag argument, and that's
	//    critical for calls like this:
	//    oc rsh must-gather-rddfk rsync --server --sender -vlDtpre.iLsfxC --numeric-ids ...
	//    without below the above example will fail, cobra will try to parse -vlDtpre.iLsfxC
	//    and will fail because it won't be able to convert that into int.
	cmd.Flags().SetInterspersed(false)
	return cmd
}

// Complete applies the command environment to RshOptions
func (o *RshOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	argsLenAtDash := cmd.ArgsLenAtDash()
	if len(args) == 0 && argsLenAtDash == 0 && len(o.FilenameOptions.Filenames) == 0 {
		return kcmdutil.UsageErrorf(cmd, "%s", rshUsageErrStr)
	}
	// this check ensures we don't accept invocation with '--' in it, iow.
	// 'oc rsh pod -- date' nor 'oc rsh -f pod.yaml -- date'
	if argsLenAtDash != -1 || (len(args) > 0 && args[0] == "--") || (len(args) > 1 && args[1] == "--") {
		return kcmdutil.UsageErrorf(cmd, "%s", rshUsageErrStr)
	}

	switch {
	case o.ForceTTY && o.DisableTTY:
		return kcmdutil.UsageErrorf(cmd, "you may not specify -t and -T together")
	case o.ForceTTY:
		o.TTY = true
	case o.DisableTTY:
		o.TTY = false
	default:
		o.TTY = term.IsTerminal(o.In)
	}

	// Value of argsLenAtDash is -1 since cmd.ArgsLenAtDash() assumes all the flags
	// of flag.FlagSet were parsed. The opposite is true. Thus, it needs to be computed manually.
	// In case the command is present, the first item in args is a pod name,
	// the rest is a command and its arguments.
	// kubectl exec expects the command to be preceded by '--'.
	// oc rsh always provides the command as the second item of args.
	if len(args) > 1 {
		argsLenAtDash = 1
	}

	if err := o.ExecOptions.Complete(f, cmd, args, argsLenAtDash); err != nil {
		return err
	}

	// overwrite ExecOptions with rsh specifics
	if len(args) > 0 && len(o.FilenameOptions.Filenames) != 0 {
		o.Command = args[0:]
		o.ResourceName = ""
	} else if len(args) > 1 {
		o.Command = args[1:]
	} else {
		o.Command = []string{o.Executable}
	}

	return nil
}

// Validate ensures that RshOptions are valid
func (o *RshOptions) Validate() error {
	return o.ExecOptions.Validate()
}

// Run starts a remote shell session on the server
func (o *RshOptions) Run() error {
	// Insert the TERM into the command to be run
	if len(o.Command) == 1 && o.Command[0] == DefaultShell {
		term := os.Getenv("TERM")
		if len(term) == 0 {
			term = "xterm"
		}
		termsh := fmt.Sprintf("TERM=%q %s", term, DefaultShell)
		o.Command = append(o.Command, "-c", termsh)
	}
	return o.ExecOptions.Run()
}
