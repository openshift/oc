package cli

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/rest"
	kubecmd "k8s.io/kubectl/pkg/cmd"
	"k8s.io/kubectl/pkg/cmd/plugin"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/completion"
	ktemplates "k8s.io/kubectl/pkg/util/templates"
	kterm "k8s.io/kubectl/pkg/util/term"

	"github.com/openshift/oc/pkg/cli/admin"
	"github.com/openshift/oc/pkg/cli/cancelbuild"
	"github.com/openshift/oc/pkg/cli/debug"
	"github.com/openshift/oc/pkg/cli/deployer"
	"github.com/openshift/oc/pkg/cli/expose"
	"github.com/openshift/oc/pkg/cli/extract"
	"github.com/openshift/oc/pkg/cli/gettoken"
	"github.com/openshift/oc/pkg/cli/idle"
	"github.com/openshift/oc/pkg/cli/image"
	"github.com/openshift/oc/pkg/cli/importimage"
	"github.com/openshift/oc/pkg/cli/kubectlwrappers"
	"github.com/openshift/oc/pkg/cli/login"
	"github.com/openshift/oc/pkg/cli/logout"
	"github.com/openshift/oc/pkg/cli/logs"
	"github.com/openshift/oc/pkg/cli/newapp"
	"github.com/openshift/oc/pkg/cli/newbuild"
	"github.com/openshift/oc/pkg/cli/observe"
	"github.com/openshift/oc/pkg/cli/options"
	"github.com/openshift/oc/pkg/cli/policy"
	"github.com/openshift/oc/pkg/cli/process"
	"github.com/openshift/oc/pkg/cli/project"
	"github.com/openshift/oc/pkg/cli/projects"
	"github.com/openshift/oc/pkg/cli/recycle"
	"github.com/openshift/oc/pkg/cli/registry"
	"github.com/openshift/oc/pkg/cli/requestproject"
	"github.com/openshift/oc/pkg/cli/rollback"
	"github.com/openshift/oc/pkg/cli/rollout"
	"github.com/openshift/oc/pkg/cli/rsh"
	"github.com/openshift/oc/pkg/cli/rsync"
	"github.com/openshift/oc/pkg/cli/secrets"
	"github.com/openshift/oc/pkg/cli/serviceaccounts"
	"github.com/openshift/oc/pkg/cli/set"
	"github.com/openshift/oc/pkg/cli/startbuild"
	"github.com/openshift/oc/pkg/cli/status"
	"github.com/openshift/oc/pkg/cli/tag"
	"github.com/openshift/oc/pkg/cli/version"
	"github.com/openshift/oc/pkg/cli/whoami"
)

const productName = `OpenShift`

var (
	cliLong = heredoc.Doc(`
    ` + productName + ` Client

    This client helps you develop, build, deploy, and run your applications on any
    OpenShift or Kubernetes cluster. It also includes the administrative
    commands for managing a cluster under the 'adm' subcommand.`)

	cliExplain = heredoc.Doc(`
    To familiarize yourself with OpenShift, login to your cluster and try creating a sample application:

        oc login mycluster.mycompany.com
        oc new-project my-example
        oc new-app django-psql-example
        oc logs -f bc/django-psql-example

    To see what has been created, run:

        oc status

    and get a command shell inside one of the created containers with:

        oc rsh dc/postgresql

    To see the list of available toolchains for building applications, run:

        oc new-app -L

    Since OpenShift runs on top of Kubernetes, your favorite kubectl commands are also present in oc,
    allowing you to quickly switch between development and debugging. You can also run kubectl directly
    against any OpenShift cluster using the kubeconfig file created by 'oc login'.

    For more on OpenShift, see the documentation at https://docs.openshift.com.

    To see the full list of commands supported, run 'oc --help'.`)
)

func defaultConfigFlags() *genericclioptions.ConfigFlags {
	return genericclioptions.NewConfigFlags(true).WithDiscoveryBurst(350).WithDiscoveryQPS(50.0)
}

