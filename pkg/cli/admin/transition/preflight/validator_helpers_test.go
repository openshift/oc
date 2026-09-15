package preflight

/*
================================================================================
INDIVIDUAL VALIDATOR TESTS
================================================================================

This file tests individual validator methods in isolation. Each test verifies
that a specific validation function (validateClusterOperatorsStable,
validateControlPlaneNodeCount, etc.) correctly checks a particular aspect of
cluster readiness and returns the expected CheckResult.

These tests complement the orchestration tests in client_validator_test.go
which test the full Validate() flow with all checks integrated.

--------------------------------------------------------------------------------
TEST COVERAGE (8 subtests)
--------------------------------------------------------------------------------

INDIVIDUAL VALIDATORS
  - ClusterOperators stable                     - Condition checking pattern
  - Control plane node count (3)                - Counting pattern
  - Infrastructure node count (0)               - Inverse counting (NOT control-plane)
  - Control plane nodes schedulable             - Taint checking
  - Control plane nodes ready                   - Node condition iteration
  - Etcd quorum available                       - library-go v1helpers usage
  - Etcd not progressing                        - Etcd-specific conditions
  - Etcd voting members (3)                     - ConfigMap data parsing

--------------------------------------------------------------------------------
*/

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	fakeconfigclient "github.com/openshift/client-go/config/clientset/versioned/fake"
	fakeoperatorclient "github.com/openshift/client-go/operator/clientset/versioned/fake"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

// validatorTestCase defines a test case for individual validator methods
type validatorTestCase struct {
	name             string
	setupKube        func() *fake.Clientset
	setupConfig      func() *fakeconfigclient.Clientset
	setupOperator    func() *fakeoperatorclient.Clientset
	validate         func(*ClientSideValidator) CheckResult
	expectedName     string
	expectedSeverity CheckSeverity
	expectedStatus   CheckStatus
}

