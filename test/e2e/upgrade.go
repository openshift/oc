package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/wait"
)

var _ = g.Describe(`[Jira:"oc adm upgrade"] accept`, func() {

	defer g.GinkgoRecover()

	var (
		oc            = NewCLI("oc", KubeConfigPath())
		testGraphHost = "https://fauxinnati-fauxinnati.apps.ota-stage.q2z4.p1.openshiftapps.com/"
	)

	g.BeforeEach(func() {
	})

	g.AfterEach(func() {
	})

	g.It("Accepted Risks for OCP Cluster Updates", g.Label("TechPreview", "88175", "Slow", "Manual"), func() {
		if !isTechPreview(oc) {
			g.Skip("Skipping test: only tech-preview clusters supported")
		}
		os.Setenv("OC_ENABLE_CMD_UPGRADE_ACCEPT_RISKS", "true")
		defer os.Unsetenv("OC_ENABLE_CMD_UPGRADE_ACCEPT_RISKS")

		g.By("remove overrides from clusterversion version if exists")
		overrides, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("clusterversion", "version", "-o", "jsonpath={.spec.overrides}").Output()
		if err == nil && strings.Contains(overrides, "ClusterImagePolicy") {
			e2e.Logf("overrides: <<%s>>", overrides)
			var data []map[string]interface{}
			err := json.Unmarshal([]byte(overrides), &data)
			o.Expect(err).NotTo(o.HaveOccurred())
			_, err = oc.Patch("", "clusterversion/version", []JSONPatchOperation{
				{"remove", "/spec/overrides", nil},
			})
			o.Expect(err).NotTo(o.HaveOccurred())
			defer oc.Patch("", "clusterversion/version", []JSONPatchOperation{
				{"add", "/spec/overrides", data},
			})
		}

		clusterVersion, err := GetVersion(oc)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("checking the help info of `oc adm upgrade --help`")
		out, err := oc.Run("adm").Args("upgrade", "--help").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("Accept risks exposed to conditional updates."))

		g.By("checking the help info of `oc adm upgrade accept --help`")
		out, err = oc.Run("adm").Args("upgrade", "accept", "--help").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("Manage update risk acceptance."))

		g.By("patch fauxinnati upstream")
		oldUpstream, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("clusterversion", "version", "-ojsonpath={.spec.upstream}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		oldChannel, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("clusterversion", "version", "-ojsonpath={.spec.channel}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		graph := fmt.Sprintf("%sapi/upgrades_info/graph", testGraphHost)
		newChannel := "OCP-88175"
		_, err = oc.Patch("", "clusterversion/version", []JSONPatchOperation{
			{"add", "/spec/upstream", graph},
			{"add", "/spec/channel", newChannel},
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer oc.Patch("", "clusterversion/version", []JSONPatchOperation{
			{"add", "/spec/upstream", oldUpstream},
			{"add", "/spec/channel", oldChannel},
		})
		g.By("check if new upstream has enough target versions")
		fullGraph := fmt.Sprintf("%s?channel=%s&version=%s&arch=amd64", graph, newChannel, clusterVersion.FullVersion)
		graphData, err := GetWebResource(fullGraph)
		o.Expect(err).NotTo(o.HaveOccurred())
		var result map[string]interface{}
		err = json.Unmarshal([]byte(graphData), &result)
		o.Expect(err).NotTo(o.HaveOccurred())
		nodes, ok := result["nodes"].([]interface{})
		o.Expect(ok).To(o.BeTrue(), "get nodes failed")

		extractVersion := func(index int) string {
			node, ok := nodes[index].(map[string]interface{})
			o.Expect(ok).To(o.BeTrue(), "node %d is not a map", index)
			version, ok := node["version"].(string)
			o.Expect(ok).To(o.BeTrue(), "node %d missing version string", index)
			return version
		}
		recommendVersion := extractVersion(1)
		notRecommendVersion1 := extractVersion(2)
		notRecommendVersion2 := extractVersion(3)
		notRecommendVersion3 := extractVersion(4)

		g.By("check default output of `oc adm upgrade`")
		out, err = oc.Run("adm").Args("upgrade").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring(recommendVersion), fmt.Sprintf("recommend version is missing %s", recommendVersion))
		o.Expect(out).NotTo(o.ContainSubstring(notRecommendVersion1), fmt.Sprintf("non-recommend version is present %s", notRecommendVersion1))
		o.Expect(out).NotTo(o.ContainSubstring(notRecommendVersion2), fmt.Sprintf("non-recommend version is present %s", notRecommendVersion2))
		o.Expect(out).NotTo(o.ContainSubstring(notRecommendVersion3), fmt.Sprintf("non-recommend version is present %s", notRecommendVersion3))

		g.By("check output of `oc adm upgrade --include-not-recommended`")
		out, err = oc.Run("adm").Args("upgrade", "--include-not-recommended").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring(recommendVersion), fmt.Sprintf("recommend version is missing %s", recommendVersion))
		o.Expect(out).To(o.ContainSubstring(notRecommendVersion1), fmt.Sprintf("non-recommend version is missing %s", notRecommendVersion1))
		o.Expect(out).To(o.ContainSubstring(notRecommendVersion2), fmt.Sprintf("non-recommend version is missing %s", notRecommendVersion2))
		o.Expect(out).To(o.ContainSubstring(notRecommendVersion3), fmt.Sprintf("non-recommend version is missing %s", notRecommendVersion3))

		g.By("upgrade to a non recommend version")
		out, err = oc.Run("adm").Args("upgrade", "--to", notRecommendVersion1).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring(fmt.Sprintf("the update %s is not one of the recommended updates, but is available as a conditional update.", notRecommendVersion1)))

		defer oc.Run("adm").Args("upgrade", "accept", "--clear").Output()

		g.By("clear risks when the accept risk list is empty")
		out, err = oc.Run("adm").Args("upgrade", "accept", "--clear").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("info: Accept risks are not changed"))

		g.By("Accept risk SomeInvokerThing")
		risks := "SomeInvokerThing"
		out, err = oc.Run("adm").Args("upgrade", "accept", risks).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring(fmt.Sprintf("info: Accept risks are [%s]", risks)))
		out, err = oc.Run("get").Args("clusterversion", "version", "-ojsonpath={.spec.desiredUpdate.acceptRisks}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring(risks))
		err = wait.Poll(20*time.Second, 10*time.Minute, func() (bool, error) {
			out, err = oc.Run("adm").Args("upgrade").Output()
			if err != nil {
				e2e.Logf("get oc adm upgrade output failed. Trying again")
				return false, nil
			}
			if !strings.Contains(out, notRecommendVersion1) {
				e2e.Logf("non-recommend version %s is not available. Trying again", notRecommendVersion1)
				return false, nil
			}
			return true, nil
		})
		AssertWaitPollNoErr(err, fmt.Sprintf("the non-recommend version %s is not available after accepting the risk %s. \nOutput:\n%s", notRecommendVersion1, risks, out))

		g.By("clear risk again")
		out, err = oc.Run("adm").Args("upgrade", "accept", "--clear").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("info: Accept risks are []"))

		err = wait.Poll(20*time.Second, 10*time.Minute, func() (bool, error) {
			out, err = oc.Run("adm").Args("upgrade").Output()
			if err != nil {
				e2e.Logf("get oc adm upgrade output failed. Trying again")
				return false, nil
			}
			if strings.Contains(out, notRecommendVersion1) {
				e2e.Logf("non-recommend version %s still exists. Trying again", notRecommendVersion1)
				return false, nil
			}
			return true, nil
		})
		AssertWaitPollNoErr(err, fmt.Sprintf("the first non-recommend version %s still exists after clearing the risks. \nOutput:\n%s", notRecommendVersion1, out))

		g.By("Accept risk SomeInvokerThing,SomeChannelThing")
		risks = "SomeInvokerThing,SomeChannelThing"
		out, err = oc.Run("adm").Args("upgrade", "accept", risks).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("SomeInvokerThing"))
		o.Expect(out).To(o.ContainSubstring("SomeChannelThing"))
		out, err = oc.Run("get").Args("clusterversion", "version", "-ojsonpath={.spec.desiredUpdate.acceptRisks}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("SomeInvokerThing"))
		o.Expect(out).To(o.ContainSubstring("SomeChannelThing"))
		err = wait.Poll(20*time.Second, 10*time.Minute, func() (bool, error) {
			out, err = oc.Run("adm").Args("upgrade").Output()
			if err != nil {
				e2e.Logf("get oc adm upgrade output failed. Trying again")
				return false, nil
			}
			if !strings.Contains(out, notRecommendVersion1) {
				e2e.Logf("non-recommend version %s is not available. Trying again", notRecommendVersion1)
				return false, nil
			}
			if !strings.Contains(out, notRecommendVersion2) {
				e2e.Logf("non-recommend version %s is not available. Trying again", notRecommendVersion2)
				return false, nil
			}
			return true, nil
		})
		AssertWaitPollNoErr(err, fmt.Sprintf("the non-recommend version %s and %s are not available after accepting the risk %s. \nOutput:\n%s", notRecommendVersion1, notRecommendVersion2, risks, out))

		g.By("Replace accept risks by SomeInfrastructureThing-")
		risks = "SomeInfrastructureThing-"
		out, err = oc.Run("adm").Args("upgrade", "accept", "--replace", risks).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("error: The suffix '-' on risks is not allowed if --replace is specified"))

		g.By("Replace nothing")
		out, err = oc.Run("adm").Args("upgrade", "accept", "--replace").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("error: no positional arguments given"))

		g.By("Replace accept risks by SomeInfrastructureThing")
		risks = "SomeInfrastructureThing"
		out, err = oc.Run("adm").Args("upgrade", "accept", "--replace", risks).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring(fmt.Sprintf("info: Accept risks are [%s]", risks)))
		out, err = oc.Run("get").Args("clusterversion", "version", "-ojsonpath={.spec.desiredUpdate.acceptRisks}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring(risks))
		o.Expect(out).NotTo(o.ContainSubstring("SomeChannelThing"))
		o.Expect(out).NotTo(o.ContainSubstring("SomeInvokerThing"))
		err = wait.Poll(20*time.Second, 10*time.Minute, func() (bool, error) {
			out, err = oc.Run("adm").Args("upgrade").Output()
			if err != nil {
				e2e.Logf("get oc adm upgrade output failed. Trying again")
				return false, nil
			}
			if strings.Contains(out, notRecommendVersion1) {
				e2e.Logf("non-recommend version %s still exists. Trying again", notRecommendVersion1)
				return false, nil
			}
			if strings.Contains(out, notRecommendVersion2) {
				e2e.Logf("non-recommend version %s still exists. Trying again", notRecommendVersion2)
				return false, nil
			}
			if !strings.Contains(out, notRecommendVersion3) {
				e2e.Logf("non-recommend version %s is not available. Trying again", notRecommendVersion3)
				return false, nil
			}
			return true, nil
		})
		AssertWaitPollNoErr(err, fmt.Sprintf("the non-recommend versions are not correct after replace the risk %s. \nOutput:\n%s", risks, out))

		g.By("Upgrade to not recommend version")
		// out, err = oc.Run("adm").Args("upgrade", "--to", notRecommendVersion3).Output()
		// o.Expect(err).NotTo(o.HaveOccurred())
		// expectedStatus := fmt.Sprintf("Cluster version is %s", notRecommendVersion3)
		// err = wait.Poll(30*time.Second, 90*time.Minute, func() (bool, error) {
		// 	out, _ = oc.Run("get").Args("clusterversion").Output()
		// 	if !strings.Contains(out, expectedStatus) {
		// 		return false, nil
		// 	}
		// 	return true, nil
		// })
		// AssertWaitPollNoErr(err, fmt.Sprintf("upgrade to version %s failed", notRecommendVersion1))
	})
})
