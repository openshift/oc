package status

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"

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

var mcpUpdatingFalse = metav1.Condition{
	Type:   MachineConfigPoolStatusInsightUpdating,
	Status: metav1.ConditionFalse,
	Reason: MachineConfigPoolStatusInsightUpdatingReasonCompleted,
}

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
