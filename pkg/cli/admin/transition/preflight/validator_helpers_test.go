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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	fakeconfigclient "github.com/openshift/client-go/config/clientset/versioned/fake"
	fakeoperatorclient "github.com/openshift/client-go/operator/clientset/versioned/fake"
	fake "k8s.io/client-go/kubernetes/fake"
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
