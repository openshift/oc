package mco

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetUnavailableSince(t *testing.T) {
	testCases := []struct {
		name     string
		l        LayeredNodeState
		expected time.Time
	}{
		{
			name: "zero node",
		},
		{
			name: "unavailable for some reason",
			l: LayeredNodeState{
				unavailable: []badCondition{
					{reason: "some reason"},
				},
			},
		},
		{
			name: "unavailable for more than one reason",
			l: LayeredNodeState{
				unavailable: []badCondition{
					{reason: "some reason"},
					{reason: "some reason", message: "disk pressure", lastTransitionTime: time.Date(2009, 11, 17, 20, 34, 58, 651387237, time.UTC)},
					{reason: "some reason", message: "unavailable network", lastTransitionTime: time.Date(2008, 11, 17, 20, 34, 58, 651387237, time.UTC)},
				},
			},
			expected: time.Date(2008, 11, 17, 20, 34, 58, 651387237, time.UTC),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := tc.l.GetUnavailableSince()
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("%s: acutal differ from expected:\n%s", tc.name, diff)
			}
		})
	}
}

func TestGetUnavailableReasonMessageDescription(t *testing.T) {
	testCases := []struct {
		name                                                 string
		l                                                    LayeredNodeState
		expectedReason, expectedMessage, expectedDescription string
	}{
		{
			name: "zero node",
		},
		{
			name: "unavailable for some reason",
			l: LayeredNodeState{
				unavailable: []badCondition{
					{reason: "some reason"},
				},
			},
			expectedReason:      "some reason",
			expectedMessage:     "some reason",
			expectedDescription: "some reason",
		},
		{
			name: "unavailable for more than one reason",
			l: LayeredNodeState{
				node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "some-node"}},
				unavailable: []badCondition{
					{reason: "some reason a"},
					{reason: reasonOfUnavailabilityNodeDiskPressure, message: "disk pressure", lastTransitionTime: time.Date(2009, 11, 17, 20, 34, 58, 651387237, time.UTC)},
					{reason: reasonOfUnavailabilityNodeNetworkUnavailable, message: "unavailable network", lastTransitionTime: time.Date(2008, 11, 17, 20, 34, 58, 651387237, time.UTC)},
				},
			},
			expectedReason:      "some reason a | Disk pressure | Network unavailable",
			expectedMessage:     "some reason a | Node some-node has disk pressure | Node some-node has unavailable network",
			expectedDescription: `some reason a | Node has a DiskPressure!=False condition with Reason "Disk pressure" and message "disk pressure" | Node has a NetworkUnavailable!=False condition with Reason "Network unavailable" and message "unavailable network"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualReason := tc.l.GetUnavailableReason()
			if diff := cmp.Diff(tc.expectedReason, actualReason); diff != "" {
				t.Errorf("%s: acutal reason differ from expected:\n%s", tc.name, diff)
			}
			actualMessage := tc.l.GetUnavailableMessage()
			if diff := cmp.Diff(tc.expectedMessage, actualMessage); diff != "" {
				t.Errorf("%s: acutal message differ from expected:\n%s", tc.name, diff)
			}
			actualDescription := tc.l.GetUnavailableDescription()
			if diff := cmp.Diff(tc.expectedDescription, actualDescription); diff != "" {
				t.Errorf("%s: acutal description differ from expected:\n%s", tc.name, diff)
			}
		})
	}
}

func TestSeriouslyUnavailable(t *testing.T) {
	testCases := []struct {
		name     string
		l        LayeredNodeState
		expected bool
	}{
		{
			name: "zero node",
		},
		{
			name: "unavailable for some reason",
			l: LayeredNodeState{
				unavailable: []badCondition{
					{reason: "some reason"},
				},
			},
			expected: true,
		},
		{
			name: "unavailable and serious",
			l: LayeredNodeState{
				unavailable: []badCondition{
					{reason: reasonOfUnavailabilityNodeDiskPressure, message: "disk pressure", lastTransitionTime: time.Date(2009, 11, 17, 20, 34, 58, 651387237, time.UTC)},
				},
			},
			expected: true,
		},
		{
			name: "unavailable and not serious",
			l: LayeredNodeState{
				unavailable: []badCondition{
					{reason: reasonOfUnavailabilityNodeDiskPressure, message: "disk pressure", lastTransitionTime: time.Now()},
				},
			},
		},
		{
			name: "unavailable and serious 2nd",
			l: LayeredNodeState{
				unavailable: []badCondition{
					{reason: reasonOfUnavailabilityNodeDiskPressure, message: "disk pressure", lastTransitionTime: time.Now()},
					{reason: reasonOfUnavailabilityNodeNetworkUnavailable, message: "unavailable network", lastTransitionTime: time.Date(2008, 11, 17, 20, 34, 58, 651387237, time.UTC)},
				},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := tc.l.SeriouslyUnavailable()
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("%s: acutal differ from expected:\n%s", tc.name, diff)
			}
		})
	}
}

func TestCheckNodeReady(t *testing.T) {
	testCases := []struct {
		name               string
		l                  *LayeredNodeState
		expected           error
		expectedConditions []badCondition
	}{
		{
			name: "node has no status",
			l: &LayeredNodeState{
				node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "some-node"}},
			},
		},
		{
			name: "node is unschedulable",
			l: &LayeredNodeState{
				node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "some-node"}, Spec: corev1.NodeSpec{Unschedulable: true}},
			},
			expected:           fmt.Errorf("node some-node is reporting Unschedulable"),
			expectedConditions: []badCondition{{reason: "Node is marked unschedulable"}},
		},
		{
			name: "node has disk pressure",
			l: &LayeredNodeState{
				node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "some-node"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue, Message: "message 1", LastTransitionTime: metav1.NewTime(time.Date(2008, 11, 17, 20, 34, 58, 651387237, time.UTC))},
				}}},
			},
			expected:           fmt.Errorf("node some-node is reporting OutOfDisk=True"),
			expectedConditions: []badCondition{{reason: "Disk pressure", message: "message 1", lastTransitionTime: time.Date(2008, 11, 17, 20, 34, 58, 651387237, time.UTC)}},
		},
		{
			name: "node is unschedulable and has disk pressure",
			l: &LayeredNodeState{
				node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "some-node"}, Spec: corev1.NodeSpec{Unschedulable: true}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue, Message: "message 1", LastTransitionTime: metav1.NewTime(time.Date(2008, 11, 17, 20, 34, 58, 651387237, time.UTC))},
				}}},
			},
			expected:           fmt.Errorf("node some-node is reporting OutOfDisk=True"),
			expectedConditions: []badCondition{{reason: "Disk pressure", message: "message 1", lastTransitionTime: time.Date(2008, 11, 17, 20, 34, 58, 651387237, time.UTC)}, {reason: "Node is marked unschedulable"}},
		},
		{
			name: "node has disk pressure and is not ready ",
			l: &LayeredNodeState{
				node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "some-node"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue, Message: "message 1", LastTransitionTime: metav1.NewTime(time.Date(2008, 11, 17, 20, 34, 58, 651387237, time.UTC))},
					{Type: corev1.NodeReady, Status: corev1.ConditionFalse, Message: "message 2", LastTransitionTime: metav1.NewTime(time.Date(2008, 11, 17, 20, 34, 38, 651387237, time.UTC))},
				}}},
			},
			expected: fmt.Errorf("node some-node is reporting OutOfDisk=True"),
			expectedConditions: []badCondition{{reason: "Disk pressure", message: "message 1", lastTransitionTime: time.Date(2008, 11, 17, 20, 34, 58, 651387237, time.UTC)},
				{reason: "Not ready", message: "message 2", lastTransitionTime: time.Date(2008, 11, 17, 20, 34, 38, 651387237, time.UTC)}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := checkNodeReady(tc.l)
			if diff := cmp.Diff(tc.expected, actual, cmp.Comparer(func(x, y error) bool {
				if x == nil || y == nil {
					return x == nil && y == nil
				}
				return x.Error() == y.Error()
			})); diff != "" {
				t.Errorf("%s: acutal error differ from expected:\n%s", tc.name, diff)
			}

			if diff := cmp.Diff(tc.expectedConditions, tc.l.unavailable, cmp.AllowUnexported(badCondition{})); diff != "" {
				t.Errorf("%s: acutal differ from expected:\n%s", tc.name, diff)
			}

		})
	}
}
