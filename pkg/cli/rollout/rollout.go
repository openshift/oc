package rollout

import (
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/kubectl/pkg/cmd/rollout"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/completion"
	"k8s.io/kubectl/pkg/util/templates"

	cmdutil "github.com/openshift/oc/pkg/helpers/cmd"
)

var (
	rolloutLong = templates.LongDesc(`
		Manage the rollout of one or more resources.` + rolloutValidResources)

	rolloutExample = templates.Examples(`
		# Roll back to the previous deployment
		oc rollout undo deployment/abc

		# Check the rollout status of a daemonset
		oc rollout status daemonset/foo

		# Restart a deployment
		oc rollout restart deployment/abc

		# Restart deployments with the 'app=nginx' label
		oc rollout restart deployment --selector=app=nginx`)

	rolloutValidResources = `
		Valid resource types include:

		   * deployments
		   * daemonsets
		   * statefulsets
		   * deploymentConfigs (deprecated)
		`
)

// NewCmdRollout facilitates kubectl rollout subcommands
func NewCmdRollout(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "rollout SUBCOMMAND",
		Short:   "Manage the rollout of a resource",
		Long:    rolloutLong,
		Example: rolloutExample,
		Run:     kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	// subcommands
	cmd.AddCommand(NewCmdRolloutHistory(f, streams))
	cmd.AddCommand(NewCmdRolloutPause(f, streams))
	cmd.AddCommand(NewCmdRolloutResume(f, streams))
	cmd.AddCommand(NewCmdRolloutUndo(f, streams))
	cmd.AddCommand(NewCmdRolloutLatest(f, streams))
	cmd.AddCommand(NewCmdRolloutStatus(f, streams))
	cmd.AddCommand(NewCmdRolloutCancel(f, streams))
	cmd.AddCommand(NewCmdRolloutRetry(f, streams))
	cmd.AddCommand(cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(rollout.NewCmdRolloutRestart(f, streams))))

	return cmd
}

var (
	historyLong = templates.LongDesc(`
		View previous rollout revisions and configurations.`)

	historyExample = templates.Examples(`
		# View the rollout history of a deployment
		oc rollout history deployment/abc

		# View the details of daemonset revision 3
		oc rollout history daemonset/abc --revision=3`)
)

// NewCmdRolloutHistory is a wrapper for the Kubernetes cli rollout history command
func NewCmdRolloutHistory(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := rollout.NewCmdRolloutHistory(f, streams)
	cmd.Long = historyLong
	cmd.Example = historyExample
	validArgs := []string{"deployment", "replicaset", "replicationcontroller", "statefulset", "deploymentconfig"}
	cmd.ValidArgsFunction = completion.SpecifiedResourceTypeAndNameCompletionFunc(f, validArgs)
	return cmd
}

var (
	pauseLong = templates.LongDesc(`
		Mark the provided resource as paused.

		Paused resources will not be reconciled by a controller.
		Use "oc rollout resume" to resume a paused resource.
		Currently, only deployments support being paused.`)

	pauseExample = templates.Examples(`
		# Mark the nginx deployment as paused
		# Any current state of the deployment will continue its function; new updates
		# to the deployment will not have an effect as long as the deployment is paused
		oc rollout pause deployment/nginx`)
)

// NewCmdRolloutPause is a wrapper for the Kubernetes cli rollout pause command
func NewCmdRolloutPause(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := rollout.NewCmdRolloutPause(f, streams)
	cmd.Long = pauseLong
	cmd.Example = pauseExample
	validArgs := []string{"deployment", "replicaset", "replicationcontroller", "statefulset", "deploymentconfig"}
	cmd.ValidArgsFunction = completion.SpecifiedResourceTypeAndNameCompletionFunc(f, validArgs)
	return cmd
}

var (
	resumeLong = templates.LongDesc(`
		Resume a paused resource.

		Paused resources will not be reconciled by a controller. By resuming a
		resource, we allow it to be reconciled again.
		Currently only deployments support being resumed.`)

	resumeExample = templates.Examples(`
		# Resume an already paused deployment
		oc rollout resume deployment/nginx`)
)

// NewCmdRolloutResume is a wrapper for the Kubernetes cli rollout resume command
func NewCmdRolloutResume(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := rollout.NewCmdRolloutResume(f, streams)
	cmd.Long = resumeLong
	cmd.Example = resumeExample
	validArgs := []string{"deployment", "replicaset", "replicationcontroller", "statefulset", "deploymentconfig"}
	cmd.ValidArgsFunction = completion.SpecifiedResourceTypeAndNameCompletionFunc(f, validArgs)
	return cmd
}

var (
	undoLong = templates.LongDesc(`
		Roll back to a previous rollout.`)

	undoExample = templates.Examples(`
		# Roll back to the previous deployment
		oc rollout undo deployment/abc

		# Roll back to daemonset revision 3
		oc rollout undo daemonset/abc --to-revision=3

		# Roll back to the previous deployment with dry-run
		oc rollout undo --dry-run=server deployment/abc`)
)

// NewCmdRolloutUndo is a wrapper for the Kubernetes cli rollout undo command
func NewCmdRolloutUndo(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := rollout.NewCmdRolloutUndo(f, streams)
	cmd.Long = undoLong
	cmd.Example = undoExample
	validArgs := []string{"deployment", "replicaset", "replicationcontroller", "statefulset", "deploymentconfig"}
	cmd.ValidArgsFunction = completion.SpecifiedResourceTypeAndNameCompletionFunc(f, validArgs)
	return cmd
}

var (
	statusLong = templates.LongDesc(`
		Show the status of the rollout.

		By default 'rollout status' will watch the status of the latest rollout
		until it is done. If you do not want to wait for the rollout to finish then
		you can use --watch=false. Note that if a new rollout starts in-between, then
		'rollout status' will continue watching the latest revision. If you want to
		pin to a specific revision and abort if it is rolled over by another revision,
		use --revision=N where N is the revision you need to watch for.`)

	statusExample = templates.Examples(`
		# Watch the rollout status of a deployment
		oc rollout status deployment/nginx`)
)

// NewCmdRolloutStatus is a wrapper for the Kubernetes cli rollout status command
func NewCmdRolloutStatus(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := rollout.NewCmdRolloutStatus(f, streams)
	cmd.Long = statusLong
	cmd.Example = statusExample
	validArgs := []string{"deployment", "replicaset", "replicationcontroller", "statefulset", "deploymentconfig"}
	cmd.ValidArgsFunction = completion.SpecifiedResourceTypeAndNameCompletionFunc(f, validArgs)
	return cmd
}
