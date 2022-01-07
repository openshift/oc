package deployments

import (
	"fmt"
	"time"

	kappsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	appsv1 "github.com/openshift/api/apps/v1"
	"github.com/openshift/library-go/pkg/apps/appsutil"
)

// ReplicaByDeploymentIndexFunc indexes Replica items by their associated Deployment, if none, index with key "orphan".
func ReplicaByDeploymentIndexFunc(obj interface{}) ([]string, error) {
	switch v := obj.(type) {
	case *corev1.ReplicationController:
		name := appsutil.DeploymentConfigNameFor(v)
		if len(name) == 0 {
			return []string{"orphan"}, nil
		}
		return []string{v.Namespace + "/" + name}, nil
	case *kappsv1.ReplicaSet:
		for _, owner := range v.OwnerReferences {
			if owner.Kind == "Deployment" && len(owner.Name) > 0 {
				return []string{v.Namespace + "/" + owner.Name}, nil
			}
		}
		return []string{"orphan"}, nil
	default:
		return nil, fmt.Errorf("unknown type: %T", obj)
	}
}

// Filter filters the set of objects.
type Filter interface {
	Filter(items []metav1.Object) []metav1.Object
}

// andFilter ands a set of predicate functions to know if it should be included in the return set.
type andFilter struct {
	filterPredicates []FilterPredicate
}

// Filter ands the set of predicates evaluated against each item to make a filtered set.
func (a *andFilter) Filter(items []metav1.Object) []metav1.Object {
	results := []metav1.Object{}
	for _, item := range items {
		include := true
		for _, filterPredicate := range a.filterPredicates {
			include = include && filterPredicate(item)
		}
		if include {
			results = append(results, item)
		}
	}
	return results
}

// FilterPredicate is a function that returns true if the object should be included in the filtered set.
type FilterPredicate func(item metav1.Object) bool

// NewFilterBeforePredicate is a function that returns true if the build was created before the current time minus specified duration.
func NewFilterBeforePredicate(d time.Duration) FilterPredicate {
	now := metav1.Now()
	before := metav1.NewTime(now.Time.Add(-1 * d))
	return func(item metav1.Object) bool {
		return item.GetCreationTimestamp().Time.Before(before.Time)
	}
}

// FilterDeploymentsPredicate is a function that returns true if the replication controller or replicaset is associated with a DeploymentConfig or Deployment.
func FilterDeploymentsPredicate(item metav1.Object) bool {
	switch v := item.(type) {
	case *corev1.ReplicationController:
		return len(appsutil.DeploymentConfigNameFor(v)) > 0
	case *kappsv1.ReplicaSet:
		for _, owner := range v.OwnerReferences {
			if owner.Kind == "Deployment" && len(owner.Name) > 0 {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// FilterZeroReplicaSize is a function that returns true if the replication controller or replicaset size is 0.
func FilterZeroReplicaSize(item metav1.Object) bool {
	switch v := item.(type) {
	case *corev1.ReplicationController:
		return *v.Spec.Replicas == 0 && v.Status.Replicas == 0
	case *kappsv1.ReplicaSet:
		return *v.Spec.Replicas == 0 && v.Status.Replicas == 0
	default:
		return false
	}
}

// DataSet provides functions for working with deployment data.
type DataSet interface {
	GetDeployment(repl metav1.Object) (metav1.Object, bool, error)
	ListDeployments() ([]metav1.Object, error)
	ListReplicas() ([]metav1.Object, error)
	ListReplicasByDeployment(config metav1.Object) ([]metav1.Object, error)
}

type dataSet struct {
	deploymentStore cache.Store
	replicaIndexer  cache.Indexer
}

// NewDataSet returns a DataSet over the specified items.
func NewDataSet(deployments []metav1.Object, replicas []metav1.Object) DataSet {
	deploymentStore := cache.NewStore(cache.MetaNamespaceKeyFunc)
	for _, deployment := range deployments {
		deploymentStore.Add(deployment)
	}

	replicaIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		"deployment": ReplicaByDeploymentIndexFunc,
	})
	for _, replica := range replicas {
		replicaIndexer.Add(replica)
	}

	return &dataSet{
		deploymentStore: deploymentStore,
		replicaIndexer:  replicaIndexer,
	}
}

// GetDeployment gets the deployment or configuration for the given replication controller or replicaset.
func (d *dataSet) GetDeployment(replica metav1.Object) (metav1.Object, bool, error) {
	switch v := replica.(type) {
	case *corev1.ReplicationController:
		name := appsutil.DeploymentConfigNameFor(v)
		if len(name) == 0 {
			return nil, false, nil
		}

		var deploymentConfig *appsv1.DeploymentConfig
		key := &appsv1.DeploymentConfig{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: v.Namespace}}
		item, exists, err := d.deploymentStore.Get(key)
		if exists {
			deploymentConfig = item.(*appsv1.DeploymentConfig)
		}
		return deploymentConfig, exists, err
	case *kappsv1.ReplicaSet:
		var name string
		for _, owner := range v.OwnerReferences {
			if owner.Kind == "Deployment" {
				name = owner.Name
				break
			}
		}

		if len(name) == 0 {
			return nil, false, nil
		}

		var deployment *kappsv1.Deployment
		key := &kappsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: v.Namespace}}
		item, exists, err := d.deploymentStore.Get(key)
		if exists {
			deployment = item.(*kappsv1.Deployment)
		}
		return deployment, exists, err
	default:
		return nil, false, fmt.Errorf("unknown type: %T", replica)
	}
}

// ListDeployment returns a list of Deployment and DeploymentConfigs.
func (d *dataSet) ListDeployments() ([]metav1.Object, error) {
	results := []metav1.Object{}
	for _, item := range d.deploymentStore.List() {
		switch v := item.(type) {
		case *appsv1.DeploymentConfig:
			results = append(results, v)
		case *kappsv1.Deployment:
			results = append(results, v)
		}
	}
	return results, nil
}

// ListReplicas returns a list of Replication Controllers or ReplicaSets.
func (d *dataSet) ListReplicas() ([]metav1.Object, error) {
	results := []metav1.Object{}
	for _, item := range d.replicaIndexer.List() {
		switch v := item.(type) {
		case *corev1.ReplicationController:
			results = append(results, v)
		case *kappsv1.ReplicaSet:
			results = append(results, v)
		}
	}
	return results, nil
}

// ListReplicasByDeployment returns a list of deployments for the provided replicas
func (d *dataSet) ListReplicasByDeployment(deployment metav1.Object) ([]metav1.Object, error) {
	results := []metav1.Object{}

	switch v := deployment.(type) {
	case *appsv1.DeploymentConfig:
		key := &corev1.ReplicationController{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   v.Namespace,
				Annotations: map[string]string{appsv1.DeploymentConfigAnnotation: v.Name},
			},
		}

		items, err := d.replicaIndexer.Index("deployment", key)
		if err != nil {
			return nil, err
		}

		for _, item := range items {
			results = append(results, item.(*corev1.ReplicationController))
		}
	case *kappsv1.Deployment:
		key := &kappsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: v.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       v.Name,
					},
				},
			},
		}

		items, err := d.replicaIndexer.Index("deployment", key)
		if err != nil {
			return nil, err
		}

		for _, item := range items {
			results = append(results, item.(*kappsv1.ReplicaSet))
		}
	default:
		return nil, fmt.Errorf("unknown type: %T", deployment)
	}

	return results, nil
}