func NewDefaultOcCommand(o kubecmd.KubectlOptions) *cobra.Command {
	cmd := NewOcCommand(o)

	if o.PluginHandler == nil {
		return cmd
	}

	if len(o.Arguments) <= 1 {
		return cmd
	}

	cmdPathPieces := o.Arguments[1:]

	// only look for suitable extension executables if
	// the specified command does not already exist
	if foundCmd, foundArgs, err := cmd.Find(cmdPathPieces); err != nil {
		// Also check the commands that will be added by Cobra.
		// These commands are only added once rootCmd.Execute() is called, so we
		// need to check them explicitly here.
		var cmdName string // first "non-flag" arguments
		for _, arg := range cmdPathPieces {
			if !strings.HasPrefix(arg, "-") {
				cmdName = arg
				break
			}
		}

		switch cmdName {
		case "help", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			// Don't search for a plugin
		default:
			if err := kubecmd.HandlePluginCommand(o.PluginHandler, cmdPathPieces, 1); err != nil {
				fmt.Fprintf(o.IOStreams.ErrOut, "%v\n", err)
				os.Exit(1)
			}
		}
	} else if err == nil {
		if !kcmdutil.CmdPluginAsSubcommand.IsDisabled() {
			// Command exists(e.g. kubectl create), but it is not certain that
			// subcommand also exists (e.g. kubectl create networkpolicy)
			// we also have to eliminate kubectl create -f
			if kubecmd.IsSubcommandPluginAllowed(foundCmd.Name()) && len(foundArgs) >= 1 && !strings.HasPrefix(foundArgs[0], "-") {
				subcommand := foundArgs[0]
				builtinSubcmdExist := false
				for _, subcmd := range foundCmd.Commands() {
					if subcmd.Name() == subcommand {
						builtinSubcmdExist = true
						break
					}
				}

				if !builtinSubcmdExist {
					if err := kubecmd.HandlePluginCommand(o.PluginHandler, cmdPathPieces, len(cmdPathPieces)-len(foundArgs)+1); err != nil {
						fmt.Fprintf(o.IOStreams.ErrOut, "Error: %v\n", err)
						os.Exit(1)
					}
				}
			}
		}
	}

	return cmd
}

