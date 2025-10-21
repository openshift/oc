package deployments

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kappsv1client "k8s.io/client-go/kubernetes/typed/apps/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	appsv1client "github.com/openshift/client-go/apps/clientset/versioned/typed/apps/v1"
)

var (
	deploymentsLongDesc = templates.LongDesc(`
		Prune old completed and failed deployment configs.

		By default, the prune operation performs a dry run making no changes to the deployment configs.
		A --confirm flag is needed for changes to be effective.
	`)

	deploymentsExample = templates.Examples(`
		# Dry run deleting all but the last complete deployment for every deployment config
		oc adm prune deployments --keep-complete=1

		 # To actually perform the prune operation, the confirm flag must be appended
		oc adm prune deployments --keep-complete=1 --confirm
	`)
)

// PruneDeploymentsOptions holds all the required options for pruning deployments.
type PruneDeploymentsOptions struct {
	Confirm         bool
	Orphans         bool
	ReplicaSets     bool
	KeepYoungerThan time.Duration
	KeepComplete    int
	KeepFailed      int
	Namespace       string

	AppsClient  appsv1client.DeploymentConfigsGetter
	KubeClient  corev1client.CoreV1Interface
	KAppsClient kappsv1client.AppsV1Interface

	genericiooptions.IOStreams
}

func NewPruneDeploymentsOptions(streams genericiooptions.IOStreams) *PruneDeploymentsOptions {
	return &PruneDeploymentsOptions{
		Confirm:         false,
		KeepYoungerThan: 60 * time.Minute,
		KeepComplete:    5,
		KeepFailed:      1,
		IOStreams:       streams,
	}
}

// NewCmdPruneDeployments implements the OpenShift cli prune deployments command.
func NewCmdPruneDeployments(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewPruneDeploymentsOptions(streams)
	cmd := &cobra.Command{
		Use:     "deployments",
		Short:   "Remove old completed and failed deployment configs",
		Long:    deploymentsLongDesc,
		Example: deploymentsExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().BoolVar(&o.Confirm, "confirm", o.Confirm, "If true, specify that deployment pruning should proceed. Defaults to false, displaying what would be deleted but not actually deleting anything.")
	cmd.Flags().BoolVar(&o.Orphans, "orphans", o.Orphans, "If true, prune all deployments where the associated DeploymentConfig no longer exists, the status is complete or failed, and the replica size is 0.")
	cmd.Flags().BoolVar(&o.ReplicaSets, "replica-sets", o.ReplicaSets, "EXPERIMENTAL: If true, ReplicaSets will be included in the pruning process.")
	cmd.Flags().DurationVar(&o.KeepYoungerThan, "keep-younger-than", o.KeepYoungerThan, "Specify the minimum age of a deployment for it to be considered a candidate for pruning.")
	cmd.Flags().IntVar(&o.KeepComplete, "keep-complete", o.KeepComplete, "Per DeploymentConfig, specify the number of deployments whose status is complete that will be preserved whose replica size is 0.")
	cmd.Flags().IntVar(&o.KeepFailed, "keep-failed", o.KeepFailed, "Per DeploymentConfig, specify the number of deployments whose status is failed that will be preserved whose replica size is 0.")

	return cmd
}

// Complete turns a partially defined PruneDeploymentsOptions into a solvent structure
// which can be validated and used for pruning deployments.
func (o *PruneDeploymentsOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return kcmdutil.UsageErrorf(cmd, "no arguments are allowed to this command")
	}

	o.Namespace = metav1.NamespaceAll
	if cmd.Flags().Lookup("namespace").Changed {
		var err error
		o.Namespace, _, err = f.ToRawKubeConfigLoader().Namespace()
		if err != nil {
			return err
		}
	}

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.KubeClient, err = corev1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	o.AppsClient, err = appsv1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	o.KAppsClient, err = kappsv1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	return nil
}

// Validate ensures that a PruneDeploymentsOptions is valid and can be used to execute pruning.
func (o PruneDeploymentsOptions) Validate() error {
	if o.KeepYoungerThan < 0 {
		return fmt.Errorf("--keep-younger-than must be greater than or equal to 0")
	}
	if o.KeepComplete < 0 {
		return fmt.Errorf("--keep-complete must be greater than or equal to 0")
	}
	if o.KeepFailed < 0 {
		return fmt.Errorf("--keep-failed must be greater than or equal to 0")
	}
	return nil
}

// Run contains all the necessary functionality for the OpenShift cli prune deployments command.
func (o PruneDeploymentsOptions) Run() error {
	deploymentConfigList, err := o.AppsClient.DeploymentConfigs(o.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	deployments := []metav1.Object{}
	for i := range deploymentConfigList.Items {
		deployments = append(deployments, &deploymentConfigList.Items[i])
	}

	if o.ReplicaSets {
		deploymentList, err := o.KAppsClient.Deployments(o.Namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}

		for i := range deploymentList.Items {
			deployments = append(deployments, &deploymentList.Items[i])
		}
	}

	replicationControllerList, err := o.KubeClient.ReplicationControllers(o.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	replicas := []metav1.Object{}
	for i := range replicationControllerList.Items {
		replicas = append(replicas, &replicationControllerList.Items[i])
	}

	if o.ReplicaSets {
		replicaSetList, err := o.KAppsClient.ReplicaSets(o.Namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}

		for i := range replicaSetList.Items {
			replicas = append(replicas, &replicaSetList.Items[i])
		}
	}

	options := PrunerOptions{
		KeepYoungerThan: o.KeepYoungerThan,
		Orphans:         o.Orphans,
		ReplicaSets:     o.ReplicaSets,
		KeepComplete:    o.KeepComplete,
		KeepFailed:      o.KeepFailed,
		Deployments:     deployments,
		Replicas:        replicas,
	}
	pruner := NewPruner(options)

	w := tabwriter.NewWriter(o.Out, 10, 4, 3, ' ', 0)
	defer w.Flush()

	replicaDeleter := &describingReplicaDeleter{w: w}

	if o.Confirm {
		replicaDeleter.delegate = NewReplicaDeleter(o.KubeClient, o.KAppsClient)
	} else {
		fmt.Fprintln(os.Stderr, "Dry run enabled - no modifications will be made. Add --confirm to remove deployments")
	}

	return pruner.Prune(replicaDeleter)
}

// describingReplicaDeleter prints information about each replication controller or replicaset it removes.
// If a delegate exists, its DeleteReplica function is invoked prior to returning.
type describingReplicaDeleter struct {
	w             io.Writer
	delegate      ReplicaDeleter
	headerPrinted bool
}

var _ ReplicaDeleter = &describingReplicaDeleter{}

func (p *describingReplicaDeleter) DeleteReplica(replica metav1.Object) error {
	if !p.headerPrinted {
		p.headerPrinted = true
		fmt.Fprintln(p.w, "NAMESPACE\tNAME")
	}

	fmt.Fprintf(p.w, "%s\t%s\n", replica.GetNamespace(), replica.GetName())

	if p.delegate == nil {
		return nil
	}

	if err := p.delegate.DeleteReplica(replica); err != nil {
		return err
	}

	return nil
}
