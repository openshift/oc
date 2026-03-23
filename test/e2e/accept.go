package e2e

import (
	"context"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	oteginkgo "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"
	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
)

var _ = g.Describe("[sig-cli] oc", g.Label("cluster-version-operator"), func() {

	var (
		ctx            = context.TODO()
		kubeConfigPath = KubeConfigPath()
		oc             = NewCLI("oc", kubeConfigPath).EnvVar("OC_ENABLE_CMD_UPGRADE_ACCEPT_RISKS", "true")
		configClient   *configv1client.ConfigV1Client
	)

	g.BeforeEach(func() {
		config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
		o.Expect(err).NotTo(o.HaveOccurred())
		configClient, err = configv1client.NewForConfig(config)
		o.Expect(err).NotTo(o.HaveOccurred())

		cv, err := configClient.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		if cv.Spec.DesiredUpdate != nil {
			o.Expect(cv.Spec.DesiredUpdate.AcceptRisks).To(o.BeEmpty())
		}
	})

	g.AfterEach(func() {
		cv, err := configClient.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		if cv.Spec.DesiredUpdate != nil && len(cv.Spec.DesiredUpdate.AcceptRisks) > 0 {
			backup := cv.DeepCopy()
			backup.Spec.DesiredUpdate.AcceptRisks = nil
			backup, err = configClient.ClusterVersions().Update(ctx, backup, metav1.UpdateOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())
		}
	})

	g.It("can operate accept risks [Serial]", g.Label("tech-preview"), oteginkgo.Informing(), func() {
		skipIfMicroShift(oc)
		SkipIfNotTechPreviewNoUpgrade(ctx, configClient)

		g.By("accepting some risks")
		out, err := oc.Run("adm").Args("upgrade", "accept", "RiskA,RiskB").WithoutNamespace().Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "output: %s", out)
		verifyAcceptRisks(ctx, configClient, []configv1.AcceptRisk{{Name: "RiskA"}, {Name: "RiskB"}})

		g.By("accepting more risks")
		out, err = oc.Run("adm").Args("upgrade", "accept", "RiskA,RiskB,RiskC").Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "output: %s", out)
		verifyAcceptRisks(ctx, configClient, []configv1.AcceptRisk{{Name: "RiskA"}, {Name: "RiskB"}, {Name: "RiskC"}})

		g.By("replacing some risks")
		out, err = oc.Run("adm").Args("upgrade", "accept", "--replace", "RiskB,RiskD").Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "output: %s", out)
		verifyAcceptRisks(ctx, configClient, []configv1.AcceptRisk{{Name: "RiskB"}, {Name: "RiskD"}})

		g.By("removing some risks")
		out, err = oc.Run("adm").Args("upgrade", "accept", "RiskB,RiskD-").Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "output: %s", out)
		verifyAcceptRisks(ctx, configClient, []configv1.AcceptRisk{{Name: "RiskB"}})

		g.By("removing all risks")
		out, err = oc.Run("adm").Args("upgrade", "accept", "--clear").Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "output: %s", out)
		verifyAcceptRisks(ctx, configClient, nil)
	})

})

func verifyAcceptRisks(ctx context.Context, client *configv1client.ConfigV1Client, risks []configv1.AcceptRisk) {
	cv, err := client.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(cv.Spec.DesiredUpdate).NotTo(o.BeNil())
	o.Expect(cv.Spec.DesiredUpdate.AcceptRisks).To(o.Equal(risks))
}