func NewOcCommand(o kubecmd.KubectlOptions) *cobra.Command {
	warningHandler := rest.NewWarningWriter(o.IOStreams.ErrOut, rest.WarningWriterOptions{Deduplicate: true, Color: kterm.AllowsColorOutput(o.IOStreams.ErrOut)})
	warningsAsErrors := false
	// Main command
	cmds := &cobra.Command{
		Use:   "oc",
		Short: "Command line tools for managing applications",
		Long:  cliLong,
		Run:   runHelp,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			rest.SetDefaultWarningHandler(warningHandler)

			if cmd.Name() == cobra.ShellCompRequestCmd {
				// This is the __complete or __completeNoDesc command which
				// indicates shell completion has been requested.
				plugin.SetupPluginCompletion(cmd, args)
			}

			return initProfiling()
		},
		PersistentPostRunE: func(*cobra.Command, []string) error {
			if err := flushProfiling(); err != nil {
				return err
			}
			if warningsAsErrors {
				count := warningHandler.WarningCount()
				switch count {
				case 0:
					// no warnings
				case 1:
					return fmt.Errorf("%d warning received", count)
				default:
					return fmt.Errorf("%d warnings received", count)
				}
			}
			return nil
		},
	}

	flags := cmds.PersistentFlags()

	addProfilingFlags(flags)

	flags.BoolVar(&warningsAsErrors, "warnings-as-errors", warningsAsErrors, "Treat warnings received from the server as errors and exit with a non-zero exit code")

	kubeConfigFlags := o.ConfigFlags
	if kubeConfigFlags == nil {
		kubeConfigFlags = defaultConfigFlags().WithWarningPrinter(o.IOStreams)
	}
	kubeConfigFlags.AddFlags(flags)
	matchVersionKubeConfigFlags := kcmdutil.NewMatchVersionFlags(kubeConfigFlags)
	matchVersionKubeConfigFlags.AddFlags(cmds.PersistentFlags())
	cmds.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	f := kcmdutil.NewFactory(matchVersionKubeConfigFlags)

	loginCmd := login.NewCmdLogin(f, o.IOStreams)
	secretcmds := secrets.NewCmdSecrets(f, o.IOStreams)

	groups := ktemplates.CommandGroups{
		{
			Message: "Basic Commands:",
			Commands: []*cobra.Command{
				loginCmd,
				requestproject.NewCmdRequestProject(f, o.IOStreams),
				newapp.NewCmdNewApplication(f, o.IOStreams),
				status.NewCmdStatus(f, o.IOStreams),
				project.NewCmdProject(f, o.IOStreams),
				projects.NewCmdProjects(f, o.IOStreams),
				kubectlwrappers.NewCmdExplain(f, o.IOStreams),
			},
		},
		{
			Message: "Build and Deploy Commands:",
			Commands: []*cobra.Command{
				rollout.NewCmdRollout(f, o.IOStreams),
				rollback.NewCmdRollback(f, o.IOStreams),
				newbuild.NewCmdNewBuild(f, o.IOStreams),
				startbuild.NewCmdStartBuild(f, o.IOStreams),
				cancelbuild.NewCmdCancelBuild(f, o.IOStreams),
				importimage.NewCmdImportImage(f, o.IOStreams),
				tag.NewCmdTag(f, o.IOStreams),
			},
		},
		{
			Message: "Application Management Commands:",
			Commands: []*cobra.Command{
				kubectlwrappers.NewCmdCreate(f, o.IOStreams),
				kubectlwrappers.NewCmdApply(f, o.IOStreams),
				kubectlwrappers.NewCmdGet(f, o.IOStreams),
				kubectlwrappers.NewCmdDescribe(f, o.IOStreams),
				kubectlwrappers.NewCmdEdit(f, o.IOStreams),
				set.NewCmdSet(f, o.IOStreams),
				kubectlwrappers.NewCmdLabel(f, o.IOStreams),
				kubectlwrappers.NewCmdAnnotate(f, o.IOStreams),
				expose.NewCmdExpose(f, o.IOStreams),
				kubectlwrappers.NewCmdDelete(f, o.IOStreams),
				kubectlwrappers.NewCmdScale(f, o.IOStreams),
				kubectlwrappers.NewCmdAutoscale(f, o.IOStreams),
				secretcmds,
				serviceaccounts.NewCmdServiceAccounts(f, o.IOStreams),
			},
		},
		{
			Message: "Troubleshooting and Debugging Commands:",
			Commands: []*cobra.Command{
				logs.NewCmdLogs(f, o.IOStreams),
				rsh.NewCmdRsh(f, o.IOStreams),
				rsync.NewCmdRsync(f, o.IOStreams),
				kubectlwrappers.NewCmdPortForward(f, o.IOStreams),
				debug.NewCmdDebug(f, o.IOStreams),
				kubectlwrappers.NewCmdExec(f, o.IOStreams),
				kubectlwrappers.NewCmdProxy(f, o.IOStreams),
				kubectlwrappers.NewCmdAttach(f, o.IOStreams),
				kubectlwrappers.NewCmdRun(f, o.IOStreams),
				kubectlwrappers.NewCmdCp(f, o.IOStreams),
				kubectlwrappers.NewCmdWait(f, o.IOStreams),
				kubectlwrappers.NewCmdEvents(f, o.IOStreams),
			},
		},
		{
			Message: "Advanced Commands:",
			Commands: []*cobra.Command{
				admin.NewCommandAdmin(f, o.IOStreams),
				kubectlwrappers.NewCmdReplace(f, o.IOStreams),
				kubectlwrappers.NewCmdPatch(f, o.IOStreams),
				process.NewCmdProcess(f, o.IOStreams),
				extract.NewCmdExtract(f, o.IOStreams),
				observe.NewCmdObserve(f, o.IOStreams),
				policy.NewCmdPolicy(f, o.IOStreams),
				kubectlwrappers.NewCmdAuth(f, o.IOStreams),
				image.NewCmdImage(f, o.IOStreams),
				registry.NewCmd(f, o.IOStreams),
				idle.NewCmdIdle(f, o.IOStreams),
				kubectlwrappers.NewCmdApiVersions(f, o.IOStreams),
				kubectlwrappers.NewCmdApiResources(f, o.IOStreams),
				kubectlwrappers.NewCmdClusterInfo(f, o.IOStreams),
				kubectlwrappers.NewCmdDiff(f, o.IOStreams),
				kubectlwrappers.NewCmdKustomize(o.IOStreams),
			},
		},
		{
			Message: "Settings Commands:",
			Commands: []*cobra.Command{
				gettoken.NewCmdGetToken(f, o.IOStreams),
				logout.NewCmdLogout(f, o.IOStreams),
				kubectlwrappers.NewCmdConfig(f, o.IOStreams),
				whoami.NewCmdWhoAmI(f, o.IOStreams),
				kubectlwrappers.NewCmdCompletion(o.IOStreams),
			},
		},
	}
	groups.Add(cmds)

	filters := []string{"options"}

	changeSharedFlagDefaults(cmds)

	ktemplates.ActsAsRootCommand(cmds, filters, groups...).
		ExposeFlags(loginCmd, "certificate-authority", "insecure-skip-tls-verify", "token")

	cmds.AddCommand(newExperimentalCommand(f, o.IOStreams))

	cmds.AddCommand(kubectlwrappers.NewCmdPlugin(f, o.IOStreams))
	cmds.AddCommand(version.NewCmdVersion(f, o.IOStreams))
	cmds.AddCommand(options.NewCmdOptions(o.IOStreams))

	registerCompletionFuncForGlobalFlags(cmds, f)

	return cmds
}

