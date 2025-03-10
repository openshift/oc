package status

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/utils/ptr"

	updatev1alpha1 "github.com/openshift/api/update/v1alpha1"
)

func compareWithFixture(t *testing.T, actualOut []byte, usPath string, outputSuffix string) {
	t.Helper()
	expectedOutPath := strings.Replace(usPath, "-us.yaml", outputSuffix, 1)

	if update := os.Getenv("UPDATE"); update != "" {
		if err := os.WriteFile(expectedOutPath, actualOut, 0644); err != nil {
			t.Fatalf("Error when writing output fixture: %v", err)
		}
		return
	}

	expectedOut, err := os.ReadFile(expectedOutPath)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("Error when reading output fixture: %v", err)
		} else {
			t.Fatalf("Output file %s does not exist. You may rerun this test with UPDATE=true to create output file with the following actual output:\n%s", expectedOutPath, actualOut)
		}
	}

	if diff := cmp.Diff(string(expectedOut), string(actualOut)); diff != "" {
		t.Errorf("Output differs from expected (%s):\n%s", filepath.Base(expectedOutPath), diff)
	}
}

var cpUpdatingTrue = metav1.Condition{
	Type:   string(updatev1alpha1.ControlPlaneUpdating),
	Status: metav1.ConditionTrue,
	Reason: string(updatev1alpha1.ControlPlaneClusterVersionProgressing),
}

var cvUpdatingFalse = metav1.Condition{
	Type:   string(updatev1alpha1.ClusterVersionStatusInsightUpdating),
	Status: metav1.ConditionFalse,
	Reason: string(updatev1alpha1.ClusterVersionNotProgressing),
}

var cvUpdatingTrue = metav1.Condition{
	Type:   string(updatev1alpha1.ClusterVersionStatusInsightUpdating),
	Status: metav1.ConditionTrue,
	Reason: string(updatev1alpha1.ClusterVersionProgressing),
}

var mcpUpdatingFalseCompleted = metav1.Condition{
	Type:   MachineConfigPoolStatusInsightUpdating,
	Status: metav1.ConditionFalse,
	Reason: MachineConfigPoolStatusInsightUpdatingReasonCompleted,
}

var mcpUpdatingFalsePending = metav1.Condition{
	Type:   MachineConfigPoolStatusInsightUpdating,
	Status: metav1.ConditionFalse,
	Reason: MachineConfigPoolStatusInsightUpdatingReasonPending,
}

var anchorLatestTime = metav1.NewTime(time.Date(2025, 2, 4, 12, 42, 0, 0, time.UTC))

var coHealthyTrue = metav1.Condition{
	Type:               string(updatev1alpha1.ClusterOperatorStatusInsightHealthy),
	Status:             metav1.ConditionTrue,
	Reason:             string(updatev1alpha1.ClusterOperatorHealthyReasonAsExpected),
	LastTransitionTime: anchorLatestTime,
}

var coUpdatingFalseUpdated = metav1.Condition{
	Type:   string(updatev1alpha1.ClusterOperatorStatusInsightUpdating),
	Status: metav1.ConditionFalse,
	Reason: string(updatev1alpha1.ClusterOperatorUpdatingReasonUpdated),
}

var coUpdatingFalsePending = metav1.Condition{
	Type:   string(updatev1alpha1.ClusterOperatorStatusInsightUpdating),
	Status: metav1.ConditionFalse,
	Reason: string(updatev1alpha1.ClusterOperatorUpdatingReasonPending),
}

var nodeUpdatingPending = metav1.Condition{
	Type:   string(updatev1alpha1.NodeStatusInsightUpdating),
	Status: metav1.ConditionFalse,
	Reason: string(updatev1alpha1.NodeUpdatePending),
}

var nodeDegradedFalse = metav1.Condition{
	Type:   string(updatev1alpha1.NodeStatusInsightDegraded),
	Status: metav1.ConditionFalse,
	Reason: "AsExpected",
}
var nodeAvailableTrue = metav1.Condition{
	Type:   string(updatev1alpha1.NodeStatusInsightAvailable),
	Status: metav1.ConditionTrue,
	Reason: "AsExpected",
}

