package transition

import (
	"context"
	"errors"
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	fakeconfigclient "github.com/openshift/client-go/config/clientset/versioned/fake"
	fakeoperatorclient "github.com/openshift/client-go/operator/clientset/versioned/fake"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	ktesting "k8s.io/client-go/testing"
)

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

// ================================================================================
// COMMAND STRUCTURE TESTS
// ================================================================================

func TestNewCmdTransition(t *testing.T) {
	streams := genericclioptions.NewTestIOStreamsDiscard()
	cmd := NewCmdTransition(nil, streams)

	if cmd.Use != "transition" {
		t.Errorf("expected Use='transition', got %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("expected non-empty Short description")
	}

	topologyCmd := findSubcommand(cmd, "topology")
	if topologyCmd == nil {
		t.Fatal("expected 'topology' subcommand to exist")
	}
	if topologyCmd.Use != "topology" {
		t.Errorf("expected topology Use='topology', got %q", topologyCmd.Use)
	}
	if topologyCmd.Short == "" {
		t.Error("expected topology non-empty Short description")
	}
	if topologyCmd.Long == "" {
		t.Error("expected topology non-empty Long description")
	}
	if topologyCmd.Example == "" {
		t.Error("expected topology non-empty Example")
	}

	requiredFlags := []string{"control-plane", "infrastructure", "confirm"}
	for _, flagName := range requiredFlags {
		if topologyCmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected flag %q to exist", flagName)
		}
	}

	statusCmd := findSubcommand(cmd, "status")
	if statusCmd == nil {
		t.Error("expected 'status' subcommand to exist under transition")
	} else if statusCmd.Short == "" {
		t.Error("expected status subcommand to have Short description")
	}
}

func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, cmd := range parent.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}

// ================================================================================
// VALIDATION TESTS
// ================================================================================

