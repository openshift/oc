package deployments

import (
	"reflect"
	"testing"
	"time"

	kappsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	appsv1 "github.com/openshift/api/apps/v1"
)

func mockDeploymentConfig(namespace, name string) *appsv1.DeploymentConfig {
	return &appsv1.DeploymentConfig{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
}

func mockDeployment(namespace, name string) *kappsv1.Deployment {
	return &kappsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
}

func withSize(item metav1.Object, replicas int32) metav1.Object {
	switch v := item.(type) {
	case *corev1.ReplicationController:
		v.Spec.Replicas = &replicas
		v.Status.Replicas = replicas
	case *kappsv1.ReplicaSet:
		v.Spec.Replicas = &replicas
		v.Status.Replicas = replicas
	}

	return item
}

func withCreated(item metav1.Object, creationTimestamp metav1.Time) metav1.Object {
	item.SetCreationTimestamp(creationTimestamp)
	return item
}

func withStatus(item *corev1.ReplicationController, status appsv1.DeploymentStatus) *corev1.ReplicationController {
	item.Annotations[appsv1.DeploymentStatusAnnotation] = string(status)
	return item
}

func mockReplicationController(namespace, name string, deploymentConfig metav1.Object) *corev1.ReplicationController {
	zero := int32(0)
	item := &corev1.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Annotations: map[string]string{}},
		Spec:       corev1.ReplicationControllerSpec{Replicas: &zero},
	}
	if deploymentConfig != nil {
		item.Annotations[appsv1.DeploymentConfigAnnotation] = deploymentConfig.GetName()

		if deploymentConfig != nil {
			item.OwnerReferences = []metav1.OwnerReference{
				{
					APIVersion: "apps.openshift.io/v1",
					Kind:       "DeploymentConfig",
					Name:       deploymentConfig.GetName(),
				},
			}
		}
	}
	item.Annotations[appsv1.DeploymentStatusAnnotation] = string(appsv1.DeploymentStatusNew)
	return item
}

func mockReplicaSet(namespace, name string, deployment metav1.Object) *kappsv1.ReplicaSet {
	zero := int32(0)
	item := &kappsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Annotations: map[string]string{}},
		Spec:       kappsv1.ReplicaSetSpec{Replicas: &zero},
	}
	if deployment != nil {
		item.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       deployment.GetName(),
			},
		}
	}
	return item
}

func TestReplicaByDeploymentConfigIndexFunc(t *testing.T) {
	config := mockDeploymentConfig("a", "b")
	replicationController := mockReplicationController("a", "c", config)
	actualKey, err := ReplicaByDeploymentIndexFunc(replicationController)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	expectedKey := []string{"a/b"}
	if !reflect.DeepEqual(actualKey, expectedKey) {
		t.Errorf("expected %v, actual %v", expectedKey, actualKey)
	}
	replicationControllerWithNoConfig := &corev1.ReplicationController{}
	actualKey, err = ReplicaByDeploymentIndexFunc(replicationControllerWithNoConfig)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	expectedKey = []string{"orphan"}
	if !reflect.DeepEqual(actualKey, expectedKey) {
		t.Errorf("expected %v, actual %v", expectedKey, actualKey)
	}
}

func TestFilterBeforePredicate(t *testing.T) {
	youngerThan := time.Hour
	now := metav1.Now()
	old := metav1.NewTime(now.Time.Add(-1 * youngerThan))
	items := []metav1.Object{}
	items = append(items, withCreated(mockReplicationController("a", "old", nil), old))
	items = append(items, withCreated(mockReplicationController("a", "new", nil), now))
	filter := &andFilter{
		filterPredicates: []FilterPredicate{NewFilterBeforePredicate(youngerThan)},
	}
	result := filter.Filter(items)
	if len(result) != 1 {
		t.Errorf("Unexpected number of results")
	}
	if expected, actual := "old", result[0].GetName(); expected != actual {
		t.Errorf("expected %v, actual %v", expected, actual)
	}
}

