package cli

import (
	"context"
	"flag"
	"fmt"
	"github.com/mgutz/ansi"
	"github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"k8s.io/client-go/tools/clientcmd"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	kubecmd "k8s.io/kubectl/pkg/cmd"
	"k8s.io/kubectl/pkg/cmd/get"
	"k8s.io/kubectl/pkg/cmd/plugin"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	kutil "k8s.io/kubectl/pkg/util"
	ktemplates "k8s.io/kubectl/pkg/util/templates"
	kterm "k8s.io/kubectl/pkg/util/term"

	"github.com/openshift/oc/pkg/cli/admin"
	"github.com/openshift/oc/pkg/cli/cancelbuild"
	"github.com/openshift/oc/pkg/cli/debug"
	"github.com/openshift/oc/pkg/cli/deployer"
	"github.com/openshift/oc/pkg/cli/expose"
	"github.com/openshift/oc/pkg/cli/extract"
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
	cmdutil "github.com/openshift/oc/pkg/helpers/cmd"
	"github.com/openshift/oc/pkg/helpers/term"

	"github.com/openziti/sdk-golang/ziti"
	"github.com/openziti/sdk-golang/ziti/config"
	"github.com/go-yaml/yaml"
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

func NewDefaultOcCommand(in io.Reader, out, errout io.Writer) *cobra.Command {
	cmd := NewOcCommand(in, out, errout)

	if len(os.Args) <= 1 {
		return cmd
	}

	cmdPathPieces := os.Args[1:]
	pluginHandler := kubecmd.NewDefaultPluginHandler(plugin.ValidPluginFilenamePrefixes)

	// only look for suitable extension executables if
	// the specified command does not already exist
	if _, _, err := cmd.Find(cmdPathPieces); err != nil {
		if err := kubecmd.HandlePluginCommand(pluginHandler, cmdPathPieces); err != nil {
			fmt.Fprintf(errout, "%v\n", err)
			os.Exit(1)
		}
	}

	return cmd
}

func NewOcCommand(in io.Reader, out, err io.Writer) *cobra.Command {
	warningHandler := rest.NewWarningWriter(err, rest.WarningWriterOptions{Deduplicate: true, Color: kterm.AllowsColorOutput(err)})
	warningsAsErrors := false
	// Main command
	cmds := &cobra.Command{
		Use:   "oc",
		Short: "Command line tools for managing applications",
		Long:  cliLong,
		Run: func(c *cobra.Command, args []string) {
			explainOut := term.NewResponsiveWriter(out)
			c.SetOutput(explainOut)
			kcmdutil.RequireNoArguments(c, args)
			fmt.Fprintf(explainOut, "%s\n\n%s\n", cliLong, cliExplain)
		},
		PersistentPreRunE: func(*cobra.Command, []string) error {
			rest.SetDefaultWarningHandler(warningHandler)
			return nil
		},
		PersistentPostRunE: func(*cobra.Command, []string) error {
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
	flags.BoolVar(&warningsAsErrors, "warnings-as-errors", warningsAsErrors, "Treat warnings received from the server as errors and exit with a non-zero exit code")

	kubeConfigFlags := genericclioptions.NewConfigFlags(true).WithDiscoveryBurst(250)
	kubeConfigFlags.AddFlags(flags)
	matchVersionKubeConfigFlags := kcmdutil.NewMatchVersionFlags(kubeConfigFlags)
	matchVersionKubeConfigFlags.AddFlags(cmds.PersistentFlags())
	cmds.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	f := kcmdutil.NewFactory(matchVersionKubeConfigFlags)

	ioStreams := genericclioptions.IOStreams{In: in, Out: out, ErrOut: err}

	loginCmd := login.NewCmdLogin(f, ioStreams)
	secretcmds := secrets.NewCmdSecrets(f, ioStreams)

	groups := ktemplates.CommandGroups{
		{
			Message: "Basic Commands:",
			Commands: []*cobra.Command{
				loginCmd,
				requestproject.NewCmdRequestProject(f, ioStreams),
				newapp.NewCmdNewApplication(f, ioStreams),
				status.NewCmdStatus(f, ioStreams),
				project.NewCmdProject(f, ioStreams),
				projects.NewCmdProjects(f, ioStreams),
				kubectlwrappers.NewCmdExplain(f, ioStreams),
			},
		},
		{
			Message: "Build and Deploy Commands:",
			Commands: []*cobra.Command{
				rollout.NewCmdRollout(f, ioStreams),
				rollback.NewCmdRollback(f, ioStreams),
				newbuild.NewCmdNewBuild(f, ioStreams),
				startbuild.NewCmdStartBuild(f, ioStreams),
				cancelbuild.NewCmdCancelBuild(f, ioStreams),
				importimage.NewCmdImportImage(f, ioStreams),
				tag.NewCmdTag(f, ioStreams),
			},
		},
		{
			Message: "Application Management Commands:",
			Commands: []*cobra.Command{
				kubectlwrappers.NewCmdCreate(f, ioStreams),
				kubectlwrappers.NewCmdApply(f, ioStreams),
				kubectlwrappers.NewCmdGet(f, ioStreams),
				kubectlwrappers.NewCmdDescribe(f, ioStreams),
				kubectlwrappers.NewCmdEdit(f, ioStreams),
				set.NewCmdSet(f, ioStreams),
				kubectlwrappers.NewCmdLabel(f, ioStreams),
				kubectlwrappers.NewCmdAnnotate(f, ioStreams),
				expose.NewCmdExpose(f, ioStreams),
				kubectlwrappers.NewCmdDelete(f, ioStreams),
				kubectlwrappers.NewCmdScale(f, ioStreams),
				kubectlwrappers.NewCmdAutoscale(f, ioStreams),
				secretcmds,
				serviceaccounts.NewCmdServiceAccounts(f, ioStreams),
			},
		},
		{
			Message: "Troubleshooting and Debugging Commands:",
			Commands: []*cobra.Command{
				logs.NewCmdLogs(f, ioStreams),
				rsh.NewCmdRsh(f, ioStreams),
				rsync.NewCmdRsync(f, ioStreams),
				kubectlwrappers.NewCmdPortForward(f, ioStreams),
				debug.NewCmdDebug(f, ioStreams),
				kubectlwrappers.NewCmdExec(f, ioStreams),
				kubectlwrappers.NewCmdProxy(f, ioStreams),
				kubectlwrappers.NewCmdAttach(f, ioStreams),
				kubectlwrappers.NewCmdRun(f, ioStreams),
				kubectlwrappers.NewCmdCp(f, ioStreams),
				kubectlwrappers.NewCmdWait(f, ioStreams),
			},
		},
		{
			Message: "Advanced Commands:",
			Commands: []*cobra.Command{
				admin.NewCommandAdmin(f, ioStreams),
				kubectlwrappers.NewCmdReplace(f, ioStreams),
				kubectlwrappers.NewCmdPatch(f, ioStreams),
				process.NewCmdProcess(f, ioStreams),
				extract.NewCmdExtract(f, ioStreams),
				observe.NewCmdObserve(f, ioStreams),
				policy.NewCmdPolicy(f, ioStreams),
				kubectlwrappers.NewCmdAuth(f, ioStreams),
				image.NewCmdImage(f, ioStreams),
				registry.NewCmd(f, ioStreams),
				idle.NewCmdIdle(f, ioStreams),
				kubectlwrappers.NewCmdApiVersions(f, ioStreams),
				kubectlwrappers.NewCmdApiResources(f, ioStreams),
				kubectlwrappers.NewCmdClusterInfo(f, ioStreams),
				kubectlwrappers.NewCmdDiff(f, ioStreams),
				kubectlwrappers.NewCmdKustomize(ioStreams),
			},
		},
		{
			Message: "Settings Commands:",
			Commands: []*cobra.Command{
				logout.NewCmdLogout(f, ioStreams),
				kubectlwrappers.NewCmdConfig(f, ioStreams),
				whoami.NewCmdWhoAmI(f, ioStreams),
				kubectlwrappers.NewCmdCompletion(ioStreams),
			},
		},
	}
	groups.Add(cmds)

	filters := []string{"options"}

	changeSharedFlagDefaults(cmds)

	cmdutil.ActsAsRootCommand(cmds, filters, groups...).
		ExposeFlags(loginCmd, "certificate-authority", "insecure-skip-tls-verify", "token")

	cmds.AddCommand(newExperimentalCommand(f, ioStreams))

	cmds.AddCommand(kubectlwrappers.NewCmdPlugin(f, ioStreams))
	cmds.AddCommand(version.NewCmdVersion(f, ioStreams))
	cmds.AddCommand(options.NewCmdOptions(ioStreams))

	registerCompletionFuncForGlobalFlags(cmds, f)

	return cmds
}

func moved(fullName, to string, parent, cmd *cobra.Command) string {
	cmd.Long = fmt.Sprintf("DEPRECATED: This command has been moved to \"%s %s\"", fullName, to)
	cmd.Short = fmt.Sprintf("DEPRECATED: %s", to)
	parent.AddCommand(cmd)
	return cmd.Name()
}

// changeSharedFlagDefaults changes values of shared flags that we disagree with.  This can't be done in godep code because
// that would change behavior in our `kubectl` symlink. Defend each change.
// 1. show-all - the most interesting pods are terminated/failed pods.  We don't want to exclude them from printing
func changeSharedFlagDefaults(rootCmd *cobra.Command) {
	cmds := []*cobra.Command{rootCmd}

	for i := 0; i < len(cmds); i++ {
		currCmd := cmds[i]
		cmds = append(cmds, currCmd.Commands()...)

		// we want to disable the --validate flag by default when we're running kube commands from oc.  We want to make sure
		// that we're only getting the upstream --validate flags, so check both the flag and the usage
		if validateFlag := currCmd.Flags().Lookup("validate"); (validateFlag != nil) && (validateFlag.Usage == "If true, use a schema to validate the input before sending it") {
			validateFlag.DefValue = "false"
			validateFlag.Value.Set("false")
			validateFlag.Changed = false
		}
	}
}

func newExperimentalCommand(f kcmdutil.Factory, ioStreams genericclioptions.IOStreams) *cobra.Command {
	experimental := &cobra.Command{
		Use:   "ex",
		Short: "Experimental commands under active development",
		Long:  "The commands grouped here are under development and may change without notice.",
		Run: func(c *cobra.Command, args []string) {
			c.SetOutput(ioStreams.Out)
			c.Help()
		},
		BashCompletionFunction: admin.BashCompletionFunc,
	}

	// remove this line, when adding experimental commands
	experimental.Hidden = true

	return experimental
}

var configFilePath string
var serviceName string

type ZitiFlags struct {
	zConfig string
	service string
}

type Context struct {
	ZConfig string `yaml:"zConfig"`
	Service string `yaml:"service"`
}

type MinKubeConfig struct {
	Contexts []struct {
		Context Context `yaml:"context"`
		Name    string  `yaml:"name"`
	} `yaml:"contexts"`
}

var zFlags = ZitiFlags{}

// function for handling the dialing with ziti
func dialFunc(ctx context.Context, network, address string) (net.Conn, error) {
	service := serviceName
	configFile, err := config.NewFromFile(configFilePath)

	if err != nil {
		logrus.WithError(err).Error("Error loading config file")
		os.Exit(1)
	}

	context := ziti.NewContextWithConfig(configFile)
	return context.Dial(service)
}

func wrapConfigFn(restConfig *rest.Config) *rest.Config {

	restConfig.Dial = dialFunc
	return restConfig
}

func setZitiFlags(command *cobra.Command) *cobra.Command {

	command.PersistentFlags().StringVarP(&zFlags.zConfig, "zConfig", "", "", "Path to ziti config file")
	command.PersistentFlags().StringVarP(&zFlags.service, "service", "", "", "Service name")

	return command
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

	logrus.Error("i am here line 399")

	switch basename {
	case "kubectl":
		//cmd = kubecmd.NewDefaultKubectlCommand()
		logrus.Error("we did it")
		kubeConfigFlags := genericclioptions.NewConfigFlags(true).WithDeprecatedPasswordFlag()

		// set the wrapper function. This allows modification to the reset Config
		kubeConfigFlags.WrapConfigFn = wrapConfigFn
		cmd = kubecmd.NewDefaultKubectlCommandWithArgsAndConfigFlags(kubecmd.NewDefaultPluginHandler(plugin.ValidPluginFilenamePrefixes), os.Args, in, out, err, kubeConfigFlags)
	case "openshift-deploy":
		cmd = deployer.NewCommandDeployer(basename)
	case "openshift-recycle":
		cmd = recycle.NewCommandRecycle(basename, out)
	default:
		logrus.Error("i am here line 415")
		shimKubectlForOc()

		logrus.Error("we did it")
		kubeConfigFlags := genericclioptions.NewConfigFlags(true).WithDeprecatedPasswordFlag()

		// set the wrapper function. This allows modification to the reset Config
		kubeConfigFlags.WrapConfigFn = wrapConfigFn
		cmd = kubecmd.NewDefaultKubectlCommandWithArgsAndConfigFlags(kubecmd.NewDefaultPluginHandler(plugin.ValidPluginFilenamePrefixes), os.Args, in, out, err, kubeConfigFlags)

		//set and parse the ziti flags
		cmd = setZitiFlags(cmd)
		cmd.PersistentFlags().Parse(os.Args)

		// try to get the ziti options from the flags
		configFilePath = cmd.Flag("zConfig").Value.String()
		serviceName = cmd.Flag("service").Value.String()

		// get the loaded kubeconfig
		kubeconfig := getKubeconfig()

		// if both the config file and service name are not set, parse the kubeconfig file
		if configFilePath == "" || serviceName == "" {
			parseKubeConfig(cmd, kubeconfig)
		}

		//cmd = NewDefaultOcCommand(in, out, err)

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
		cmdutil.ActsAsRootCommand(cmd, []string{"options"})
	}
	return cmd
}

func registerCompletionFuncForGlobalFlags(cmd *cobra.Command, f kcmdutil.Factory) {
	kcmdutil.CheckErr(cmd.RegisterFlagCompletionFunc(
		"namespace",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return get.CompGetResource(f, cmd, "namespace", toComplete), cobra.ShellCompDirectiveNoFileComp
		}))
	kcmdutil.CheckErr(cmd.RegisterFlagCompletionFunc(
		"context",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return kutil.ListContextsInConfig(toComplete), cobra.ShellCompDirectiveNoFileComp
		}))
	kcmdutil.CheckErr(cmd.RegisterFlagCompletionFunc(
		"cluster",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return kutil.ListClustersInConfig(toComplete), cobra.ShellCompDirectiveNoFileComp
		}))
	kcmdutil.CheckErr(cmd.RegisterFlagCompletionFunc(
		"user",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return kutil.ListUsersInConfig(toComplete), cobra.ShellCompDirectiveNoFileComp
		}))
}