func TestValidate_TopologyFlags(t *testing.T) {
	testCases := []struct {
		name           string
		controlPlane   string
		infrastructure string
		expectErr      bool
	}{
		{name: "valid HighlyAvailable control plane", controlPlane: "HighlyAvailable"},
		{name: "valid SingleReplica control plane", controlPlane: "SingleReplica"},
		{name: "valid HighlyAvailable infrastructure", infrastructure: "HighlyAvailable"},
		{name: "valid both topologies", controlPlane: "HighlyAvailable", infrastructure: "HighlyAvailable"},
		{name: "invalid control plane topology", controlPlane: "InvalidTopology", expectErr: true},
		{name: "invalid infrastructure topology", infrastructure: "InvalidTopology", expectErr: true},
		{name: "empty (discovery mode)"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			o := newTransitionOptions(genericclioptions.NewTestIOStreamsDiscard())
			o.args.targetControlPlaneTopology = tc.controlPlane
			o.args.targetInfrastructureTopology = tc.infrastructure

			err := o.validateCommand()
			if tc.expectErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func TestValidate_FlagDependencies(t *testing.T) {
	testCases := []struct {
		name           string
		controlPlane   string
		infrastructure string
		confirm        bool
		expectErr      bool
		errContains    string
	}{
		{
			name: "--confirm requires at least one topology flag", confirm: true,
			expectErr: true, errContains: "--confirm requires at least one of --control-plane or --infrastructure",
		},
		{name: "--control-plane alone is valid", controlPlane: "HighlyAvailable"},
		{name: "--infrastructure alone is valid", infrastructure: "HighlyAvailable"},
		{name: "--control-plane with --confirm is valid", controlPlane: "HighlyAvailable", confirm: true},
		{name: "--infrastructure with --confirm is valid", infrastructure: "HighlyAvailable", confirm: true},
		{name: "both with --confirm is valid", controlPlane: "HighlyAvailable", infrastructure: "HighlyAvailable", confirm: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			o := newTransitionOptions(genericclioptions.NewTestIOStreamsDiscard())
			o.args.targetControlPlaneTopology = tc.controlPlane
			o.args.targetInfrastructureTopology = tc.infrastructure
			o.args.confirm = tc.confirm

			err := o.validateCommand()
			if tc.expectErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got %v", tc.errContains, err)
				}
			} else if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// ================================================================================
// DISCOVERY MODE TESTS
// ================================================================================

func TestRunDiscoveryMode_WithServerTransitions(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()
	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}

	transitionStatus := &TopologyTransitionStatus{
		Conditions: []metav1.Condition{
			{
				Type:    TopologyTransitionsEvaluatedConditionType,
				Status:  metav1.ConditionTrue,
				Reason:  "EvaluationComplete",
				Message: "Supported transitions evaluated",
			},
			{
				Type:    TopologyTransitionCompletedConditionType,
				Status:  metav1.ConditionUnknown,
				Reason:  "NoTransitionRequested",
				Message: "No transition has been requested",
			},
		},
		Transitions: []TopologyTransition{
			{
				Source: APITopologyState{
					ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
					InfrastructureTopology: configv1.SingleReplicaTopologyMode,
				},
				Target: APITopologyState{
					ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
					InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
				},
				Evaluations: []metav1.Condition{
					{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Reason: "TransitionAvailable", Message: "All checks passed"},
					{Type: "WorkerNodesReady", Status: metav1.ConditionTrue, Reason: "SufficientWorkers", Message: "3 worker nodes Ready and schedulable"},
				},
			},
		},
	}

	o.validator = TransitionValidator{Current: o.current, Transitions: transitionStatus.Transitions}
	o.topologyClient = &fakeTopologyClient{
		transitionStatus: transitionStatus,
		infraSpec:        configv1.SingleReplicaTopologyMode,
	}

	err := o.runDiscoveryMode(context.Background(), transitionStatus)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	expectedPhrases := []string{
		"Current Topology Configuration:",
		"Control Plane:",
		"Infrastructure:",
		"Transition Evaluation: EvaluationComplete",
		"Available Transitions:",
		"Infrastructure: SingleReplica -> HighlyAvailable",
		"available",
		"WorkerNodesReady",
	}

	for _, phrase := range expectedPhrases {
		if !strings.Contains(output, phrase) {
			t.Errorf("expected output to contain %q, got:\n%s", phrase, output)
		}
	}
}

func TestRunDiscoveryMode_NoTransitionsAvailable(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()
	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.HighlyAvailableTopologyMode,
	}
	o.validator = TransitionValidator{Current: o.current}
	o.topologyClient = &fakeTopologyClient{
		infraSpec: configv1.HighlyAvailableTopologyMode,
	}

	err := o.runDiscoveryMode(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out.String(), "No available transitions") {
		t.Errorf("expected 'No available transitions' in output, got:\n%s", out.String())
	}
}

func TestRunDiscoveryMode_TransitionNotAvailable(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()
	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}

	transitionStatus := &TopologyTransitionStatus{
		Transitions: []TopologyTransition{
			{
				Source: APITopologyState{
					ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
					InfrastructureTopology: configv1.SingleReplicaTopologyMode,
				},
				Target: APITopologyState{
					ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
					InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
				},
				Evaluations: []metav1.Condition{
					{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionFalse, Reason: "PrerequisitesFailed", Message: "Prerequisites not met"},
					{Type: "WorkerNodesReady", Status: metav1.ConditionFalse, Reason: "InsufficientWorkers", Message: "1 worker node, minimum 2 required"},
				},
			},
		},
	}

	o.validator = TransitionValidator{Current: o.current, Transitions: transitionStatus.Transitions}
	o.topologyClient = &fakeTopologyClient{
		transitionStatus: transitionStatus,
		infraSpec:        configv1.SingleReplicaTopologyMode,
	}

	err := o.runDiscoveryMode(context.Background(), transitionStatus)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "not available") {
		t.Errorf("expected 'not available' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "minimum 2 required") {
		t.Errorf("expected worker evaluation details in output, got:\n%s", output)
	}
}

func TestRunDiscoveryModeReturnsWriteError(t *testing.T) {
	wantErr := errors.New("write failed")
	o := newTransitionOptions(genericclioptions.IOStreams{Out: failingWriter{err: wantErr}})
	o.current = TopologyState{
		ControlPlane:   configv1.SingleReplicaTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	o.validator = TransitionValidator{Current: o.current}
	o.topologyClient = &fakeTopologyClient{infraSpec: configv1.SingleReplicaTopologyMode}

	if err := o.runDiscoveryMode(context.Background(), nil); !errors.Is(err, wantErr) {
		t.Fatalf("runDiscoveryMode() error = %v, want %v", err, wantErr)
	}
}

// ================================================================================
// INITIATE MODE TESTS
// ================================================================================

func TestRunInitiateMode_InfrastructureOnlyPatch(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
	}

	fake := &fakeTopologyClient{
		transitionStatus: &TopologyTransitionStatus{
			Transitions: []TopologyTransition{
				{
					Source: APITopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.SingleReplicaTopologyMode,
					},
					Target: APITopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
					},
					Evaluations: []metav1.Condition{
						{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Reason: "TransitionAvailable", Message: "All checks passed"},
					},
				},
			},
		},
		infraSpec: configv1.SingleReplicaTopologyMode,
	}

	configClient := fakeconfigclient.NewSimpleClientset(infra)

	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	o.target = TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode}
	o.validator = TransitionValidator{
		Current:     o.current,
		Transitions: fake.transitionStatus.Transitions,
	}
	o.args.confirm = true
	o.configClient = configClient
	o.topologyClient = fake

	err := o.runInitiateMode(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify infrastructure topology was patched
	if !fake.patchCalled {
		t.Fatal("expected patchTopologySpec to be called")
	}
	if fake.patchedInfrastructure != configv1.HighlyAvailableTopologyMode {
		t.Errorf("expected patched infrastructure=%s, got %s",
			configv1.HighlyAvailableTopologyMode, fake.patchedInfrastructure)
	}
	// Control plane should NOT be patched (empty = unchanged)
	if fake.patchedControlPlane != "" {
		t.Errorf("expected patched control plane to be empty (unchanged), got %s", fake.patchedControlPlane)
	}

	output := out.String()
	if !strings.Contains(output, "spec.infrastructureTopology") {
		t.Errorf("expected output to mention spec.infrastructureTopology, got:\n%s", output)
	}
	if !strings.Contains(output, "oc adm transition status") {
		t.Errorf("expected output to suggest monitoring command, got:\n%s", output)
	}
}

