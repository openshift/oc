package completion

import (
	"fmt"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	completionLong = templates.LongDesc(i18n.T(`
		Output shell completion code for the specified shell (bash, zsh).
		The shell code must be evaluated to provide interactive
		completion of oc commands.  This can be done by sourcing it from
		the .bash_profile.

		Note for zsh users: [1] zsh completions are only supported in versions of zsh >= 5.2.`))

	completionExample = templates.Examples(i18n.T(`
		# Installing bash completion on macOS using homebrew
		## If running Bash 3.2 included with macOS
		brew install bash-completion
		## or, if running Bash 4.1+
		brew install bash-completion@2
		## If oc is installed via homebrew, this should start working immediately
		## If you've installed via other means, you may need add the completion to your completion directory
		oc completion bash > $(brew --prefix)/etc/bash_completion.d/oc


		# Installing bash completion on Linux
		## If bash-completion is not installed on Linux, install the 'bash-completion' package
		## via your distribution's package manager.
		## Load the oc completion code for bash into the current shell
		source <(oc completion bash)
		## Write bash completion code to a file and source it from .bash_profile
		oc completion bash > ~/.kube/completion.bash.inc
		printf "
		# Kubectl shell completion
		source '$HOME/.kube/completion.bash.inc'
		" >> $HOME/.bash_profile
		source $HOME/.bash_profile

		# Load the oc completion code for zsh[1] into the current shell
		source <(oc completion zsh)
		# Set the oc completion code for zsh[1] to autoload on startup
		oc completion zsh > "${fpath[1]}/_oc"`))

	completionShells = map[string]func(streams genericclioptions.IOStreams, cmd *cobra.Command) error{
		"bash": runCompletionBash,
		"zsh":  runCompletionZsh,
	}
)

// NewCmdCompletion creates the `completion` command
func NewCmdCompletion(streams genericclioptions.IOStreams) *cobra.Command {
	shells := []string{}
	for s := range completionShells {
		shells = append(shells, s)
	}

	cmd := &cobra.Command{
		Use:                   "completion SHELL",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Output shell completion code for the specified shell (bash or zsh"),
		Long:                  completionLong,
		Example:               completionExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(RunCompletion(streams, cmd, args))
		},
		ValidArgs: shells,
	}

	return cmd
}

// RunCompletion checks given arguments and executes command
func RunCompletion(streams genericclioptions.IOStreams, cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return kcmdutil.UsageErrorf(cmd, "Shell not specified.")
	}
	if len(args) > 1 {
		return kcmdutil.UsageErrorf(cmd, "Too many arguments. Expected only the shell type.")
	}
	run, found := completionShells[args[0]]
	if !found {
		return kcmdutil.UsageErrorf(cmd, "Unsupported shell type %q.", args[0])
	}

	return run(streams, cmd.Parent())
}

func runCompletionBash(streams genericclioptions.IOStreams, cmd *cobra.Command) error {
	return cmd.GenBashCompletion(streams.Out)
}

func runCompletionZsh(streams genericclioptions.IOStreams, cmd *cobra.Command) error {
	zshHead := fmt.Sprintf("#compdef %[1]s\ncompdef _%[1]s %[1]s\n", cmd.Name())
	streams.Out.Write([]byte(zshHead))

	return cmd.GenZshCompletion(streams.Out)
}
