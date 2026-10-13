package transition

// topologyTransitionClient abstracts access to Infrastructure CR fields that are not yet
// in the vendored openshift/api types.
// TODO(vendor): When openshift/api PR #3029 is vendored, replace this interface with
// direct typed client calls and remove the REST-based implementation.

import (
	"context"
	"encoding/json"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

// topologyTransitionClient provides access to Infrastructure CR fields that
// are not yet present in the vendored openshift/api types.
type topologyTransitionClient interface {
	// getTopologyTransitionStatus reads status.topologyTransitionStatus.
	getTopologyTransitionStatus(ctx context.Context) (*TopologyTransitionStatus, error)

	// getInfrastructureTopologySpec reads spec.infrastructureTopology.
	getInfrastructureTopologySpec(ctx context.Context) (configv1.TopologyMode, error)

	// patchTopologySpec patches spec topology fields on the Infrastructure CR.
	// Only non-empty values are included in the patch.
	patchTopologySpec(ctx context.Context, controlPlane, infrastructure configv1.TopologyMode) error
}

// restTopologyClient implements topologyTransitionClient using the config API REST client.
type restTopologyClient struct {
	restClient rest.Interface
}

func newRESTTopologyClient(restClient rest.Interface) *restTopologyClient {
	return &restTopologyClient{restClient: restClient}
}

// infrastructureRaw is a partial representation of the Infrastructure resource
// for unmarshaling fields not in the vendored types.
type infrastructureRaw struct {
	Spec struct {
		InfrastructureTopology configv1.TopologyMode `json:"infrastructureTopology,omitempty"`
	} `json:"spec"`
	Status struct {
		TopologyTransitionStatus *TopologyTransitionStatus `json:"topologyTransitionStatus,omitempty"`
	} `json:"status"`
}

func (c *restTopologyClient) getRaw(ctx context.Context) (*infrastructureRaw, error) {
	data, err := c.restClient.
		Get().
		Resource("infrastructures").
		Name(infrastructureResourceName).
		Do(ctx).
		Raw()
	if err != nil {
		return nil, fmt.Errorf("failed to get Infrastructure resource: %w", err)
	}

	var raw infrastructureRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Infrastructure resource: %w", err)
	}

	return &raw, nil
}

func (c *restTopologyClient) getTopologyTransitionStatus(ctx context.Context) (*TopologyTransitionStatus, error) {
	raw, err := c.getRaw(ctx)
	if err != nil {
		return nil, err
	}

	return raw.Status.TopologyTransitionStatus, nil
}

func (c *restTopologyClient) getInfrastructureTopologySpec(ctx context.Context) (configv1.TopologyMode, error) {
	raw, err := c.getRaw(ctx)
	if err != nil {
		return "", err
	}

	return raw.Spec.InfrastructureTopology, nil
}

func (c *restTopologyClient) patchTopologySpec(ctx context.Context, controlPlane, infrastructure configv1.TopologyMode) error {
	spec := map[string]interface{}{}
	if controlPlane != "" {
		spec["controlPlaneTopology"] = string(controlPlane)
	}
	if infrastructure != "" {
		spec["infrastructureTopology"] = string(infrastructure)
	}

	if len(spec) == 0 {
		return nil
	}

	patchBody := map[string]interface{}{
		"spec": spec,
	}

	patchBytes, err := json.Marshal(patchBody)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %w", err)
	}

	_, err = c.restClient.
		Patch(types.MergePatchType).
		Resource("infrastructures").
		Name(infrastructureResourceName).
		Body(patchBytes).
		Do(ctx).
		Raw()
	if err != nil {
		return fmt.Errorf("failed to patch Infrastructure spec: %w", err)
	}

	return nil
}

// fakeTopologyClient implements topologyTransitionClient for unit tests.
type fakeTopologyClient struct {
	transitionStatus *TopologyTransitionStatus
	infraSpec        configv1.TopologyMode
	patchErr         error

	// patchedControlPlane records the CP topology value from the last patchTopologySpec call.
	patchedControlPlane configv1.TopologyMode
	// patchedInfrastructure records the infra topology value from the last patchTopologySpec call.
	patchedInfrastructure configv1.TopologyMode
	// patchCalled records whether patchTopologySpec was called.
	patchCalled bool
}

func (f *fakeTopologyClient) getTopologyTransitionStatus(ctx context.Context) (*TopologyTransitionStatus, error) {
	return f.transitionStatus, nil
}

func (f *fakeTopologyClient) getInfrastructureTopologySpec(ctx context.Context) (configv1.TopologyMode, error) {
	return f.infraSpec, nil
}

func (f *fakeTopologyClient) patchTopologySpec(ctx context.Context, controlPlane, infrastructure configv1.TopologyMode) error {
	f.patchCalled = true
	f.patchedControlPlane = controlPlane
	f.patchedInfrastructure = infrastructure
	return f.patchErr
}