func TestRunInitiateMode_ControlPlaneOnlyPatch(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
	}

	fake := &fakeTopologyClient{
		transitionStatus: &TopologyTransitionStatus{
			Transitions: []TopologyTransition{
				{
					Source: APITopologyState{
						ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
						InfrastructureTopology: configv1.SingleReplicaTopologyMode,
					},
					Target: APITopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
					},
					Evaluations: []metav1.Condition{
						{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Reason: "TransitionAvailable", Message: "All checks passed"},
					},
				},
			},
		},
	}

	configClient := fakeconfigclient.NewSimpleClientset(infra)

	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.SingleReplicaTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	// CP only — compact auto-fill should set infra too
	o.target = TopologyState{ControlPlane: configv1.HighlyAvailableTopologyMode}
	o.validator = TransitionValidator{
		Current:     o.current,
		Transitions: fake.transitionStatus.Transitions,
	}
	o.args.confirm = true
	o.configClient = configClient
	o.topologyClient = fake

	err := o.runInitiateMode(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Compact auto-fill: should patch both
	if !fake.patchCalled {
		t.Fatal("expected patchTopologySpec to be called")
	}
	if fake.patchedControlPlane != configv1.HighlyAvailableTopologyMode {
		t.Errorf("expected patched control plane=%s, got %s",
			configv1.HighlyAvailableTopologyMode, fake.patchedControlPlane)
	}
	if fake.patchedInfrastructure != configv1.HighlyAvailableTopologyMode {
		t.Errorf("expected patched infrastructure=%s (auto-fill), got %s",
			configv1.HighlyAvailableTopologyMode, fake.patchedInfrastructure)
	}

	output := out.String()
	if !strings.Contains(output, "spec.controlPlaneTopology") {
		t.Errorf("expected output to mention spec.controlPlaneTopology, got:\n%s", output)
	}
}

func TestRunInitiateMode_BothTopologiesPatch(t *testing.T) {
	streams, _, _, _ := genericclioptions.NewTestIOStreams()

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
	}

	fake := &fakeTopologyClient{
		transitionStatus: &TopologyTransitionStatus{
			Transitions: []TopologyTransition{
				{
					Source: APITopologyState{
						ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
						InfrastructureTopology: configv1.SingleReplicaTopologyMode,
					},
					Target: APITopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
					},
					Evaluations: []metav1.Condition{
						{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Reason: "TransitionAvailable", Message: "All checks passed"},
					},
				},
			},
		},
	}

	configClient := fakeconfigclient.NewSimpleClientset(infra)

	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.SingleReplicaTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	o.target = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.HighlyAvailableTopologyMode,
	}
	o.validator = TransitionValidator{
		Current:     o.current,
		Transitions: fake.transitionStatus.Transitions,
	}
	o.args.confirm = true
	o.configClient = configClient
	o.topologyClient = fake

	err := o.runInitiateMode(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fake.patchCalled {
		t.Fatal("expected patchTopologySpec to be called")
	}
	if fake.patchedControlPlane != configv1.HighlyAvailableTopologyMode {
		t.Errorf("expected patched control plane=%s, got %s",
			configv1.HighlyAvailableTopologyMode, fake.patchedControlPlane)
	}
	if fake.patchedInfrastructure != configv1.HighlyAvailableTopologyMode {
		t.Errorf("expected patched infrastructure=%s, got %s",
			configv1.HighlyAvailableTopologyMode, fake.patchedInfrastructure)
	}
}

