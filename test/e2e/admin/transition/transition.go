package transition

import (
	"encoding/json"
	"os"

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
var _ = g.Describe("[sig-cli] oc adm transition", g.Label("topology-transition"), func() {
	var (
		fakeServer     *fakeAPIServer
		kubeconfigPath string
		oc             *e2e.CLI
	)

	g.BeforeEach(func() {
		fakeServer = newFakeAPIServer()
		path, err := writeKubeconfig(fakeServer.URL())
		o.Expect(err).NotTo(o.HaveOccurred())
		kubeconfigPath = path
		oc = e2e.NewCLI("oc", kubeconfigPath).EnvVar("OC_ENABLE_CMD_TRANSITION_TOPOLOGY", "true")
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
})
