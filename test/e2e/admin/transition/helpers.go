package transition

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"

	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

const infrastructuresPath = "/apis/config.openshift.io/v1/infrastructures/cluster"

// getsBeforeCompletion is the number of GETs served in the in-progress phase
// after a PATCH before the fake reports the transition completed. The
// lifecycle spec reads the in-progress state twice (default and JSON output),
// then polls: the first poll still sees in-progress, the next one completion,
// which exercises the wait-and-retry path without timers.
const getsBeforeCompletion = 3

// fakeAPIServer is a scripted API server serving the single Infrastructure
// resource the transition commands read and patch. Specs run the real oc
// binary against it, so the CLI is exercised end-to-end without a cluster.
type fakeAPIServer struct {
	server *httptest.Server

	// mutex guards every field below; the server handles requests on its own goroutines.
	mutex sync.Mutex
	infra *configv1.Infrastructure

	// patchBodies records the raw body of every PATCH request, so specs can
	// assert what oc sent (and that dry-runs send none).
	patchBodies []string

	// getsSincePatch counts GETs served since the last PATCH.
	getsSincePatch int
}

// newFakeAPIServer starts a server whose Infrastructure is an SNO cluster with
// current evaluations and an available SNO->HA transition advertised.
func newFakeAPIServer() *fakeAPIServer {
	f := &fakeAPIServer{
		infra: idleInfrastructure(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(infrastructuresPath, f.handleInfrastructure)
	f.server = httptest.NewServer(mux)
	return f
}

// URL returns the server's base URL.
func (f *fakeAPIServer) URL() string { return f.server.URL }

// Close shuts the server down.
func (f *fakeAPIServer) Close() { f.server.Close() }

// PatchBodies returns the raw bodies of all PATCH requests received so far.
func (f *fakeAPIServer) PatchBodies() []string {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return append([]string(nil), f.patchBodies...)
}

func (f *fakeAPIServer) handleInfrastructure(w http.ResponseWriter, r *http.Request) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	switch r.Method {
	case http.MethodGet:
		f.writeInfra(w)
	case http.MethodPatch:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		f.patchBodies = append(f.patchBodies, string(body))
		f.applyTransitionPatch(body)
		f.writeInfra(w)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// writeInfra serves the Infrastructure, advancing the transition state machine
// on every GET once a patch has been received.
func (f *fakeAPIServer) writeInfra(w http.ResponseWriter) {
	if len(f.patchBodies) > 0 && f.getsSincePatch >= getsBeforeCompletion {
		f.completeTransition()
	}
	f.getsSincePatch++
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(f.infra); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// applyTransitionPatch applies the spec fields from a merge-patch sent by oc,
// bumps the generation, and reports the transition in progress. It plays the
// role of both the API server and the topology controller.
func (f *fakeAPIServer) applyTransitionPatch(body []byte) {
	patch := struct {
		Spec struct {
			ControlPlaneTopology   configv1.TopologyMode `json:"controlPlaneTopology"`
			InfrastructureTopology configv1.TopologyMode `json:"infrastructureTopology"`
		} `json:"spec"`
	}{}
	if err := json.Unmarshal(body, &patch); err != nil {
		return
	}

	f.getsSincePatch = 0
	if patch.Spec.ControlPlaneTopology != "" {
		f.infra.Spec.ControlPlaneTopology = patch.Spec.ControlPlaneTopology
	}
	if patch.Spec.InfrastructureTopology != "" {
		f.infra.Spec.InfrastructureTopology = patch.Spec.InfrastructureTopology
	}
	f.infra.Generation++
	// The controller stops advertising transitions once one is initiated.
	f.infra.Status.TopologyTransitionStatus.Transitions = nil
	setTransitionConditions(f.infra, metav1.ConditionFalse, "TopologyTransitionInProgress", "Topology transition in progress")
}

// completeTransition moves the status topologies to the requested target and
// reports the transition completed. Idempotent.
func (f *fakeAPIServer) completeTransition() {
	f.infra.Status.ControlPlaneTopology = f.infra.Spec.ControlPlaneTopology
	f.infra.Status.InfrastructureTopology = f.infra.Spec.InfrastructureTopology
	setTransitionConditions(f.infra, metav1.ConditionTrue, "TopologyTransitionCompleted", "Topology transition completed")
}

// idleInfrastructure returns the Infrastructure served before any transition:
// an SNO cluster with current evaluations and one available SNO->HA transition.
func idleInfrastructure() *configv1.Infrastructure {
	infra := &configv1.Infrastructure{
		TypeMeta:   metav1.TypeMeta{APIVersion: "config.openshift.io/v1", Kind: "Infrastructure"},
		ObjectMeta: metav1.ObjectMeta{Name: "cluster", Generation: 1},
		Spec: configv1.InfrastructureSpec{
			ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
			InfrastructureTopology: configv1.SingleReplicaTopologyMode,
		},
	}
	infra.Status.TopologyTransitionStatus = configv1.TopologyTransitionStatus{
		Transitions: []configv1.TopologyTransition{
			{
				Source: configv1.TopologyState{
					ControlPlaneTopology:   configv1.SingleReplicaTopologyMode,
					InfrastructureTopology: configv1.SingleReplicaTopologyMode,
				},
				Target: configv1.TopologyState{
					ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
					InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
				},
				Evaluations: []metav1.Condition{
					{
						Type:    configv1.TopologyTransitionAvailableConditionType,
						Status:  metav1.ConditionTrue,
						Reason:  "TransitionSupported",
						Message: "Transition from SingleReplica to HighlyAvailable is supported",
					},
				},
			},
		},
	}
	setTransitionConditions(infra, metav1.ConditionUnknown, "NoTransitionRequested", "No topology transition has been requested")
	return infra
}

// setTransitionConditions reports both lifecycle conditions as current for the
// Infrastructure's generation, the way the topology controller does.
func setTransitionConditions(infra *configv1.Infrastructure, completed metav1.ConditionStatus, reason, message string) {
	infra.Status.TopologyTransitionStatus.Conditions = []metav1.Condition{
		{
			Type:               configv1.TopologyTransitionsEvaluatedConditionType,
			Status:             metav1.ConditionTrue,
			Reason:             "EvaluationComplete",
			Message:            "Topology transition evaluations completed successfully",
			ObservedGeneration: infra.Generation,
		},
		{
			Type:               configv1.TopologyTransitionCompletedConditionType,
			Status:             completed,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: infra.Generation,
		},
	}
}

// writeKubeconfig writes a kubeconfig pointing at serverURL and returns its path.
func writeKubeconfig(serverURL string) (string, error) {
	config := api.NewConfig()
	config.Clusters["transition"] = &api.Cluster{Server: serverURL}
	config.AuthInfos["transition"] = &api.AuthInfo{}
	config.Contexts["transition"] = &api.Context{Cluster: "transition", AuthInfo: "transition"}
	config.CurrentContext = "transition"

	f, err := os.CreateTemp("", "oc-transition-*.kubeconfig")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		return path, err
	}
	if err := clientcmd.WriteToFile(*config, path); err != nil {
		return path, err
	}
	return path, nil
}

// discoveryReport mirrors the JSON contract of `oc adm transition topology -o json`:
// Infrastructure metadata plus the controller's TopologyTransitionStatus at the root.
type discoveryReport struct {
	Metadata struct {
		Generation int64 `json:"generation"`
	} `json:"metadata"`
	configv1.TopologyTransitionStatus
}

// statusReport mirrors the JSON contract of `oc adm transition status -o json`:
// Infrastructure metadata, the spec and status topologies, and the raw
// TopologyTransitionStatus report.
type statusReport struct {
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