func TestRunInitiateMode_DryRun_InfrastructureOnly(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
	}

	fake := &fakeTopologyClient{
		transitionStatus: &TopologyTransitionStatus{
			Transitions: []TopologyTransition{
				{
					Source: APITopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.SingleReplicaTopologyMode,
					},
					Target: APITopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
					},
					Evaluations: []metav1.Condition{
						{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Reason: "TransitionAvailable", Message: "All checks passed"},
						{Type: "WorkerNodesReady", Status: metav1.ConditionTrue, Reason: "SufficientWorkers", Message: "3 workers ready"},
					},
				},
			},
		},
	}

	configClient := fakeconfigclient.NewSimpleClientset(infra)

	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	o.target = TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode}
	o.validator = TransitionValidator{
		Current:     o.current,
		Transitions: fake.transitionStatus.Transitions,
	}
	o.args.confirm = false
	o.configClient = configClient
	o.topologyClient = fake

	err := o.runInitiateMode(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no patch was made
	if fake.patchCalled {
		t.Error("expected no patch in dry-run mode")
	}

	output := out.String()
	if !strings.Contains(output, "Dry run") {
		t.Error("expected output to indicate dry-run mode")
	}
	if !strings.Contains(output, "spec.infrastructureTopology") {
		t.Error("expected output to mention spec.infrastructureTopology")
	}
	if !strings.Contains(output, "Add --confirm") {
		t.Error("expected output to suggest --confirm")
	}
	if !strings.Contains(output, "WorkerNodesReady") {
		t.Error("expected output to show prerequisite evaluation")
	}
}

func TestRunInitiateMode_AlreadyAtTarget(t *testing.T) {
	streams, _, out, _ := genericclioptions.NewTestIOStreams()

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
			InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
		},
	}

	fake := &fakeTopologyClient{}
	configClient := fakeconfigclient.NewSimpleClientset(infra)

	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.HighlyAvailableTopologyMode,
	}
	o.target = TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode}
	o.configClient = configClient
	o.topologyClient = fake

	err := o.runInitiateMode(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fake.patchCalled {
		t.Error("expected no patch when already at target")
	}

	output := out.String()
	if !strings.Contains(output, "Cluster is already at target topology") {
		t.Error("expected output to indicate already at target")
	}
	if !strings.Contains(output, "No transition initiated") {
		t.Error("expected output to indicate no transition was initiated")
	}
}

func TestRunInitiateMode_ValidationFails(t *testing.T) {
	streams := genericclioptions.NewTestIOStreamsDiscard()

	fake := &fakeTopologyClient{
		transitionStatus: &TopologyTransitionStatus{
			Transitions: []TopologyTransition{
				{
					Source: APITopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.SingleReplicaTopologyMode,
					},
					Target: APITopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
					},
					Evaluations: []metav1.Condition{
						{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionFalse, Reason: "PrerequisitesFailed", Message: "Not all prerequisites met"},
						{Type: "WorkerNodesReady", Status: metav1.ConditionFalse, Reason: "InsufficientWorkers", Message: "0 workers, need 2"},
					},
				},
			},
		},
	}

	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	o.target = TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode}
	o.validator = TransitionValidator{
		Current:     o.current,
		Transitions: fake.transitionStatus.Transitions,
	}
	o.args.confirm = true
	o.topologyClient = fake

	err := o.runInitiateMode(context.Background())
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "topology validation failed") {
		t.Errorf("expected 'topology validation failed', got: %v", err)
	}
	if !strings.Contains(err.Error(), "WorkerNodesReady") {
		t.Errorf("expected error to mention WorkerNodesReady, got: %v", err)
	}
}

func TestRunInitiateMode_NoTransitionsAvailable(t *testing.T) {
	streams := genericclioptions.NewTestIOStreamsDiscard()

	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	o.target = TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode}
	o.validator = TransitionValidator{Current: o.current}
	o.args.confirm = true
	o.topologyClient = &fakeTopologyClient{}

	err := o.runInitiateMode(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no topology transitions are available") {
		t.Errorf("expected 'no topology transitions', got: %v", err)
	}
}

func TestRunInitiateMode_PatchError(t *testing.T) {
	streams := genericclioptions.NewTestIOStreamsDiscard()

	fake := &fakeTopologyClient{
		transitionStatus: &TopologyTransitionStatus{
			Transitions: []TopologyTransition{
				{
					Source: APITopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.SingleReplicaTopologyMode,
					},
					Target: APITopologyState{
						ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
						InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
					},
					Evaluations: []metav1.Condition{
						{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Reason: "TransitionAvailable", Message: "All checks passed"},
					},
				},
			},
		},
		patchErr: errors.New("conflict"),
	}

	o := newTransitionOptions(streams)
	o.current = TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	o.target = TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode}
	o.validator = TransitionValidator{
		Current:     o.current,
		Transitions: fake.transitionStatus.Transitions,
	}
	o.args.confirm = true
	o.topologyClient = fake

	err := o.runInitiateMode(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to update Infrastructure resource") {
		t.Errorf("expected update error, got: %v", err)
	}
}

