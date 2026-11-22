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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	restfake "k8s.io/client-go/rest/fake"
	ktesting "k8s.io/client-go/testing"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
	"sigs.k8s.io/yaml"
)

// discoveryInfrastructure builds a current report with one available transition for discovery tests.
func discoveryInfrastructure() *configv1.Infrastructure {
	return &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster", Generation: 2},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
			TopologyTransitionStatus: configv1.TopologyTransitionStatus{
				Conditions: []metav1.Condition{{
					Type:   configv1.TopologyTransitionsEvaluatedConditionType,
					Status: metav1.ConditionTrue, ObservedGeneration: 2,
				}},
				Transitions: []configv1.TopologyTransition{{
					Source: configv1.TopologyState{
						ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
						InfrastructureTopology: configv1.SingleReplicaTopologyMode,
					},
					Target: configv1.TopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableArbiterMode,
						InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
					},
					Evaluations: []metav1.Condition{{
						Type:   configv1.TopologyTransitionAvailableConditionType,
						Status: metav1.ConditionTrue, ObservedGeneration: 2,
					}},
				}},
			},
		},
	}
}

// TestDiscoveryStructuredOutput verifies JSON and YAML include the Infrastructure
// generation and preserve the raw report, including missing, pending, and stale
// results, using only one Infrastructure GET.
func TestDiscoveryStructuredOutput(t *testing.T) {
	type discoveryOutput struct {
		Metadata struct {
			Generation int64 `json:"generation"`
		} `json:"metadata"`
		configv1.TopologyTransitionStatus
	}

	for _, format := range []string{"json", "yaml"} {
		for _, state := range []string{"available", "blocked", "pending", "empty", "missing", "old generation"} {
			t.Run(format+"/"+state, func(t *testing.T) {
				infra := discoveryInfrastructure()
				switch state {
				case "blocked":
					infra.Status.TopologyTransitionStatus.Transitions[0].Evaluations[0].Status = metav1.ConditionFalse
				case "pending":
					infra.Status.TopologyTransitionStatus.Conditions[0].Status = metav1.ConditionUnknown
				case "empty":
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
				cmd := newCmdTopology(factory, streams)
				cmd.SetArgs([]string{"-o", format})

				if err := cmd.ExecuteContext(t.Context()); err != nil {
					t.Fatal(err)
				}

				var got discoveryOutput
				if err := yaml.UnmarshalStrict(out.Bytes(), &got); err != nil {
					t.Fatalf("invalid %s output: %v\n%s", format, err, out)
				}
				if format == "json" && !json.Valid(out.Bytes()) {
					t.Fatalf("invalid JSON output:\n%s", out)
				}
				want := discoveryOutput{TopologyTransitionStatus: infra.Status.TopologyTransitionStatus}
				want.Metadata.Generation = infra.Generation

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

// TestDiscoveryReadsServerResults checks that human output uses the controller's
// report and does not advertise retained transitions when evaluations are stale.
func TestDiscoveryReadsServerResults(t *testing.T) {
	tests := []struct {
		name   string
		change func(*configv1.Infrastructure)
		want   []string
		absent []string
	}{
		{
			name: "server defines target",
			want: []string{"Current Topology Configuration:", "SingleReplica -> HighlyAvailableArbiter", "Available"},
		},
		{
			name: "blocked transition",
			change: func(infra *configv1.Infrastructure) {
				availability := &infra.Status.TopologyTransitionStatus.Transitions[0].Evaluations[0]
				availability.Status = metav1.ConditionFalse
				availability.Reason = "PreflightCheckFailed"
				availability.Message = "One or more preflight checks failed"
				infra.Status.TopologyTransitionStatus.Transitions[0].Evaluations = append(
					infra.Status.TopologyTransitionStatus.Transitions[0].Evaluations,
					metav1.Condition{Type: "NodesReady", Status: metav1.ConditionFalse, Message: "A node is not ready"},
				)
			},
			want: []string{"Blocked", "PreflightCheckFailed", "A node is not ready", "No available transitions"},
		},
		{
			name:   "no supported transitions",
			change: func(infra *configv1.Infrastructure) { infra.Status.TopologyTransitionStatus.Transitions = nil },
			want:   []string{"No available transitions"},
			absent: []string{"SingleReplica -> SingleReplica"},
		},
		{
			name: "gate disabled or older cluster",
			change: func(infra *configv1.Infrastructure) {
				infra.Status.TopologyTransitionStatus = configv1.TopologyTransitionStatus{}
			},
			want:   []string{"Discovery results are unavailable"},
			absent: []string{"No available transitions", "Available\n"},
		},
		{
			name: "evaluation pending",
			change: func(infra *configv1.Infrastructure) {
				infra.Status.TopologyTransitionStatus.Conditions[0].Status = metav1.ConditionUnknown
			},
			want:   []string{"Discovery results are not current"},
			absent: []string{"SingleReplica -> HighlyAvailableArbiter", "No available transitions"},
		},
		{
			name: "evaluation failed",
			change: func(infra *configv1.Infrastructure) {
				condition := &infra.Status.TopologyTransitionStatus.Conditions[0]
				condition.Status = metav1.ConditionFalse
				condition.Reason = "TransitionEvaluationFailed"
				condition.Message = "Could not read nodes"
			},
			want:   []string{"Discovery results are not current", "TransitionEvaluationFailed", "Could not read nodes"},
			absent: []string{"SingleReplica -> HighlyAvailableArbiter"},
		},
		{
			name:   "old generation",
			change: func(infra *configv1.Infrastructure) { infra.Generation++ },
			want:   []string{"Discovery results are not current"},
			absent: []string{"SingleReplica -> HighlyAvailableArbiter"},
		},
		{
			name: "missing availability",
			change: func(infra *configv1.Infrastructure) {
				infra.Status.TopologyTransitionStatus.Transitions[0].Evaluations = nil
			},
			want: []string{"Unknown", "No available transitions"},
		},
		{
			name: "unknown availability",
			change: func(infra *configv1.Infrastructure) {
				infra.Status.TopologyTransitionStatus.Transitions[0].Evaluations[0].Status = metav1.ConditionUnknown
			},
			want: []string{"Unknown", "No available transitions"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			infra := discoveryInfrastructure()
			if tt.change != nil {
				tt.change(infra)
			}
			client := fakeconfigclient.NewClientset(infra)
			streams, _, out, _ := genericclioptions.NewTestIOStreams()
			o := newTransitionOptions(streams)
			o.configClient = client

			if err := o.run(t.Context()); err != nil {
				t.Fatal(err)
			}

			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output missing %q:\n%s", want, out)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(out.String(), absent) {
					t.Errorf("unexpected %q in output:\n%s", absent, out)
				}
			}
			actions := client.Actions()
			if len(actions) != 1 || !actions[0].Matches("get", "infrastructures") {
				t.Errorf("discovery must only GET Infrastructure, got %v", actions)
			}
		})
	}
}

// TestDiscoveryReturnsWriteError verifies all discovery formats return output write errors.
func TestDiscoveryReturnsWriteError(t *testing.T) {
	for _, format := range []string{"", "json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			wantErr := errors.New("write failed")
			o := newTransitionOptions(genericclioptions.IOStreams{Out: failingWriter{err: wantErr}})
			o.configClient = fakeconfigclient.NewClientset(discoveryInfrastructure())
			*o.printFlags.OutputFormat = format

			if err := o.run(t.Context()); !errors.Is(err, wantErr) {
				t.Fatalf("got %v, want %v", err, wantErr)
			}
		})
	}
}

// TestDiscoveryReturnsAPIError verifies failed Infrastructure reads return the API
// error without printing a discovery report.
func TestDiscoveryReturnsAPIError(t *testing.T) {
	resource := schema.GroupResource{Group: configv1.GroupName, Resource: "infrastructures"}
	tests := []struct {
		name    string
		wantErr error
	}{
		{name: "forbidden", wantErr: apierrors.NewForbidden(resource, "cluster", errors.New("access denied"))},
		{name: "not found", wantErr: apierrors.NewNotFound(resource, "cluster")},
		{name: "connection error", wantErr: errors.New("connection failed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantErr := tt.wantErr

			client := fakeconfigclient.NewClientset()
			client.PrependReactor("get", "infrastructures", func(ktesting.Action) (bool, runtime.Object, error) {
				return true, nil, wantErr
			})
			streams, _, out, _ := genericclioptions.NewTestIOStreams()
			o := newTransitionOptions(streams)
			o.configClient = client

			if err := o.run(t.Context()); !errors.Is(err, wantErr) {
				t.Errorf("got %v, want %v", err, wantErr)
			}
			if out.Len() != 0 {
				t.Errorf("unexpected output after API failure: %s", out)
			}
		})
	}
}

// TestValidateDiscoveryOutput checks supported formats and rejects output flags
// combined with a target topology or confirmation.
func TestValidateDiscoveryOutput(t *testing.T) {
	tests := []struct {
		name           string
		format         string
		controlPlane   string
		infrastructure string
		confirm        bool
		wantErr        string
	}{
		{name: "json", format: "json"},
		{name: "yaml", format: "yaml"},
		{name: "invalid format", format: "wide", wantErr: "must be json or yaml"},
		{name: "control plane target", format: "json", controlPlane: "HighlyAvailable", wantErr: "--output cannot be combined"},
		{name: "infrastructure target", format: "yaml", infrastructure: "HighlyAvailable", wantErr: "--output cannot be combined"},
		{name: "confirm", format: "json", confirm: true, wantErr: "--output cannot be combined"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := newTransitionOptions(genericclioptions.NewTestIOStreamsDiscard())
			*o.printFlags.OutputFormat = tt.format
			o.args.targetControlPlaneTopology = tt.controlPlane
			o.args.targetInfrastructureTopology = tt.infrastructure
			o.args.confirm = tt.confirm

			err := o.validateCommand()

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
