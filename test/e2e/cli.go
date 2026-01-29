package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/wait"
)

var _ = g.Describe("[sig-cli] Workloads test oc works well", func() {
	defer g.GinkgoRecover()

	var (
		oc = NewCLI("oc", KubeConfigPath())
	)

	// author: knarra@redhat.com
	g.It("ROSA-OSD_CCS-ARO-Author:knarra-Medium-48681-Could start debug pod using pod definition yaml", func() {
		buildPruningBaseDir := FixturePath("testdata", "oc_cli")
		debugPodUsingDefinitionT := filepath.Join(buildPruningBaseDir, "debugpod_48681.yaml")

		g.By("create new namespace")
		oc.SetupProject()
		g.By("Get the cli image from openshift")
		cliImage := getCliImage(oc)

		pod48681 := debugPodUsingDefinition{
			name:       "pod48681",
			namespace:  oc.Namespace(),
			cliImageID: cliImage,
			template:   debugPodUsingDefinitionT,
		}

		g.By("Create test pod and verify oc debug works with pod definition yaml")
		// createDebugPodUsingDefinition verifies:
		// 1. Pod is created successfully from template
		// 2. Pod reaches Running state within 1 minute
		// 3. oc debug -f command executes successfully
		// 4. Debug output contains "Starting pod/pod48681-debug" (not image debug container)
		pod48681.createDebugPodUsingDefinition(oc)
		defer oc.WithoutNamespace().Run("delete").Args("pod/pod48681", "-n", oc.Namespace()).Execute()
	})

	// author: yinzhou@redhat.com
	g.It("Author:yinzhou-NonPreRelease-Longduration-High-45307-Critical-45327-check oc adm prune deployments to prune RS [Serial][Timeout:30m]", func() {
		g.By("create new namespace")
		oc.SetupProject()

		g.By("Create deployments and trigger more times")
		createDeployment(oc, oc.Namespace(), "mydep45307")
		triggerSucceedDeployment(oc, oc.Namespace(), "mydep45307", 6, 20)
		triggerFailedDeployment(oc, oc.Namespace(), "mydep45307")

		g.By("get the completed rs infomation")
		totalCompletedRsList, totalCompletedRsListNum := getCompeletedRsInfo(oc, oc.Namespace(), "mydep45307")

		g.By("Dry run the prune deployments for RS")
		keepCompletedRsNum := 3
		pruneRsNumCMD := fmt.Sprintf("oc adm prune deployments --keep-complete=%v --keep-younger-than=10s --replica-sets=true  |grep %s |wc -l", keepCompletedRsNum, oc.Namespace())
		pruneRsDryCMD := fmt.Sprintf("oc adm prune deployments --keep-complete=%v --keep-younger-than=10s --replica-sets=true  |grep %s|awk '{print $2}'", keepCompletedRsNum, oc.Namespace())
		rsListFromPrune := getShouldPruneRSFromPrune(oc, pruneRsNumCMD, pruneRsDryCMD, (totalCompletedRsListNum - keepCompletedRsNum))
		shouldPruneRsList := getShouldPruneRSFromCreateTime(totalCompletedRsList, totalCompletedRsListNum, keepCompletedRsNum)
		if comparePrunedRS(shouldPruneRsList, rsListFromPrune) {
			e2e.Logf("Checked the pruned rs is expected")
		} else {
			e2e.Failf("Pruned the wrong RS with dry run")
		}

		g.By("Make sure never prune RS with replicas num >0")
		//before prune ,check the running rs list
		runningRsList := checkRunningRsList(oc, oc.Namespace(), "mydep45307")

		//checking the should prune rs list
		completedRsNum := 0
		pruneRsNumCMD = fmt.Sprintf("oc adm prune deployments --keep-complete=%v --keep-younger-than=10s --replica-sets=true  |grep %s |wc -l", completedRsNum, oc.Namespace())
		pruneRsDryCMD = fmt.Sprintf("oc adm prune deployments --keep-complete=%v --keep-younger-than=10s --replica-sets=true  |grep %s|awk '{print $2}'", completedRsNum, oc.Namespace())

		rsListFromPrune = getShouldPruneRSFromPrune(oc, pruneRsNumCMD, pruneRsDryCMD, (totalCompletedRsListNum - completedRsNum))
		shouldPruneRsList = getShouldPruneRSFromCreateTime(totalCompletedRsList, totalCompletedRsListNum, completedRsNum)
		if comparePrunedRS(shouldPruneRsList, rsListFromPrune) {
			e2e.Logf("dry run prune all completed rs is expected")
		} else {
			e2e.Failf("Pruned the wrong RS with dry run")
		}

		//prune all the completed rs list
		pruneCompletedRs(oc, "prune", "deployments", "--keep-complete=0", "--keep-younger-than=10s", "--replica-sets=true", "--confirm")

		//after prune , check the remaining rs list
		remainingRsList := getRemainingRs(oc, oc.Namespace(), "mydep45307")
		if comparePrunedRS(runningRsList, remainingRsList) {
			e2e.Logf("pruned all completed rs is expected")
		} else {
			e2e.Failf("Pruned the wrong")
		}
	})

	// author: yinzhou@redhat.com
	g.It("ROSA-OSD_CCS-ARO-Author:yinzhou-Medium-49859-should failed when oc import-image setting with Garbage values for --reference-policy", func() {
		g.By("create new namespace")
		oc.SetupProject()

		g.By("import image with garbage values set for reference-policy")
		out, err := oc.Run("import-image").Args("registry.redhat.io/openshift3/jenkins-2-rhel7", "--reference-policy=sdfsdfds", "--confirm").Output()
		o.Expect(err).Should(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("reference policy values are source or local"))

		g.By("check should no imagestream created")
		out, err = oc.Run("get").Args("is").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("No resources found"))
	})

	// author: yinzhou@redhat.com
	g.It("ROSA-OSD_CCS-ARO-NonPreRelease-Longduration-Author:yinzhou-High-42982-Describe quota output should always show units [Timeout:30m]", func() {
		if isBaselineCapsSet(oc, "None") || isBaselineCapsSet(oc, "v4.13") || isBaselineCapsSet(oc, "v4.12") || isBaselineCapsSet(oc, "v4.14") || isBaselineCapsSet(oc, "v4.15") || isBaselineCapsSet(oc, "v4.11") && !isEnabledCapability(oc, "DeploymentConfig") {
			skipMsg := "Skipping the test as baselinecaps have been set to None and some of API capabilities are not enabled!"
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}

		// Skip Hypershift external OIDC clusters against which all test cases run as the same (external) user
		isExternalOIDCCluster, err := IsExternalOIDCCluster(oc)
		o.Expect(err).NotTo(o.HaveOccurred())
		if isExternalOIDCCluster {
			skipMsg := "Skipping the test as we are running against a Hypershift external OIDC cluster"
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}

		buildPruningBaseDir := FixturePath("testdata", "oc_cli")
		deploymentconfigF := filepath.Join(buildPruningBaseDir, "deploymentconfig_with_quota.yaml")
		clusterresourceF := filepath.Join(buildPruningBaseDir, "clusterresource_for_user.yaml")
		g.By("create new namespace")
		oc.SetupProject()
		err = oc.AsAdmin().Run("create").Args("quota", "compute-resources-42982", "--hard=requests.cpu=4,requests.memory=8Gi,pods=4,limits.cpu=4,limits.memory=8Gi").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.Run("create").Args("-f", deploymentconfigF).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		//wait for pod running
		checkPodStatus(oc, "deploymentconfig=hello-openshift", oc.Namespace(), "Running")
		checkPodStatus(oc, "openshift.io/deployer-pod-for.name=hello-openshift-1", oc.Namespace(), "Succeeded")
		output, err := oc.Run("describe").Args("quota", "compute-resources-42982").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if matched, _ := regexp.MatchString("requests.memory.*Ki.*8Gi", output); matched {
			e2e.Logf("describe the quota with units:\n%s", output)
		}

		//check for clusterresourcequota
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("clusterresourcequota", "for-user42982").Execute()
		userName, err := oc.Run("whoami").Args("").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", clusterresourceF).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("current user name is %v", userName)
		patchPath := fmt.Sprintf("-p=[{\"op\": \"replace\", \"path\": \"/spec/selector/annotations\", \"value\":{ \"openshift.io/requester\": \"%s\" }}]", userName)
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("clusterresourcequota", "for-user42982", "--type=json", patchPath).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.WithoutNamespace().Run("new-project").Args("p42982-1").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("project", "p42982-1").Execute()
		err = oc.WithoutNamespace().Run("create").Args("-f", deploymentconfigF, "-n", "p42982-1").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		//wait for pod running
		checkPodStatus(oc, "deploymentconfig=hello-openshift", "p42982-1", "Running")
		checkPodStatus(oc, "openshift.io/deployer-pod-for.name=hello-openshift-1", "p42982-1", "Succeeded")
		output, err = oc.AsAdmin().WithoutNamespace().Run("describe").Args("clusterresourcequota", "for-user42982").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("clusterresourcequota output:\n%s", output)
		// Check if quota shows resources with correct units (may not show Used if no consumption yet)
		if matched, _ := regexp.MatchString("requests\\.memory.*8Gi", output); !matched {
			e2e.Logf("Warning: clusterresourcequota output did not show expected memory quota format")
		}

	})

	// author: yinzhou@redhat.com
	g.It("Author:yinzhou-High-38178-oc should be able to debug init container", func() {
		oc.SetupProject()
		podBaseDir := FixturePath("testdata", "oc_cli")
		initPodFile := filepath.Join(podBaseDir, "initContainer.yaml")

		SetNamespacePrivileged(oc, oc.Namespace())
		g.By("Create pod with init container")
		err := oc.Run("create").Args("-f", initPodFile).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		g.By("Make sure pod with init container running well")
		checkPodStatus(oc, "name=hello-pod", oc.Namespace(), "Running")
		g.By("Run debug command with init container")
		err = wait.Poll(10*time.Second, 100*time.Second, func() (bool, error) {
			output, err := oc.Run("debug").Args("pod/hello-pod", "-c", "wait").Output()
			if err != nil {
				e2e.Logf("debug failed with error: %s. Trying again", err)
				return false, nil
			}
			if matched, _ := regexp.MatchString("sleep", output); matched {
				e2e.Logf("Check the debug pod with init container command succeeded\n")
				return true, nil
			}
			return false, nil
		})
		AssertWaitPollNoErr(err, fmt.Sprintf("Cannot get debug with init container"))
	})

	// author: yinzhou@redhat.com
	g.It("ROSA-OSD_CCS-ARO-ConnectedOnly-Author:yinzhou-Medium-51018-oc adm release extract support manifest list", func() {
		skipIfDisconnected(oc)

		if !assertPullSecret(oc) {
			skipMsg := "The cluster does not have pull secret for public registry hence skipping..."
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}
		extractTmpDirName := "/tmp/case51018"
		err := os.MkdirAll(extractTmpDirName, 0700)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer os.RemoveAll(extractTmpDirName)

		_, err = oc.AsAdmin().WithoutNamespace().Run("extract").Args("secret/pull-secret", "-n", "openshift-config", fmt.Sprintf("--to=%s", extractTmpDirName), "--confirm").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		pullSpec := getLatestPayload("https://multi.ocp.releases.ci.openshift.org/api/v1/releasestream/4-stable-multi/latest")
		e2e.Logf("The pullSpec is %s \n", pullSpec)
		if len(pullSpec) == 0 || strings.TrimSpace(pullSpec) == "" {
			skipMsg := "pullSpec is empty, so skipping the test"
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}
		err = oc.WithoutNamespace().WithoutKubeconf().Run("adm").Args("release", "extract", "-a", extractTmpDirName+"/.dockerconfigjson", "--command=oc.rhel8", "--to="+extractTmpDirName, pullSpec).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("Check oc executable to make sure match the platform")
		_, err = exec.Command("bash", "-c", "/tmp/case51018/oc version").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.WithoutNamespace().WithoutKubeconf().Run("adm").Args("release", "extract", "-a", extractTmpDirName+"/.dockerconfigjson", "--command=oc", "--to="+extractTmpDirName+"/mac", pullSpec, "--command-os=mac/amd64").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		macocheckcmd := "file /tmp/case51018/mac/oc"
		output, err := exec.Command("bash", "-c", macocheckcmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Mach-O"))
		err = oc.WithoutNamespace().WithoutKubeconf().Run("adm").Args("release", "extract", "-a", extractTmpDirName+"/.dockerconfigjson", "--command=oc", "--to="+extractTmpDirName+"/macarm", pullSpec, "--command-os=mac/arm64").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		macocheckcmd = "file /tmp/case51018/macarm/oc"
		output, err = exec.Command("bash", "-c", macocheckcmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Mach-O 64-bit arm64 executable"))
		err = oc.WithoutNamespace().WithoutKubeconf().Run("adm").Args("release", "extract", "-a", extractTmpDirName+"/.dockerconfigjson", "--command=oc", "--to="+extractTmpDirName+"/windows", pullSpec, "--command-os=windows").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		windowocheckcmd := "file /tmp/case51018/windows/oc"
		output, err = exec.Command("bash", "-c", windowocheckcmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Windows"))
	})

	// author: knarra@redhat.com
	g.It("ROSA-OSD_CCS-ARO-ConnectedOnly-Author:knarra-Medium-66989-Workloads oc debug with or without init container for pod", func() {
		skipIfDisconnected(oc)

		oc.SetupProject()
		testBaseDir := FixturePath("testdata", "oc_cli")
		initContainerFile := filepath.Join(testBaseDir, "initContainer66989.yaml")
		SetNamespacePrivileged(oc, oc.Namespace())
		g.By("Create pod with InitContainer")
		err := oc.Run("create").Args("-f", initContainerFile).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		g.By("Make sure pod with init container running well")
		checkPodStatus(oc, "name=hello-pod", oc.Namespace(), "Running")
		g.By("Run debug command with init container")
		cmd, _, _, err := oc.Run("debug").Args("pod/hello-pod", "--keep-init-containers=true").Background()
		defer cmd.Process.Kill()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = wait.Poll(5*time.Second, 100*time.Second, func() (bool, error) {
			debugPodName, err := oc.WithoutNamespace().Run("get").Args("pods", "-n", oc.Namespace()).Output()
			if err != nil {
				e2e.Logf("debug failed with error: %s. Trying again", err)
				return false, nil
			}
			if matched, _ := regexp.MatchString("hello-pod-debug", debugPodName); matched {
				e2e.Logf("Check the debug pod command succeeded\n")
				return true, nil
			}
			return false, nil
		})
		AssertWaitPollNoErr(err, fmt.Sprintf("Cannot get debug with init container"))

		g.By("Check if Init Containers present in debug pod output")
		debugPodName, err := oc.WithoutNamespace().Run("get").Args("pods", "-n", oc.Namespace(), "-o=jsonpath={.items[1].metadata.name}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		Output, err := oc.WithoutNamespace().Run("describe").Args("pods", debugPodName, "-n", oc.Namespace()).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		if matched, _ := regexp.MatchString("Init Containers", Output); !matched {
			e2e.Failf("Init Containers are not seen in the output when run with keep init containers true")
		}
		_, err = oc.WithoutNamespace().Run("delete").Args("pods", debugPodName, "-n", oc.Namespace(), "--wait=false").Output()
		o.Expect(err).NotTo(o.HaveOccurred())

	})

	// author: yinzhou@redhat.com
	g.It("ROSA-OSD_CCS-ARO-Author:yinzhou-LEVEL0-Critical-63002-oc new-app propagate containerPort information to the deployment if import-mode is PreserveOriginal", func() {
		g.By("create new namespace")
		oc.SetupProject()
		g.By("create new-app with import-mode as PreserveOrigin")
		err := oc.WithoutNamespace().Run("new-app").Args("quay.io/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83", "-n", oc.Namespace(), "--name=example-preserveoriginal", "--import-mode=PreserveOriginal").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		out, err := oc.WithoutNamespace().Run("get").Args("svc", "-n", oc.Namespace()).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(out, "example-preserveoriginal")).To(o.BeTrue())
	})

	// author: knarra@redhat.com
	g.It("ROSA-OSD_CCS-ARO-Author:knarra-LEVEL0-Critical-64921-Critical-63854-Verify oc adm release info and oc image extract using --idms-file flag", func() {
		skipIfDisconnected(oc)

		buildPruningBaseDir := FixturePath("testdata", "oc_cli")
		idmsFile64921 := filepath.Join(buildPruningBaseDir, "idmsFile64921.yaml")
		var (
			image string
		)

		g.By("Get desired image from ocp cluster")
		pullSpec, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("clusterversion", "-o", "jsonpath={..desired.image}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(pullSpec).NotTo(o.BeEmpty())
		e2e.Logf("pullspec is %v", pullSpec)

		g.By("Check if imageContentSourcePolicy image-policy-aosqe exists, if not skip the case")
		existingIcspOutput, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("ImageContentSourcePolicy", "--ignore-not-found").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if !(strings.Contains(existingIcspOutput, "image-policy-aosqe")) {
			skipMsg := "Image-policy-aosqe icsp not found, skipping the case"
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}

		// Retreive image registry name
		imageRegistryName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("ImageContentSourcePolicy", "image-policy-aosqe", "-o=jsonpath={.spec.repositoryDigestMirrors[0].mirrors[0]}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		imageRegistryName = strings.Split(imageRegistryName, ":")[0]
		e2e.Logf("ImageRegistryName is %s", imageRegistryName)

		// Replace localhost with retreived registry name from the cluster in idms file
		sedCmd := fmt.Sprintf(`sed -i 's/localhost/%s/g' %s`, imageRegistryName, idmsFile64921)
		_, err = exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		// Replace target correctly in the icsp file
		sedCmdOne := fmt.Sprintf(`sed -i 's/target/%s/g' %s`, strings.Split(pullSpec, "/")[1], idmsFile64921)
		_, err = exec.Command("bash", "-c", sedCmdOne).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		// Extract secret and store it
		extractTmpDirName := "/tmp/case64921"
		err = os.MkdirAll(extractTmpDirName, 0700)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer os.RemoveAll(extractTmpDirName)
		_, err = oc.AsAdmin().WithoutNamespace().Run("extract").Args("secret/pull-secret", "-n", "openshift-config", fmt.Sprintf("--to=%s", extractTmpDirName), "--confirm").Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		// Retreive image digest
		imageDigest := strings.Split(pullSpec, "@")[1]
		e2e.Logf("imageDigest is %s", imageDigest)

		// Remove auth & run command oc adm release info with out --idms-flag
		dockerTmpDirName := "/tmp/case64921/.dockerconfigjson"
		authContent, readErr := os.ReadFile(dockerTmpDirName)
		o.Expect(readErr).NotTo(o.HaveOccurred())

		// Parse auth JSON and remove specific auth entry
		var authData map[string]interface{}
		err = json.Unmarshal(authContent, &authData)
		o.Expect(err).NotTo(o.HaveOccurred())
		auths, _ := authData["auths"].(map[string]interface{})

		if strings.Contains(pullSpec, "quay.io") {
			image = "quay.io/openshift-release-dev/ocp-v4.0-art-dev@" + imageDigest
			delete(auths, "quay.io")
		} else if strings.Contains(pullSpec, "registry.ci.openshift.org") {
			image = "registry.ci.openshift.org/ocp/release@" + imageDigest
			delete(auths, "registry.ci.openshift.org")
		} else {
			sourceImage := strings.Split(pullSpec, "/")[0]
			image = pullSpec
			delete(auths, sourceImage)
		}

		authContentBytes, err := json.Marshal(authData)
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(os.WriteFile(dockerTmpDirName, authContentBytes, 0640)).NotTo(o.HaveOccurred())

		// Run command oc adm release info with --idms-flag
		o.Expect(oc.WithoutNamespace().WithoutKubeconf().Run("adm").Args("release", "info", image, "-a", dockerTmpDirName, "--idms-file="+idmsFile64921).Execute()).NotTo(o.HaveOccurred())

		// Run command oc adm release info to get oc-mirror image
		ocMirrorImage, _, err := oc.WithoutNamespace().WithoutKubeconf().Run("adm").Args("release", "info", image, "-a", dockerTmpDirName, "--idms-file="+idmsFile64921, `-ojsonpath={.references.spec.tags[?(@.name=="oc-mirror")].from.name}`).Outputs()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("ocMirrorImage is %s", ocMirrorImage)

		// Run command oc image extract with --idms-flag
		o.Expect(oc.WithoutNamespace().WithoutKubeconf().Run("image").Args("extract", "-a", dockerTmpDirName, ocMirrorImage, "--path=/usr/bin/oc-mirror:"+extractTmpDirName, "--idms-file="+idmsFile64921, "--insecure", "--confirm").Execute()).NotTo(o.HaveOccurred())

		// Verify oc-mirror is present
		output, err := exec.Command("bash", "-c", "stat "+extractTmpDirName+"/oc-mirror").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(string(output), "File: /tmp/case64921/oc-mirror")).To(o.BeTrue())
	})

	// author: yinzhou@redhat.com
	g.It("ROSA-OSD_CCS-ARO-ConnectedOnly-Author:yinzhou-High-67013-oc image mirror with multi-arch images and --filter-by-os", func() {
		skipIfDisconnected(oc)

		if !assertPullSecret(oc) {
			skipMsg := "The cluster does not have pull secret for public registry hence skipping..."
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}
		g.By("Skip if the cluster is AzureStackCloud")
		azureStackCloud, azureErr := oc.AsAdmin().WithoutNamespace().Run("get").Args("infrastructure", "cluster", "-o=jsonpath={.status.platformStatus.azure.cloudName}").Output()
		o.Expect(azureErr).NotTo(o.HaveOccurred())
		if azureStackCloud == "AzureStackCloud" {
			skipMsg := "Skip for cluster with AzureStackCloud!"
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}
		g.By("create new namespace")
		oc.SetupProject()
		registry := registry{
			dockerImage: "quay.io/openshifttest/registry@sha256:1106aedc1b2e386520bc2fb797d9a7af47d651db31d8e7ab472f2352da37d1b3",
			namespace:   oc.Namespace(),
		}

		g.By("Trying to launch a registry app")
		defer registry.deleteregistry(oc)
		serInfo := registry.createregistry(oc)

		err := wait.Poll(30*time.Second, 180*time.Second, func() (bool, error) {
			err := oc.WithoutNamespace().Run("image").Args("mirror", "quay.io/openshifttest/base-alpine@sha256:3126e4eed4a3ebd8bf972b2453fa838200988ee07c01b2251e3ea47e4b1f245c"+"="+serInfo.serviceName+"/testimage:ppc64", "--filter-by-os=linux/ppc64le", "--insecure").Execute()
			if err != nil {
				e2e.Logf("mirror failed, retrying...")
				return false, nil
			}
			return true, nil
		})
		AssertWaitPollNoErr(err, fmt.Sprintf("max time reached but mirror still falied"))
		out, err := oc.WithoutNamespace().Run("image").Args("info", serInfo.serviceName+"/testimage:ppc64", "--insecure").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(out, "ppc64le")).To(o.BeTrue())
		err = wait.Poll(30*time.Second, 180*time.Second, func() (bool, error) {
			err := oc.WithoutNamespace().Run("image").Args("mirror", "quay.io/openshifttest/base-alpine@sha256:3126e4eed4a3ebd8bf972b2453fa838200988ee07c01b2251e3ea47e4b1f245c"+"="+serInfo.serviceName+"/testimage:default", "--insecure").Execute()
			if err != nil {
				e2e.Logf("mirror failed, retrying...")
				return false, nil
			}
			return true, nil
		})
		AssertWaitPollNoErr(err, fmt.Sprintf("max time reached but mirror still falied"))
		o.Expect(err).NotTo(o.HaveOccurred())
		imageInfo, err := oc.WithoutNamespace().Run("image").Args("info", serInfo.serviceName+"/testimage:default", "--insecure").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		architecture, err := exec.Command("bash", "-c", "uname -a").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		architectureStr := string(architecture)
		if o.Expect(strings.Contains(architectureStr, "x86_64")).To(o.BeTrue()) {
			if o.Expect(strings.Contains(imageInfo, "amd64")).To(o.BeTrue()) {
				e2e.Logf("Found the expected Arch amd64")
			} else {
				e2e.Failf("Failed to find the expected Arch for mirrored image")
			}
		} else if o.Expect(strings.Contains(architectureStr, "aarch64")).To(o.BeTrue()) {
			if o.Expect(strings.Contains(imageInfo, "arm64")).To(o.BeTrue()) {
				e2e.Logf("Found the expected Arch aarch64")
			} else {
				e2e.Failf("Failed to find the expected Arch for mirrored image")
			}
		} else if o.Expect(strings.Contains(architectureStr, "ppc64le")).To(o.BeTrue()) {
			if o.Expect(strings.Contains(imageInfo, "ppc64le")).To(o.BeTrue()) {
				e2e.Logf("Found the expected Arch ppc64le")
			} else {
				e2e.Failf("Failed to find the expected Arch for mirrored image")
			}
		} else {
			if o.Expect(strings.Contains(imageInfo, "s390x")).To(o.BeTrue()) {
				e2e.Logf("Found the expected Arch s390x")
			} else {
				e2e.Failf("Failed to find the expected Arch for mirrored image")
			}
		}

	})
})

var _ = g.Describe("[sig-cli] oc CLI additional tests", func() {
	defer g.GinkgoRecover()

	var (
		oc = NewCLIWithoutNamespace(KubeConfigPath())
	)

	// author: yinzhou@redhat.com
	g.It("MicroShiftBoth-ROSA-OSD_CCS-ARO-Author:yinzhou-High-43030-oc get events always show the timestamp as LAST SEEN", func() {
		// Check if cluster is microshift or OCP
		By("Check if cluster is microshift or OCP")
		masterNodes, getAllMasterNodesErr := GetMasterNodes(oc)
		if getAllMasterNodesErr != nil || len(masterNodes) == 0 {
			skipMsg := "Skipping test - no master/control-plane nodes accessible (likely HyperShift/managed cluster)"
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}

		By("Get all the namespaces")
		var output string
		_, err := DebugNodeWithOptionsAndChroot(oc, masterNodes[0], []string{"-q"}, "bash", "-c", "microshift version")
		if err != nil {
			output, err = oc.AsAdmin().Run("get").Args("projects", "-o=custom-columns=NAME:.metadata.name", "--no-headers").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
		} else {
			output, err = oc.AsAdmin().Run("get").Args("ns", "-o=custom-columns=NAME:.metadata.name", "--no-headers").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
		}
		projectList := strings.Fields(output)

		g.By("check the events per project")
		for _, projectN := range projectList {
			output, _ = oc.AsAdmin().WithoutNamespace().Run("get").Args("events", "-n", projectN).Output()
			if match, _ := regexp.MatchString("No resources found", string(output)); match {
				e2e.Logf("No events in project: %v", projectN)
			} else {
				result, _ := exec.Command("bash", "-c", "cat "+output+" | awk '{print $1}'").Output()
				if match, _ := regexp.MatchString("unknown", string(result)); match {
					e2e.Failf("Does not show timestamp as expected: %v", result)
				}
			}
		}

	})

	// author: yinzhou@redhat.com
	g.It("MicroShiftBoth-VMonly-ROSA-OSD_CCS-ARO-Author:yinzhou-Medium-42983-always delete the debug pod when the oc debug node command exist [Flaky]", func() {
		By("Check if a cluster is Microshift or OCP")
		masterNodes, getAllMasterNodesErr := GetMasterNodes(oc)
		if getAllMasterNodesErr != nil || len(masterNodes) == 0 {
			skipMsg := "Skipping test - no master/control-plane nodes accessible (likely HyperShift/managed cluster)"
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}

		project42983 := "project42983"
		_, err := DebugNodeWithOptionsAndChroot(oc, masterNodes[0], []string{"-q"}, "bash", "-c", "microshift version")
		if err != nil {
			oc.SetupProject()
			project42983 = oc.Namespace()
		} else {
			defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("ns", project42983).Execute()
			createNSErr := oc.AsAdmin().WithoutNamespace().Run("create").Args("ns", project42983).Execute()
			o.Expect(createNSErr).NotTo(o.HaveOccurred())
		}

		By("Set namespace as privileged namespace")
		SetNamespacePrivileged(oc, project42983)

		g.By("Get all the node name list")
		out, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("node", "-o=jsonpath={.items[*].metadata.name}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		nodeList := strings.Fields(out)

		g.By("Run debug node")
		for _, nodeName := range nodeList {
			err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("node/"+nodeName, "-n", project42983, "--", "chroot", "/host", "date").Execute()
			o.Expect(err).NotTo(o.HaveOccurred())
		}

		g.By("Make sure debug pods have been deleted")
		err = wait.Poll(5*time.Second, 30*time.Second, func() (bool, error) {
			output, err := oc.Run("get").Args("pods", "-n", project42983).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			if matched, _ := regexp.MatchString("No resources found", output); !matched {
				e2e.Logf("pods still not deleted :\n%s, try again ", output)
				return false, nil
			}
			return true, nil
		})
		AssertWaitPollNoErr(err, "pods still not deleted")

	})

	// author: yinzhou@redhat.com
	g.It("MicroShiftBoth-ROSA-OSD_CCS-ARO-Author:yinzhou-Medium-34155-oc get events sorted by lastTimestamp", func() {
		g.By("Get events sorted by lastTimestamp")
		err := oc.AsAdmin().WithoutNamespace().Run("get").Args("events", "-n", "openshift-operator-lifecycle-manager", "--sort-by="+".lastTimestamp").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	// author: yinzhou@redhat.com
	g.It("MicroShiftBoth-ROSA-OSD_CCS-ARO-Author:yinzhou-Medium-47555-Should not update data when use oc set data with dry-run as server", func() {
		By("Check if cluster is microshift or OCP")
		masterNodes, getAllMasterNodesErr := GetMasterNodes(oc)
		if getAllMasterNodesErr != nil || len(masterNodes) == 0 {
			skipMsg := "Skipping test - no master/control-plane nodes accessible (likely HyperShift/managed cluster)"
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}

		project47555 := "project47555"
		_, err := DebugNodeWithOptionsAndChroot(oc, masterNodes[0], []string{"-q"}, "bash", "-c", "microshift version")
		if err != nil {
			oc.SetupProject()
			project47555 = oc.Namespace()
		} else {
			defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("ns", project47555).Execute()
			createNSErr := oc.AsAdmin().WithoutNamespace().Run("create").Args("ns", project47555).Execute()
			o.Expect(createNSErr).NotTo(o.HaveOccurred())
		}

		g.By("Create new configmap")
		err = oc.Run("create").Args("configmap", "cm-47555", "--from-literal=name=abc", "-n", project47555).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		g.By("Save the data for configmap")
		beforeSetcm, err := oc.Run("get").Args("cm", "cm-47555", "-o=jsonpath={.data.name}", "-n", project47555).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		g.By("Run the set with server dry-run")
		err = oc.Run("set").Args("data", "cm", "cm-47555", "--from-literal=name=def", "--dry-run=server", "-n", project47555).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		afterSetcm, err := oc.Run("get").Args("cm", "cm-47555", "-o=jsonpath={.data.name}", "-n", project47555).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if match, _ := regexp.MatchString(beforeSetcm, afterSetcm); !match {
			e2e.Failf("Should not persistent update configmap with server dry-run")
		}
		g.By("Create new secret")
		err = oc.Run("create").Args("secret", "generic", "secret-47555", "--from-literal=name=abc", "-n", project47555).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		g.By("Save the data for secret")
		beforeSetse, err := oc.Run("get").Args("secret", "secret-47555", "-o=jsonpath={.data.name}", "-n", project47555).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		g.By("Run the set with server dry-run")
		err = oc.Run("set").Args("data", "secret", "secret-47555", "--from-literal=name=def", "--dry-run=server", "-n", project47555).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		afterSetse, err := oc.Run("get").Args("secret", "secret-47555", "-o=jsonpath={.data.name}", "-n", project47555).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if match, _ := regexp.MatchString(beforeSetse, afterSetse); !match {
			e2e.Failf("Should not persistent update secret with server dry-run")
		}

	})

	// author: yinzhou@redhat.com
	g.It("MicroShiftBoth-ROSA-OSD_CCS-ARO-Author:yinzhou-Medium-49116-oc debug should remove startupProbe when create debug pod", func() {
		By("Check if cluster is microshift or OCP")
		masterNodes, getAllMasterNodesErr := GetMasterNodes(oc)
		if getAllMasterNodesErr != nil || len(masterNodes) == 0 {
			skipMsg := "Skipping test - no master/control-plane nodes accessible (likely HyperShift/managed cluster)"
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}

		project49116 := "project49116"
		_, err := DebugNodeWithOptionsAndChroot(oc, masterNodes[0], []string{"-q"}, "bash", "-c", "microshift version")
		if err != nil {
			oc.SetupProject()
			project49116 = oc.Namespace()
		} else {
			defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("ns", project49116).Execute()
			createNSErr := oc.AsAdmin().WithoutNamespace().Run("create").Args("ns", project49116).Execute()
			o.Expect(createNSErr).NotTo(o.HaveOccurred())
		}

		g.By("Create the deploy")
		err = oc.Run("create").Args("deploy", "d49116", "--image", "quay.io/openshifttest/hello-openshift@sha256:56c354e7885051b6bb4263f9faa58b2c292d44790599b7dde0e49e7c466cf339", "-n", project49116).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("patch the deploy with startupProbe")
		patchS := `[{"op": "add", "path": "/spec/template/spec/containers/0/startupProbe", "value":{ "exec": {"command": [ "false" ]}}}]`
		err = oc.Run("patch").Args("deploy", "d49116", "--type=json", "-p", patchS, "-n", project49116).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("run the debug with jsonpath")
		out, err := oc.Run("debug").Args("deploy/d49116", "-o=jsonpath='{.spec.containers[0].startupProbe}'", "-n", project49116).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if out != "''" {
			e2e.Failf("The output should be empty, but not: %v", out)
		}
	})
})

var _ = g.Describe("[sig-cli] Workloads client test", func() {
	defer g.GinkgoRecover()

	var (
		oc = NewCLI("oc", KubeConfigPath())
	)

	// author: yinzhou@redhat.com
	g.It("ROSA-OSD_CCS-ARO-Longduration-NonPreRelease-Author:yinzhou-Medium-49395-oc debug node should exit when timeout [Timeout:30m]", func() {
		workerNodeList, err := GetClusterNodesBy(oc, "worker")
		o.Expect(err).NotTo(o.HaveOccurred())
		g.By("Create new namespace")
		oc.SetupProject()
		ns49395 := oc.Namespace()

		SetNamespacePrivileged(oc, ns49395)

		e2e.Logf("Running: %s debug --to-namespace %s node/%s -- sleep 900", oc.execPath, ns49395, workerNodeList[0])
		cmd := exec.Command(oc.execPath, "debug", "--to-namespace", ns49395, "node/"+workerNodeList[0], "--", "sleep", "900")
		if oc.kubeconfig != "" {
			cmd.Env = append(os.Environ(), "KUBECONFIG="+oc.kubeconfig)
		}
		err = cmd.Start()
		o.Expect(err).NotTo(o.HaveOccurred())
		defer cmd.Process.Kill()
		err = wait.Poll(10*time.Second, 600*time.Second, func() (bool, error) {
			output, err1 := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", ns49395).Output()
			if err1 != nil {
				e2e.Logf("the err:%v, and try next round", err1)
				return false, nil
			}
			if matched, _ := regexp.MatchString("debug", output); matched {
				e2e.Logf("Check the debug pod in own namespace\n")
				return true, nil
			}
			return false, nil
		})
		AssertWaitPollNoErr(err, fmt.Sprintf("Cannot find the debug pod in own namespace"))
		err = wait.Poll(30*time.Second, 960*time.Second, func() (bool, error) {
			output, err1 := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", ns49395).Output()
			if err1 != nil {
				e2e.Logf("the err:%v, and try next round", err1)
				return false, nil
			}
			if matched, _ := regexp.MatchString("debug", output); !matched {
				e2e.Logf("Check the debug pod disappeared in own namespace\n")
				return true, nil
			}
			return false, nil
		})
		AssertWaitPollNoErr(err, fmt.Sprintf("Still find the debug pod in own namespace even wait for 15 mins"))
	})

	// author: yinzhou@redhat.com
	g.It("Author:yinzhou-ROSA-OSD_CCS-ARO-NonPreRelease-High-68647-oc whoami must work without oauth-apiserver", func() {
		isExternalOIDCCluster, err := IsExternalOIDCCluster(oc)
		o.Expect(err).NotTo(o.HaveOccurred())
		if !isExternalOIDCCluster {
			skipMsg := "Skipping the test as we are not running in a cluster without OAuth servers."
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}
		err = oc.AsAdmin().Run("whoami").Args("").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		By("Create new namespace")
		oc.SetupProject()
		// Test normal user runs oc whoami well
		err = oc.Run("whoami").Args("").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		By("Create new project to make sure that openshift-apiserver still functions well.")
		projectName := "ocp-68647" + GetRandomString()
		err = oc.AsAdmin().WithoutNamespace().Run("new-project").Args(projectName, "--skip-config-write").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("project", projectName).Execute()

		By("Create new app")
		err = oc.AsAdmin().WithoutNamespace().Run("new-app").Args("quay.io/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83", "-n", projectName, "--import-mode=PreserveOriginal").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("Waiting for all pods of hello-openshift application to be ready ...")
		var poderr error
		errPod := wait.Poll(10*time.Second, 300*time.Second, func() (bool, error) {
			podOutput, poderr := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", projectName, "--no-headers").Output()
			if poderr == nil && strings.Contains(podOutput, "Running") {
				e2e.Logf("Pod %v succesfully", podOutput)
				return true, nil
			}
			return false, nil
		})
		AssertWaitPollNoErr(errPod, fmt.Sprintf("Pod not running :: %v", poderr))
	})

	// author: yinzhou@redhat.com
	g.It("ROSA-OSD_CCS-ARO-Author:yinzhou-High-10136-Project should only watch its owned cache events", func() {
		By("Create the first namespace")
		oc.SetupProject()
		ns1 := oc.Namespace()
		By("Create deployment in the first namespace")
		deployCreationErr := oc.WithoutNamespace().Run("create").Args("deployment", "deploy10136-1", "-n", ns1, "--image", "quay.io/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83").Execute()
		o.Expect(deployCreationErr).NotTo(o.HaveOccurred())
		if ok := waitForAvailableRsRunning(oc, "deployment", "deploy10136-1", ns1, "1"); ok {
			e2e.Logf("All pods are runnnig now\n")
		} else {
			e2e.Failf("deploy10136-1 pods are not running as expected")
		}

		By("Create the second namespace")
		oc.SetupProject()
		ns2 := oc.Namespace()
		By("Get deployment under the second project with watch")
		cmd2, backgroundBufNs2, _, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment", "-n", ns2, "-o", "name", "-w").Background()
		defer cmd2.Process.Kill()
		o.Expect(err).NotTo(o.HaveOccurred())
		By("Create deployment in the second namespace")
		deployCreationErr2 := oc.WithoutNamespace().Run("create").Args("deployment", "deploy10136-2", "-n", ns2, "--image", "quay.io/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83").Execute()
		o.Expect(deployCreationErr2).NotTo(o.HaveOccurred())
		if ok := waitForAvailableRsRunning(oc, "deployment", "deploy10136-2", ns2, "1"); ok {
			e2e.Logf("All pods are runnnig now\n")
		} else {
			e2e.Failf("deploy10136-2 pods are not running as expected")
		}

		By("Get deployment in the first namespace with watch")
		cmd1, backgroundBuf, _, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment", "-n", ns1, "-o", "name", "-w").Background()
		defer cmd1.Process.Kill()
		o.Expect(err).NotTo(o.HaveOccurred())

		By("Delete the deployment in the second namespace")
		deleteDeploymentErr := oc.WithoutNamespace().Run("delete").Args("deployment", "deploy10136-2", "-n", ns2).Execute()
		o.Expect(deleteDeploymentErr).NotTo(o.HaveOccurred())

		By("Get deployment in the first namespace again")
		out, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment", "-n", ns1, "-o", "name").Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		By("Make sure the watch events matched")
		deploymentWatchOut := strings.Replace(backgroundBuf.String(), "\n", "", -1)
		if matched, _ := regexp.MatchString(deploymentWatchOut, out); matched {
			e2e.Logf("All deployment events matched\n")
		} else {
			e2e.Failf("Deployment events not matched")
		}

		By("Make sure no trace under the second project for the resource under the first project")
		if matched, _ := regexp.MatchString(backgroundBufNs2.String(), "deploy10136-1"); matched {
			e2e.Failf("Should not see any trace for the resource under the first project in the second project\n")
		}
	})

	// author: yinzhou@redhat.com
	g.It("ROSA-OSD_CCS-ARO-ConnectedOnly-Author:yinzhou-High-71178-Make sure no mismatch for sha256sum of openshift install for mac version", func() {
		skipIfDisconnected(oc)

		if !assertPullSecret(oc) {
			skipMsg := "The cluster does not have pull secret for public registry hence skipping..."
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}

		extractTmpDirName := "/tmp/d71178"
		err := os.MkdirAll(extractTmpDirName, 0700)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer os.RemoveAll(extractTmpDirName)
		secretFile, secretErr := oc.AsAdmin().WithoutNamespace().Run("get").Args("secret/pull-secret", "-n", "openshift-config", `--template={{index .data ".dockerconfigjson" | base64decode}}`).OutputToFile("auth.dockerconfigjson")
		o.Expect(secretErr).NotTo(o.HaveOccurred())
		By("Get the payload")
		payloadPullSpec, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("clusterversion", "-o", "jsonpath={..desired.image}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(payloadPullSpec).NotTo(o.BeEmpty())
		e2e.Logf("pullspec is %v", payloadPullSpec)

		By("Extract the darwin tools")
		os.RemoveAll("/tmp/d71178")
		err = oc.AsAdmin().WithoutNamespace().Run("adm").Args("release", "extract", payloadPullSpec, "--registry-config="+secretFile, "--command-os=darwin/arm64", "--tools", "--to=/tmp/d71178", "--insecure").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		By("Make sure no mismatch for sha256sum")
		files := getSpecificFileName("/tmp/d71178", "openshift-install")
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("%v", files)
		fileSum, err := sha256File("/tmp/d71178/" + files[0])
		e2e.Logf("%v", fileSum)
		o.Expect(err).NotTo(o.HaveOccurred())
		fileSumFromResult := getSha256SumFromFile("/tmp/d71178/sha256sum.txt")
		e2e.Logf("%v", fileSumFromResult)
		if match, _ := regexp.MatchString(fileSum, fileSumFromResult); !match {
			e2e.Failf("File sum not matched")
		}
	})

	// author: yinzhou@redhat.com
	g.It("ROSA-OSD_CCS-ARO-ConnectedOnly-Author:yinzhou-Medium-71273-Medium-71275-Validate user is able to extract rhel8 and rhel9 oc from the ocp payload", func() {
		skipIfDisconnected(oc)

		if !assertPullSecret(oc) {
			skipMsg := "The cluster does not have pull secret for public registry hence skipping..."
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}
		extractTmpDirName := "/tmp/case71273"
		defer os.RemoveAll(extractTmpDirName)
		err := os.MkdirAll(extractTmpDirName, 0700)
		o.Expect(err).NotTo(o.HaveOccurred())

		_, err = oc.AsAdmin().WithoutNamespace().Run("extract").Args("secret/pull-secret", "-n", "openshift-config", fmt.Sprintf("--to=%s", extractTmpDirName), "--confirm").Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		By("Get desired image from ocp cluster")
		pullSpec, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("clusterversion", "-o", "jsonpath={..desired.image}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(pullSpec).NotTo(o.BeEmpty())

		By("Extract oc.rhel8 from ocp payload")
		_, err = oc.AsAdmin().WithoutNamespace().Run("adm").Args("release", "extract", "--command=oc.rhel8", pullSpec, "-a", extractTmpDirName+"/.dockerconfigjson", "--to", extractTmpDirName, "--insecure").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if _, statErr := os.Stat(extractTmpDirName + "/oc"); os.IsNotExist(statErr) {
			e2e.Failf("Get extracted oc failed")
		}
		removeErr := os.Remove(extractTmpDirName + "/oc")
		o.Expect(removeErr).NotTo(o.HaveOccurred())

		By("Extract oc.rhel9 from ocp payload")
		_, err = oc.AsAdmin().WithoutNamespace().Run("adm").Args("release", "extract", "--command=oc.rhel9", pullSpec, "-a", extractTmpDirName+"/.dockerconfigjson", "--to", extractTmpDirName, "--insecure").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if _, statErr := os.Stat(extractTmpDirName + "/oc"); os.IsNotExist(statErr) {
			e2e.Failf("Get extracted oc failed")
		}
		removeErr = os.Remove(extractTmpDirName + "/oc")
		o.Expect(removeErr).NotTo(o.HaveOccurred())

		By("Extract oc from ocp payload")
		_, err = oc.AsAdmin().WithoutNamespace().Run("adm").Args("release", "extract", "--command=oc", pullSpec, "-a", extractTmpDirName+"/.dockerconfigjson", "--to", extractTmpDirName, "--insecure").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if _, statErr := os.Stat(extractTmpDirName + "/oc"); os.IsNotExist(statErr) {
			e2e.Failf("Get extracted oc failed")
		}
		removeErr = os.Remove(extractTmpDirName + "/oc")
		o.Expect(removeErr).NotTo(o.HaveOccurred())

		By("Get the oc-mirror image from ocp payload")
		ocMirrorImage, _, err := oc.WithoutNamespace().WithoutKubeconf().Run("adm").Args("release", "info", pullSpec, "-a", extractTmpDirName+"/.dockerconfigjson", "--insecure", `-ojsonpath={.references.spec.tags[?(@.name=="oc-mirror")].from.name}`).Outputs()
		o.Expect(err).NotTo(o.HaveOccurred())

		By("Extract oc-mirror.rhel8")
		_, err = oc.WithoutNamespace().WithoutKubeconf().Run("image").Args("extract", ocMirrorImage, "-a", extractTmpDirName+"/.dockerconfigjson", "--path=/usr/bin/oc-mirror.rhel8:"+extractTmpDirName, "--confirm", "--insecure").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if _, statErr := os.Stat(extractTmpDirName + "/oc-mirror.rhel8"); os.IsNotExist(statErr) {
			e2e.Failf("Get extracted oc-mirror.rhel8 failed")
		}
		removeErr = os.Remove(extractTmpDirName + "/oc-mirror.rhel8")
		o.Expect(removeErr).NotTo(o.HaveOccurred())

		By("Extract oc-mirror.rhel9")
		_, err = oc.WithoutNamespace().WithoutKubeconf().Run("image").Args("extract", ocMirrorImage, "-a", extractTmpDirName+"/.dockerconfigjson", "--path=/usr/bin/oc-mirror.rhel9:"+extractTmpDirName, "--confirm", "--insecure").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if _, statErr := os.Stat(extractTmpDirName + "/oc-mirror.rhel9"); os.IsNotExist(statErr) {
			e2e.Failf("Get extracted oc-mirror.rhel9 failed")
		}
		removeErr = os.Remove(extractTmpDirName + "/oc-mirror.rhel9")
		o.Expect(removeErr).NotTo(o.HaveOccurred())
	})
	// author: yinzhou@redhat.com
	g.It("ROSA-OSD_CCS-ARO-Author:yinzhou-Medium-72217-Should get warning when there is an identical short name for two or more resources", func() {
		customResourceBaseDir := FixturePath("testdata", "oc_cli/case72217")
		cronTabCRDF := filepath.Join(customResourceBaseDir, "crd-crontab-72217.yaml")
		cronCRF := filepath.Join(customResourceBaseDir, "cr-cron-72217.yaml")
		customTaskCRDF := filepath.Join(customResourceBaseDir, "crd-customtask-72217.yaml")
		customCRF := filepath.Join(customResourceBaseDir, "cr-custom-72217.yaml")
		catToyCRDF := filepath.Join(customResourceBaseDir, "crd-cattoy-72217.yaml")
		catCRF := filepath.Join(customResourceBaseDir, "cr-cat-72217.yaml")

		g.By("Create new namespace")
		oc.SetupProject()
		ns72217 := oc.Namespace()

		By("Create the first CRD and get by short name should no warning")
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("-f", cronTabCRDF).Execute()
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", cronTabCRDF).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitCRDAvailable(oc, "crontabs72217.stable.example.com")
		AssertWaitPollNoErr(err, "The crd crontabs72217.stable.example.com is not available in 60 seconds")
		err = waitCreateCr(oc, cronCRF, ns72217)
		AssertWaitPollNoErr(err, "The cr of  crontabs is not created in 120 seconds")
		err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ct72217", "-n", ns72217).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		By("Create the second CRD and get by short name should see warning")
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("-f", customTaskCRDF).Execute()
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", customTaskCRDF).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitCRDAvailable(oc, "customtasks72217.example.com")
		AssertWaitPollNoErr(err, "The crd customtasks72217.example.com is not available in 60 seconds")
		err = waitCreateCr(oc, customCRF, ns72217)
		AssertWaitPollNoErr(err, "The cr of custometask is not created in 120 seconds")
		_, outputWarning, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("ct72217", "-n", ns72217).Outputs()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(outputWarning).To(o.ContainSubstring("could also match lower priority resource"))

		By("Create the third CRD and get by short name should see warning")
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("-f", catToyCRDF).Execute()
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", catToyCRDF).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitCRDAvailable(oc, "cattoys72217.bate.example.com")
		AssertWaitPollNoErr(err, "The crd cattoys72217.bate.example.com is not available in 60 seconds")
		err = waitCreateCr(oc, catCRF, ns72217)
		AssertWaitPollNoErr(err, "The cr of cattoy is not created in 120 seconds")
		_, outputWarning, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ct72217", "-n", ns72217).Outputs()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(outputWarning).To(o.ContainSubstring("could also match lower priority resource customtasks72217.example.com"))
		o.Expect(outputWarning).To(o.ContainSubstring("could also match lower priority resource crontabs72217.stable.example.com"))
	})

	g.It("Author:yinzhou-ROSA-OSD_CCS-ARO-Medium-76150-Make sure oc debug node has set HOST env var", func() {
		mnodeName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("node", "-o=jsonpath={.items[0].metadata.name}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		By("Create new namespace")
		oc.SetupProject()
		project76150 := oc.Namespace()
		By("Set namespace as privileged namespace")
		SetNamespacePrivileged(oc, project76150)
		filePath, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("node/"+mnodeName, "-n", project76150, "-o=yaml").OutputToFile(getRandomString() + "workload-debug.yaml")
		o.Expect(err).NotTo(o.HaveOccurred())
		regV1 := checkFileContent(filePath, "name: HOST")
		regV2 := checkFileContent(filePath, "value: /host")
		if regV1 && regV2 {
			e2e.Logf("Found the expected host env setting for debug pod")
		} else {
			e2e.Failf("Don't find the host env set for debug pod")
		}
	})

	g.It("Author:yinzhou-ROSA-OSD_CCS-ARO-High-76116-Make sure oc could run on rhel with fips on", func() {
		workerNodeList, err := GetClusterNodesBy(oc, "worker")
		o.Expect(err).NotTo(o.HaveOccurred())
		By("Create new namespace")
		oc.SetupProject()
		project76116 := oc.Namespace()
		By("Set namespace as privileged namespace")
		SetNamespacePrivileged(oc, project76116)
		By("Check if fips enable")
		efips, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", project76116, "node/"+workerNodeList[0], "--", "chroot", "/host", "fips-mode-setup", "--check").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if !strings.Contains(efips, "FIPS mode is enabled.") {
			skipMsg := "Fips mode is disabled, skip it."
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}

		By("Check if oc could run with fips on")
		clientVersion, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", project76116, "node/"+workerNodeList[0], "--", "chroot", "/host", "oc", "version").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if !strings.Contains(clientVersion, "Client Version") {
			e2e.Failf("Failed to run oc client with fips on")
		}
	})

	g.It("Author:yinzhou-ROSA-OSD_CCS-ARO-Critical-11882-Return description of resources with cli describe", func() {
		By("Create new namespace")
		oc.SetupProject()
		project11882 := oc.Namespace()
		err := oc.WithoutNamespace().Run("new-app").Args("quay.io/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83", "-n", project11882, "--name=example-11882", "--import-mode=PreserveOriginal").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		if ok := waitForAvailableRsRunning(oc, "deployment", "example-11882", oc.Namespace(), "1"); ok {
			e2e.Logf("All pods are runnnig now\n")
		} else {
			oc.Run("get").Args("events", "-n", project11882).Output()
			e2e.Failf("Deploment failed to roll out")
		}

		output, err := oc.Run("describe").Args("svc", "example-11882", "-n", project11882).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(output, "deployment=example-11882")).To(o.BeTrue())
		svcyamleFile, err := oc.Run("get").Args("svc", "example-11882", "-n", project11882, "-o", "yaml").OutputToFile("svc-11882.yaml")
		o.Expect(err).NotTo(o.HaveOccurred())
		outputY, err := oc.Run("describe").Args("-f", svcyamleFile).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(outputY, "deployment=example-11882")).To(o.BeTrue())
		outputL, err := oc.Run("describe").Args("svc", "-l", "app=example-11882", "-n", project11882).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(outputL, "example-11882")).To(o.BeTrue())
		outputN, err := oc.Run("describe").Args("svc", "example", "-n", project11882).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(outputN, "example-11882")).To(o.BeTrue())
		rsName, err := oc.Run("get").Args("rs", "-o=jsonpath={.items[0].metadata.name}", "-n", project11882).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		outputM, err := oc.Run("describe").Args("deploy/example-11882", "rs/"+rsName, "-n", project11882).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(outputM, "example-11882")).To(o.BeTrue())
		o.Expect(strings.Contains(outputM, rsName)).To(o.BeTrue())
	})

	g.It("Author:yinzhou-ROSA-OSD_CCS-ARO-High-76287-make sure tools imagestream contains sosreport", func() {
		// Skip the case if cluster doest not have the imageRegistry installed
		if !isEnabledCapability(oc, "ImageRegistry") {
			skipMsg := "Skipped: cluster does not have imageRegistry installed"
			e2e.Warningf("SKIPPING TEST: %s", skipMsg)
			g.Skip(skipMsg)
		}
		oc.SetupProject()
		project76287 := oc.Namespace()

		By("Set namespace as privileged namespace")
		SetNamespacePrivileged(oc, project76287)

		By("Get all the node name list")
		out, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("node", "-o=jsonpath={.items[*].metadata.name}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		nodeList := strings.Fields(out)

		By("Check tools imagestream  with sos command")
		err = oc.Run("run").Args("testsos76287", "-n", project76287, "--image", "image-registry.openshift-image-registry.svc:5000/openshift/tools", "--restart", "Never", "--", "sos", "help").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		By("Run debug node with sos command")
		err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("node/"+nodeList[0], "-n", project76287, "--", "sos", "help").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
	})
})

// ClientVersion ...
type ClientVersion struct {
	BuildDate    string `json:"buildDate"`
	Compiler     string `json:"compiler"`
	GitCommit    string `json:"gitCommit"`
	GitTreeState string `json:"gitTreeState"`
	GitVersion   string `json:"gitVersion"`
	GoVersion    string `json:"goVersion"`
	Major        string `json:"major"`
	Minor        string `json:"minor"`
	Platform     string `json:"platform"`
}

// ServerVersion ...
type ServerVersion struct {
	BuildDate    string `json:"buildDate"`
	Compiler     string `json:"compiler"`
	GitCommit    string `json:"gitCommit"`
	GitTreeState string `json:"gitTreeState"`
	GitVersion   string `json:"gitVersion"`
	GoVersion    string `json:"goVersion"`
	Major        string `json:"major"`
	Minor        string `json:"minor"`
	Platform     string `json:"platform"`
}

// VersionInfo ...
type VersionInfo struct {
	ClientInfo           ClientVersion `json:"ClientVersion"`
	OpenshiftVersion     string        `json:"openshiftVersion"`
	ServerInfo           ServerVersion `json:"ServerVersion"`
	ReleaseClientVersion string        `json:"releaseClientVersion"`
}