func TestRunInitiateModeReturnsWriteError(t *testing.T) {
	wantErr := errors.New("write failed")
	o := newTransitionOptions(genericclioptions.IOStreams{Out: failingWriter{err: wantErr}})
	o.current = TopologyState{
		ControlPlane:   configv1.SingleReplicaTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}
	// CP SNO->HA triggers compact auto-fill for infra
	o.target = TopologyState{ControlPlane: configv1.HighlyAvailableTopologyMode}
	o.validator = TransitionValidator{
		Current: o.current,
		Transitions: []TopologyTransition{
			{
				Source: APITopologyState{
					ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
					InfrastructureTopology: configv1.SingleReplicaTopologyMode,
				},
				Target: APITopologyState{
					ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
					InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
				},
				Evaluations: []metav1.Condition{
					{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Reason: "Available"},
				},
			},
		},
	}
	o.topologyClient = &fakeTopologyClient{}

	err := o.runInitiateMode(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ================================================================================
// STATUS COMMAND TESTS
// ================================================================================

func TestStatusCommand(t *testing.T) {
	testCases := []struct {
		name                   string
		infraSpec              configv1.TopologyMode
		infraStatus            configv1.TopologyMode
		infraTopology          configv1.TopologyMode
		infraTopologySpec      configv1.TopologyMode
		transitionStatus       *TopologyTransitionStatus
		progressingStatus      configv1.ConditionStatus
		progressingReason      string
		progressingMessage     string
		upgradeableStatus      configv1.ConditionStatus
		upgradeableReason      string
		upgradeableMessage     string
		expectedOutputContains []string
	}{
		{
			name:              "infrastructure transition in progress",
			infraSpec:         configv1.HighlyAvailableTopologyMode,
			infraStatus:       configv1.HighlyAvailableTopologyMode,
			infraTopology:     configv1.SingleReplicaTopologyMode,
			infraTopologySpec: configv1.HighlyAvailableTopologyMode,
			transitionStatus: &TopologyTransitionStatus{
				Conditions: []metav1.Condition{
					{Type: TopologyTransitionCompletedConditionType, Status: metav1.ConditionFalse, Reason: "TransitionInProgress", Message: "Infrastructure topology transition in progress"},
				},
			},
			progressingStatus:  configv1.ConditionTrue,
			progressingReason:  "TopologyTransitionInProgress",
			progressingMessage: "Infrastructure topology transition in progress",
			upgradeableStatus:  configv1.ConditionFalse,
			upgradeableReason:  "TopologyTransitionInProgress",
			upgradeableMessage: "Cluster upgrade is not allowed during topology transition",
			expectedOutputContains: []string{
				"Control Plane Topology:",
				"Infrastructure Topology:",
				"Spec (desired):   HighlyAvailable",
				"Topology Transition Lifecycle:",
				"TransitionInProgress",
				"CCO Transition Conditions:",
				"  Progressing\n    Status: True",
				"  Upgradeable\n    Status: False",
			},
		},
		{
			name:              "no transition",
			infraSpec:         configv1.HighlyAvailableTopologyMode,
			infraStatus:       configv1.HighlyAvailableTopologyMode,
			infraTopology:     configv1.HighlyAvailableTopologyMode,
			infraTopologySpec: configv1.HighlyAvailableTopologyMode,
			upgradeableStatus:  configv1.ConditionTrue,
			upgradeableReason:  "AsExpected",
			upgradeableMessage: "No topology transition in progress",
			expectedOutputContains: []string{
				"Control Plane Topology:",
				"Spec (desired):   HighlyAvailable",
				"Status (current): HighlyAvailable",
				"Infrastructure Topology:",
				"No topology transition status available",
				"  Progressing: Condition not available",
				"  Upgradeable\n    Status: True\n    Reason: AsExpected",
			},
		},
		{
			name:              "empty infrastructure topology",
			infraSpec:         configv1.HighlyAvailableTopologyMode,
			infraStatus:       configv1.HighlyAvailableTopologyMode,
			infraTopology:     "",
			infraTopologySpec: "",
			upgradeableStatus:  configv1.ConditionTrue,
			upgradeableReason:  "AsExpected",
			upgradeableMessage: "No topology transition in progress",
			expectedOutputContains: []string{
				"Infrastructure Topology:",
				"Spec (desired):   (not set)",
				"Status (current): (not set)",
				"  Progressing: Condition not available",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			streams, _, out, _ := genericclioptions.NewTestIOStreams()

			infra := &configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: configv1.InfrastructureSpec{
					ControlPlaneTopology: tc.infraSpec,
				},
				Status: configv1.InfrastructureStatus{
					ControlPlaneTopology:   tc.infraStatus,
					InfrastructureTopology: tc.infraTopology,
				},
			}

			operatorConfig := &operatorv1.Config{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			}

			if tc.progressingStatus != "" {
				operatorConfig.Status.Conditions = append(operatorConfig.Status.Conditions,
					operatorv1.OperatorCondition{
						Type:    topologyTransitionControllerProgressingCondition,
						Status:  operatorv1.ConditionStatus(tc.progressingStatus),
						Reason:  tc.progressingReason,
						Message: tc.progressingMessage,
					},
				)
			}

			if tc.upgradeableStatus != "" {
				operatorConfig.Status.Conditions = append(operatorConfig.Status.Conditions,
					operatorv1.OperatorCondition{
						Type:    topologyTransitionControllerUpgradeableCondition,
						Status:  operatorv1.ConditionStatus(tc.upgradeableStatus),
						Reason:  tc.upgradeableReason,
						Message: tc.upgradeableMessage,
					},
				)
			}

			configClient := fakeconfigclient.NewSimpleClientset(infra)
			operatorClient := fakeoperatorclient.NewSimpleClientset()
			gvr := schema.GroupVersionResource{
				Group:    "operator.openshift.io",
				Version:  "v1",
				Resource: "configs",
			}
			if err := operatorClient.Tracker().Add(operatorConfig); err != nil {
				if err := operatorClient.Tracker().Create(gvr, operatorConfig, ""); err != nil {
					t.Fatalf("failed to add operatorConfig to tracker: %v", err)
				}
			}

			o := &statusOptions{
				IOStreams:       streams,
				configClient:   configClient,
				operatorClient: operatorClient,
				topologyClient: &fakeTopologyClient{
					transitionStatus: tc.transitionStatus,
					infraSpec:        tc.infraTopologySpec,
				},
			}

			err := o.run(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := out.String()
			for _, expected := range tc.expectedOutputContains {
				if !strings.Contains(output, expected) {
					t.Errorf("expected output to contain %q, got:\n%s", expected, output)
				}
			}
		})
	}
}

func TestStatusCommandErrors(t *testing.T) {
	tests := []struct {
		name           string
		failResource   string
		out            failingWriter
		wantWriteError bool
	}{
		{name: "Infrastructure read", failResource: "infrastructures"},
		{name: "operator Config read", failResource: "configs"},
		{name: "output write", out: failingWriter{err: errors.New("write failed")}, wantWriteError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			streams := genericclioptions.NewTestIOStreamsDiscard()
			if tc.wantWriteError {
				streams.Out = tc.out
			}
			configClient := fakeconfigclient.NewSimpleClientset(&configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{Name: infrastructureResourceName},
			})
			operatorClient := fakeoperatorclient.NewSimpleClientset()
			if tc.failResource == "infrastructures" {
				configClient.PrependReactor("get", tc.failResource, func(ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("read failed")
				})
			}
			if tc.failResource == "configs" {
				operatorClient.PrependReactor("get", tc.failResource, func(ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("read failed")
				})
			}

			o := &statusOptions{
				IOStreams:       streams,
				configClient:   configClient,
				operatorClient: operatorClient,
				topologyClient: &fakeTopologyClient{},
			}
			err := o.run(context.Background())
			if err == nil {
				t.Fatal("run() error = nil, want error")
			}
			if tc.wantWriteError && !errors.Is(err, tc.out.err) {
				t.Fatalf("run() error = %v, want %v", err, tc.out.err)
			}
		})
	}
}

