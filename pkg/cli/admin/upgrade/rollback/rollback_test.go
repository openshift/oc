package rollback

import (
	"context"
	"fmt"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type mockClusterVersionInterface struct {
	clusterVersion *configv1.ClusterVersion
	patch          string
}

func (i *mockClusterVersionInterface) Get(_ context.Context, name string, _ metav1.GetOptions) (*configv1.ClusterVersion, error) {
	expectedName := "version"
	if name != expectedName {
		return nil, fmt.Errorf("unrecognized Get name: %q != %q", name, expectedName)
	}

	return i.clusterVersion, nil
}

func (i *mockClusterVersionInterface) Patch(_ context.Context, name string, pt types.PatchType, data []byte, _ metav1.PatchOptions, subresources ...string) (result *configv1.ClusterVersion, err error) {
	expectedName := "version"
	if name != expectedName {
		return nil, fmt.Errorf("unrecognized Patch name: %q != %q", name, expectedName)
	}

	expectedPatchType := types.MergePatchType
	if pt != expectedPatchType {
		return nil, fmt.Errorf("unrecognized Patch type: %v != %v", pt, expectedPatchType)
	}

	if len(subresources) > 0 {
		return nil, fmt.Errorf("unrecognized Patch subresources: %v", subresources)
	}

	i.patch = string(data)
	var clusterVersion *configv1.ClusterVersion // nil, because we know options.Run does not consume this return value
	return clusterVersion, nil
}

func TestRollback(t *testing.T) {
	ctx := context.Background()

	for _, testCase := range []struct {
		name           string
		clusterVersion *configv1.ClusterVersion
		expectedPatch  string
		expectedError  string
		expectedOut    string
	}{
		{
			name: "installing",
			clusterVersion: &configv1.ClusterVersion{
				Status: configv1.ClusterVersionStatus{
					Desired: configv1.Release{Version: "1.2.3", Image: "example.com/a"},
					Conditions: []configv1.ClusterOperatorStatusCondition{
						{
							Type:    configv1.OperatorProgressing,
							Status:  configv1.ConditionTrue,
							Reason:  "ClusterOperatorNotAvailable",
							Message: "Still installing",
						},
					},
					History: []configv1.UpdateHistory{
						{State: configv1.PartialUpdate, Version: "1.2.3", Image: "example.com/a"},
					},
				},
			},
			expectedError: "no previous version found in ClusterVersion's status.history besides the current 1.2.3 (example.com/a).",
		}, {
			name: "installed",
			clusterVersion: &configv1.ClusterVersion{
				Status: configv1.ClusterVersionStatus{
					Desired: configv1.Release{Version: "1.2.3", Image: "example.com/a"},
					Conditions: []configv1.ClusterOperatorStatusCondition{
						{
							Type:    configv1.OperatorProgressing,
							Status:  configv1.ConditionFalse,
							Reason:  "AsExpected",
							Message: "Happy on 1.2.3",
						},
					},
					History: []configv1.UpdateHistory{
						{State: configv1.CompletedUpdate, Version: "1.2.3", Image: "example.com/a"},
					},
				},
			},
			expectedError: "no previous version found in ClusterVersion's status.history besides the current 1.2.3 (example.com/a).",
		}, {
			name: "update in progress",
			clusterVersion: &configv1.ClusterVersion{
				Status: configv1.ClusterVersionStatus{
					Desired: configv1.Release{Version: "1.2.4", Image: "example.com/b"},
					Conditions: []configv1.ClusterOperatorStatusCondition{
						{
							Type:    configv1.OperatorProgressing,
							Status:  configv1.ConditionTrue,
							Reason:  "Updating",
							Message: "Off to 1.2.4",
						},
					},
					History: []configv1.UpdateHistory{
						{State: configv1.PartialUpdate, Version: "1.2.4", Image: "example.com/b"},
						{State: configv1.CompletedUpdate, Version: "1.2.3", Image: "example.com/a"},
					},
				},
			},
			expectedError: "unable to rollback while an update is Progressing=True: Updating: Off to 1.2.4.",
		}, {
			name: "after architecture pivot",
			clusterVersion: &configv1.ClusterVersion{
				Status: configv1.ClusterVersionStatus{
					Desired: configv1.Release{Version: "1.2.3", Image: "example.com/b"},
					Conditions: []configv1.ClusterOperatorStatusCondition{
						{
							Type:    configv1.OperatorProgressing,
							Status:  configv1.ConditionFalse,
							Reason:  "AsExpected",
							Message: "Happy on a new multi-arch image for 1.2.3",
						},
					},
					History: []configv1.UpdateHistory{
						{State: configv1.CompletedUpdate, Version: "1.2.3", Image: "example.com/b"},
						{State: configv1.CompletedUpdate, Version: "1.2.3", Image: "example.com/a"},
					},
				},
			},
			expectedError: "previous version 1.2.3 (example.com/a) is greater than or equal to current version 1.2.3 (example.com/b).  Use 'oc adm upgrade ...' to update, and not this rollback command.",
		}, {
			name: "after minor update",
			clusterVersion: &configv1.ClusterVersion{
				Status: configv1.ClusterVersionStatus{
					Desired: configv1.Release{Version: "1.3.0", Image: "example.com/b"},
					Conditions: []configv1.ClusterOperatorStatusCondition{
						{
							Type:    configv1.OperatorProgressing,
							Status:  configv1.ConditionFalse,
							Reason:  "AsExpected",
							Message: "Happy on 1.3.0",
						},
					},
					History: []configv1.UpdateHistory{
						{State: configv1.CompletedUpdate, Version: "1.3.0", Image: "example.com/b"},
						{State: configv1.CompletedUpdate, Version: "1.2.3", Image: "example.com/a"},
					},
				},
			},
			expectedError: "1.2.3 is less than the current target 1.3.0 and matches the cluster's previous version, but rollbacks that change major or minor versions are not recommended.",
		}, {
			name: "after patch update",
			clusterVersion: &configv1.ClusterVersion{
				Status: configv1.ClusterVersionStatus{
					Desired: configv1.Release{Version: "1.2.4", Image: "example.com/b"},
					Conditions: []configv1.ClusterOperatorStatusCondition{
						{
							Type:    configv1.OperatorProgressing,
							Status:  configv1.ConditionFalse,
							Reason:  "AsExpected",
							Message: "Happy on 1.2.4",
						},
					},
					History: []configv1.UpdateHistory{
						{State: configv1.CompletedUpdate, Version: "1.2.4", Image: "example.com/b"},
						{State: configv1.CompletedUpdate, Version: "1.2.3", Image: "example.com/a"},
					},
				},
			},
			expectedPatch: `{"spec":{"desiredUpdate": {"architecture":"","version":"1.2.3","image":"example.com/a","force":false}}}`,
			expectedOut:   "Requested rollback from 1.2.4 to 1.2.3\n",
		}, {
			name: "after re-targeted partial update",
			clusterVersion: &configv1.ClusterVersion{
				Status: configv1.ClusterVersionStatus{
					Desired: configv1.Release{Version: "1.2.5", Image: "example.com/c"},
					Conditions: []configv1.ClusterOperatorStatusCondition{
						{
							Type:    configv1.OperatorProgressing,
							Status:  configv1.ConditionFalse,
							Reason:  "AsExpected",
							Message: "Happy on 1.2.5",
						},
					},
					History: []configv1.UpdateHistory{
						{State: configv1.CompletedUpdate, Version: "1.2.5", Image: "example.com/c"},
						{State: configv1.PartialUpdate, Version: "1.2.4", Image: "example.com/b"},
						{State: configv1.CompletedUpdate, Version: "1.2.3", Image: "example.com/a"},
					},
				},
			},
			expectedPatch: `{"spec":{"desiredUpdate": {"architecture":"","version":"1.2.4","image":"example.com/b","force":false}}}`,
			expectedOut:   "Requested rollback from 1.2.5 to 1.2.4\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			streams, _, out, errOut := genericiooptions.NewTestIOStreams()
			clusterVersion := testCase.clusterVersion
			if clusterVersion != nil {
				clusterVersion.ObjectMeta.Name = "version"
			}
			client := &mockClusterVersionInterface{clusterVersion: clusterVersion}
			o := &options{
				IOStreams: streams,
				Client:    client,
			}
			err := o.Run(ctx)
			if (err == nil && testCase.expectedError != "") || (err != nil && err.Error() != testCase.expectedError) {
				t.Errorf("Run() error: %v (expected %q)", err, testCase.expectedError)
			}

			if client.patch != testCase.expectedPatch {
				t.Errorf("Run() patch %q, expected %q", client.patch, testCase.expectedPatch)
			}

			if out.String() != testCase.expectedOut {
				t.Errorf("Run() output %q, expected %q", out.String(), testCase.expectedOut)
			}

			if errOut.String() != "" {
				t.Errorf("Run() error output %q, expected none", errOut.String())
			}
		})
	}
}
