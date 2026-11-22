package transition

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	configv1 "github.com/openshift/api/config/v1"
	fakeconfigclient "github.com/openshift/client-go/config/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	restfake "k8s.io/client-go/rest/fake"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
	"sigs.k8s.io/yaml"
)

// TestValidateStatusOutput checks the status command accepts only json and
// yaml output formats.
func TestValidateStatusOutput(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		wantErr string
	}{
		{name: "empty"},
		{name: "json", format: "json"},
		{name: "yaml", format: "yaml"},
		{name: "invalid format", format: "wide", wantErr: "must be json or yaml"},
		{name: "name rejected", format: "name", wantErr: "must be json or yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := newStatusOptions(genericclioptions.NewTestIOStreamsDiscard())
			*o.printFlags.OutputFormat = tt.format

			err := o.validate()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("got %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

// statusInfrastructure builds an in-progress report with one evaluated
// transition for status tests: spec is ahead of status.
func statusInfrastructure() *configv1.Infrastructure {
	return &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster", Generation: 3},
		Spec: configv1.InfrastructureSpec{
			ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
			InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
		},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
			TopologyTransitionStatus: configv1.TopologyTransitionStatus{
				Conditions: []metav1.Condition{
					{
						Type:               configv1.TopologyTransitionsEvaluatedConditionType,
						Status:             metav1.ConditionTrue,
						ObservedGeneration: 3,
					},
					{
						Type:               configv1.TopologyTransitionCompletedConditionType,
						Status:             metav1.ConditionFalse,
						Reason:             "TransitionInProgress",
						Message:            "Topology transition in progress",
						ObservedGeneration: 3,
					},
				},
				Transitions: []configv1.TopologyTransition{{
					Source: configv1.TopologyState{
						ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
						InfrastructureTopology: configv1.SingleReplicaTopologyMode,
					},
					Target: configv1.TopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
					},
					Evaluations: []metav1.Condition{{
						Type:               configv1.TopologyTransitionAvailableConditionType,
						Status:             metav1.ConditionTrue,
						ObservedGeneration: 3,
					}},
				}},
			},
		},
	}
}

// TestStatusStructuredOutput verifies JSON and YAML include the Infrastructure
// generation, the spec and status topologies, and the raw transition status
// report, using only one Infrastructure GET.
func TestStatusStructuredOutput(t *testing.T) {
	type statusOutput struct {
		Metadata struct {
			Generation int64 `json:"generation"`
		} `json:"metadata"`
		Spec struct {
			ControlPlaneTopology   configv1.TopologyMode `json:"controlPlaneTopology"`
			InfrastructureTopology configv1.TopologyMode `json:"infrastructureTopology"`
		} `json:"spec"`
		Status struct {
			ControlPlaneTopology     configv1.TopologyMode             `json:"controlPlaneTopology"`
			InfrastructureTopology   configv1.TopologyMode             `json:"infrastructureTopology"`
			TopologyTransitionStatus configv1.TopologyTransitionStatus `json:"topologyTransitionStatus"`
		} `json:"status"`
	}

	for _, format := range []string{"json", "yaml"} {
		for _, state := range []string{"in progress", "complete", "empty transitions", "missing", "old generation"} {
			t.Run(format+"/"+state, func(t *testing.T) {
				infra := statusInfrastructure()
				switch state {
				case "complete":
					infra.Status.ControlPlaneTopology = infra.Spec.ControlPlaneTopology
					infra.Status.InfrastructureTopology = infra.Spec.InfrastructureTopology
					infra.Status.TopologyTransitionStatus.Conditions[1].Status = metav1.ConditionTrue
				case "empty transitions":
					infra.Status.TopologyTransitionStatus.Transitions = nil
				case "missing":
					infra.Status.TopologyTransitionStatus = configv1.TopologyTransitionStatus{}
				case "old generation":
					infra.Generation++
				}
				body, err := json.Marshal(infra)
				if err != nil {
					t.Fatal(err)
				}
				factory := cmdtesting.NewTestFactory()
				t.Cleanup(factory.Cleanup)
				requests := 0
				factory.ClientConfigVal.Transport = restfake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
					requests++
					if req.Method != http.MethodGet || req.URL.Path != "/apis/config.openshift.io/v1/infrastructures/cluster" {
						t.Errorf("unexpected request: %s %s", req.Method, req.URL)
					}

					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(bytes.NewReader(body)),
					}, nil
				}).Transport
				streams, _, out, errOut := genericclioptions.NewTestIOStreams()
				cmd := newCmdStatus(factory, streams)
				cmd.SetArgs([]string{"-o", format})

				if err := cmd.ExecuteContext(t.Context()); err != nil {
					t.Fatal(err)
				}

				var got statusOutput
				if err := yaml.UnmarshalStrict(out.Bytes(), &got); err != nil {
					t.Fatalf("invalid %s output: %v\n%s", format, err, out)
				}
				if format == "json" && !json.Valid(out.Bytes()) {
					t.Fatalf("invalid JSON output:\n%s", out)
				}
				want := statusOutput{}
				want.Metadata.Generation = infra.Generation
				want.Spec.ControlPlaneTopology = infra.Spec.ControlPlaneTopology
				want.Spec.InfrastructureTopology = infra.Spec.InfrastructureTopology
				want.Status.ControlPlaneTopology = infra.Status.ControlPlaneTopology
				want.Status.InfrastructureTopology = infra.Status.InfrastructureTopology
				want.Status.TopologyTransitionStatus = infra.Status.TopologyTransitionStatus

				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("report differs (-want +got):\n%s", diff)
				}
				if requests != 1 || errOut.Len() != 0 {
					t.Errorf("requests=%d, stderr=%s", requests, errOut)
				}
			})
		}
	}
}

// TestStatusReturnsWriteError verifies all status formats return output write errors.
func TestStatusReturnsWriteError(t *testing.T) {
	for _, format := range []string{"", "json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			wantErr := errors.New("write failed")
			o := newStatusOptions(genericclioptions.IOStreams{Out: failingWriter{err: wantErr}})
			o.configClient = fakeconfigclient.NewSimpleClientset(statusInfrastructure())
			*o.printFlags.OutputFormat = format

			if err := o.run(t.Context()); !errors.Is(err, wantErr) {
				t.Fatalf("got %v, want %v", err, wantErr)
			}
		})
	}
}
