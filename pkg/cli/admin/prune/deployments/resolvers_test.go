package deployments

import (
	"fmt"
	"sort"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	appsv1 "github.com/openshift/api/apps/v1"
)

type mockResolver struct {
	items []metav1.Object
	err   error
}

func (m *mockResolver) Resolve() ([]metav1.Object, error) {
	return m.items, m.err
}

func TestMergeResolver(t *testing.T) {
	resolverA := &mockResolver{
		items: []metav1.Object{
			mockReplicationController("a", "b", nil),
		},
	}
	resolverB := &mockResolver{
		items: []metav1.Object{
			mockReplicationController("c", "d", nil),
		},
	}
	resolver := &mergeResolver{resolvers: []Resolver{resolverA, resolverB}}
	results, err := resolver.Resolve()
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Unexpected results %v", results)
	}
	expectedNames := sets.NewString("b", "d")
	for _, item := range results {
		if !expectedNames.Has(item.GetName()) {
			t.Errorf("Unexpected name %v", item.GetName())
		}
	}
}

func TestOrphanDeploymentResolver(t *testing.T) {
	activeDeploymentConfig := mockDeploymentConfig("a", "active-deployment-config")
	inactiveDeploymentConfig := mockDeploymentConfig("a", "inactive-deployment-config")

	deployments := []metav1.Object{activeDeploymentConfig}
	replicas := []metav1.Object{}

	expectedNames := sets.String{}
	deploymentStatusOptions := []appsv1.DeploymentStatus{
		appsv1.DeploymentStatusComplete,
		appsv1.DeploymentStatusFailed,
		appsv1.DeploymentStatusNew,
		appsv1.DeploymentStatusPending,
		appsv1.DeploymentStatusRunning,
	}

	deploymentStatusFilter := []appsv1.DeploymentStatus{
		appsv1.DeploymentStatusComplete,
		appsv1.DeploymentStatusFailed,
	}
	deploymentStatusFilterSet := sets.String{}
	for _, deploymentStatus := range deploymentStatusFilter {
		deploymentStatusFilterSet.Insert(string(deploymentStatus))
	}

	for _, deploymentStatusOption := range deploymentStatusOptions {
		replicas = append(replicas, withStatus(mockReplicationController("a", string(deploymentStatusOption)+"-active", activeDeploymentConfig), deploymentStatusOption))
		replicas = append(replicas, withStatus(mockReplicationController("a", string(deploymentStatusOption)+"-inactive", inactiveDeploymentConfig), deploymentStatusOption))
		replicas = append(replicas, withStatus(mockReplicationController("a", string(deploymentStatusOption)+"-orphan", nil), deploymentStatusOption))
		if deploymentStatusFilterSet.Has(string(deploymentStatusOption)) {
			expectedNames.Insert(string(deploymentStatusOption) + "-inactive")
			expectedNames.Insert(string(deploymentStatusOption) + "-orphan")
		}
	}

	dataSet := NewDataSet(deployments, replicas)
	resolver := NewOrphanReplicaResolver(dataSet, deploymentStatusFilter)
	results, err := resolver.Resolve()
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	foundNames := sets.String{}
	for _, result := range results {
		foundNames.Insert(result.GetName())
	}
	if len(foundNames) != len(expectedNames) || !expectedNames.HasAll(foundNames.List()...) {
		t.Errorf("expected %v, actual %v", expectedNames, foundNames)
	}
}

func TestPerDeploymentConfigResolver(t *testing.T) {
	deploymentStatusOptions := []appsv1.DeploymentStatus{
		appsv1.DeploymentStatusComplete,
		appsv1.DeploymentStatusFailed,
		appsv1.DeploymentStatusNew,
		appsv1.DeploymentStatusPending,
		appsv1.DeploymentStatusRunning,
	}
	deployments := []metav1.Object{
		mockDeploymentConfig("a", "deployment-config-1"),
		mockDeploymentConfig("b", "deployment-config-2"),
	}
	deploymentsPerStatus := 100
	replicas := []metav1.Object{}
	for _, deployment := range deployments {
		for _, deploymentStatusOption := range deploymentStatusOptions {
			for i := 0; i < deploymentsPerStatus; i++ {
				replica := withStatus(mockReplicationController(deployment.GetNamespace(), fmt.Sprintf("%v-%v-%v", deployment.GetName(), deploymentStatusOption, i), deployment), deploymentStatusOption)
				replicas = append(replicas, replica)
			}
		}
	}

	now := metav1.Now()
	for i := range replicas {
		creationTimestamp := metav1.NewTime(now.Time.Add(-1 * time.Duration(i) * time.Hour))
		replicas[i].SetCreationTimestamp(creationTimestamp)
	}

	// test number to keep at varying ranges
	for keep := 0; keep < deploymentsPerStatus*2; keep++ {
		dataSet := NewDataSet(deployments, replicas)

		expectedNames := sets.String{}
		deploymentCompleteStatusFilterSet := sets.NewString(string(appsv1.DeploymentStatusComplete))
		deploymentFailedStatusFilterSet := sets.NewString(string(appsv1.DeploymentStatusFailed))

		for _, deployment := range deployments {
			replicaItems, err := dataSet.ListReplicasByDeployment(deployment)
			if err != nil {
				t.Errorf("Unexpected err %v", err)
			}
			completedDeployments, failedDeployments := []metav1.Object{}, []metav1.Object{}
			for _, replica := range replicaItems {
				status := replica.GetAnnotations()[appsv1.DeploymentStatusAnnotation]
				if deploymentCompleteStatusFilterSet.Has(status) {
					completedDeployments = append(completedDeployments, replica)
				} else if deploymentFailedStatusFilterSet.Has(status) {
					failedDeployments = append(failedDeployments, replica)
				}
			}
			sort.Sort(ByMostRecent(completedDeployments))
			sort.Sort(ByMostRecent(failedDeployments))
			purgeCompleted := []metav1.Object{}
			purgeFailed := []metav1.Object{}
			if keep >= 0 && keep < len(completedDeployments) {
				purgeCompleted = completedDeployments[keep:]
			}
			if keep >= 0 && keep < len(failedDeployments) {
				purgeFailed = failedDeployments[keep:]
			}
			for _, replica := range purgeCompleted {
				expectedNames.Insert(replica.GetName())
			}
			for _, replica := range purgeFailed {
				expectedNames.Insert(replica.GetName())
			}
		}

		resolver := NewPerDeploymentResolver(dataSet, keep, keep)
		results, err := resolver.Resolve()
		if err != nil {
			t.Errorf("Unexpected error %v", err)
		}
		foundNames := sets.String{}
		for _, result := range results {
			foundNames.Insert(result.GetName())
		}
		if len(foundNames) != len(expectedNames) || !expectedNames.HasAll(foundNames.List()...) {
			expectedValues := expectedNames.List()
			actualValues := foundNames.List()
			sort.Strings(expectedValues)
			sort.Strings(actualValues)
			t.Errorf("keep %v\n, expected \n\t%v\n, actual \n\t%v\n", keep, expectedValues, actualValues)
		}
	}
}
