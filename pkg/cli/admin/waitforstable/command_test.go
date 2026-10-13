package waitforstable

import (
	"context"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	fakeconfig "github.com/openshift/client-go/config/clientset/versioned/fake"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func TestWaitForStableOptions_Run(t *testing.T) {
	tests := []struct {
		name             string
		clusterOperators []*configv1.ClusterOperator
		timeout          time.Duration
		wantErr          bool
	}{
		{
			name: "never available",
			clusterOperators: []*configv1.ClusterOperator{
				testClusterOperator(false, false, false),
			},
			timeout: time.Second,
			wantErr: true,
		},
		{
			name: "keeps progressing",
			clusterOperators: []*configv1.ClusterOperator{
				testClusterOperator(true, true, false),
			},
			timeout: time.Second,
			wantErr: true,
		},
		{
			name: "stays degraded",
			clusterOperators: []*configv1.ClusterOperator{
				testClusterOperator(true, false, true),
			},
			timeout: time.Second,
			wantErr: true,
		},
		{
			name: "available right away",
			clusterOperators: []*configv1.ClusterOperator{
				testClusterOperator(true, false, false),
			},
			timeout: time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs := []runtime.Object{}
			for i := range tt.clusterOperators {
				objs = append(objs, tt.clusterOperators[i])
			}

			fakeClient := fakeconfig.NewSimpleClientset(objs...)

			o := WaitForStableOptions{
				configClient: fakeClient.ConfigV1(),
				Timeout:      tt.timeout,
				waitInterval: 500 * time.Millisecond,
				IOStreams:    genericiooptions.NewTestIOStreamsDiscard(),
			}
			if err := o.Run(context.Background()); (err != nil) != tt.wantErr {
				t.Errorf("WaitForStableOptions.Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func testClusterOperator(available, progressing, degraded bool) *configv1.ClusterOperator {
	return &configv1.ClusterOperator{
		ObjectMeta: v1.ObjectMeta{
			Name: "test-operator",
		},
		Status: configv1.ClusterOperatorStatus{
			Conditions: []configv1.ClusterOperatorStatusCondition{
				{Type: configv1.OperatorAvailable, Status: boolToConditionStatus(available)},
				{Type: configv1.OperatorProgressing, Status: boolToConditionStatus(progressing)},
				{Type: configv1.OperatorDegraded, Status: boolToConditionStatus(degraded)},
			},
		},
	}

}

func boolToConditionStatus(b bool) configv1.ConditionStatus {
	if b {
		return configv1.ConditionTrue
	}
	return configv1.ConditionFalse
}