func runHelp(cmd *cobra.Command, args []string) {
	cmd.Help()
}

func moved(fullName, to string, parent, cmd *cobra.Command) string {
	cmd.Long = fmt.Sprintf("DEPRECATED: This command has been moved to \"%s %s\"", fullName, to)
	cmd.Short = fmt.Sprintf("DEPRECATED: %s", to)
	parent.AddCommand(cmd)
	return cmd.Name()
}

// changeSharedFlagDefaults changes values of shared flags that we disagree with.  This can't be done in godep code because
// that would change behavior in our `kubectl` symlink. Defend each change.
func changeSharedFlagDefaults(rootCmd *cobra.Command) {
	cmds := []*cobra.Command{rootCmd}

	for i := 0; i < len(cmds); i++ {
		currCmd := cmds[i]
		cmds = append(cmds, currCmd.Commands()...)

		// we want to disable the --validate flag by default when we're running kube commands from oc.  We want to make sure
		// that we're only getting the upstream --validate flags, so check both the flag and the usage
		if validateFlag := currCmd.Flags().Lookup("validate"); (validateFlag != nil) && (strings.Contains(validateFlag.Usage, "Must be one of: strict (or true), warn, ignore (or false)")) {
			validateFlag.DefValue = "ignore"
			validateFlag.Value.Set("ignore")
			validateFlag.Changed = false
		}
	}
}

