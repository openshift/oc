package deployments

import (
	"context"
	"time"

	kappsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1client "k8s.io/client-go/kubernetes/typed/apps/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"

	appsv1 "github.com/openshift/api/apps/v1"
)

type Pruner interface {
	// Prune is responsible for actual removal of deployments identified as candidates
	// for pruning based on pruning algorithm.
	Prune(deleter ReplicaDeleter) error
}

// ReplicaDeleter knows how to delete replication controllers and replicasets from OpenShift.
type ReplicaDeleter interface {
	// DeleteDeployment removes the deployment from OpenShift's storage.
	DeleteReplica(replica metav1.Object) error
}

// pruner is an object that knows how to prune a data set
type pruner struct {
	resolver Resolver
}

var _ Pruner = &pruner{}

// PrunerOptions contains the fields used to initialize a new Pruner.
type PrunerOptions struct {
	// KeepYoungerThan will filter out all objects from prune data set that are younger than the specified time duration.
	KeepYoungerThan time.Duration
	// Orphans if true will include inactive orphan deployments in candidate prune set.
	Orphans bool
	// ReplicaSets if true will include ReplicaSets when considering deployments.
	ReplicaSets bool
	// KeepComplete is per DeploymentConfig how many of the most recent deployments should be preserved.
	KeepComplete int
	// KeepFailed is per DeploymentConfig how many of the most recent failed deployments should be preserved.
	KeepFailed int
	// Deployments is the entire list of deployments and deploymentconfigs across all namespaces in the cluster.
	Deployments []metav1.Object
	// Replicas is the entire list of replication controllers and replicasets across all namespaces in the cluster.
	Replicas []metav1.Object
}

// NewPruner returns a Pruner over specified data using specified options.
// deploymentConfigs, deployments, opts.KeepYoungerThan, opts.Orphans, opts.KeepComplete, opts.KeepFailed, deploymentPruneFunc
func NewPruner(options PrunerOptions) Pruner {
	klog.V(1).Infof("Creating deployment pruner with keepYoungerThan=%v, orphans=%v, replicaSets=%v, keepComplete=%v, keepFailed=%v",
		options.KeepYoungerThan, options.Orphans, options.ReplicaSets, options.KeepComplete, options.KeepFailed)

	filter := &andFilter{
		filterPredicates: []FilterPredicate{
			FilterZeroReplicaSize,
			NewFilterBeforePredicate(options.KeepYoungerThan),
		},
	}

	if !options.Orphans {
		filter.filterPredicates = append(filter.filterPredicates, FilterDeploymentsPredicate)
	}

	replicas := filter.Filter(options.Replicas)
	dataSet := NewDataSet(options.Deployments, replicas)

	resolvers := []Resolver{}
	if options.Orphans {
		inactiveDeploymentStatus := []appsv1.DeploymentStatus{
			appsv1.DeploymentStatusComplete,
			appsv1.DeploymentStatusFailed,
		}
		resolvers = append(resolvers, NewOrphanReplicaResolver(dataSet, inactiveDeploymentStatus))
	}
	resolvers = append(resolvers, NewPerDeploymentResolver(dataSet, options.KeepComplete, options.KeepFailed))

	return &pruner{
		resolver: &mergeResolver{resolvers: resolvers},
	}
}

// Prune will visit each item in the prunable set and invoke the associated DeploymentDeleter.
func (p *pruner) Prune(deleter ReplicaDeleter) error {
	deployments, err := p.resolver.Resolve()
	if err != nil {
		return err
	}
	for _, deployment := range deployments {
		if err := deleter.DeleteReplica(deployment); err != nil {
			return err
		}
	}
	return nil
}

// replicaDeleter removes a replication controller or replicaset from OpenShift.
type replicaDeleter struct {
	replicationControllers corev1client.ReplicationControllersGetter
	replicaSets            appsv1client.ReplicaSetsGetter
}

var _ ReplicaDeleter = &replicaDeleter{}

// NeReplicaDeleter creates a new replicaDeleter.
func NewReplicaDeleter(replicationControllers corev1client.ReplicationControllersGetter, replicaSets appsv1client.ReplicaSetsGetter) ReplicaDeleter {
	return &replicaDeleter{
		replicationControllers: replicationControllers,
		replicaSets:            replicaSets,
	}
}

func (p *replicaDeleter) DeleteReplica(replica metav1.Object) error {
	klog.V(4).Infof("Deleting replica %q", replica.GetName())
	policy := metav1.DeletePropagationBackground

	switch replica.(type) {
	case *corev1.ReplicationController:
		return p.replicationControllers.ReplicationControllers(replica.GetNamespace()).Delete(context.TODO(), replica.GetName(), metav1.DeleteOptions{PropagationPolicy: &policy})
	case *kappsv1.ReplicaSet:
		return p.replicaSets.ReplicaSets(replica.GetNamespace()).Delete(context.TODO(), replica.GetName(), metav1.DeleteOptions{PropagationPolicy: &policy})
	}
	return nil
}