func TestFormatTopologyConditionStatus(t *testing.T) {
	tests := []struct {
		name      string
		label     string
		condition *operatorv1.OperatorCondition
		want      string
	}{
		{
			name:  "missing condition",
			label: "Progressing",
			want:  "  Progressing: Condition not available\n",
		},
		{
			name:  "available condition",
			label: "Upgradeable",
			condition: &operatorv1.OperatorCondition{
				Status:  operatorv1.ConditionFalse,
				Reason:  "TopologyTransitionInProgress",
				Message: "Cluster upgrade is not allowed during topology transition",
			},
			want: "  Upgradeable\n    Status: False\n    Reason: TopologyTransitionInProgress\n    Condition: Cluster upgrade is not allowed during topology transition\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatTopologyConditionStatus(tc.label, tc.condition); got != tc.want {
				t.Errorf("formatTopologyConditionStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ================================================================================
// VALIDATOR TESTS
// ================================================================================

func TestValidator_GetAvailableTransitions(t *testing.T) {
	current := TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.SingleReplicaTopologyMode,
	}

	transitions := []TopologyTransition{
		{
			Source: APITopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.SingleReplicaTopologyMode,
			},
			Target: APITopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
			},
			Evaluations: []metav1.Condition{
				{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue},
			},
		},
	}

	v := TransitionValidator{Current: current, Transitions: transitions}
	available := v.GetAvailableTransitions()

	if len(available) != 1 {
		t.Fatalf("expected 1 available transition, got %d", len(available))
	}

	if available[0].Target.InfrastructureTopology != configv1.HighlyAvailableTopologyMode {
		t.Errorf("expected target infra=HighlyAvailable, got %s", available[0].Target.InfrastructureTopology)
	}
}

func TestValidator_GetAvailableTransitions_FiltersNoOp(t *testing.T) {
	current := TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.HighlyAvailableTopologyMode,
	}

	transitions := []TopologyTransition{
		{
			Source: APITopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
			},
			Target: APITopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
			},
		},
	}

	v := TransitionValidator{Current: current, Transitions: transitions}
	available := v.GetAvailableTransitions()

	if len(available) != 0 {
		t.Errorf("expected 0 available transitions (noop filtered), got %d", len(available))
	}
}

