package deployments

import (
	"sort"

	kappsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	appsv1 "github.com/openshift/api/apps/v1"
	"github.com/openshift/library-go/pkg/apps/appsutil"
)

// Resolver knows how to resolve the set of candidate objects to prune
type Resolver interface {
	Resolve() ([]metav1.Object, error)
}

// mergeResolver merges the set of results from multiple resolvers
type mergeResolver struct {
	resolvers []Resolver
}

func (m *mergeResolver) Resolve() ([]metav1.Object, error) {
	results := []metav1.Object{}
	for _, resolver := range m.resolvers {
		items, err := resolver.Resolve()
		if err != nil {
			return nil, err
		}
		results = append(results, items...)
	}
	return results, nil
}

// NewOrphanReplicaResolver returns a Resolver that matches objects with no associated Deployment or DeploymentConfig and has a DeploymentStatus in filter
func NewOrphanReplicaResolver(dataSet DataSet, replicaStatusFilter []appsv1.DeploymentStatus) Resolver {
	filter := sets.NewString()
	for _, replicaStatus := range replicaStatusFilter {
		filter.Insert(string(replicaStatus))
	}
	return &orphanReplicaResolver{
		dataSet:                dataSet,
		deploymentStatusFilter: filter,
	}
}

// orphanReplicaResolver resolves orphan replicas that match the specified filter
type orphanReplicaResolver struct {
	dataSet                DataSet
	deploymentStatusFilter sets.String
}

// Resolve the matching set of objects
func (o *orphanReplicaResolver) Resolve() ([]metav1.Object, error) {
	replicas, err := o.dataSet.ListReplicas()
	if err != nil {
		return nil, err
	}

	results := []metav1.Object{}
	for _, replica := range replicas {
		var status appsv1.DeploymentStatus

		switch v := replica.(type) {
		case *corev1.ReplicationController:
			status = appsutil.DeploymentStatusFor(v)
		case *kappsv1.ReplicaSet:
			status = appsutil.DeploymentStatusFor(v)
		}

		if !o.deploymentStatusFilter.Has(string(status)) {
			continue
		}
		_, exists, _ := o.dataSet.GetDeployment(replica)
		if !exists {
			results = append(results, replica)
		}
	}
	return results, nil
}

type perDeploymentResolver struct {
	dataSet      DataSet
	keepComplete int
	keepFailed   int
}

// NewPerDeploymentResolver returns a Resolver that selects items to prune per config
func NewPerDeploymentResolver(dataSet DataSet, keepComplete int, keepFailed int) Resolver {
	return &perDeploymentResolver{
		dataSet:      dataSet,
		keepComplete: keepComplete,
		keepFailed:   keepFailed,
	}
}

// ByMostRecent sorts deployments by most recently created.
type ByMostRecent []metav1.Object

func (s ByMostRecent) Len() int      { return len(s) }
func (s ByMostRecent) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s ByMostRecent) Less(i, j int) bool {
	return !s[i].GetCreationTimestamp().Time.Before(s[j].GetCreationTimestamp().Time)
}

func (o *perDeploymentResolver) Resolve() ([]metav1.Object, error) {
	deployments, err := o.dataSet.ListDeployments()
	if err != nil {
		return nil, err
	}

	completeStates := sets.NewString(string(appsv1.DeploymentStatusComplete))
	failedStates := sets.NewString(string(appsv1.DeploymentStatusFailed))

	results := []metav1.Object{}
	for _, deployment := range deployments {
		replicas, err := o.dataSet.ListReplicasByDeployment(deployment)
		if err != nil {
			return nil, err
		}

		sort.Sort(ByMostRecent(replicas))

		completeDeployments, failedDeployments := []metav1.Object{}, []metav1.Object{}
		for i, replica := range replicas {
			var status appsv1.DeploymentStatus

			switch v := replica.(type) {
			case *corev1.ReplicationController:
				status = appsutil.DeploymentStatusFor(v)
			case *kappsv1.ReplicaSet:
				if v.Status.Replicas == 0 && i < len(replicas)-1 {
					status = appsv1.DeploymentStatusComplete
				}
			}

			if completeStates.Has(string(status)) {
				completeDeployments = append(completeDeployments, replica)
			} else if failedStates.Has(string(status)) {
				failedDeployments = append(failedDeployments, replica)
			}
		}
		sort.Sort(ByMostRecent(completeDeployments))
		sort.Sort(ByMostRecent(failedDeployments))

		if o.keepComplete >= 0 && o.keepComplete < len(completeDeployments) {
			results = append(results, completeDeployments[o.keepComplete:]...)
		}
		if o.keepFailed >= 0 && o.keepFailed < len(failedDeployments) {
			results = append(results, failedDeployments[o.keepFailed:]...)
		}
	}
	return results, nil
}