// TestIndividualValidators tests individual validator methods using table-driven approach
func TestIndividualValidators(t *testing.T) {
	testCases := []validatorTestCase{
		{
			name: "ClusterOperators stable - all healthy",
			setupKube: func() *fake.Clientset {
				return fake.NewClientset()
			},
			setupConfig: func() *fakeconfigclient.Clientset {
				return fakeconfigclient.NewSimpleClientset(
					newFakeClusterOperator("kube-apiserver", true, false, false),
					newFakeClusterOperator("etcd", true, false, false),
				)
			},
			setupOperator: func() *fakeoperatorclient.Clientset {
				return fakeoperatorclient.NewSimpleClientset()
			},
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateClusterOperatorsStable(context.Background())
			},
			expectedName:     CheckNameClusterOperatorsStable,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusPassed,
		},
		{
			name: "Control plane node count - exactly 3",
			setupKube: func() *fake.Clientset {
				return fake.NewClientset(
					newFakeNode("master-0", true, false, true, true),
					newFakeNode("master-1", true, false, true, true),
					newFakeNode("master-2", true, false, true, true),
				)
			},
			setupConfig: func() *fakeconfigclient.Clientset {
				return fakeconfigclient.NewSimpleClientset()
			},
			setupOperator: func() *fakeoperatorclient.Clientset {
				return fakeoperatorclient.NewSimpleClientset()
			},
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateControlPlaneNodeCount(context.Background(), 3)
			},
			expectedName:     CheckNameControlPlaneNodeCount,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusPassed,
		},
		{
			name: "Infrastructure node count - no workers (compact topology)",
			setupKube: func() *fake.Clientset {
				return fake.NewClientset(
					newFakeNode("master-0", true, false, true, true),
					newFakeNode("master-1", true, false, true, true),
					newFakeNode("master-2", true, false, true, true),
				)
			},
			setupConfig: func() *fakeconfigclient.Clientset {
				return fakeconfigclient.NewSimpleClientset()
			},
			setupOperator: func() *fakeoperatorclient.Clientset {
				return fakeoperatorclient.NewSimpleClientset()
			},
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateExactInfrastructureNodeCount(context.Background(), 0)
			},
			expectedName:     CheckNameInfrastructureNodeCount,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusPassed,
		},
		{
			name: "Control plane nodes schedulable - all 3 schedulable",
			setupKube: func() *fake.Clientset {
				return fake.NewClientset(
					newFakeNode("master-0", true, false, true, true),
					newFakeNode("master-1", true, false, true, true),
					newFakeNode("master-2", true, false, true, true),
				)
			},
			setupConfig: func() *fakeconfigclient.Clientset {
				return fakeconfigclient.NewSimpleClientset()
			},
			setupOperator: func() *fakeoperatorclient.Clientset {
				return fakeoperatorclient.NewSimpleClientset()
			},
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateControlPlaneNodesSchedulable(context.Background(), 3)
			},
			expectedName:     CheckNameControlPlaneNodesSchedulable,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusPassed,
		},
		{
			name: "Control plane nodes ready - all 3 ready",
			setupKube: func() *fake.Clientset {
				return fake.NewClientset(
					newFakeNode("master-0", true, false, true, true),
					newFakeNode("master-1", true, false, true, true),
					newFakeNode("master-2", true, false, true, true),
				)
			},
			setupConfig: func() *fakeconfigclient.Clientset {
				return fakeconfigclient.NewSimpleClientset()
			},
			setupOperator: func() *fakeoperatorclient.Clientset {
				return fakeoperatorclient.NewSimpleClientset()
			},
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateControlPlaneNodesReady(context.Background(), 3)
			},
			expectedName:     CheckNameControlPlaneNodesReady,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusPassed,
		},
		{
			name: "Etcd quorum - EtcdMembersAvailable=True",
			setupKube: func() *fake.Clientset {
				return fake.NewClientset()
			},
			setupConfig: func() *fakeconfigclient.Clientset {
				return fakeconfigclient.NewSimpleClientset()
			},
			setupOperator: func() *fakeoperatorclient.Clientset {
				return fakeoperatorclient.NewSimpleClientset(
					newFakeEtcdOperator(true, false),
				)
			},
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateEtcdQuorum(context.Background())
			},
			expectedName:     CheckNameEtcdQuorum,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusPassed,
		},
		{
			name: "Etcd not progressing - Progressing=False",
			setupKube: func() *fake.Clientset {
				return fake.NewClientset()
			},
			setupConfig: func() *fakeconfigclient.Clientset {
				return fakeconfigclient.NewSimpleClientset()
			},
			setupOperator: func() *fakeoperatorclient.Clientset {
				return fakeoperatorclient.NewSimpleClientset(
					newFakeEtcdOperator(true, false),
				)
			},
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateEtcdNotProgressing(context.Background())
			},
			expectedName:     CheckNameEtcdNotProgressing,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusPassed,
		},
		{
			name: "Etcd voting members - exactly 3",
			setupKube: func() *fake.Clientset {
				return fake.NewClientset(
					newFakeEtcdConfigMap(3),
				)
			},
			setupConfig: func() *fakeconfigclient.Clientset {
				return fakeconfigclient.NewSimpleClientset()
			},
			setupOperator: func() *fakeoperatorclient.Clientset {
				return fakeoperatorclient.NewSimpleClientset()
			},
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateEtcdVotingMembers(context.Background(), 3)
			},
			expectedName:     CheckNameEtcdVotingMembers,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusPassed,
		},
		{
			name:      "ClusterOperators stable - degraded",
			setupKube: func() *fake.Clientset { return fake.NewClientset() },
			setupConfig: func() *fakeconfigclient.Clientset {
				return fakeconfigclient.NewSimpleClientset(newFakeClusterOperator("kube-apiserver", true, false, true))
			},
			setupOperator: func() *fakeoperatorclient.Clientset { return fakeoperatorclient.NewSimpleClientset() },
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateClusterOperatorsStable(context.Background())
			},
			expectedName:     CheckNameClusterOperatorsStable,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusFailed,
		},
		{
			name: "Control plane node count - insufficient",
			setupKube: func() *fake.Clientset {
				return fake.NewClientset(newFakeNode("master-0", true, false, true, true), newFakeNode("master-1", true, false, true, true))
			},
			setupConfig:   func() *fakeconfigclient.Clientset { return fakeconfigclient.NewSimpleClientset() },
			setupOperator: func() *fakeoperatorclient.Clientset { return fakeoperatorclient.NewSimpleClientset() },
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateControlPlaneNodeCount(context.Background(), 3)
			},
			expectedName:     CheckNameControlPlaneNodeCount,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusFailed,
		},
		{
			name: "Infrastructure node count - dedicated worker",
			setupKube: func() *fake.Clientset {
				return fake.NewClientset(newFakeNode("worker-0", false, true, true, true))
			},
			setupConfig:   func() *fakeconfigclient.Clientset { return fakeconfigclient.NewSimpleClientset() },
			setupOperator: func() *fakeoperatorclient.Clientset { return fakeoperatorclient.NewSimpleClientset() },
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateExactInfrastructureNodeCount(context.Background(), 0)
			},
			expectedName:     CheckNameInfrastructureNodeCount,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusFailed,
		},
		{
			name: "Control plane nodes schedulable - NoExecute taint",
			setupKube: func() *fake.Clientset {
				return fake.NewClientset(
					newFakeNode("master-0", true, false, true, true),
					newFakeNode("master-1", true, false, true, true),
					newFakeNodeWithTaint("master-2", corev1.TaintEffectNoExecute),
				)
			},
			setupConfig:   func() *fakeconfigclient.Clientset { return fakeconfigclient.NewSimpleClientset() },
			setupOperator: func() *fakeoperatorclient.Clientset { return fakeoperatorclient.NewSimpleClientset() },
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateControlPlaneNodesSchedulable(context.Background(), 3)
			},
			expectedName:     CheckNameControlPlaneNodesSchedulable,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusFailed,
		},
		{
			name: "Control plane nodes ready - one not ready",
			setupKube: func() *fake.Clientset {
				return fake.NewClientset(
					newFakeNode("master-0", true, false, true, true),
					newFakeNode("master-1", true, false, true, true),
					newFakeNode("master-2", true, false, false, true),
				)
			},
			setupConfig:   func() *fakeconfigclient.Clientset { return fakeconfigclient.NewSimpleClientset() },
			setupOperator: func() *fakeoperatorclient.Clientset { return fakeoperatorclient.NewSimpleClientset() },
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateControlPlaneNodesReady(context.Background(), 3)
			},
			expectedName:     CheckNameControlPlaneNodesReady,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusFailed,
		},
		{
			name:        "Etcd quorum - unavailable",
			setupKube:   func() *fake.Clientset { return fake.NewClientset() },
			setupConfig: func() *fakeconfigclient.Clientset { return fakeconfigclient.NewSimpleClientset() },
			setupOperator: func() *fakeoperatorclient.Clientset {
				return fakeoperatorclient.NewSimpleClientset(newFakeEtcdOperator(false, false))
			},
			validate:         func(v *ClientSideValidator) CheckResult { return v.validateEtcdQuorum(context.Background()) },
			expectedName:     CheckNameEtcdQuorum,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusFailed,
		},
		{
			name:        "Etcd not progressing - progressing",
			setupKube:   func() *fake.Clientset { return fake.NewClientset() },
			setupConfig: func() *fakeconfigclient.Clientset { return fakeconfigclient.NewSimpleClientset() },
			setupOperator: func() *fakeoperatorclient.Clientset {
				return fakeoperatorclient.NewSimpleClientset(newFakeEtcdOperator(true, true))
			},
			validate:         func(v *ClientSideValidator) CheckResult { return v.validateEtcdNotProgressing(context.Background()) },
			expectedName:     CheckNameEtcdNotProgressing,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusFailed,
		},
		{
			name:             "Etcd voting members - wrong count",
			setupKube:        func() *fake.Clientset { return fake.NewClientset(newFakeEtcdConfigMap(2)) },
			setupConfig:      func() *fakeconfigclient.Clientset { return fakeconfigclient.NewSimpleClientset() },
			setupOperator:    func() *fakeoperatorclient.Clientset { return fakeoperatorclient.NewSimpleClientset() },
			validate:         func(v *ClientSideValidator) CheckResult { return v.validateEtcdVotingMembers(context.Background(), 3) },
			expectedName:     CheckNameEtcdVotingMembers,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusFailed,
		},
		{
			name:          "ClusterOperators stable - API error",
			setupKube:     func() *fake.Clientset { return fake.NewClientset() },
			setupConfig:   func() *fakeconfigclient.Clientset { return configClientWithError("list", "clusteroperators") },
			setupOperator: func() *fakeoperatorclient.Clientset { return fakeoperatorclient.NewSimpleClientset() },
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateClusterOperatorsStable(context.Background())
			},
			expectedName:     CheckNameClusterOperatorsStable,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusUnknown,
		},
		{
			name:          "Control plane node count - API error",
			setupKube:     func() *fake.Clientset { return kubeClientWithError("list", "nodes") },
			setupConfig:   func() *fakeconfigclient.Clientset { return fakeconfigclient.NewSimpleClientset() },
			setupOperator: func() *fakeoperatorclient.Clientset { return fakeoperatorclient.NewSimpleClientset() },
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateControlPlaneNodeCount(context.Background(), 3)
			},
			expectedName:     CheckNameControlPlaneNodeCount,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusUnknown,
		},
		{
			name:          "Infrastructure node count - API error",
			setupKube:     func() *fake.Clientset { return kubeClientWithError("list", "nodes") },
			setupConfig:   func() *fakeconfigclient.Clientset { return fakeconfigclient.NewSimpleClientset() },
			setupOperator: func() *fakeoperatorclient.Clientset { return fakeoperatorclient.NewSimpleClientset() },
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateExactInfrastructureNodeCount(context.Background(), 0)
			},
			expectedName:     CheckNameInfrastructureNodeCount,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusUnknown,
		},
		{
			name:          "Control plane nodes schedulable - API error",
			setupKube:     func() *fake.Clientset { return kubeClientWithError("list", "nodes") },
			setupConfig:   func() *fakeconfigclient.Clientset { return fakeconfigclient.NewSimpleClientset() },
			setupOperator: func() *fakeoperatorclient.Clientset { return fakeoperatorclient.NewSimpleClientset() },
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateControlPlaneNodesSchedulable(context.Background(), 3)
			},
			expectedName:     CheckNameControlPlaneNodesSchedulable,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusUnknown,
		},
		{
			name:          "Control plane nodes ready - API error",
			setupKube:     func() *fake.Clientset { return kubeClientWithError("list", "nodes") },
			setupConfig:   func() *fakeconfigclient.Clientset { return fakeconfigclient.NewSimpleClientset() },
			setupOperator: func() *fakeoperatorclient.Clientset { return fakeoperatorclient.NewSimpleClientset() },
			validate: func(v *ClientSideValidator) CheckResult {
				return v.validateControlPlaneNodesReady(context.Background(), 3)
			},
			expectedName:     CheckNameControlPlaneNodesReady,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusUnknown,
		},
		{
			name:             "Etcd quorum - API error",
			setupKube:        func() *fake.Clientset { return fake.NewClientset() },
			setupConfig:      func() *fakeconfigclient.Clientset { return fakeconfigclient.NewSimpleClientset() },
			setupOperator:    func() *fakeoperatorclient.Clientset { return operatorClientWithError("get", "etcds") },
			validate:         func(v *ClientSideValidator) CheckResult { return v.validateEtcdQuorum(context.Background()) },
			expectedName:     CheckNameEtcdQuorum,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusUnknown,
		},
		{
			name:             "Etcd not progressing - API error",
			setupKube:        func() *fake.Clientset { return fake.NewClientset() },
			setupConfig:      func() *fakeconfigclient.Clientset { return fakeconfigclient.NewSimpleClientset() },
			setupOperator:    func() *fakeoperatorclient.Clientset { return operatorClientWithError("get", "etcds") },
			validate:         func(v *ClientSideValidator) CheckResult { return v.validateEtcdNotProgressing(context.Background()) },
			expectedName:     CheckNameEtcdNotProgressing,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusUnknown,
		},
		{
			name:             "Etcd voting members - API error",
			setupKube:        func() *fake.Clientset { return kubeClientWithError("get", "configmaps") },
			setupConfig:      func() *fakeconfigclient.Clientset { return fakeconfigclient.NewSimpleClientset() },
			setupOperator:    func() *fakeoperatorclient.Clientset { return fakeoperatorclient.NewSimpleClientset() },
			validate:         func(v *ClientSideValidator) CheckResult { return v.validateEtcdVotingMembers(context.Background(), 3) },
			expectedName:     CheckNameEtcdVotingMembers,
			expectedSeverity: CheckSeverityWarning,
			expectedStatus:   CheckStatusUnknown,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			validator := NewClientSideValidator(
				tc.setupKube(),
				tc.setupConfig(),
				tc.setupOperator(),
			)

			result := tc.validate(validator)

			expected := CheckResult{
				Name:     tc.expectedName,
				Severity: tc.expectedSeverity,
				Status:   tc.expectedStatus,
			}
			// Ignore Message field since it's not part of the test expectations
			if diff := cmp.Diff(expected, result, cmpopts.IgnoreFields(CheckResult{}, "Message")); diff != "" {
				t.Errorf("unexpected check result (-want +got):\n%s", diff)
			}
		})
	}
}