func TestValidator_FindTransition(t *testing.T) {
	transitions := []TopologyTransition{
		{
			Source: APITopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.SingleReplicaTopologyMode,
			},
			Target: APITopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
			},
		},
	}

	v := TransitionValidator{Transitions: transitions}

	// Match by infra only (CP empty)
	t.Run("match by infra only", func(t *testing.T) {
		found := v.FindTransition(TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode})
		if found == nil {
			t.Fatal("expected to find transition")
		}
	})

	// Match by both
	t.Run("match by both", func(t *testing.T) {
		found := v.FindTransition(TopologyState{
			ControlPlane:   configv1.HighlyAvailableTopologyMode,
			Infrastructure: configv1.HighlyAvailableTopologyMode,
		})
		if found == nil {
			t.Fatal("expected to find transition")
		}
	})

	// No match
	t.Run("no match", func(t *testing.T) {
		found := v.FindTransition(TopologyState{Infrastructure: configv1.SingleReplicaTopologyMode})
		if found != nil {
			t.Error("expected no match")
		}
	})
}

func TestValidator_Validate(t *testing.T) {
	transitions := []TopologyTransition{
		{
			Source: APITopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.SingleReplicaTopologyMode,
			},
			Target: APITopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
			},
			Evaluations: []metav1.Condition{
				{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Reason: "TransitionAvailable", Message: "All checks passed"},
			},
		},
	}

	v := TransitionValidator{
		Current: TopologyState{
			ControlPlane:   configv1.HighlyAvailableTopologyMode,
			Infrastructure: configv1.SingleReplicaTopologyMode,
		},
		Transitions: transitions,
	}

	// Valid and available
	err := v.Validate(TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.HighlyAvailableTopologyMode,
	})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidator_Validate_NotAvailable(t *testing.T) {
	transitions := []TopologyTransition{
		{
			Source: APITopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.SingleReplicaTopologyMode,
			},
			Target: APITopologyState{
				ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
				InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
			},
			Evaluations: []metav1.Condition{
				{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionFalse, Reason: "Failed", Message: "Prerequisites not met"},
				{Type: "WorkerNodesReady", Status: metav1.ConditionFalse, Reason: "InsufficientWorkers", Message: "0 workers"},
			},
		},
	}

	v := TransitionValidator{
		Current: TopologyState{
			ControlPlane:   configv1.HighlyAvailableTopologyMode,
			Infrastructure: configv1.SingleReplicaTopologyMode,
		},
		Transitions: transitions,
	}

	err := v.Validate(TopologyState{
		ControlPlane:   configv1.HighlyAvailableTopologyMode,
		Infrastructure: configv1.HighlyAvailableTopologyMode,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("expected 'not available' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "WorkerNodesReady") {
		t.Errorf("expected prerequisite details in error, got: %v", err)
	}
}

func TestValidator_Validate_NoTransitions(t *testing.T) {
	v := TransitionValidator{}
	err := v.Validate(TopologyState{Infrastructure: configv1.HighlyAvailableTopologyMode})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no topology transitions are available") {
		t.Errorf("expected 'no topology transitions', got: %v", err)
	}
}

// ================================================================================
// HELPER TESTS
// ================================================================================

func TestIsTransitionAvailable(t *testing.T) {
	tests := []struct {
		name        string
		evaluations []metav1.Condition
		wantAvail   bool
		wantMsg     string
	}{
		{
			name: "available",
			evaluations: []metav1.Condition{
				{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue, Message: "All checks passed"},
			},
			wantAvail: true,
			wantMsg:   "All checks passed",
		},
		{
			name: "not available",
			evaluations: []metav1.Condition{
				{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionFalse, Message: "Prerequisites not met"},
			},
			wantAvail: false,
			wantMsg:   "Prerequisites not met",
		},
		{
			name:      "no availability condition",
			wantAvail: false,
			wantMsg:   "TopologyTransitionAvailable condition not found in evaluations",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			transition := &TopologyTransition{Evaluations: tc.evaluations}
			avail, msg := IsTransitionAvailable(transition)
			if avail != tc.wantAvail {
				t.Errorf("IsTransitionAvailable() available = %v, want %v", avail, tc.wantAvail)
			}
			if msg != tc.wantMsg {
				t.Errorf("IsTransitionAvailable() message = %q, want %q", msg, tc.wantMsg)
			}
		})
	}
}

