package deployments

import (
	"sort"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	appsv1 "github.com/openshift/api/apps/v1"
)

type mockDeleteRecorder struct {
	set sets.String
	err error
}

var _ ReplicaDeleter = &mockDeleteRecorder{}

func (m *mockDeleteRecorder) DeleteReplica(replica metav1.Object) error {
	m.set.Insert(replica.GetName())
	return m.err
}

func (m *mockDeleteRecorder) Verify(t *testing.T, expected sets.String) {
	if len(m.set) != len(expected) || !m.set.HasAll(expected.List()...) {
		expectedValues := expected.List()
		actualValues := m.set.List()
		sort.Strings(expectedValues)
		sort.Strings(actualValues)
		t.Errorf("expected \n\t%v\n, actual \n\t%v\n", expectedValues, actualValues)
	}
}

func TestPruneTask(t *testing.T) {
	now := metav1.Now()
	old := metav1.NewTime(now.Time.Add(-1 * time.Hour))

	deployments := []metav1.Object{
		mockDeploymentConfig("a", "deployment-config"),
		mockDeployment("b", "deployment"),
	}

	replicas := []metav1.Object{
		withCreated(withStatus(mockReplicationController("a", "build-1", deployments[0]), appsv1.DeploymentStatusComplete), now),
		withCreated(withStatus(mockReplicationController("a", "build-2", deployments[0]), appsv1.DeploymentStatusComplete), old),
		withCreated(withStatus(mockReplicationController("a", "build-3", deployments[0]), appsv1.DeploymentStatusFailed), old),
		withSize(withCreated(withStatus(mockReplicationController("a", "build-3-with-replicas", deployments[0]), appsv1.DeploymentStatusRunning), old), 4),
		withCreated(withStatus(mockReplicationController("a", "orphan-build-1", nil), appsv1.DeploymentStatusFailed), now),
		withCreated(withStatus(mockReplicationController("a", "orphan-build-2", nil), appsv1.DeploymentStatusComplete), old),
		withSize(withCreated(withStatus(mockReplicationController("a", "orphan-build-3-with-replicas", nil), appsv1.DeploymentStatusRunning), old), 4),
		mockReplicaSet("b", "rs1", deployments[1]),
		mockReplicaSet("b", "orphan-rs1", nil),
		withCreated(mockReplicaSet("b", "rs2", deployments[1]), now),
		withCreated(mockReplicaSet("b", "rs3", deployments[1]), old),
		withCreated(withSize(mockReplicaSet("b", "rs4", deployments[1]), 3), now),
		withCreated(withSize(mockReplicaSet("b", "rs5", deployments[1]), 3), old),
		withCreated(withSize(mockReplicaSet("b", "orphan-rs2-with-replicas", nil), 3), old),
	}

	testCases := map[string]struct {
		Orphans            bool
		ReplicaSets        bool
		KeepToungerThan    time.Duration
		KeepComplete       int
		KeepFailed         int
		ExpectedPruneNames []string
	}{
		"prune nothing": {
			Orphans:            false,
			ReplicaSets:        false,
			KeepToungerThan:    24 * time.Hour,
			KeepComplete:       5,
			KeepFailed:         5,
			ExpectedPruneNames: []string{},
		},
		"prune all failed, non-orphan replicationControlers": {
			Orphans:         false,
			ReplicaSets:     false,
			KeepToungerThan: 0,
			KeepComplete:    5,
			KeepFailed:      0,
			ExpectedPruneNames: []string{
				"build-3",
			},
		},
		"prune all completed and failed, non-orphan replicas": {
			Orphans:         false,
			ReplicaSets:     true,
			KeepToungerThan: 0,
			KeepComplete:    0,
			KeepFailed:      0,
			ExpectedPruneNames: []string{
				"build-1",
				"build-2",
				"build-3",
				"rs1",
				"rs2",
				"rs3",
			},
		},
		"prune all old non-orphan replicas": {
			Orphans:         false,
			ReplicaSets:     true,
			KeepToungerThan: time.Hour,
			KeepComplete:    0,
			KeepFailed:      0,
			ExpectedPruneNames: []string{
				"build-2",
				"build-3",
				"rs1",
				"rs3",
			},
		},
		"prune all orphan replicationControlers": {
			Orphans:         true,
			ReplicaSets:     false,
			KeepToungerThan: 0,
			KeepComplete:    10,
			KeepFailed:      1,
			ExpectedPruneNames: []string{
				"orphan-build-1",
				"orphan-build-2",
			},
		},
		"prune all orphans + failed replicas": {
			Orphans:         true,
			ReplicaSets:     true,
			KeepToungerThan: 0,
			KeepComplete:    10,
			KeepFailed:      0,
			ExpectedPruneNames: []string{
				"build-3",
				"orphan-build-1",
				"orphan-build-2",
				"orphan-rs1",
			},
		},
		"prune keep 2 non-orphaned, completed relpicas": {
			Orphans:         false,
			ReplicaSets:     true,
			KeepToungerThan: 0,
			KeepComplete:    2,
			KeepFailed:      0,
			ExpectedPruneNames: []string{
				"build-3",
				"rs1",
			},
		},
		"prune keep 1 non-orphaned, failed relpicas and 2 non-orphaned, completed replicas": {
			Orphans:         false,
			ReplicaSets:     true,
			KeepToungerThan: 0,
			KeepComplete:    2,
			KeepFailed:      1,
			ExpectedPruneNames: []string{
				"rs1",
			},
		},
		"prune everything not running": {
			Orphans:         true,
			ReplicaSets:     true,
			KeepToungerThan: 0,
			KeepComplete:    0,
			KeepFailed:      0,
			ExpectedPruneNames: []string{
				"build-1",
				"build-2",
				"build-3",
				"orphan-build-1",
				"orphan-build-2",
				"orphan-rs1",
				"rs1",
				"rs2",
				"rs3",
			},
		},
	}

	for testName, test := range testCases {
		recorder := &mockDeleteRecorder{set: sets.String{}}

		options := PrunerOptions{
			KeepYoungerThan: test.KeepToungerThan,
			Orphans:         test.Orphans,
			ReplicaSets:     test.ReplicaSets,
			KeepComplete:    test.KeepComplete,
			KeepFailed:      test.KeepFailed,
			Deployments:     deployments,
			Replicas:        replicas,
		}
		pruner := NewPruner(options)
		if err := pruner.Prune(recorder); err != nil {
			t.Errorf("Unexpected error in test: %s: %v", testName, err)
		}

		recorder.Verify(t, sets.NewString(test.ExpectedPruneNames...))
	}
}
