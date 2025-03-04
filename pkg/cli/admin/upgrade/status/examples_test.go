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

var mcpUpdatingFalse = metav1.Condition{
	Type:   MachineConfigPoolStatusInsightUpdating,
	Status: metav1.ConditionFalse,
	Reason: MachineConfigPoolStatusInsightUpdatingReasonCompleted,
}

var anchorLatestTime = metav1.Now()

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

var coUpdatingTrueProgressing = metav1.Condition{
	Type:   string(updatev1alpha1.ClusterOperatorStatusInsightUpdating),
	Status: metav1.ConditionTrue,
	Reason: string(updatev1alpha1.ClusterOperatorUpdatingReasonProgressing),
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
										Conditions: []metav1.Condition{mcpUpdatingFalse},
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
			Informers: []updatev1alpha1.ControlPlaneInformer{
				{
					Name: "cpi",
					Insights: []updatev1alpha1.ControlPlaneInsight{
						{
							UID: "cv-version",
							ControlPlaneInsightUnion: updatev1alpha1.ControlPlaneInsightUnion{
								Type: updatev1alpha1.ClusterVersionStatusInsightType,
								ClusterVersionStatusInsight: &updatev1alpha1.ClusterVersionStatusInsight{
									Conditions:           []metav1.Condition{cvUpdatingTrue},
									Assessment:           updatev1alpha1.ControlPlaneAssessmentProgressing,
									Completion:           3,
									StartedAt:            metav1.NewTime(anchorLatestTime.Time.Add(-89 * time.Second)),
									EstimatedCompletedAt: ptr.To(metav1.NewTime(anchorLatestTime.Time.Add(85*time.Minute + 1*time.Second))),
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
										Conditions: []metav1.Condition{mcpUpdatingFalse},
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
