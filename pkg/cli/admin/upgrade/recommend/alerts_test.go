package recommend

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/api/features"
)

func TestIsAcceptRisksEnabled(t *testing.T) {

	for _, testCase := range []struct {
		name              string
		featureGateConfig *configv1.FeatureGate
		expected          bool
	}{
		{
			name: "no feature gates",
		},
		{
			name: "ClusterUpdateAcceptRisks feature gate is enabled",
			featureGateConfig: &configv1.FeatureGate{
				Status: configv1.FeatureGateStatus{
					FeatureGates: []configv1.FeatureGateDetails{
						{
							Version: "4.22.0",
							Enabled: []configv1.FeatureGateAttributes{
								{
									Name: features.FeatureGateClusterUpdateAcceptRisks,
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "ClusterUpdateAcceptRisks feature gate is explicitly disabled",
			featureGateConfig: &configv1.FeatureGate{
				Status: configv1.FeatureGateStatus{
					FeatureGates: []configv1.FeatureGateDetails{
						{
							Version: "4.22.0",
							Disabled: []configv1.FeatureGateAttributes{
								{
									Name: features.FeatureGateClusterUpdateAcceptRisks,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "ClusterUpdateAcceptRisks feature gate is not explicitly enabled or disabled",
			featureGateConfig: &configv1.FeatureGate{
				Status: configv1.FeatureGateStatus{
					FeatureGates: []configv1.FeatureGateDetails{
						{
							Version:  "4.22.0",
							Enabled:  []configv1.FeatureGateAttributes{},
							Disabled: []configv1.FeatureGateAttributes{},
						},
					},
				},
			},
		},
		{
			name: "ClusterUpdateAcceptRisks feature gate is enabled for a different cluster version",
			featureGateConfig: &configv1.FeatureGate{
				Status: configv1.FeatureGateStatus{
					FeatureGates: []configv1.FeatureGateDetails{
						{
							Version: "4.21.0",
							Enabled: []configv1.FeatureGateAttributes{
								{
									Name: features.FeatureGateClusterUpdateAcceptRisks,
								},
							},
						},
					},
				},
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			actual := isAcceptRisksEnabled(testCase.featureGateConfig, "4.22.0")

			if actual != testCase.expected {
				t.Errorf("%v != %v", actual, testCase.expected)
			}
		})
	}
}

func TestIsHypershiftEnabled(t *testing.T) {
	for _, testCase := range []struct {
		name           string
		infrastructure *configv1.Infrastructure
		expected       bool
	}{
		{
			name: "no infrastructure",
		},
		{
			name: "hypershift enabled",
			infrastructure: &configv1.Infrastructure{
				Status: configv1.InfrastructureStatus{
					ControlPlaneTopology: configv1.ExternalTopologyMode,
				},
			},
			expected: true,
		},
		{
			name: "hypershift not enabled",
			infrastructure: &configv1.Infrastructure{
				Status: configv1.InfrastructureStatus{
					ControlPlaneTopology: configv1.HighlyAvailableTopologyMode,
				},
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			actual := isHostedCluster(testCase.infrastructure)

			if actual != testCase.expected {
				t.Errorf("%v != %v", actual, testCase.expected)
			}
		})
	}
}