func TestGetFailedEvaluations(t *testing.T) {
	transition := &TopologyTransition{
		Evaluations: []metav1.Condition{
			{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionFalse},
			{Type: "WorkerNodesReady", Status: metav1.ConditionFalse, Message: "0 workers"},
			{Type: "PlatformSupported", Status: metav1.ConditionTrue, Message: "platform: none"},
		},
	}

	failed := GetFailedEvaluations(transition)
	if len(failed) != 1 {
		t.Fatalf("expected 1 failed evaluation, got %d", len(failed))
	}
	if failed[0].Type != "WorkerNodesReady" {
		t.Errorf("expected WorkerNodesReady, got %s", failed[0].Type)
	}
}

func TestGetPassedEvaluations(t *testing.T) {
	transition := &TopologyTransition{
		Evaluations: []metav1.Condition{
			{Type: TopologyTransitionAvailableConditionType, Status: metav1.ConditionTrue},
			{Type: "WorkerNodesReady", Status: metav1.ConditionTrue, Message: "3 workers ready"},
			{Type: "PlatformSupported", Status: metav1.ConditionTrue, Message: "platform: none"},
		},
	}

	passed := GetPassedEvaluations(transition)
	if len(passed) != 2 {
		t.Fatalf("expected 2 passed evaluations, got %d", len(passed))
	}
}

func TestDescribeTransition(t *testing.T) {
	tests := []struct {
		name       string
		transition TopologyTransition
		want       string
	}{
		{
			name: "infra only change",
			transition: TopologyTransition{
				Source: APITopologyState{ControlPlaneTopology: "HighlyAvailable", InfrastructureTopology: "SingleReplica"},
				Target: APITopologyState{ControlPlaneTopology: "HighlyAvailable", InfrastructureTopology: "HighlyAvailable"},
			},
			want: "Infrastructure: SingleReplica -> HighlyAvailable (control plane unchanged)",
		},
		{
			name: "CP only change",
			transition: TopologyTransition{
				Source: APITopologyState{ControlPlaneTopology: "SingleReplica", InfrastructureTopology: "SingleReplica"},
				Target: APITopologyState{ControlPlaneTopology: "HighlyAvailable", InfrastructureTopology: "SingleReplica"},
			},
			want: "Control Plane: SingleReplica -> HighlyAvailable (infrastructure unchanged)",
		},
		{
			name: "both change",
			transition: TopologyTransition{
				Source: APITopologyState{ControlPlaneTopology: "SingleReplica", InfrastructureTopology: "SingleReplica"},
				Target: APITopologyState{ControlPlaneTopology: "HighlyAvailable", InfrastructureTopology: "HighlyAvailable"},
			},
			want: "Control Plane: SingleReplica -> HighlyAvailable, Infrastructure: SingleReplica -> HighlyAvailable",
		},
		{
			name: "no change",
			transition: TopologyTransition{
				Source: APITopologyState{ControlPlaneTopology: "HighlyAvailable", InfrastructureTopology: "HighlyAvailable"},
				Target: APITopologyState{ControlPlaneTopology: "HighlyAvailable", InfrastructureTopology: "HighlyAvailable"},
			},
			want: "No topology changes",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := describeTransition(&tc.transition)
			if got != tc.want {
				t.Errorf("describeTransition() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFindCondition(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: "TypeA", Status: metav1.ConditionTrue},
		{Type: "TypeB", Status: metav1.ConditionFalse},
	}

	t.Run("found", func(t *testing.T) {
		c := findCondition(conditions, "TypeA")
		if c == nil {
			t.Fatal("expected to find condition")
		}
		if c.Status != metav1.ConditionTrue {
			t.Errorf("expected status True, got %s", c.Status)
		}
	})

	t.Run("not found", func(t *testing.T) {
		c := findCondition(conditions, "TypeC")
		if c != nil {
			t.Error("expected nil for missing condition")
		}
	})
}

func TestFormatTopologyValue(t *testing.T) {
	if got := formatTopologyValue(""); got != "(not set)" {
		t.Errorf("formatTopologyValue(\"\") = %q, want \"(not set)\"", got)
	}
	if got := formatTopologyValue("HighlyAvailable"); got != "HighlyAvailable" {
		t.Errorf("formatTopologyValue(\"HighlyAvailable\") = %q, want \"HighlyAvailable\"", got)
	}
}