func TestEmptyDataSet(t *testing.T) {
	replicas := []metav1.Object{}
	deployments := []metav1.Object{}
	dataSet := NewDataSet(deployments, replicas)
	_, exists, err := dataSet.GetDeployment(&corev1.ReplicationController{})
	if exists || err != nil {
		t.Errorf("Unexpected result %v, %v", exists, err)
	}
	deploymentResults, err := dataSet.ListDeployments()
	if err != nil {
		t.Errorf("Unexpected result %v", err)
	}
	if len(deploymentResults) != 0 {
		t.Errorf("Unexpected result %v", deploymentResults)
	}
	replicaResults, err := dataSet.ListReplicas()
	if err != nil {
		t.Errorf("Unexpected result %v", err)
	}
	if len(replicaResults) != 0 {
		t.Errorf("Unexpected result %v", replicaResults)
	}
	deploymentResults, err = dataSet.ListReplicasByDeployment(&appsv1.DeploymentConfig{})
	if err != nil {
		t.Errorf("Unexpected result %v", err)
	}
	if len(deploymentResults) != 0 {
		t.Errorf("Unexpected result %v", deploymentResults)
	}
}

func TestPopulatedDataSet(t *testing.T) {
	deployments := []metav1.Object{
		mockDeploymentConfig("a", "deployment-config-1"),
		mockDeploymentConfig("b", "deployment-config-2"),
		mockDeployment("d", "deployment-1"),
	}
	replicas := []metav1.Object{
		mockReplicationController("a", "replication-controller-1", deployments[0]),
		mockReplicationController("a", "replication-controller-2", deployments[0]),
		mockReplicationController("b", "replication-controller-3", deployments[1]),
		mockReplicationController("c", "replication-controller-4", nil),
		mockReplicaSet("d", "replica-set-1", deployments[2]),
		mockReplicaSet("d", "replica-set-2", nil),
	}
	dataSet := NewDataSet(deployments, replicas)
	for _, replica := range replicas {
		var config string
		var hasConfig bool
		deployment, exists, err := dataSet.GetDeployment(replica)

		switch replica.(type) {
		case *corev1.ReplicationController:
			config, hasConfig = replica.GetAnnotations()[appsv1.DeploymentConfigAnnotation]
		case *kappsv1.ReplicaSet:
			hasConfig = len(replica.GetOwnerReferences()) > 0
			if hasConfig {
				config = replica.GetOwnerReferences()[0].Name
			}
		}
		if hasConfig {
			if err != nil {
				t.Errorf("Item %v, unexpected error: %v", replica, err)
			}
			if !exists {
				t.Errorf("Item %v, unexpected result: %v", replica, exists)
			}
			if expected, actual := config, deployment.GetName(); expected != actual {
				t.Errorf("expected %v, actual %v", expected, actual)
			}
			if expected, actual := replica.GetNamespace(), deployment.GetNamespace(); expected != actual {
				t.Errorf("expected %v, actual %v", expected, actual)
			}
		} else {
			if err != nil {
				t.Errorf("Item %v, unexpected error: %v", replica, err)
			}
			if exists {
				t.Errorf("Item %v, unexpected result: %v", replica, exists)
			}
		}
	}

	expectedNames := sets.NewString("replication-controller-1", "replication-controller-2")
	deploymentResults, err := dataSet.ListReplicasByDeployment(deployments[0])
	if err != nil {
		t.Errorf("Unexpected result %v", err)
	}
	if len(deploymentResults) != len(expectedNames) {
		t.Errorf("Unexpected result %v", deploymentResults)
	}
	for _, deployment := range deploymentResults {
		if !expectedNames.Has(deployment.GetName()) {
			t.Errorf("Unexpected name: %v", deployment.GetName())
		}
	}

	expectedNames = sets.NewString("replica-set-1")
	deploymentResults, err = dataSet.ListReplicasByDeployment(deployments[2])
	if err != nil {
		t.Errorf("Unexpected result %v", err)
	}
	if len(deploymentResults) != len(expectedNames) {
		t.Errorf("Unexpected result %v", deploymentResults)
	}
	for _, deployment := range deploymentResults {
		if !expectedNames.Has(deployment.GetName()) {
			t.Errorf("Unexpected name: %v", deployment.GetName())
		}
	}
}