// function for getting the current kubeconfig
func getKubeconfig() clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules,
		configOverrides)

	return kubeConfig
}

func parseKubeConfig(command *cobra.Command, kubeconfig clientcmd.ClientConfig) {
	// attempt to get the kubeconfig path from the command flags
	kubeconfigPath := command.Flag("kubeconfig").Value.String()

	// if the path is not set, attempt to get it from the kubeconfig precedence
	if kubeconfigPath == "" {
		// obtain the list of kubeconfig files from the current kubeconfig
		kubeconfigPrcedence := kubeconfig.ConfigAccess().GetLoadingPrecedence()

		// get the raw API config
		apiConfig, err := kubeconfig.RawConfig()

		if err != nil {
			panic(err)
		}

		// set the ziti options from one of the config files
		getZitiOptionsFromConfigList(kubeconfigPrcedence, apiConfig.CurrentContext)

	} else {
		// get the ziti options form the specified path
		getZitiOptionsFromConfig(kubeconfigPath)
	}

}


func getZitiOptionsFromConfigList(kubeconfigPrcedence []string, currentContext string) {
	// for the kubeconfig files in the precedence
	for _, path := range kubeconfigPrcedence {

		// read the config file
		config := readKubeConfig(path)

		// loop through the context list
		for _, context := range config.Contexts {

			// if the context name matches the current context
			if currentContext == context.Name {

				// set the config file path if it's not already set
				if configFilePath == "" {
					configFilePath = context.Context.ZConfig
				}

				// set the service name if it's not already set
				if serviceName == "" {
					serviceName = context.Context.Service
				}

				break
			}
		}
	}
}

