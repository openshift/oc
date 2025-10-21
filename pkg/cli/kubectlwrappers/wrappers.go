package kubectlwrappers

import (
	"bufio"
	"strings"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/kubectl/pkg/cmd/annotate"
	"k8s.io/kubectl/pkg/cmd/apiresources"
	"k8s.io/kubectl/pkg/cmd/apply"
	"k8s.io/kubectl/pkg/cmd/attach"
	kcmdauth "k8s.io/kubectl/pkg/cmd/auth"
	"k8s.io/kubectl/pkg/cmd/autoscale"
	"k8s.io/kubectl/pkg/cmd/clusterinfo"
	"k8s.io/kubectl/pkg/cmd/completion"
	"k8s.io/kubectl/pkg/cmd/cp"
	kcreate "k8s.io/kubectl/pkg/cmd/create"
	"k8s.io/kubectl/pkg/cmd/delete"
	"k8s.io/kubectl/pkg/cmd/describe"
	"k8s.io/kubectl/pkg/cmd/diff"
	"k8s.io/kubectl/pkg/cmd/edit"
	"k8s.io/kubectl/pkg/cmd/events"
	"k8s.io/kubectl/pkg/cmd/exec"
	"k8s.io/kubectl/pkg/cmd/explain"
	kget "k8s.io/kubectl/pkg/cmd/get"
	"k8s.io/kubectl/pkg/cmd/kustomize"
	"k8s.io/kubectl/pkg/cmd/label"
	"k8s.io/kubectl/pkg/cmd/patch"
	"k8s.io/kubectl/pkg/cmd/plugin"
	"k8s.io/kubectl/pkg/cmd/portforward"
	"k8s.io/kubectl/pkg/cmd/proxy"
	"k8s.io/kubectl/pkg/cmd/replace"
	"k8s.io/kubectl/pkg/cmd/run"
	"k8s.io/kubectl/pkg/cmd/scale"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	kwait "k8s.io/kubectl/pkg/cmd/wait"
	utilcomp "k8s.io/kubectl/pkg/util/completion"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/cli/create"
	cmdutil "github.com/openshift/oc/pkg/helpers/cmd"
)

func adjustCmdExamples(cmd *cobra.Command, name string) {
	for _, subCmd := range cmd.Commands() {
		adjustCmdExamples(subCmd, cmd.Name())
	}
	cmd.Example = strings.Replace(cmd.Example, "kubectl", "oc", -1)
	tabbing := "  "
	examples := []string{}
	scanner := bufio.NewScanner(strings.NewReader(cmd.Example))
	for scanner.Scan() {
		examples = append(examples, tabbing+strings.TrimSpace(scanner.Text()))
	}
	cmd.Example = strings.Join(examples, "\n")
}

// NewCmdGet is a wrapper for the Kubernetes cli get command
func NewCmdGet(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	get := kget.NewCmdGet("oc", f, streams)
	get.ValidArgsFunction = utilcomp.ResourceTypeAndNameCompletionFunc(f)
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(get))
}

// NewCmdReplace is a wrapper for the Kubernetes cli replace command
func NewCmdReplace(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(replace.NewCmdReplace(f, streams)))
}

func NewCmdClusterInfo(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(clusterinfo.NewCmdClusterInfo(f, streams)))
}

// NewCmdPatch is a wrapper for the Kubernetes cli patch command
func NewCmdPatch(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(patch.NewCmdPatch(f, streams)))
}

// NewCmdDelete is a wrapper for the Kubernetes cli delete command
func NewCmdDelete(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(delete.NewCmdDelete(f, streams)))
}

// NewCmdCreate is a wrapper for the Kubernetes cli create command
func NewCmdCreate(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(kcreate.NewCmdCreate(f, streams)))

	// create subcommands
	cmd.AddCommand(create.NewCmdCreateRoute(f, streams))
	cmd.AddCommand(create.NewCmdCreateDeploymentConfig(f, streams))
	cmd.AddCommand(create.NewCmdCreateClusterQuota(f, streams))

	cmd.AddCommand(create.NewCmdCreateUser(f, streams))
	cmd.AddCommand(create.NewCmdCreateIdentity(f, streams))
	cmd.AddCommand(create.NewCmdCreateUserIdentityMapping(f, streams))
	cmd.AddCommand(create.NewCmdCreateImageStream(f, streams))
	cmd.AddCommand(create.NewCmdCreateImageStreamTag(f, streams))
	cmd.AddCommand(create.NewCmdCreateBuild(f, streams))

	adjustCmdExamples(cmd, "create")

	return cmd
}

