package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"k8s.io/client-go/tools/clientcmd"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"

	ote "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"
)

var _ = g.Describe(`[sig-updates] OTA should`, g.Label("upgrade"), g.Ordered, func() {

	defer g.GinkgoRecover()

	var (
		ctx            = context.TODO()
		kubeConfigPath = KubeConfigPath()
		configClient   *configv1client.ConfigV1Client
		testGraphHost  = "https://fauxinnati-fauxinnati.apps.ota-stage.q2z4.p1.openshiftapps.com/"
		cvBackup       *configv1.ClusterVersionSpec
		needRecover    bool = false
	)

	g.BeforeAll(func() {
		config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
		o.Expect(err).NotTo(o.HaveOccurred())
		configClient, err = configv1client.NewForConfig(config)
		o.Expect(err).NotTo(o.HaveOccurred())
		oc := NewCLI("oc", kubeConfigPath)
		skipIfMicroShift(oc)
		SkipIfHypershift(ctx, configClient)

		cv, err := configClient.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		cvBackup = cv.Spec.DeepCopy()
		overrides := cv.Spec.Overrides

		if overrides != nil {
			newOverrides := []configv1.ComponentOverride{}
			for _, override := range overrides {
				if override.Kind != "ClusterImagePolicy" {
					newOverrides = append(newOverrides, override)
				}
			}
			cv.Spec.Overrides = newOverrides
			_, err = configClient.ClusterVersions().Update(ctx, cv, metav1.UpdateOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())
			needRecover = true
		}
	})

	g.AfterAll(func() {
		if needRecover {
			cv, err := configClient.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())
			cv.Spec = *cvBackup
			_, err = configClient.ClusterVersions().Update(ctx, cv, metav1.UpdateOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())
		}
	})

	g.Context("accept risks exposed to conditional updates", g.Label("accept"), func() {
		g.It("Accepted Risks for OCP Cluster Updates", ote.Informing(), g.Label("TechPreview", "ConnectedOnly", "88175", "Slow", "Manual"), func() {
			SkipIfNotTechPreviewNoUpgrade(ctx, configClient)
			oc := NewCLI("oc", kubeConfigPath).EnvVar("OC_ENABLE_CMD_UPGRADE_ACCEPT_RISKS", "true")
			skipIfDisconnected(oc)

			g.By("patch fauxinnati upstream")
			graph := fmt.Sprintf("%sapi/upgrades_info/graph", testGraphHost)
			cv, err := configClient.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())
			cv.Spec.Channel = "OCP-88175"
			cv.Spec.Upstream = configv1.URL(graph)
			_, err = configClient.ClusterVersions().Update(ctx, cv, metav1.UpdateOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())
			defer func() {
				restoreCV, err := configClient.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
				restoreCV.Spec.Channel = cvBackup.Channel
				restoreCV.Spec.Upstream = cvBackup.Upstream
				_, err = configClient.ClusterVersions().Update(ctx, restoreCV, metav1.UpdateOptions{})
				o.Expect(err).NotTo(o.HaveOccurred())
			}()

			g.By("wait there are enough availableUpdates and conditionalUpdates")
			err = wait.Poll(5*time.Second, 3*time.Minute, func() (bool, error) {
				tempCV, err := configClient.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
				o.Expect(err).NotTo(o.HaveOccurred())
				conditionalUpdates := tempCV.Status.ConditionalUpdates
				if len(conditionalUpdates) != 3 {
					return false, nil
				}
				availableUpdates := tempCV.Status.AvailableUpdates
				if len(availableUpdates) != 1 {
					return false, nil
				}
				return true, nil
			})
			AssertWaitPollNoErr(err, fmt.Sprintf("After waiting 3 minutes, there are still not enough availableUpdates and conditionalUpdates %d, %d", len(cv.Status.ConditionalUpdates), len(cv.Status.AvailableUpdates)))

			cv, err = configClient.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())
			recommendVersion := cv.Status.AvailableUpdates[0].Version
			o.Expect(recommendVersion).NotTo(o.BeEmpty())

			notRecommendVersion1 := ""
			notRecommendVersion2 := ""
			notRecommendVersion3 := ""
			for _, cu := range cv.Status.ConditionalUpdates {
				if len(cu.RiskNames) == 2 {
					notRecommendVersion2 = cu.Release.Version
				} else if len(cu.RiskNames) == 1 {
					if cu.RiskNames[0] == "SomeInvokerThing" {
						notRecommendVersion1 = cu.Release.Version
					} else {
						// SomeInfrastructureThing
						notRecommendVersion3 = cu.Release.Version
					}
				}
			}
			o.Expect(notRecommendVersion1).NotTo(o.BeEmpty())
			o.Expect(notRecommendVersion2).NotTo(o.BeEmpty())
			o.Expect(notRecommendVersion3).NotTo(o.BeEmpty())

			g.By("default output of `oc adm upgrade` should have the recommend version but not non-recommend versions")
			out, err := oc.Run("adm").Args("upgrade").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			o.Expect(out).To(o.ContainSubstring(recommendVersion), fmt.Sprintf("recommend version is missing %s", recommendVersion))
			o.Expect(out).NotTo(o.ContainSubstring(notRecommendVersion1), fmt.Sprintf("non-recommend version is unexpectedly present %s", notRecommendVersion1))
			o.Expect(out).NotTo(o.ContainSubstring(notRecommendVersion2), fmt.Sprintf("non-recommend version is unexpectedly present %s", notRecommendVersion2))
			o.Expect(out).NotTo(o.ContainSubstring(notRecommendVersion3), fmt.Sprintf("non-recommend version is unexpectedly present %s", notRecommendVersion3))

			g.By("output of `oc adm upgrade --include-not-recommended` should have all versions")
			out, err = oc.Run("adm").Args("upgrade", "--include-not-recommended").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			o.Expect(out).To(o.ContainSubstring(recommendVersion), fmt.Sprintf("recommend version is missing %s", recommendVersion))
			o.Expect(out).To(o.ContainSubstring(notRecommendVersion1), fmt.Sprintf("non-recommend version is missing %s", notRecommendVersion1))
			o.Expect(out).To(o.ContainSubstring(notRecommendVersion2), fmt.Sprintf("non-recommend version is missing %s", notRecommendVersion2))
			o.Expect(out).To(o.ContainSubstring(notRecommendVersion3), fmt.Sprintf("non-recommend version is missing %s", notRecommendVersion3))

			g.By("upgrade to a non recommend version is blocked")
			out, err = oc.Run("adm").Args("upgrade", "--to", notRecommendVersion1).Output()
			o.Expect(err).To(o.HaveOccurred())
			o.Expect(out).To(o.ContainSubstring(fmt.Sprintf("the update %s is not one of the recommended updates, but is available as a conditional update.", notRecommendVersion1)))

			g.By("clear risks when the accept risk list is empty should work")
			out, err = oc.Run("adm").Args("upgrade", "accept", "--clear").Output()
			o.Expect(err).NotTo(o.HaveOccurred())

			upgradeComplete, err := WaitUpgradeComplete(oc, 5*time.Second, 5*time.Minute)
			o.Expect(err).NotTo(o.HaveOccurred())
			o.Expect(upgradeComplete).To(o.BeTrue())

			g.By("Accept risk SomeInvokerThing")
			risks := "SomeInvokerThing"
			out, err = oc.Run("adm").Args("upgrade", "accept", risks).Output()
			o.Expect(err).NotTo(o.HaveOccurred())

			afterAcceptRiskCV, err := configClient.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())
			o.Expect(len(afterAcceptRiskCV.Spec.DesiredUpdate.AcceptRisks)).To(o.Equal(1))
			o.Expect(afterAcceptRiskCV.Spec.DesiredUpdate.AcceptRisks[0].Name).To(o.Equal(risks))

			err = wait.Poll(20*time.Second, 10*time.Minute, func() (bool, error) {
				out, err := oc.Run("adm").Args("upgrade").Output()
				if err != nil {
					return false, nil
				}
				if !strings.Contains(out, notRecommendVersion1) {
					return false, nil
				}
				return true, nil
			})
			AssertWaitPollNoErr(err, fmt.Sprintf("the non-recommend version %s is not available after accepting the risk %s. \nOutput:\n%s", notRecommendVersion1, risks, out))

			g.By("cv.Status.History should not change after accepting risks")
			diff := cmp.Diff(&cv.Status.History, &afterAcceptRiskCV.Status.History)
			o.Expect(diff).To(o.BeEmpty(), diff)

			upgradeComplete, err = WaitUpgradeComplete(oc, 5*time.Second, 5*time.Minute)
			o.Expect(err).NotTo(o.HaveOccurred())
			o.Expect(upgradeComplete).To(o.BeTrue())

			g.By("clear command should be able to clear the accepted risks")
			out, err = oc.Run("adm").Args("upgrade", "accept", "--clear").Output()
			o.Expect(err).NotTo(o.HaveOccurred())

			afterClearRiskCV, err := configClient.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())
			o.Expect(len(afterClearRiskCV.Spec.DesiredUpdate.AcceptRisks)).To(o.Equal(0))

			g.By("non-recommend version should be removed from oc adm upgrade after clearing the risks")
			err = wait.Poll(20*time.Second, 10*time.Minute, func() (bool, error) {
				out, err = oc.Run("adm").Args("upgrade").Output()
				if err != nil {
					return false, err
				}
				if strings.Contains(out, notRecommendVersion1) {
					return false, nil
				}
				return true, nil
			})
			AssertWaitPollNoErr(err, fmt.Sprintf("the first non-recommend version %s still exists after clearing the risks. \nOutput:\n%s", notRecommendVersion1, out))

			upgradeComplete, err = WaitUpgradeComplete(oc, 10*time.Second, 5*time.Minute)
			o.Expect(err).NotTo(o.HaveOccurred())
			o.Expect(upgradeComplete).To(o.BeTrue())

			g.By("cv.Status.History should not change after clearing the accepted risk")
			diff = cmp.Diff(&cv.Status.History, &afterClearRiskCV.Status.History)
			o.Expect(diff).To(o.BeEmpty(), diff)

			g.By("Accept risk SomeInvokerThing,SomeChannelThing")
			risks = "SomeInvokerThing,SomeChannelThing"
			out, err = oc.Run("adm").Args("upgrade", "accept", risks).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			afterAcceptTwoRisksCV, err := configClient.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())
			o.Expect(len(afterAcceptTwoRisksCV.Spec.DesiredUpdate.AcceptRisks)).To(o.Equal(2))
			o.Expect(risks).To(o.ContainSubstring(afterAcceptTwoRisksCV.Spec.DesiredUpdate.AcceptRisks[0].Name))
			o.Expect(risks).To(o.ContainSubstring(afterAcceptTwoRisksCV.Spec.DesiredUpdate.AcceptRisks[1].Name))
			o.Expect(afterAcceptTwoRisksCV.Spec.DesiredUpdate.AcceptRisks[0].Name).NotTo(o.Equal(afterAcceptTwoRisksCV.Spec.DesiredUpdate.AcceptRisks[1].Name))

			err = wait.Poll(20*time.Second, 10*time.Minute, func() (bool, error) {
				out, err := oc.Run("adm").Args("upgrade").Output()
				if err != nil {
					return false, err
				}
				if !strings.Contains(out, notRecommendVersion1) {
					return false, nil
				}
				if !strings.Contains(out, notRecommendVersion2) {
					return false, nil
				}
				return true, nil
			})
			AssertWaitPollNoErr(err, fmt.Sprintf("the non-recommend version %s and %s are not available after accepting the risk %s. \nOutput:\n%s", notRecommendVersion1, notRecommendVersion2, risks, out))

			g.By("Replace accept risks by SomeInfrastructureThing- will cause error because the suffix '-' is not allowed when --replace is specified")
			risks = "SomeInfrastructureThing-"
			out, err = oc.Run("adm").Args("upgrade", "accept", "--replace", risks).Output()
			o.Expect(err).To(o.HaveOccurred())
			o.Expect(out).To(o.ContainSubstring("error: The suffix '-' on risks is not allowed if --replace is specified"))

			g.By("Replace nothing should cause error because no positional argument is given")
			out, err = oc.Run("adm").Args("upgrade", "accept", "--replace").Output()
			o.Expect(err).To(o.HaveOccurred())
			o.Expect(out).To(o.ContainSubstring("error: no positional arguments given"))

			g.By("Replace accepted risks by SomeInfrastructureThing should work")
			risks = "SomeInfrastructureThing"
			out, err = oc.Run("adm").Args("upgrade", "accept", "--replace", risks).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			afterReplaceRisksCV, err := configClient.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())
			o.Expect(len(afterReplaceRisksCV.Spec.DesiredUpdate.AcceptRisks)).To(o.Equal(1))
			o.Expect(risks).To(o.Equal(afterReplaceRisksCV.Spec.DesiredUpdate.AcceptRisks[0].Name))

			err = wait.Poll(20*time.Second, 10*time.Minute, func() (bool, error) {
				out, err = oc.Run("adm").Args("upgrade").Output()
				if err != nil {
					return false, err
				}
				if strings.Contains(out, notRecommendVersion1) {
					return false, nil
				}
				if strings.Contains(out, notRecommendVersion2) {
					return false, nil
				}
				if !strings.Contains(out, notRecommendVersion3) {
					return false, nil
				}
				return true, nil
			})
			AssertWaitPollNoErr(err, fmt.Sprintf("the non-recommend versions are not correct after replace the risk %s. \nOutput:\n%s", risks, out))

			upgradeComplete, err = WaitUpgradeComplete(oc, 10*time.Second, 5*time.Minute)
			o.Expect(err).NotTo(o.HaveOccurred())
			o.Expect(upgradeComplete).To(o.BeTrue())

			g.By("cv.Status.History should not change after replacing the accepted risk")
			diff = cmp.Diff(&cv.Status.History, &afterReplaceRisksCV.Status.History)
			o.Expect(diff).To(o.BeEmpty(), diff)

			g.By("Upgrade to a version which risks are accepted should work")
			out, err = oc.Run("adm").Args("upgrade", "--to", notRecommendVersion3).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			complete, err := WaitUpgradeComplete(oc, 30*time.Second, 90*time.Minute)
			o.Expect(err).NotTo(o.HaveOccurred())
			o.Expect(complete).To(o.BeTrue(), fmt.Sprintf("upgrade to version %s is not complete. \nOutput:\n%s", notRecommendVersion3, out))
			afterUpgradeCV, err := configClient.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())
			version, _ := GetCurrentVersionAndImage(afterUpgradeCV.Status.History)
			o.Expect(version).To(o.Equal(notRecommendVersion3))

			g.By("Spec.DesiredUpdate.AcceptRisks should not change after upgrade")
			diff = cmp.Diff(&afterReplaceRisksCV.Spec.DesiredUpdate.AcceptRisks, &afterUpgradeCV.Spec.DesiredUpdate.AcceptRisks)
			o.Expect(diff).To(o.BeEmpty(), fmt.Sprintf("the accepted risks after upgrade is different from before upgrade. \nDiff:\n%s", diff))
			g.By("The accept risks should displayed in history")
			acceptRisks := afterUpgradeCV.Status.History[0].AcceptedRisks
			o.Expect(acceptRisks).To(o.ContainSubstring(risks))

			defer oc.Run("adm").Args("upgrade", "accept", "--clear").Output()
		})
	})
})