func readKubeConfig(kubeconfig string) MinKubeConfig {
	// get the file name from the path
	filename, _ := filepath.Abs(kubeconfig)

	// read the yaml file
	yamlFile, err := ioutil.ReadFile(filename)

	if err != nil {
		panic(err)
	}

	var minKubeConfig MinKubeConfig

	//parse the yaml file
	err = yaml.Unmarshal(yamlFile, &minKubeConfig)
	if err != nil {
		panic(err)
	}

	return minKubeConfig

}

func getZitiOptionsFromConfig(kubeconfig string) {

	// get the config from the path
	config := clientcmd.GetConfigFromFileOrDie(kubeconfig)

	// get the current context
	currentContext := config.CurrentContext

	// read the yaml file
	minKubeConfig := readKubeConfig(kubeconfig)

	var context Context
	// find the context that matches the current context
	for _, ctx := range minKubeConfig.Contexts {

		if ctx.Name == currentContext {
			context = ctx.Context
		}
	}

	// set the config file if not already set
	if configFilePath == "" {
		configFilePath = context.ZConfig
	}

	// set the service name if not already set
	if serviceName == "" {
		serviceName = context.Service
	}
}



type logrusFormatter struct {
}

func (fa *logrusFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	level := toLevel(entry)
	return []byte(fmt.Sprintf("%s\t%s\n", level, entry.Message)), nil
}

func toLevel(entry *logrus.Entry) string {
	switch entry.Level {
	case logrus.PanicLevel:
		return panicColor
	case logrus.FatalLevel:
		return fatalColor
	case logrus.ErrorLevel:
		return errorColor
	case logrus.WarnLevel:
		return warnColor
	case logrus.InfoLevel:
		return infoColor
	case logrus.DebugLevel:
		return debugColor
	case logrus.TraceLevel:
		return traceColor
	default:
		return infoColor
	}
}

var panicColor = ansi.Red + "PANIC" + ansi.DefaultFG
var fatalColor = ansi.Red + "FATAL" + ansi.DefaultFG
var errorColor = ansi.Red + "ERROR" + ansi.DefaultFG
var warnColor = ansi.Yellow + "WARN " + ansi.DefaultFG
var infoColor = ansi.LightGreen + "INFO " + ansi.DefaultFG
var debugColor = ansi.LightBlue + "DEBUG" + ansi.DefaultFG
var traceColor = ansi.LightBlack + "TRACE" + ansi.DefaultFG