func xPendingHealthyClusterOperators(n int) []updatev1alpha1.ControlPlaneInsight {
	co := make([]updatev1alpha1.ControlPlaneInsight, n)
	for i := 0; i < n; i++ {
		co[i] = updatev1alpha1.ControlPlaneInsight{
			UID: fmt.Sprintf("co-%d", i),
			ControlPlaneInsightUnion: updatev1alpha1.ControlPlaneInsightUnion{
				Type: updatev1alpha1.ClusterOperatorStatusInsightType,
				ClusterOperatorStatusInsight: &updatev1alpha1.ClusterOperatorStatusInsight{
					Conditions: []metav1.Condition{coHealthyTrue, coUpdatingFalsePending},
				},
			},
		}
	}
	return co
}

var thirtyPendingHealthyClusterOperators = xPendingHealthyClusterOperators(30)

var fixtures = map[string]updatev1alpha1.UpdateStatusStatus{
	"examples/not-upgrading-us.yaml": {
		ControlPlane: updatev1alpha1.ControlPlane{
			Informers: []updatev1alpha1.ControlPlaneInformer{
				{
					Name: "cpi",
					Insights: []updatev1alpha1.ControlPlaneInsight{
						{
							UID: "cv-version",
							ControlPlaneInsightUnion: updatev1alpha1.ControlPlaneInsightUnion{
								Type: updatev1alpha1.ClusterVersionStatusInsightType,
								ClusterVersionStatusInsight: &updatev1alpha1.ClusterVersionStatusInsight{
									Conditions: []metav1.Condition{cvUpdatingFalse},
								},
							},
						},
					},
				},
			},
		},
		WorkerPools: []updatev1alpha1.Pool{
			{
				Name: "worker",
				Informers: []updatev1alpha1.WorkerPoolInformer{
					{
						Name: "nodes",
						Insights: []updatev1alpha1.WorkerPoolInsight{
							{
								UID: "mcp-worker-status",
								WorkerPoolInsightUnion: updatev1alpha1.WorkerPoolInsightUnion{
									Type: updatev1alpha1.MachineConfigPoolStatusInsightType,
									MachineConfigPoolStatusInsight: &updatev1alpha1.MachineConfigPoolStatusInsight{
										Conditions: []metav1.Condition{mcpUpdatingFalseCompleted},
									},
								},
							},
						},
					},
				},
			},
		},
	},
	"examples/4.15.0-ec2-early-us.yaml": {
		ControlPlane: updatev1alpha1.ControlPlane{
			Conditions: []metav1.Condition{cpUpdatingTrue},
			Resource: updatev1alpha1.ResourceRef{
				Group:    "config.openshift.io",
				Resource: "clusterversions",
				Name:     "version",
			},
			PoolResource: &updatev1alpha1.PoolResourceRef{
				ResourceRef: updatev1alpha1.ResourceRef{
					Group:    "machineconfiguration.openshift.io",
					Resource: "machineconfigpools",
					Name:     "master",
				},
			},
			Informers: []updatev1alpha1.ControlPlaneInformer{
				{
					Name: "cpi",
					Insights: []updatev1alpha1.ControlPlaneInsight{
						{
							UID: "cv-version",
							ControlPlaneInsightUnion: updatev1alpha1.ControlPlaneInsightUnion{
								Type: updatev1alpha1.ClusterVersionStatusInsightType,
								ClusterVersionStatusInsight: &updatev1alpha1.ClusterVersionStatusInsight{
									Conditions: []metav1.Condition{cvUpdatingTrue},
									Resource: updatev1alpha1.ResourceRef{
										Group:    "config.openshift.io",
										Resource: "clusterversions",
										Name:     "version",
									},
									Assessment: updatev1alpha1.ControlPlaneAssessmentProgressing,
									Versions: updatev1alpha1.ControlPlaneUpdateVersions{
										Previous: updatev1alpha1.Version{
											Version: "4.14.1",
											Metadata: []updatev1alpha1.VersionMetadata{
												{
													Key: updatev1alpha1.PartialMetadata,
												},
											},
										},
										Target: updatev1alpha1.Version{
											Version: "4.15.0-ec.2",
										},
									},
									Completion:           3,
									StartedAt:            metav1.NewTime(anchorLatestTime.Time.Add(-89 * time.Second)),
									EstimatedCompletedAt: ptr.To(metav1.NewTime(anchorLatestTime.Time.Add(85*time.Minute + 1*time.Second))),
								},
							},
						},
						{
							UID: "co-config-operator",
							ControlPlaneInsightUnion: updatev1alpha1.ControlPlaneInsightUnion{
								Type: updatev1alpha1.ClusterOperatorStatusInsightType,
								ClusterOperatorStatusInsight: &updatev1alpha1.ClusterOperatorStatusInsight{
									Conditions: []metav1.Condition{coHealthyTrue, coUpdatingFalseUpdated},
									Name:       "config-operator",
									Resource: updatev1alpha1.ResourceRef{
										Group:    "config.openshift.io",
										Resource: "clusteroperators",
										Name:     "config-operator",
									},
								},
							},
						},
						{
							UID: "co-etcd",
							ControlPlaneInsightUnion: updatev1alpha1.ControlPlaneInsightUnion{
								Type: updatev1alpha1.ClusterOperatorStatusInsightType,
								ClusterOperatorStatusInsight: &updatev1alpha1.ClusterOperatorStatusInsight{
									Conditions: []metav1.Condition{
										coHealthyTrue,
										{
											Type:   string(updatev1alpha1.ClusterOperatorStatusInsightUpdating),
											Status: metav1.ConditionTrue,
											// Reason:             fmt.Sprintf("%s:%s", updatev1alpha1.ClusterOperatorUpdatingReasonProgressing, "NodeInstaller"),
											LastTransitionTime: metav1.NewTime(anchorLatestTime.Time.Add(-45 * time.Second)),
											Message:            "NodeInstallerProgressing: 2 nodes are at revision 33; 1 nodes are at revision 34",
											Reason:             "NodeInstaller",
										},
									},
									Name: "etcd",
									Resource: updatev1alpha1.ResourceRef{
										Group:    "config.openshift.io",
										Resource: "clusteroperators",
										Name:     "etcd",
									},
								},
							},
						},
						{
							UID: "co-kube-apiserver",
							ControlPlaneInsightUnion: updatev1alpha1.ControlPlaneInsightUnion{
								Type: updatev1alpha1.ClusterOperatorStatusInsightType,
								ClusterOperatorStatusInsight: &updatev1alpha1.ClusterOperatorStatusInsight{
									Conditions: []metav1.Condition{
										coHealthyTrue,
										{
											Type:   string(updatev1alpha1.ClusterOperatorStatusInsightUpdating),
											Status: metav1.ConditionTrue,
											// Reason:             fmt.Sprintf("%s:%s", updatev1alpha1.ClusterOperatorUpdatingReasonProgressing, "NodeInstaller"),
											LastTransitionTime: metav1.NewTime(anchorLatestTime.Time.Add(-368 * time.Second)),
											Message:            "NodeInstallerProgressing: 3 nodes are at revision 274; 0 nodes have achieved new revision 276",
											Reason:             "NodeInstaller",
										},
									},
									Name: "kube-apiserver",
									Resource: updatev1alpha1.ResourceRef{
										Group:    "config.openshift.io",
										Resource: "clusteroperators",
										Name:     "kube-apiserver",
									},
								},
							},
						},
						thirtyPendingHealthyClusterOperators[0],
						thirtyPendingHealthyClusterOperators[1],
						thirtyPendingHealthyClusterOperators[2],
						thirtyPendingHealthyClusterOperators[3],
						thirtyPendingHealthyClusterOperators[4],
						thirtyPendingHealthyClusterOperators[5],
						thirtyPendingHealthyClusterOperators[6],
						thirtyPendingHealthyClusterOperators[7],
						thirtyPendingHealthyClusterOperators[8],
						thirtyPendingHealthyClusterOperators[9],
						thirtyPendingHealthyClusterOperators[10],
						thirtyPendingHealthyClusterOperators[11],
						thirtyPendingHealthyClusterOperators[12],
						thirtyPendingHealthyClusterOperators[13],
						thirtyPendingHealthyClusterOperators[14],
						thirtyPendingHealthyClusterOperators[15],
						thirtyPendingHealthyClusterOperators[16],
						thirtyPendingHealthyClusterOperators[17],
						thirtyPendingHealthyClusterOperators[18],
						thirtyPendingHealthyClusterOperators[19],
						thirtyPendingHealthyClusterOperators[20],
						thirtyPendingHealthyClusterOperators[21],
						thirtyPendingHealthyClusterOperators[22],
						thirtyPendingHealthyClusterOperators[23],
						thirtyPendingHealthyClusterOperators[24],
						thirtyPendingHealthyClusterOperators[25],
						thirtyPendingHealthyClusterOperators[26],
						thirtyPendingHealthyClusterOperators[27],
						thirtyPendingHealthyClusterOperators[28],
						thirtyPendingHealthyClusterOperators[29],
						{
							UID: "node-cp-1",
							ControlPlaneInsightUnion: updatev1alpha1.ControlPlaneInsightUnion{
								Type: updatev1alpha1.NodeStatusInsightType,
								NodeStatusInsight: &updatev1alpha1.NodeStatusInsight{
									Resource: updatev1alpha1.ResourceRef{
										Group:    "core",
										Resource: "nodes",
										Name:     "ip-10-0-30-217.us-east-2.compute.internal",
									},
									PoolResource: updatev1alpha1.PoolResourceRef{
										ResourceRef: updatev1alpha1.ResourceRef{
											Group:    "machineconfiguration.openshift.io",
											Resource: "machineconfigpools",
											Name:     "master",
										},
									},
									Conditions:    []metav1.Condition{nodeUpdatingPending, nodeDegradedFalse, nodeAvailableTrue},
									Scope:         updatev1alpha1.ControlPlaneScope,
									Name:          "ip-10-0-30-217.us-east-2.compute.internal",
									Version:       "4.14.1",
									EstToComplete: nil,
								},
							},
						},
						{
							UID: "node-cp-2",
							ControlPlaneInsightUnion: updatev1alpha1.ControlPlaneInsightUnion{
								Type: updatev1alpha1.NodeStatusInsightType,
								NodeStatusInsight: &updatev1alpha1.NodeStatusInsight{
									Resource: updatev1alpha1.ResourceRef{
										Group:    "core",
										Resource: "nodes",
										Name:     "ip-10-0-53-40.us-east-2.compute.internal",
									},
									PoolResource: updatev1alpha1.PoolResourceRef{
										ResourceRef: updatev1alpha1.ResourceRef{
											Group:    "machineconfiguration.openshift.io",
											Resource: "machineconfigpools",
											Name:     "master",
										},
									},
									Conditions:    []metav1.Condition{nodeUpdatingPending, nodeDegradedFalse, nodeAvailableTrue},
									Scope:         updatev1alpha1.ControlPlaneScope,
									Name:          "ip-10-0-53-40.us-east-2.compute.internal",
									Version:       "4.14.1",
									EstToComplete: nil,
								},
							},
						},
						{
							UID: "node-cp-3",
							ControlPlaneInsightUnion: updatev1alpha1.ControlPlaneInsightUnion{
								Type: updatev1alpha1.NodeStatusInsightType,
								NodeStatusInsight: &updatev1alpha1.NodeStatusInsight{
									Resource: updatev1alpha1.ResourceRef{
										Group:    "core",
										Resource: "nodes",
										Name:     "ip-10-0-92-180.us-east-2.compute.internal",
									},
									PoolResource: updatev1alpha1.PoolResourceRef{
										ResourceRef: updatev1alpha1.ResourceRef{
											Group:    "machineconfiguration.openshift.io",
											Resource: "machineconfigpools",
											Name:     "master",
										},
									},
									Conditions:    []metav1.Condition{nodeUpdatingPending, nodeDegradedFalse, nodeAvailableTrue},
									Scope:         updatev1alpha1.ControlPlaneScope,
									Name:          "ip-10-0-92-180.us-east-2.compute.internal",
									Version:       "4.14.1",
									EstToComplete: nil,
								},
							},
						},
						{
							UID: "mcp-master",
							ControlPlaneInsightUnion: updatev1alpha1.ControlPlaneInsightUnion{
								Type: updatev1alpha1.MachineConfigPoolStatusInsightType,
								MachineConfigPoolStatusInsight: &updatev1alpha1.MachineConfigPoolStatusInsight{
									Conditions: []metav1.Condition{mcpUpdatingFalsePending},
									Name:       "master",
									Resource: updatev1alpha1.PoolResourceRef{
										ResourceRef: updatev1alpha1.ResourceRef{
											Group:    "machineconfiguration.openshift.io",
											Resource: "machineconfigpools",
											Name:     "master",
										},
									},
									Scope:      updatev1alpha1.ControlPlaneScope,
									Assessment: updatev1alpha1.PoolPending,
									Completion: 0,
									Summaries: []updatev1alpha1.NodeSummary{
										{Type: updatev1alpha1.NodesTotal, Count: 3},
										{Type: updatev1alpha1.NodesAvailable, Count: 3},
										{Type: updatev1alpha1.NodesProgressing, Count: 0},
										{Type: updatev1alpha1.NodesOutdated, Count: 3},
										{Type: updatev1alpha1.NodesDraining, Count: 0},
										{Type: updatev1alpha1.NodesExcluded, Count: 0},
										{Type: updatev1alpha1.NodesDegraded, Count: 0},
									},
								},
							},
						},
						{
							UID: "health-partial-update",
							ControlPlaneInsightUnion: updatev1alpha1.ControlPlaneInsightUnion{
								Type: updatev1alpha1.HealthInsightType,
								HealthInsight: &updatev1alpha1.HealthInsight{
									StartedAt: metav1.NewTime(anchorLatestTime.Time.Add(-89 * time.Second)),
									Scope: updatev1alpha1.InsightScope{
										Type: updatev1alpha1.ControlPlaneScope,
										Resources: []updatev1alpha1.ResourceRef{
											{
												Group:    "config.openshift.io",
												Resource: "clusterversions",
												Name:     "version",
											},
										},
									},
									Impact: updatev1alpha1.InsightImpact{
										Level:       updatev1alpha1.WarningImpactLevel,
										Type:        updatev1alpha1.NoneImpactType,
										Summary:     "Previous update to 4.14.1 never completed, last complete update was 4.14.0-rc.7",
										Description: "Current update to 4.15.0-ec.2 was initiated while the previous update to version 4.14.1 was still in progress",
									},
									Remediation: updatev1alpha1.InsightRemediation{
										Reference: "https://docs.openshift.com/container-platform/latest/updating/troubleshooting_updates/gathering-data-cluster-update.html#gathering-clusterversion-history-cli_troubleshooting_updates",
									},
								},
							},
						},
					},
				},
			},
		},
		WorkerPools: []updatev1alpha1.Pool{
			{
				Name: "worker",
				Resource: updatev1alpha1.PoolResourceRef{
					ResourceRef: updatev1alpha1.ResourceRef{
						Group:    "machineconfiguration.openshift.io",
						Resource: "machineconfigpools",
						Name:     "worker",
					},
				},
				Informers: []updatev1alpha1.WorkerPoolInformer{
					{
						Name: "nodes",
						Insights: []updatev1alpha1.WorkerPoolInsight{
							{
								UID: "mcp-worker",
								WorkerPoolInsightUnion: updatev1alpha1.WorkerPoolInsightUnion{
									Type: updatev1alpha1.MachineConfigPoolStatusInsightType,
									MachineConfigPoolStatusInsight: &updatev1alpha1.MachineConfigPoolStatusInsight{
										Conditions: []metav1.Condition{mcpUpdatingFalsePending},
										Name:       "worker",
										Resource: updatev1alpha1.PoolResourceRef{
											ResourceRef: updatev1alpha1.ResourceRef{
												Group:    "machineconfiguration.openshift.io",
												Resource: "machineconfigpools",
												Name:     "worker",
											},
										},
										Scope:      updatev1alpha1.WorkerPoolScope,
										Assessment: updatev1alpha1.PoolPending,
										Completion: 0,
										Summaries: []updatev1alpha1.NodeSummary{
											{Type: updatev1alpha1.NodesTotal, Count: 3},
											{Type: updatev1alpha1.NodesAvailable, Count: 3},
											{Type: updatev1alpha1.NodesProgressing, Count: 0},
											{Type: updatev1alpha1.NodesOutdated, Count: 3},
											{Type: updatev1alpha1.NodesDraining, Count: 0},
											{Type: updatev1alpha1.NodesExcluded, Count: 0},
											{Type: updatev1alpha1.NodesDegraded, Count: 0},
										},
									},
								},
							},
							{
								UID: "node-wp-1",
								WorkerPoolInsightUnion: updatev1alpha1.WorkerPoolInsightUnion{
									Type: updatev1alpha1.NodeStatusInsightType,
									NodeStatusInsight: &updatev1alpha1.NodeStatusInsight{
										PoolResource: updatev1alpha1.PoolResourceRef{
											ResourceRef: updatev1alpha1.ResourceRef{Name: "worker"},
										},
										Conditions:    []metav1.Condition{nodeUpdatingPending, nodeDegradedFalse, nodeAvailableTrue},
										Name:          "ip-10-0-20-162.us-east-2.compute.internal",
										Version:       "4.14.1",
										EstToComplete: nil,
									},
								},
							},
							{
								UID: "node-wp-2",
								WorkerPoolInsightUnion: updatev1alpha1.WorkerPoolInsightUnion{
									Type: updatev1alpha1.NodeStatusInsightType,
									NodeStatusInsight: &updatev1alpha1.NodeStatusInsight{
										PoolResource: updatev1alpha1.PoolResourceRef{
											ResourceRef: updatev1alpha1.ResourceRef{Name: "worker"},
										},
										Conditions:    []metav1.Condition{nodeUpdatingPending, nodeDegradedFalse, nodeAvailableTrue},
										Name:          "ip-10-0-4-159.us-east-2.compute.internal",
										Version:       "4.14.1",
										EstToComplete: nil,
									},
								},
							},
							{
								UID: "node-wp-3",
								WorkerPoolInsightUnion: updatev1alpha1.WorkerPoolInsightUnion{
									Type: updatev1alpha1.NodeStatusInsightType,
									NodeStatusInsight: &updatev1alpha1.NodeStatusInsight{
										PoolResource: updatev1alpha1.PoolResourceRef{
											ResourceRef: updatev1alpha1.ResourceRef{Name: "worker"},
										},
										Conditions:    []metav1.Condition{nodeUpdatingPending, nodeDegradedFalse, nodeAvailableTrue},
										Name:          "ip-10-0-99-40.us-east-2.compute.internal",
										Version:       "4.14.1",
										EstToComplete: nil,
									},
								},
							},
						},
					},
				},
			},
		},
	},
}