var (
	completionLong = templates.LongDesc(`
		Output shell completion code for the specified shell (bash or zsh).
		The shell code must be evaluated to provide interactive
		completion of oc commands.  This can be done by sourcing it from
		the .bash_profile.

		Note for zsh users: [1] zsh completions are only supported in versions of zsh >= 5.2`)
)

func NewCmdCompletion(streams genericiooptions.IOStreams) *cobra.Command {
	cmd := cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(completion.NewCmdCompletion(streams.Out, "\n")))
	cmd.Long = completionLong
	return cmd
}

// NewCmdExec is a wrapper for the Kubernetes cli exec command
func NewCmdExec(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(exec.NewCmdExec(f, streams)))
}

// NewCmdPortForward is a wrapper for the Kubernetes cli port-forward command
func NewCmdPortForward(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(portforward.NewCmdPortForward(f, streams)))
}

// NewCmdDescribe is a wrapper for the Kubernetes cli describe command
func NewCmdDescribe(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(describe.NewCmdDescribe("oc", f, streams)))
}

// NewCmdProxy is a wrapper for the Kubernetes cli proxy command
func NewCmdProxy(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(proxy.NewCmdProxy(f, streams)))
}

// NewCmdScale is a wrapper for the Kubernetes cli scale command
func NewCmdScale(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(scale.NewCmdScale(f, streams)))
	cmd.ValidArgs = append(cmd.ValidArgs, "deploymentconfig")
	return cmd
}

// NewCmdAutoscale is a wrapper for the Kubernetes cli autoscale command
func NewCmdAutoscale(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(autoscale.NewCmdAutoscale(f, streams)))
	cmd.Short = "Autoscale a deployment config, deployment, replica set, stateful set, or replication controller"
	cmd.ValidArgs = append(cmd.ValidArgs, "deploymentconfig")
	return cmd
}

// NewCmdRun is a wrapper for the Kubernetes cli run command
func NewCmdRun(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(run.NewCmdRun(f, streams)))
	return cmd
}

// NewCmdAttach is a wrapper for the Kubernetes cli attach command
func NewCmdAttach(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(attach.NewCmdAttach(f, streams)))
}

// NewCmdAnnotate is a wrapper for the Kubernetes cli annotate command
func NewCmdAnnotate(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(annotate.NewCmdAnnotate("oc", f, streams)))
}

// NewCmdLabel is a wrapper for the Kubernetes cli label command
func NewCmdLabel(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(label.NewCmdLabel(f, streams)))
}

// NewCmdApply is a wrapper for the Kubernetes cli apply command
func NewCmdApply(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(apply.NewCmdApply("oc", f, streams)))
}

// NewCmdExplain is a wrapper for the Kubernetes cli explain command
func NewCmdExplain(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(explain.NewCmdExplain("oc", f, streams)))
}

// NewCmdEdit is a wrapper for the Kubernetes cli edit command
func NewCmdEdit(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(edit.NewCmdEdit(f, streams)))
}

// NewCmdCp is a wrapper for the Kubernetes cli cp command
func NewCmdCp(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(cp.NewCmdCp(f, streams)))
}

func NewCmdWait(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(kwait.NewCmdWait(f, streams)))
}

func NewCmdEvents(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(events.NewCmdEvents(f, streams)))
}

func NewCmdAuth(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(kcmdauth.NewCmdAuth(f, streams)))
}

func NewCmdPlugin(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(plugin.NewCmdPlugin(streams)))
}

func NewCmdApiResources(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(apiresources.NewCmdAPIResources(f, streams)))
}

func NewCmdApiVersions(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(apiresources.NewCmdAPIVersions(f, streams)))
}

func NewCmdKustomize(streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(kustomize.NewCmdKustomize(streams)))
}

func NewCmdDiff(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(diff.NewCmdDiff(f, streams)))
}
