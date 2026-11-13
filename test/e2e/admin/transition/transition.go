package transition

import (
	"encoding/json"
	"os"
	"os/exec"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	metav1helpers "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"

	"github.com/openshift/oc/test/e2e"
)

// These specs run the real oc binary against a scripted fake API server
// (helpers.go), so they need no cluster and stay deterministic: they test the
// CLI, not the cluster-side transition, which origin's topology-transitions
// suite covers against a live cluster.
var _ = g.Describe("oc adm transition", g.Label("topology-transition"), func() {
	var (
		fakeServer     *fakeAPIServer
		kubeconfigPath string
		oc             *e2e.CLI
	)

	g.BeforeEach(func() {
		// Resolve `oc` from PATH up front so a missing binary fails here with
		// a clear message instead of an obscure exec error in every spec.
		ocPath, err := exec.LookPath("oc")
		o.Expect(err).NotTo(o.HaveOccurred(), "oc not found on PATH: build it with 'make oc' and export PATH=$PWD:$PATH so the suite exercises the built binary")
		fakeServer = newFakeAPIServer()
		path, err := writeKubeconfig(fakeServer.URL())
		o.Expect(err).NotTo(o.HaveOccurred())
		kubeconfigPath = path
		oc = e2e.NewCLI(ocPath, kubeconfigPath).EnvVar("OC_ENABLE_CMD_TRANSITION_TOPOLOGY", "true")
	})

	g.AfterEach(func() {
		fakeServer.Close()
		os.Remove(kubeconfigPath)
	})

	g.It("Author:jcope-Medium-OCPEDGE-2953-discovery lists available topology transitions [TopologyTransition][Serial]", func() {
		g.By("reading the discovery output")
		out, err := oc.Run("adm").Args("transition", "topology").WithoutNamespace().Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "output: %s", out)
		o.Expect(out).To(o.ContainSubstring("Current Topology Configuration:"))
		o.Expect(out).To(o.ContainSubstring("Control Plane:  SingleReplica"))
		o.Expect(out).To(o.ContainSubstring("Infrastructure: SingleReplica"))
		o.Expect(out).To(o.ContainSubstring("Supported Transitions:"))
		o.Expect(out).To(o.ContainSubstring("Control Plane:  SingleReplica -> HighlyAvailable"))
		o.Expect(out).To(o.ContainSubstring("Infrastructure: SingleReplica -> HighlyAvailable"))
		o.Expect(out).To(o.ContainSubstring("Availability:   Available"))

		g.By("reading the discovery report as JSON")
		stdout, _, err := oc.Run("adm").Args("transition", "topology", "-o", "json").WithoutNamespace().Outputs()
		o.Expect(err).NotTo(o.HaveOccurred(), "output: %s", stdout)
		var report discoveryReport
		o.Expect(json.Unmarshal([]byte(stdout), &report)).To(o.Succeed())
		o.Expect(report.Metadata.Generation).To(o.Equal(int64(1)))
		evaluated := metav1helpers.FindStatusCondition(report.Conditions, configv1.TopologyTransitionsEvaluatedConditionType)
		o.Expect(evaluated).NotTo(o.BeNil())
		o.Expect(evaluated.Status).To(o.Equal(metav1.ConditionTrue))
		o.Expect(evaluated.ObservedGeneration).To(o.Equal(report.Metadata.Generation))
		o.Expect(report.Transitions).To(o.HaveLen(1))
		o.Expect(report.Transitions[0].Target.ControlPlaneTopology).To(o.Equal(configv1.HighlyAvailableTopologyMode))
		o.Expect(report.Transitions[0].Target.InfrastructureTopology).To(o.Equal(configv1.HighlyAvailableTopologyMode))
		available := metav1helpers.FindStatusCondition(report.Transitions[0].Evaluations, configv1.TopologyTransitionAvailableConditionType)
		o.Expect(available).NotTo(o.BeNil())
		o.Expect(available.Status).To(o.Equal(metav1.ConditionTrue))

		g.By("leaving the Infrastructure untouched")
		o.Expect(fakeServer.PatchBodies()).To(o.BeEmpty())
	})

	g.It("Author:jcope-Medium-OCPEDGE-2953-dry-run previews an available topology transition [TopologyTransition][Serial]", func() {
		g.By("previewing the transition without --confirm")
		out, err := oc.Run("adm").Args("transition", "topology", "--control-plane=HighlyAvailable", "--infrastructure=HighlyAvailable").WithoutNamespace().Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "output: %s", out)
		o.Expect(out).To(o.ContainSubstring("Dry run: The following changes would be applied:"))
		o.Expect(out).To(o.ContainSubstring("spec.controlPlaneTopology:   SingleReplica -> HighlyAvailable"))
		o.Expect(out).To(o.ContainSubstring("spec.infrastructureTopology: SingleReplica -> HighlyAvailable"))
		o.Expect(out).To(o.ContainSubstring("Add --confirm to apply this transition"))

		g.By("leaving the Infrastructure untouched")
		o.Expect(fakeServer.PatchBodies()).To(o.BeEmpty())
	})

	g.It("Author:jcope-High-OCPEDGE-2953-initiate completes a topology transition through the status-read path [TopologyTransition][Serial]", func() {
		g.By("initiating the transition")
		out, err := oc.Run("adm").Args("transition", "topology", "--control-plane=HighlyAvailable", "--infrastructure=HighlyAvailable", "--confirm").WithoutNamespace().Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "output: %s", out)
		o.Expect(out).To(o.ContainSubstring("Transition initiated. Monitor progress with:"))
		o.Expect(out).To(o.ContainSubstring("oc adm transition status"))

		g.By("patching the Infrastructure spec")
		patches := fakeServer.PatchBodies()
		o.Expect(patches).To(o.HaveLen(1))
		var patch struct {
			Spec struct {
				ControlPlaneTopology   configv1.TopologyMode `json:"controlPlaneTopology"`
				InfrastructureTopology configv1.TopologyMode `json:"infrastructureTopology"`
			} `json:"spec"`
		}
		o.Expect(json.Unmarshal([]byte(patches[0]), &patch)).To(o.Succeed())
		o.Expect(patch.Spec.ControlPlaneTopology).To(o.Equal(configv1.HighlyAvailableTopologyMode))
		o.Expect(patch.Spec.InfrastructureTopology).To(o.Equal(configv1.HighlyAvailableTopologyMode))

		g.By("reading the in-progress status")
		out, err = oc.Run("adm").Args("transition", "status").WithoutNamespace().Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "output: %s", out)
		o.Expect(out).To(o.ContainSubstring("Spec (desired):   HighlyAvailable"))
		o.Expect(out).To(o.ContainSubstring("Status (current): SingleReplica"))
		o.Expect(out).To(o.ContainSubstring("Reason: TopologyTransitionInProgress"))
		o.Expect(out).To(o.ContainSubstring(`Status: "False"`))

		stdout, _, err := oc.Run("adm").Args("transition", "status", "-o", "json").WithoutNamespace().Outputs()
		o.Expect(err).NotTo(o.HaveOccurred(), "output: %s", stdout)
		var report statusReport
		o.Expect(json.Unmarshal([]byte(stdout), &report)).To(o.Succeed())
		o.Expect(report.Metadata.Generation).To(o.Equal(int64(2)))
		o.Expect(report.Spec.ControlPlaneTopology).To(o.Equal(configv1.HighlyAvailableTopologyMode))
		o.Expect(report.Spec.InfrastructureTopology).To(o.Equal(configv1.HighlyAvailableTopologyMode))
		o.Expect(report.Status.ControlPlaneTopology).To(o.Equal(configv1.SingleReplicaTopologyMode))
		o.Expect(report.Status.InfrastructureTopology).To(o.Equal(configv1.SingleReplicaTopologyMode))
		completed := metav1helpers.FindStatusCondition(report.Status.TopologyTransitionStatus.Conditions, configv1.TopologyTransitionCompletedConditionType)
		o.Expect(completed).NotTo(o.BeNil())
		o.Expect(completed.Status).To(o.Equal(metav1.ConditionFalse))
		o.Expect(completed.ObservedGeneration).To(o.Equal(report.Metadata.Generation))

		g.By("polling the status until the transition completes")
		var finalReport statusReport
		o.Eventually(func(g o.Gomega) {
			stdout, _, err := oc.Run("adm").Args("transition", "status", "-o", "json").WithoutNamespace().Outputs()
			g.Expect(err).NotTo(o.HaveOccurred(), "output: %s", stdout)
			var report statusReport
			g.Expect(json.Unmarshal([]byte(stdout), &report)).To(o.Succeed())
			completed := metav1helpers.FindStatusCondition(report.Status.TopologyTransitionStatus.Conditions, configv1.TopologyTransitionCompletedConditionType)
			g.Expect(completed).NotTo(o.BeNil())
			g.Expect(completed.Status).To(o.Equal(metav1.ConditionTrue))
			g.Expect(completed.ObservedGeneration).To(o.Equal(report.Metadata.Generation))
			finalReport = report
		}, "2m", "1s").Should(o.Succeed())

		g.By("reading the completed status")
		out, err = oc.Run("adm").Args("transition", "status").WithoutNamespace().Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "output: %s", out)
		o.Expect(out).To(o.ContainSubstring("Control Plane Topology:"))
		o.Expect(out).To(o.ContainSubstring("Infrastructure Topology:"))
		o.Expect(out).To(o.ContainSubstring("Spec (desired):   HighlyAvailable"))
		o.Expect(out).To(o.ContainSubstring("Status (current): HighlyAvailable"))
		o.Expect(out).To(o.ContainSubstring("Reason: TopologyTransitionCompleted"))
		o.Expect(out).To(o.ContainSubstring(`Status: "True"`))

		g.By("reading the completed status as JSON")
		o.Expect(finalReport.Metadata.Generation).To(o.Equal(int64(2)))
		o.Expect(finalReport.Spec.ControlPlaneTopology).To(o.Equal(configv1.HighlyAvailableTopologyMode))
		o.Expect(finalReport.Spec.InfrastructureTopology).To(o.Equal(configv1.HighlyAvailableTopologyMode))
		o.Expect(finalReport.Status.ControlPlaneTopology).To(o.Equal(configv1.HighlyAvailableTopologyMode))
		o.Expect(finalReport.Status.InfrastructureTopology).To(o.Equal(configv1.HighlyAvailableTopologyMode))
		completed = metav1helpers.FindStatusCondition(finalReport.Status.TopologyTransitionStatus.Conditions, configv1.TopologyTransitionCompletedConditionType)
		o.Expect(completed).NotTo(o.BeNil())
		o.Expect(completed.Status).To(o.Equal(metav1.ConditionTrue))
		o.Expect(completed.ObservedGeneration).To(o.Equal(finalReport.Metadata.Generation))
		evaluated := metav1helpers.FindStatusCondition(finalReport.Status.TopologyTransitionStatus.Conditions, configv1.TopologyTransitionsEvaluatedConditionType)
		o.Expect(evaluated).NotTo(o.BeNil())
		o.Expect(evaluated.Status).To(o.Equal(metav1.ConditionTrue))
		o.Expect(evaluated.ObservedGeneration).To(o.Equal(finalReport.Metadata.Generation))
	})
})