func updateFixtureInput(t *testing.T, path string) {
	t.Helper()
	fixtureStatus, ok := fixtures[path]
	if !ok {
		return
	}

	fixture := updatev1alpha1.UpdateStatus{
		TypeMeta: metav1.TypeMeta{
			Kind:       "UpdateStatus",
			APIVersion: "update.openshift.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: fixtureStatus,
	}

	fixtureFile, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatalf("Error when opening fixture: %v", err)
	}
	defer fixtureFile.Close()

	jsonSerializer := json.NewSerializerWithOptions(json.DefaultMetaFactory, nil, nil, json.SerializerOptions{Yaml: true, Pretty: true})
	err = jsonSerializer.Encode(&fixture, fixtureFile)
	if err != nil {
		t.Fatalf("Error when encoding fixture: %v", err)
	}
}

func TestExamples(t *testing.T) {
	updateStatuses, err := filepath.Glob("examples/*-us.yaml")
	if err != nil {
		t.Fatalf("Error when listing examples: %v", err)
	}

	variants := []struct {
		name         string
		detailed     string
		outputSuffix string
	}{
		{
			name:         "normal output",
			detailed:     "none",
			outputSuffix: ".output",
		},
		{
			name:         "detailed output",
			detailed:     "all",
			outputSuffix: ".detailed-output",
		},
	}

	for _, us := range updateStatuses {
		updateFixtureInput(t, us)
		for _, variant := range variants {
			variant := variant
			t.Run(fmt.Sprintf("%s-%s", us, variant.name), func(t *testing.T) {
				t.Parallel()
				opts := &options{
					mockData:       mockData{updateStatusPath: us},
					detailedOutput: variant.detailed,
				}
				if err := opts.Complete(nil, nil, nil); err != nil {
					t.Fatalf("Error when completing options: %v", err)
				}

				var stdout, stderr bytes.Buffer
				opts.Out = &stdout
				opts.ErrOut = &stderr

				if err := opts.Run(context.Background()); err != nil {
					t.Fatalf("Error when running: %v", err)
				}

				compareWithFixture(t, stdout.Bytes(), us, variant.outputSuffix)
			})
		}
	}
}