func newFakeNodeWithTaint(name string, effect corev1.TaintEffect) *corev1.Node {
	node := newFakeNode(name, true, false, true, true)
	node.Spec.Taints = []corev1.Taint{{Key: "test", Effect: effect}}
	return node
}

func TestHasSchedulingBlockingTaint(t *testing.T) {
	tests := []struct {
		name   string
		effect corev1.TaintEffect
		want   bool
	}{
		{name: "no taint"},
		{name: "NoSchedule", effect: corev1.TaintEffectNoSchedule, want: true},
		{name: "NoExecute", effect: corev1.TaintEffectNoExecute, want: true},
		{name: "PreferNoSchedule", effect: corev1.TaintEffectPreferNoSchedule},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			node := &corev1.Node{}
			if tc.effect != "" {
				node.Spec.Taints = []corev1.Taint{{Key: "test", Effect: tc.effect}}
			}
			if got := hasSchedulingBlockingTaint(node); got != tc.want {
				t.Errorf("hasSchedulingBlockingTaint() = %t, want %t", got, tc.want)
			}
		})
	}
}

func kubeClientWithError(verb, resource string) *fake.Clientset {
	client := fake.NewClientset()
	client.PrependReactor(verb, resource, func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected API error")
	})
	return client
}

func configClientWithError(verb, resource string) *fakeconfigclient.Clientset {
	client := fakeconfigclient.NewSimpleClientset()
	client.PrependReactor(verb, resource, func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected API error")
	})
	return client
}

func operatorClientWithError(verb, resource string) *fakeoperatorclient.Clientset {
	client := fakeoperatorclient.NewSimpleClientset()
	client.PrependReactor(verb, resource, func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected API error")
	})
	return client
}