func newExperimentalCommand(f kcmdutil.Factory, ioStreams genericiooptions.IOStreams) *cobra.Command {
	experimental := &cobra.Command{
		Use:   "ex",
		Short: "Experimental commands under active development",
		Long:  "The commands grouped here are under development and may change without notice.",
		Run: func(c *cobra.Command, args []string) {
			c.SetOutput(ioStreams.Out)
			c.Help()
		},
	}

	// remove this line, when adding experimental commands
	experimental.Hidden = true

	return experimental
}

// CommandFor returns the appropriate command for this base name,
// or the OpenShift CLI command.
func CommandFor(basename string) *cobra.Command {
	var cmd *cobra.Command

	in, out, err := os.Stdin, os.Stdout, os.Stderr

	// Make case-insensitive and strip executable suffix if present
	if runtime.GOOS == "windows" {
		basename = strings.ToLower(basename)
		basename = strings.TrimSuffix(basename, ".exe")
	}

	switch basename {
	case "kubectl":
		cmd = kubecmd.NewDefaultKubectlCommand()
	case "openshift-deploy":
		cmd = deployer.NewCommandDeployer(basename)
	case "openshift-recycle":
		cmd = recycle.NewCommandRecycle(basename, out)
	default:
		shimKubectlForOc()

		ioStreams := genericiooptions.IOStreams{In: in, Out: out, ErrOut: err}
		cmd = NewDefaultOcCommand(kubecmd.KubectlOptions{
			PluginHandler: kubecmd.NewDefaultPluginHandler(plugin.ValidPluginFilenamePrefixes),
			Arguments:     os.Args,
			ConfigFlags:   defaultConfigFlags().WithWarningPrinter(ioStreams),
			IOStreams:     ioStreams,
		})

		// treat oc as a kubectl plugin
		if strings.HasPrefix(basename, "kubectl-") {
			args := strings.Split(strings.TrimPrefix(basename, "kubectl-"), "-")

			// the plugin mechanism interprets "_" as dashes. Convert any "_" our basename
			// might have in order to find the appropriate command in the `oc` tree.
			for i := range args {
				args[i] = strings.Replace(args[i], "_", "-", -1)
			}

			if targetCmd, _, err := cmd.Find(args); targetCmd != nil && err == nil {
				// since cobra refuses to execute a child command, executing its root
				// any time Execute() is called, we must create a completely new command
				// and "deep copy" the targetCmd information to it.
				newParent := &cobra.Command{
					Use:     targetCmd.Use,
					Short:   targetCmd.Short,
					Long:    targetCmd.Long,
					Example: targetCmd.Example,
					Run:     targetCmd.Run,
				}

				// copy flags
				newParent.Flags().AddFlagSet(cmd.Flags())
				newParent.Flags().AddFlagSet(targetCmd.Flags())
				newParent.PersistentFlags().AddFlagSet(targetCmd.PersistentFlags())

				// copy subcommands
				newParent.AddCommand(targetCmd.Commands()...)
				cmd = newParent
			}
		}
	}

	if cmd.UsageFunc() == nil {
		ktemplates.ActsAsRootCommand(cmd, []string{"options"})
	}
	return cmd
}

func registerCompletionFuncForGlobalFlags(cmd *cobra.Command, f kcmdutil.Factory) {
	kcmdutil.CheckErr(cmd.RegisterFlagCompletionFunc(
		"namespace",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.CompGetResource(f, "namespace", toComplete), cobra.ShellCompDirectiveNoFileComp
		}))
	kcmdutil.CheckErr(cmd.RegisterFlagCompletionFunc(
		"context",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ListContextsInConfig(toComplete), cobra.ShellCompDirectiveNoFileComp
		}))
	kcmdutil.CheckErr(cmd.RegisterFlagCompletionFunc(
		"cluster",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ListClustersInConfig(toComplete), cobra.ShellCompDirectiveNoFileComp
		}))
	kcmdutil.CheckErr(cmd.RegisterFlagCompletionFunc(
		"user",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ListUsersInConfig(toComplete), cobra.ShellCompDirectiveNoFileComp
		}))
}
