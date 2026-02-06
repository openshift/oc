package e2e

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"regexp"

	o "github.com/onsi/gomega"

	"math/rand"
	"net/http"

	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"github.com/openshift/oc/test/testdata"
)

// e2e compatibility layer - simple wrappers to avoid importing k8s.io/kubernetes/test/e2e/framework
// which has ginkgo v1/v2 compatibility issues
// All e2e logging methods automatically log to klog for structured logging
type e2eCompat struct{}

var e2e = e2eCompat{}

// Logf logs an info message to both klog and Ginkgo writer
func (e2eCompat) Logf(format string, args ...interface{}) {
	klog.Infof(format, args...)
	fmt.Fprintf(g.GinkgoWriter, format+"\n", args...)
}

// Warningf logs a warning message to both klog and Ginkgo writer
func (e2eCompat) Warningf(format string, args ...interface{}) {
	klog.Warningf(format, args...)
	fmt.Fprintf(g.GinkgoWriter, "WARNING: "+format+"\n", args...)
}

// Errorf logs an error message to both klog and Ginkgo writer (without failing the test)
func (e2eCompat) Errorf(format string, args ...interface{}) {
	klog.Errorf(format, args...)
	fmt.Fprintf(g.GinkgoWriter, "ERROR: "+format+"\n", args...)
}

// Failf logs an error to klog and fails the test
func (e2eCompat) Failf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	klog.Errorf("TEST FAILED: %s", msg)
	g.Fail(msg)
}

// CLI represents an oc CLI interface for running commands
type CLI struct {
	execPath      string
	kubeconfig    string
	namespace     string
	asAdmin       bool
	withNamespace bool
}

// NewCLI creates a new CLI instance
func NewCLI(execPath string, kubeconfig string) *CLI {
	return &CLI{
		execPath:      execPath,
		kubeconfig:    kubeconfig,
		asAdmin:       false,
		withNamespace: true,
	}
}

// NewCLIWithoutNamespace creates a new CLI instance without a default namespace
func NewCLIWithoutNamespace(kubeconfig string) *CLI {
	return &CLI{
		execPath:      "oc",
		kubeconfig:    kubeconfig,
		asAdmin:       false,
		withNamespace: false,
	}
}

// KubeConfigPath returns the path to the kubeconfig file
func KubeConfigPath() string {
	if kc := os.Getenv("KUBECONFIG"); kc != "" {
		return kc
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kube", "config")
}

// AsAdmin returns a copy of the CLI with admin privileges
func (c *CLI) AsAdmin() *CLI {
	newCLI := *c
	newCLI.asAdmin = true
	return &newCLI
}

// WithoutNamespace returns a copy of the CLI without namespace set
func (c *CLI) WithoutNamespace() *CLI {
	newCLI := *c
	newCLI.withNamespace = false
	return &newCLI
}

// WithoutKubeconf returns a copy of the CLI without kubeconfig
func (c *CLI) WithoutKubeconf() *CLI {
	newCLI := *c
	newCLI.kubeconfig = ""
	return &newCLI
}

// WithKubectl returns a copy of the CLI that uses kubectl instead of oc
func (c *CLI) WithKubectl() *CLI {
	newCLI := *c
	newCLI.execPath = "kubectl"
	return &newCLI
}

// AdminKubeClient returns a kube client (stub for deployment polling)
func (c *CLI) AdminKubeClient() *dummyKubeClientComplete { return &dummyKubeClientComplete{} }

// AsGuestKubeconf sets guest kubeconfig (stub for unused functions)
func (c *CLI) AsGuestKubeconf(path string) *CLI { return c }

// CLICommand represents a command to be executed
type CLICommand struct {
	cli  *CLI
	verb string
	args []string
}

// Run sets the verb for the CLI command
func (c *CLI) Run(verb string) *CLICommand {
	return &CLICommand{
		cli:  c,
		verb: verb,
		args: []string{},
	}
}

// Args sets the arguments for the CLI command
func (cmd *CLICommand) Args(args ...string) *CLICommand {
	cmd.args = append(cmd.args, args...)
	return cmd
}

// WithoutNamespace returns the command with namespace disabled
func (cmd *CLICommand) WithoutNamespace() *CLICommand {
	newCmd := *cmd
	newCLI := *cmd.cli
	newCLI.withNamespace = false
	newCmd.cli = &newCLI
	return &newCmd
}

// Execute runs the CLI command and returns an error if it fails
func (cmd *CLICommand) Execute() error {
	_, err := cmd.Output()
	return err
}

// Output runs the CLI command and returns its output
func (cmd *CLICommand) Output() (string, error) {
	args := []string{}
	if cmd.verb != "" {
		args = append(args, cmd.verb)
	}
	args = append(args, cmd.args...)

	// Add namespace flag if withNamespace is true and namespace is set
	if cmd.cli.withNamespace && cmd.cli.namespace != "" {
		args = append(args, "-n", cmd.cli.namespace)
	}

	// Log the command being executed (similar to test-private client.go:1013)
	var logParts []string
	logParts = append(logParts, "oc")

	// Add namespace flag to log if present (test-private shows it as --namespace=X)
	if cmd.cli.withNamespace && cmd.cli.namespace != "" {
		logParts = append(logParts, fmt.Sprintf("--namespace=%s", cmd.cli.namespace))
	}

	// Add kubeconfig to log if present
	if cmd.cli.kubeconfig != "" {
		logParts = append(logParts, fmt.Sprintf("--kubeconfig=%s", cmd.cli.kubeconfig))
	}

	// Add the actual command args
	logParts = append(logParts, strings.Join(args, " "))

	e2e.Logf("Running '%s'", strings.Join(logParts, " "))

	execCmd := exec.Command(cmd.cli.execPath, args...)
	if cmd.cli.kubeconfig != "" {
		execCmd.Env = append(os.Environ(), "KUBECONFIG="+cmd.cli.kubeconfig)
	}

	output, err := execCmd.CombinedOutput()
	if err != nil {
		// Log the error output to help debug failures
		e2e.Errorf("Command failed with error: %v\nOutput: %s", err, string(output))
	}
	return string(output), err
}

// Outputs runs the CLI command and returns stdout and stderr separately
func (cmd *CLICommand) Outputs() (string, string, error) {
	args := []string{}
	if cmd.verb != "" {
		args = append(args, cmd.verb)
	}
	args = append(args, cmd.args...)

	// Add namespace flag if withNamespace is true and namespace is set
	if cmd.cli.withNamespace && cmd.cli.namespace != "" {
		args = append(args, "-n", cmd.cli.namespace)
	}

	// Log the command being executed (similar to test-private client.go:1013)
	var logParts []string
	logParts = append(logParts, "oc")

	// Add namespace flag to log if present (test-private shows it as --namespace=X)
	if cmd.cli.withNamespace && cmd.cli.namespace != "" {
		logParts = append(logParts, fmt.Sprintf("--namespace=%s", cmd.cli.namespace))
	}

	// Add kubeconfig to log if present
	if cmd.cli.kubeconfig != "" {
		logParts = append(logParts, fmt.Sprintf("--kubeconfig=%s", cmd.cli.kubeconfig))
	}

	// Add the actual command args
	logParts = append(logParts, strings.Join(args, " "))

	e2e.Logf("Running '%s'", strings.Join(logParts, " "))

	execCmd := exec.Command(cmd.cli.execPath, args...)
	if cmd.cli.kubeconfig != "" {
		execCmd.Env = append(os.Environ(), "KUBECONFIG="+cmd.cli.kubeconfig)
	}

	var stdout, stderr strings.Builder
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	err := execCmd.Run()
	return stdout.String(), stderr.String(), err
}

// OutputToFile runs the CLI command and writes output to a file
func (cmd *CLICommand) OutputToFile(filename string) (string, error) {
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	filePath := filepath.Join("/tmp", filename)
	err = os.WriteFile(filePath, []byte(output), 0644)
	return filePath, err
}

// Background runs the CLI command in the background
func (cmd *CLICommand) Background() (*exec.Cmd, *strings.Builder, *strings.Builder, error) {
	args := []string{}
	if cmd.verb != "" {
		args = append(args, cmd.verb)
	}
	args = append(args, cmd.args...)

	// Add namespace flag if withNamespace is true and namespace is set
	if cmd.cli.withNamespace && cmd.cli.namespace != "" {
		args = append(args, "-n", cmd.cli.namespace)
	}

	// Log the command being executed (similar to test-private client.go:1013)
	var logParts []string
	logParts = append(logParts, "oc")

	// Add namespace flag to log if present (test-private shows it as --namespace=X)
	if cmd.cli.withNamespace && cmd.cli.namespace != "" {
		logParts = append(logParts, fmt.Sprintf("--namespace=%s", cmd.cli.namespace))
	}

	// Add kubeconfig to log if present
	if cmd.cli.kubeconfig != "" {
		logParts = append(logParts, fmt.Sprintf("--kubeconfig=%s", cmd.cli.kubeconfig))
	}

	// Add the actual command args
	logParts = append(logParts, strings.Join(args, " "))

	e2e.Logf("Running '%s' in background", strings.Join(logParts, " "))

	execCmd := exec.Command(cmd.cli.execPath, args...)
	if cmd.cli.kubeconfig != "" {
		execCmd.Env = append(os.Environ(), "KUBECONFIG="+cmd.cli.kubeconfig)
	}

	var stdout, stderr strings.Builder
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	err := execCmd.Start()
	return execCmd, &stdout, &stderr, err
}

// SetupProject creates a new project for the test
func (c *CLI) SetupProject() {
	projectName := fmt.Sprintf("e2e-test-%s", GetRandomString())
	e2e.Logf("Creating project %q", projectName)
	err := c.Run("new-project").Args(projectName, "--skip-config-write").Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
	c.namespace = projectName
	e2e.Logf("Project %q has been created", projectName)

	g.DeferCleanup(func() {
		e2e.Logf("Cleaning up project %q", projectName)
		c.AsAdmin().WithoutNamespace().Run("delete").Args("project", projectName, "--wait=false").Execute()
	})
}

// Namespace returns the current namespace
func (c *CLI) Namespace() string {
	return c.namespace
}

// Helper functions from compat_otp

// FixturePath returns the path to test fixture files.
// This delegates to the testdata package which uses embedded go-bindata fixtures.
// Testdata files are embedded in the test binary at build time and extracted to
// a temporary directory at runtime, so they work regardless of where the binary executes.
func FixturePath(elem ...string) string {
	return testdata.FixturePath(elem...)
}

// AssertWaitPollNoErr asserts that a wait.Poll operation completed without error
func AssertWaitPollNoErr(err error, message string) {
	if err == wait.ErrWaitTimeout {
		e2e.Failf("%s: timed out waiting", message)
	}
	o.Expect(err).NotTo(o.HaveOccurred(), message)
}

// GetRandomString generates a random string for unique naming
func GetRandomString() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, 8)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

// By prints a test step message using Ginkgo
func By(message string) {
	e2e.Logf("STEP: %s", message)
	g.By(message)
}

// IsExternalOIDCCluster checks if the cluster is using external OIDC
func IsExternalOIDCCluster(c *CLI) (bool, error) {
	output, err := c.AsAdmin().WithoutNamespace().Run("get").Args("authentication.config.openshift.io/cluster", "-o=jsonpath={.spec.type}").Output()
	if err != nil {
		return false, err
	}
	return strings.Contains(output, "OIDC"), nil
}

// SkipIfPlatformTypeNot skips the test if platform type doesn't match
func SkipIfPlatformTypeNot(c *CLI, platformType string) {
	platform := CheckPlatform(c)
	if !strings.EqualFold(platform, platformType) {
		skipMsg := fmt.Sprintf("Test requires platform type %s, but cluster is %s", platformType, platform)
		e2e.Warningf("SKIPPING TEST: %s", skipMsg)
		g.Skip(skipMsg)
	}
}

// CheckPlatform returns the infrastructure platform type
func CheckPlatform(c *CLI) string {
	output, err := c.AsAdmin().WithoutNamespace().Run("get").Args("infrastructure", "cluster", "-o=jsonpath={.status.platformStatus.type}").Output()
	if err != nil {
		e2e.Warningf("Failed to get platform type: %v", err)
		return "Unknown"
	}
	platform := strings.TrimSpace(output)
	e2e.Logf("Cluster platform type: %s", platform)
	return platform
}

// SetNamespacePrivileged sets a namespace as privileged
func SetNamespacePrivileged(c *CLI, namespace string) {
	e2e.Logf("Setting namespace %s as privileged", namespace)
	err := c.AsAdmin().WithoutNamespace().Run("label").Args("namespace", namespace, "pod-security.kubernetes.io/enforce=privileged", "pod-security.kubernetes.io/audit=privileged", "pod-security.kubernetes.io/warn=privileged", "--overwrite").Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// GetClusterNodesBy returns nodes filtered by role (master, worker, etc.)
func GetClusterNodesBy(c *CLI, role string) ([]string, error) {
	e2e.Logf("Getting cluster nodes with role: %s", role)
	labelSelector := fmt.Sprintf("node-role.kubernetes.io/%s=", role)
	output, err := c.AsAdmin().WithoutNamespace().Run("get").Args("nodes", "-l", labelSelector, "-o=jsonpath={.items[*].metadata.name}").Output()
	if err != nil {
		return nil, err
	}

	nodes := strings.Fields(strings.TrimSpace(output))
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no %s nodes found", role)
	}
	e2e.Logf("Found %d %s node(s): %v", len(nodes), role, nodes)
	return nodes, nil
}

// GetMasterNodes returns master/control-plane nodes, trying both labels for compatibility
func GetMasterNodes(c *CLI) ([]string, error) {
	e2e.Logf("Getting master/control-plane nodes")
	// Try "master" label first (older clusters)
	nodes, err := GetClusterNodesBy(c, "master")
	if err == nil && len(nodes) > 0 {
		e2e.Logf("Found master nodes using 'master' label")
		return nodes, nil
	}

	// Fallback to "control-plane" label (newer Kubernetes versions)
	e2e.Logf("No master nodes found, trying 'control-plane' label")
	nodes, err = GetClusterNodesBy(c, "control-plane")
	if err == nil && len(nodes) > 0 {
		e2e.Logf("Found master nodes using 'control-plane' label")
		return nodes, nil
	}

	return nil, fmt.Errorf("no master or control-plane nodes found")
}

// DebugNodeWithOptionsAndChroot runs a debug session on a node with chroot
func DebugNodeWithOptionsAndChroot(c *CLI, nodeName string, options []string, command string, args ...string) (string, error) {
	e2e.Logf("Running debug command on node %s", nodeName)
	debugArgs := []string{"node/" + nodeName}
	debugArgs = append(debugArgs, options...)

	fullCommand := command
	if len(args) > 0 {
		fullCommand = fmt.Sprintf("%s %s", command, strings.Join(args, " "))
	}
	debugArgs = append(debugArgs, "--", "chroot", "/host", "sh", "-c", fullCommand)

	// Use "default" namespace explicitly to avoid issues with non-existent context namespaces
	// The debug pod will be created in the default namespace, but it debugs the node itself
	return c.AsAdmin().Run("debug").Args(append([]string{"-n", "default"}, debugArgs...)...).Output()
}

// DebugNodeWithChroot runs a debug session on a node with chroot (simpler version)

// AssertPodToBeReady waits for a pod to be ready
func AssertPodToBeReady(c *CLI, podName string, namespace string) {
	e2e.Logf("Waiting for pod %s to be ready in namespace %s", podName, namespace)
	err := wait.Poll(5*time.Second, 5*time.Minute, func() (bool, error) {
		output, err := c.AsAdmin().WithoutNamespace().Run("get").Args("pod", podName, "-n", namespace, "-o=jsonpath={.status.conditions[?(@.type=='Ready')].status}").Output()
		if err != nil {
			return false, nil
		}
		if strings.TrimSpace(output) == "True" {
			e2e.Logf("Pod %s in namespace %s is ready", podName, namespace)
			return true, nil
		}
		return false, nil
	})
	AssertWaitPollNoErr(err, fmt.Sprintf("Pod %s in namespace %s failed to become ready", podName, namespace))
}

// RemoteShPod runs a shell command in a pod

// Architecture types and functions

// Architecture type constants
type ArchitectureType string

const (
	MULTI   ArchitectureType = "Multi"
	X86                      = "amd64"
	ARM64                    = "arm64"
	PPC64LE                  = "ppc64le"
	S390X                    = "s390x"
)

// SkipArchitectures skips the test if the cluster architecture matches
func SkipArchitectures(c *CLI, skipArch ArchitectureType) {
	clusterArch := ClusterArchitecture(c)
	if clusterArch == string(skipArch) {
		skipMsg := fmt.Sprintf("Test not applicable for architecture: %s", skipArch)
		e2e.Warningf("SKIPPING TEST: %s", skipMsg)
		g.Skip(skipMsg)
	}
}

// ClusterArchitecture returns the cluster architecture
func ClusterArchitecture(c *CLI) string {
	e2e.Logf("Detecting cluster architecture")
	output, err := c.AsAdmin().WithoutNamespace().Run("get").Args("infrastructure", "cluster", "-o=jsonpath={.status.cpuArchitecture}").Output()
	if err != nil {
		e2e.Warningf("Failed to get cluster architecture: %v", err)
		return "unknown"
	}
	arch := strings.TrimSpace(output)

	// Check if multi-arch
	nodesOutput, err := c.AsAdmin().WithoutNamespace().Run("get").Args("nodes", "-o=jsonpath={.items[*].status.nodeInfo.architecture}").Output()
	if err == nil {
		archs := strings.Fields(nodesOutput)
		archMap := make(map[string]bool)
		for _, a := range archs {
			archMap[a] = true
		}
		if len(archMap) > 1 {
			e2e.Logf("Cluster architecture: Multi-arch")
			return string(MULTI)
		}
	}

	e2e.Logf("Cluster architecture: %s", arch)
	return arch
}

// ClusterInfra platform types and functions

// Platform type constants
type PlatformType string

const (
	AWS          PlatformType = "AWS"
	Azure                     = "Azure"
	GCP                       = "GCP"
	VSphere                   = "VSphere"
	Nutanix                   = "Nutanix"
	IBMCloud                  = "IBMCloud"
	AlibabaCloud              = "AlibabaCloud"
)

// SkipTestIfSupportedPlatformNotMatched skips test if platform doesn't match
func SkipTestIfSupportedPlatformNotMatched(c *CLI, supportedPlatforms ...PlatformType) {
	currentPlatform := CheckPlatform(c)

	for _, platform := range supportedPlatforms {
		if strings.EqualFold(currentPlatform, string(platform)) {
			return // Platform matches, don't skip
		}
	}

	// No match found, skip the test
	skipMsg := fmt.Sprintf("Test not applicable for platform: %s", currentPlatform)
	e2e.Warningf("SKIPPING TEST: %s", skipMsg)
	g.Skip(skipMsg)
}

// ControlplaneInfo ...
type ControlplaneInfo struct {
	HolderIdentity       string `json:"holderIdentity"`
	LeaseDurationSeconds int    `json:"leaseDurationSeconds"`
	AcquireTime          string `json:"acquireTime"`
	RenewTime            string `json:"renewTime"`
	LeaderTransitions    int    `json:"leaderTransitions"`
}

type serviceInfo struct {
	serviceIP   string
	namespace   string
	servicePort string
	serviceURL  string
	serviceName string
}

type registry struct {
	dockerImage string
	namespace   string
}

type podMirror struct {
	name            string
	namespace       string
	cliImageID      string
	imagePullSecret string
	imageSource     string
	imageTo         string
	imageToRelease  string
	template        string
}

type debugPodUsingDefinition struct {
	name       string
	namespace  string
	cliImageID string
	template   string
}

type priorityPod struct {
	dName      string
	namespace  string
	replicaSum int
	template   string
}

func getRandomString() string {
	chars := "abcdefghijklmnopqrstuvwxyz0123456789"
	seed := rand.New(rand.NewSource(time.Now().UnixNano()))
	buffer := make([]byte, 8)
	for index := range buffer {
		buffer[index] = chars[seed.Intn(len(chars))]
	}
	return string(buffer)
}

func (registry *registry) createregistry(oc *CLI) serviceInfo {
	e2e.Logf("Creating registry server from image %s in namespace %s", registry.dockerImage, registry.namespace)
	err := oc.AsAdmin().WithoutNamespace().Run("new-app").Args("--image", registry.dockerImage, "REGISTRY_STORAGE_DELETE_ENABLED=true", "--import-mode=PreserveOriginal", "-n", registry.namespace).Execute()
	if err != nil {
		e2e.Failf("Failed to create the registry server: %v", err)
	}
	err = oc.AsAdmin().WithoutNamespace().Run("set").Args("probe", "deploy/registry", "--readiness", "--liveness", "--get-url="+"http://:5000/v2", "-n", registry.namespace).Execute()
	if err != nil {
		e2e.Failf("Failed to config the registry: %v", err)
	}
	e2e.Logf("Waiting for registry pods to be running in namespace %s", registry.namespace)
	if ok := waitForAvailableRsRunning(oc, "deployment", "registry", registry.namespace, "1"); ok {
		e2e.Logf("Registry pods are running in namespace %s", registry.namespace)
	} else {
		e2e.Failf("private registry pod is not running even afer waiting for about 3 minutes")
	}

	e2e.Logf("Getting service info for the registry in namespace %s", registry.namespace)
	regSvcIP, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("svc", "registry", "-n", registry.namespace, "-o=jsonpath={.spec.clusterIP}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	_, err = oc.AsAdmin().WithoutNamespace().Run("create").Args("route", "edge", "my-route", "--service=registry", "-n", registry.namespace).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	regSvcPort, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("svc", "registry", "-n", registry.namespace, "-o=jsonpath={.spec.ports[0].port}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	regRoute, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", "my-route", "-n", registry.namespace, "-o=jsonpath={.spec.host}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())

	regSvcURL := regSvcIP + ":" + regSvcPort
	svc := serviceInfo{
		serviceIP:   regSvcIP,
		namespace:   registry.namespace,
		servicePort: regSvcPort,
		serviceURL:  regSvcURL,
		serviceName: regRoute,
	}
	return svc

}

func (registry *registry) deleteregistry(oc *CLI) {
	e2e.Logf("Deleting registry resources in namespace %s", registry.namespace)
	_ = oc.WithoutNamespace().Run("delete").Args("svc", "registry", "-n", registry.namespace).Execute()
	_ = oc.WithoutNamespace().Run("delete").Args("deploy", "registry", "-n", registry.namespace).Execute()
	_ = oc.WithoutNamespace().Run("delete").Args("is", "registry", "-n", registry.namespace).Execute()
}

func (pod *podMirror) createPodMirror(oc *CLI) {
	err := wait.Poll(5*time.Second, 20*time.Second, func() (bool, error) {
		err1 := nonAdminApplyResourceFromTemplate(oc, "--ignore-unknown-parameters=true", "-f", pod.template, "-p", "NAME="+pod.name, "NAMESPACE="+pod.namespace, "CLIIMAGEID="+pod.cliImageID, "IMAGEPULLSECRET="+pod.imagePullSecret, "IMAGESOURCE="+pod.imageSource, "IMAGETO="+pod.imageTo, "IMAGETORELEASE="+pod.imageToRelease)
		if err1 != nil {
			e2e.Logf("the err:%v, and try next round", err1)
			return false, nil
		}
		return true, nil
	})
	AssertWaitPollNoErr(err, fmt.Sprintf("pod %s with %s is not created successfully", pod.name, pod.cliImageID))
}

func getCliImage(oc *CLI) string {
	e2e.Logf("Getting CLI image from openshift namespace")
	cliImage, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("imagestreams", "cli", "-n", "openshift", "-o=jsonpath={.spec.tags[0].from.name}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("CLI image: %s", cliImage)
	return cliImage
}

func checkMustgatherPodNode(oc *CLI) {
	var nodeNameList []string
	e2e.Logf("Get the node list of the must-gather pods running on")
	err := wait.Poll(5*time.Second, 20*time.Second, func() (bool, error) {
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-l", "app=must-gather", "-A", "-o=jsonpath={.items[*].spec.nodeName}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		nodeNameList = strings.Fields(output)
		if nodeNameList == nil {
			e2e.Logf("Can't find must-gather pod now, and try next round")
			return false, nil
		}
		return true, nil
	})
	AssertWaitPollNoErr(err, fmt.Sprintf("must-gather pod is not created successfully"))
	e2e.Logf("must-gather scheduled on: %v", nodeNameList)

	e2e.Logf("make sure all the nodes in nodeNameList are not windows node")
	expectedNodeLabels := getScanNodesLabels(oc, nodeNameList, "windows")
	if expectedNodeLabels == nil {
		e2e.Logf("must-gather scheduled as expected, no windows node found in the cluster")
	} else {
		e2e.Failf("Scheduled the must-gather pod to windows node: %v", expectedNodeLabels)
	}
}

func (pod *debugPodUsingDefinition) createDebugPodUsingDefinition(oc *CLI) {
	err := wait.Poll(5*time.Second, 20*time.Second, func() (bool, error) {
		outputFile, err1 := applyResourceFromTemplate48681(oc, "--ignore-unknown-parameters=true", "-f", pod.template, "-p", "NAME="+pod.name, "NAMESPACE="+pod.namespace, "CLIIMAGEID="+pod.cliImageID)
		if err1 != nil {
			e2e.Logf("the err:%v, and try next round", err1)
			return false, nil
		}
		e2e.Logf("Waiting for pod running")
		err := wait.PollImmediate(5*time.Second, 1*time.Minute, func() (bool, error) {
			phase, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", pod.name, "--template", "{{.status.phase}}", "-n", pod.namespace).Output()
			if err != nil {
				return false, nil
			}
			if phase != "Running" {
				return false, nil
			}
			return true, nil
		})
		if err != nil {
			e2e.Logf("Error waiting for pod to be in 'Running' phase: %v", err)
			return false, nil
		}

		debugPod, err := oc.Run("debug").Args("-f", outputFile).Output()
		if err != nil {
			e2e.Logf("Error running 'debug' command: %v", err)
			return false, nil
		}
		if match, _ := regexp.MatchString("Starting pod/pod48681-debug", debugPod); !match {
			e2e.Failf("Image debug container is being started instead of debug pod using the pod definition yaml file")
		}
		return true, nil
	})
	if err != nil {
		e2e.Failf("Error creating debug pod: %v", err)
	}
}

func createDeployment(oc *CLI, namespace string, deployname string) {
	err := oc.WithoutNamespace().Run("create").Args("-n", namespace, "deployment", deployname, "--image=quay.io/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83", "--replicas=20").Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

func triggerSucceedDeployment(oc *CLI, namespace string, deployname string, num int, expectedPods int) {
	var generation string
	var getGenerationerr error
	err := wait.Poll(3*time.Second, 60*time.Second, func() (bool, error) {
		generation, getGenerationerr = oc.AsAdmin().WithoutNamespace().Run("get").Args("deploy", deployname, "-n", namespace, "-o=jsonpath={.status.observedGeneration}").Output()
		if getGenerationerr != nil {
			e2e.Logf("Err Occurred, try again: %v", getGenerationerr)
			return false, nil
		}
		if generation == "" {
			e2e.Logf("Can't get generation, try again: %v", generation)
			return false, nil
		}
		return true, nil
	})
	AssertWaitPollNoErr(err, fmt.Sprintf("Failed to get  generation "))

	generationNum, err := strconv.Atoi(generation)
	o.Expect(err).NotTo(o.HaveOccurred())
	for i := 0; i < num; i++ {
		generationNum++
		err := oc.WithoutNamespace().Run("set").Args("-n", namespace, "env", "deployment", deployname, "paramtest=test"+strconv.Itoa(i)).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		_, currentRsName := getCurrentRs(oc, namespace, "app="+deployname, generationNum)
		err = wait.Poll(5*time.Second, 120*time.Second, func() (bool, error) {
			availablePodNum, errGet := oc.WithoutNamespace().Run("get").Args("-n", namespace, "rs", currentRsName, "-o=jsonpath='{.status.availableReplicas}'").Output()
			if errGet != nil {
				e2e.Logf("Err Occurred: %v", errGet)
				return false, errGet
			}
			availableNum, _ := strconv.Atoi(strings.ReplaceAll(availablePodNum, "'", ""))
			if availableNum != expectedPods {
				e2e.Logf("new triggered apps not deploy successfully, wait more times")
				return false, nil
			}
			return true, nil
		})
		AssertWaitPollNoErr(err, fmt.Sprintf("failed to deploy %v", deployname))

	}
}
func triggerFailedDeployment(oc *CLI, namespace string, deployname string) {
	patchYaml := `[{"op": "replace", "path": "/spec/template/spec/containers/0/image", "value": "quay.io/openshifttest/hello-openshift:nonexist"}]`
	err := oc.WithoutNamespace().Run("patch").Args("-n", namespace, "deployment", deployname, "--type=json", "-p", patchYaml).Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

func getShouldPruneRSFromPrune(oc *CLI, pruneRsNumCMD string, pruneRsCMD string, prunedNum int) []string {
	e2e.Logf("Get pruned rs name by dry-run")
	e2e.Logf("pruneRsNumCMD %v:", pruneRsNumCMD)
	err := wait.Poll(5*time.Second, 300*time.Second, func() (bool, error) {
		pruneRsNum, err := exec.Command("bash", "-c", pruneRsNumCMD).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		pruneNum, err := strconv.Atoi(strings.ReplaceAll(string(pruneRsNum), "\n", ""))
		o.Expect(err).NotTo(o.HaveOccurred())
		if pruneNum != prunedNum {
			e2e.Logf("pruneNum is not equal %v: ", prunedNum)
			return false, nil
		}
		return true, nil
	})
	AssertWaitPollNoErr(err, fmt.Sprintf("Check pruned RS failed"))

	e2e.Logf("pruneRsCMD %v:", pruneRsCMD)
	pruneRsName, err := exec.Command("bash", "-c", pruneRsCMD).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	pruneRsList := strings.Fields(strings.ReplaceAll(string(pruneRsName), "\n", " "))
	sort.Strings(pruneRsList)
	e2e.Logf("pruneRsList %v:", pruneRsList)
	return pruneRsList
}

func getCompeletedRsInfo(oc *CLI, namespace string, deployname string) (completedRsList []string, completedRsNum int) {
	out, err := oc.WithoutNamespace().Run("get").Args("-n", namespace, "rs", "--sort-by={.metadata.creationTimestamp}", "-o=jsonpath='{.items[?(@.spec.replicas == 0)].metadata.name}'").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("string out %v:", out)
	totalCompletedRsList := strings.Fields(strings.ReplaceAll(out, "'", ""))
	totalCompletedRsListNum := len(totalCompletedRsList)
	return totalCompletedRsList, totalCompletedRsListNum
}

func getShouldPruneRSFromCreateTime(totalCompletedRsList []string, totalCompletedRsListNum int, keepNum int) []string {
	rsList := totalCompletedRsList[0:(totalCompletedRsListNum - keepNum)]
	sort.Strings(rsList)
	e2e.Logf("rsList %v:", rsList)
	return rsList

}

func comparePrunedRS(rsList []string, pruneRsList []string) bool {
	e2e.Logf("Check pruned rs whether right")
	if !reflect.DeepEqual(rsList, pruneRsList) {
		return false
	}
	return true
}

func checkRunningRsList(oc *CLI, namespace string, deployname string) []string {
	e2e.Logf("Get all the running RSs")
	out, err := oc.WithoutNamespace().Run("get").Args("-n", namespace, "rs", "-o=jsonpath='{.items[?(@.spec.replicas > 0)].metadata.name}'").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	runningRsList := strings.Fields(strings.ReplaceAll(out, "'", ""))
	sort.Strings(runningRsList)
	e2e.Logf("runningRsList %v:", runningRsList)
	return runningRsList
}

func pruneCompletedRs(oc *CLI, parameters ...string) {
	e2e.Logf("Delete all the completed RSs")
	err := oc.AsAdmin().WithoutNamespace().Run("adm").Args(parameters...).Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

func getRemainingRs(oc *CLI, namespace string, deployname string) []string {
	e2e.Logf("Get all the remaining RSs")
	remainRs, err := oc.WithoutNamespace().Run("get").Args("rs", "-l", "app="+deployname, "-n", namespace, "-o=jsonpath={.items[*].metadata.name}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	remainRsList := strings.Fields(string(remainRs))
	sort.Strings(remainRsList)
	e2e.Logf("remainRsList %v:", remainRsList)
	return remainRsList
}

func checkPodStatus(oc *CLI, podLabel string, namespace string, expected string) {
	err := wait.Poll(20*time.Second, 300*time.Second, func() (bool, error) {
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", namespace, "-l", podLabel, "-o=jsonpath={.items[*].status.phase}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("the result of pod:%v", output)
		if strings.Contains(output, expected) && (!(strings.Contains(strings.ToLower(output), "error"))) && (!(strings.Contains(strings.ToLower(output), "crashLoopbackOff"))) {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", namespace, "-l", podLabel, "-o", "yaml").Execute()
	}
	AssertWaitPollNoErr(err, fmt.Sprintf("the state of pod with %s is not expected %s", podLabel, expected))
}

func checkNetworkType(oc *CLI) string {
	e2e.Logf("Checking cluster network type")
	output, _ := oc.WithoutNamespace().AsAdmin().Run("get").Args("network.operator", "cluster", "-o=jsonpath={.spec.defaultNetwork.type}").Output()
	networkType := strings.ToLower(output)
	e2e.Logf("Cluster network type: %s", networkType)
	return networkType
}

func getLatestPayload(url string) string {
	res, err := http.Get(url)
	if err != nil {
		e2e.Failf("unable to get http with error: %v", err)
	}
	body, err := ioutil.ReadAll(res.Body)
	defer res.Body.Close()
	if err != nil {
		e2e.Failf("unable to parse the http result with error: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		e2e.Failf("unable to parse JSON with error: %v", err)
	}
	pullSpec, _ := data["pullSpec"].(string)
	return pullSpec
}

func assertPodOutput(oc *CLI, podLabel string, namespace string, expected string) {
	err := wait.PollUntilContextTimeout(context.Background(), 1*time.Minute, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		podStatus, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", namespace, "-l", podLabel).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("the result of pod:%v", podStatus)
		if strings.Contains(podStatus, expected) {
			return true, nil
		} else {
			podDesp, err := oc.AsAdmin().WithoutNamespace().Run("describe").Args("pods", "-n", namespace, "-l", podLabel).Output()
			e2e.Logf("the details of pod: %v", podDesp)
			o.Expect(err).NotTo(o.HaveOccurred())
			return false, nil
		}
	})
	AssertWaitPollNoErr(err, fmt.Sprintf("the state of pod with %s is not expected %s", podLabel, expected))
}

// this function is used to check whether proxy is configured or not
// As restart the microshift service, the debug node pod will quit with error

// get cluster resource name list
// Check if BaselineCapabilities have been set to None
func isBaselineCapsSet(oc *CLI, component string) bool {
	baselineCapabilitySet, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("clusterversion", "version", "-o=jsonpath={.spec.capabilities.baselineCapabilitySet}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("baselineCapabilitySet parameters: %v\n", baselineCapabilitySet)
	return strings.Contains(baselineCapabilitySet, component)
}

// Check if component is listed in clusterversion.status.capabilities.enabledCapabilities
func isEnabledCapability(oc *CLI, component string) bool {
	enabledCapabilities, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("clusterversion", "-o=jsonpath={.items[*].status.capabilities.enabledCapabilities}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("Cluster enabled capability parameters: %v\n", enabledCapabilities)
	return strings.Contains(enabledCapabilities, component)
}

// this function is used to check whether openshift-samples installed or not
// WaitForDeploymentPodsToBeReady waits for the specific deployment to be ready
// make sure the PVC is Bound to the PV
// wait for DC to be ready
func getClusterRegion(oc *CLI) string {
	e2e.Logf("Getting cluster region")
	node := getWorkersList(oc)[0]
	region, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("node", node, "-o=jsonpath={.metadata.labels.failure-domain\\.beta\\.kubernetes\\.io\\/region}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("Cluster region: %s", region)
	return region
}

// skipIfDisconnected skips the test if the cluster is disconnected/airgapped.
// This is useful for ConnectedOnly tests that require external network access.
// Works across all platforms (AWS, Azure, GCP, bare metal, etc.)
// Uses multiple detection methods:
// 1. Quick check: AWS C2S/SC2S regions (us-iso prefix)
// 2. Actual connectivity test: curl to quay.io from worker node
func skipIfDisconnected(oc *CLI) {
	e2e.Logf("Checking if cluster is disconnected")

	// Fast path: Check for AWS C2S/SC2S disconnected regions
	region := getClusterRegion(oc)
	if strings.HasPrefix(region, "us-iso") {
		skipMsg := fmt.Sprintf("Skipping ConnectedOnly test: AWS C2S/SC2S disconnected region (%s)", region)
		e2e.Warningf("SKIPPING TEST: %s", skipMsg)
		g.Skip(skipMsg)
	}

	// Actual connectivity test: Try to reach public internet from worker node
	e2e.Logf("Testing actual connectivity to public internet")
	workerNodes := getWorkersList(oc)
	if len(workerNodes) == 0 {
		e2e.Logf("Warning: No worker nodes found, assuming cluster is connected")
		return
	}
	workNode := workerNodes[0]

	curlCMD := "curl -I https://quay.io --connect-timeout 10"
	output, err := DebugNodeWithOptionsAndChroot(oc, workNode, []string{}, curlCMD)

	if !strings.Contains(output, "HTTP") || err != nil {
		skipMsg := "Skipping ConnectedOnly test: cluster cannot access public internet (disconnected/airgapped)"
		e2e.Logf("Unable to access quay.io from worker node %s. Output: %s, Error: %v", workNode, output, err)
		e2e.Warningf("SKIPPING TEST: %s", skipMsg)
		g.Skip(skipMsg)
	}

	e2e.Logf("Successfully verified cluster has public internet connectivity (quay.io accessible) - test will proceed")
}

// skipIfMicroShift skips the test if running on a MicroShift cluster.
// Use this for tests that are not compatible with MicroShift.
func skipIfMicroShift(oc *CLI) {
	// Try to get a node to check - first try master nodes, then worker nodes
	// MicroShift clusters may only have worker-labeled nodes
	var nodeToCheck string
	masterNodes, err := GetMasterNodes(oc)
	if err == nil && len(masterNodes) > 0 {
		nodeToCheck = masterNodes[0]
	} else {
		workerNodes := getWorkersList(oc)
		if len(workerNodes) > 0 {
			nodeToCheck = workerNodes[0]
		} else {
			// Cannot determine, assume not MicroShift
			return
		}
	}

	// Check if microshift binary exists on the node
	// We use "which microshift" - if successful, it returns the path (e.g., /usr/bin/microshift)
	// If not found, it returns an error message like "which: no microshift in..."
	output, err := DebugNodeWithOptionsAndChroot(oc, nodeToCheck, []string{"-q"}, "which microshift")
	// MicroShift is detected only if the command succeeds (err == nil) and returns a valid path
	if err == nil && strings.HasPrefix(strings.TrimSpace(output), "/") {
		// MicroShift detected
		skipMsg := "Skipping test: not supported on MicroShift cluster"
		e2e.Warningf("SKIPPING TEST: %s", skipMsg)
		g.Skip(skipMsg)
	}
}

func assertPullSecret(oc *CLI) bool {
	dirName := "/tmp/" + GetRandomString()
	err := os.MkdirAll(dirName, 0o755)
	o.Expect(err).NotTo(o.HaveOccurred())
	defer os.RemoveAll(dirName)
	err = oc.AsAdmin().WithoutNamespace().Run("extract").Args("secret/pull-secret", "-n", "openshift-config", "--to", dirName, "--confirm").Execute()
	o.Expect(err).NotTo(o.HaveOccurred())

	oauthFilePath := dirName + "/.dockerconfigjson"
	secretContent, err := exec.Command("bash", "-c", fmt.Sprintf("cat %v", oauthFilePath)).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	if matched, _ := regexp.MatchString("registry.ci.openshift.org", string(secretContent)); !matched {
		return false
	} else {
		return true
	}
}

func getSpecificFileName(fileDir string, pattern string) []string {
	files, err := ioutil.ReadDir(fileDir)
	o.Expect(err).NotTo(o.HaveOccurred())

	var matchingFiles []string
	e2e.Logf("the origin files %v", files)
	for _, file := range files {
		match, err := regexp.MatchString(pattern, string(file.Name()))
		o.Expect(err).NotTo(o.HaveOccurred())
		if match {
			matchingFiles = append(matchingFiles, string(file.Name()))
		}
	}
	e2e.Logf("the result files %v", matchingFiles)
	o.Expect(len(matchingFiles) > 0).To(o.BeTrue())
	return matchingFiles
}

func sha256File(fileName string) (string, error) {
	file, err := os.Open(fileName)
	o.Expect(err).NotTo(o.HaveOccurred())
	defer file.Close()
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	o.Expect(err).NotTo(o.HaveOccurred())
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func getSha256SumFromFile(fileName string) string {
	var fileSum string
	content, err := ioutil.ReadFile(fileName)
	o.Expect(err).NotTo(o.HaveOccurred())
	lines := strings.Split(string(content), "\n")
	for _, v := range lines {
		trimline := strings.TrimSpace(v)
		if strings.Contains(trimline, "openshift-install") {
			fileSum = strings.Fields(trimline)[0]
			o.Expect(fileSum).NotTo(o.BeEmpty())
		}
	}
	return fileSum
}

func waitCRDAvailable(oc *CLI, crdName string) error {
	e2e.Logf("Waiting for CRD %s to be available", crdName)
	err := wait.Poll(5*time.Second, 60*time.Second, func() (bool, error) {
		err := oc.AsAdmin().WithoutNamespace().Run("get").Args("crd", crdName).Execute()
		if err != nil {
			e2e.Logf("The crd with name %v still not ready, please try again", crdName)
			return false, nil
		}
		e2e.Logf("CRD %s is now available", crdName)
		return true, nil
	})
	return err
}

func waitCreateCr(oc *CLI, crFileName string, namespace string) error {
	e2e.Logf("Waiting to create CR from file %s in namespace %s", crFileName, namespace)
	err := wait.Poll(20*time.Second, 300*time.Second, func() (bool, error) {
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", crFileName, "-n", namespace).Execute()
		if err != nil {
			e2e.Logf("The cr with file %v created failed, please try again", crFileName)
			return false, nil
		}
		e2e.Logf("CR from file %s created successfully in namespace %s", crFileName, namespace)
		return true, nil
	})
	return err
}

// CatalogResponse matches the JSON structure of the /v2/_catalog endpoint.
type CatalogResponse struct {
	Repositories []string `json:"repositories"`
}

// TagsResponse matches the JSON structure of the /v2/<repo>/tags/list endpoint.
type TagsResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

func createEmptyAuth(authfilepath string) {
	authF, err := os.Create(authfilepath)
	o.Expect(err).NotTo(o.HaveOccurred())
	defer authF.Close()
	authContent := fmt.Sprintf(`{}`)
	authW := bufio.NewWriter(authF)
	_, werr := authW.WriteString(authContent)
	authW.Flush()
	o.Expect(werr).NotTo(o.HaveOccurred())
}

func checkFileContent(filename string, expectedStr string) bool {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		e2e.Failf("failed to read the file ")
	}
	s := string(b)
	if strings.Contains(s, expectedStr) {
		return true
	} else {
		return false
	}
}

func checkOcPlatform(oc *CLI) string {
	ocVersion, err := oc.Run("version").Args("--client", "-o", "yaml").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	if strings.Contains(ocVersion, "amd64") {
		return "amd64"
	} else if strings.Contains(ocVersion, "arm64") {
		return "arm64"
	} else if strings.Contains(ocVersion, "s390x") {
		return "s390x"
	} else if strings.Contains(ocVersion, "ppc64le") {
		return "ppc64le"
	} else {
		return "Unknown platform"
	}

}

type AuthEntry struct {
	Auth string `json:"auth"`
}
type AuthsData struct {
	Auths map[string]AuthEntry `json:"auths"`
}

func waitForAvailableRsRunning(oc *CLI, resourceType string, resourceName string, namespace string, expectedReplicas string) bool {
	e2e.Logf("Waiting for %s %s in namespace %s to have %s available replicas", resourceType, resourceName, namespace, expectedReplicas)
	err := wait.Poll(5*time.Second, 5*time.Minute, func() (bool, error) {
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args(resourceType, resourceName, "-n", namespace, "-o=jsonpath={.status.availableReplicas}").Output()
		if err != nil {
			return false, nil
		}
		if strings.TrimSpace(output) == expectedReplicas {
			e2e.Logf("%s %s in namespace %s has reached %s available replicas", resourceType, resourceName, namespace, expectedReplicas)
			return true, nil
		}
		return false, nil
	})
	return err == nil
}

// Dummy client types for stub methods
// Expanded dummy types with required methods
// Update dummy client methods to return proper types
// Dummy resource types with Get method
type dummyCoreV1 struct{}

func (d *dummyCoreV1) Pods(string) interface{} { return nil }

// Update dummy types to return proper resource types
type dummyAppsV1Updated struct{}

// Dummy deployment and statefulset types with Spec and Status
type dummyDeploymentSpec struct {
	Replicas *int32
}
type dummyDeploymentStatus struct {
	Replicas          int32
	UpdatedReplicas   int32
	AvailableReplicas int32
}
type dummyDeployment struct {
	Spec   dummyDeploymentSpec
	Status dummyDeploymentStatus
}

type dummyStatefulSetStatus struct {
	Replicas int32
}
type dummyStatefulSet struct {
	Status dummyStatefulSetStatus
}

// Update resource getter to return proper types
type dummyDeploymentsFinal struct{}

type dummyStatefulSetsFinal struct{}

// Add missing fields and methods
type dummyStatefulSetSpec struct {
	Replicas *int32
}
type dummyStatefulSetStatusFinal struct {
	Replicas      int32
	ReadyReplicas int32
}
type dummyStatefulSetFinal struct {
	Spec   dummyStatefulSetSpec
	Status dummyStatefulSetStatusFinal
}

type dummyPods struct{}

func (d *dummyPods) List(context.Context, metav1.ListOptions) (interface{}, error) { return nil, nil }

type dummyCoreV1Final struct{}

func (d *dummyCoreV1Final) Pods(string) *dummyPods { return &dummyPods{} }

type dummyAppsV1Ultimate struct{}

// Final missing fields
type dummyPodList struct {
	Items []interface{}
}

type dummyKubeClientComplete struct{}

func (d *dummyKubeClientComplete) AppsV1() *dummyAppsV1Ultimate { return &dummyAppsV1Ultimate{} }

func (d *dummyAppsV1Ultimate) Deployments(string) *dummyDeploymentsFinal {
	return &dummyDeploymentsFinal{}
}

func (d *dummyDeploymentsFinal) Get(ctx context.Context, name string, opts metav1.GetOptions) (*dummyDeployment, error) {
	return &dummyDeployment{
		Spec: dummyDeploymentSpec{
			Replicas: new(int32),
		},
		Status: dummyDeploymentStatus{
			AvailableReplicas: 0,
		},
	}, nil
}

// Helper functions for internal use
func applyResourceFromTemplate(oc *CLI, args ...string) error {
	e2e.Logf("Processing and applying template with args: %v", args)
	output, err := oc.Run("process").Args(args...).Output()
	if err != nil {
		e2e.Errorf("Failed to process template: %v", err)
		return err
	}

	// Apply the processed template
	e2e.Logf("Applying processed template")
	e2e.Logf("Processed template content:\n%s", output)
	cmd := exec.Command(oc.execPath, "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(output)
	if oc.namespace != "" {
		cmd.Args = append(cmd.Args, "-n", oc.namespace)
	}
	if oc.kubeconfig != "" {
		cmd.Env = append(os.Environ(), "KUBECONFIG="+oc.kubeconfig)
	}
	applyOutput, err := cmd.CombinedOutput()
	if err != nil {
		e2e.Errorf("Failed to apply template: %v\nCommand output: %s\nTemplate was:\n%s", err, string(applyOutput), output)
	}
	return err
}

func applyResourceFromTemplate48681(oc *CLI, args ...string) (string, error) {
	e2e.Logf("Processing template with args: %v", args)
	output, err := oc.Run("process").Args(args...).Output()
	if err != nil {
		e2e.Errorf("Failed to process template: %v", err)
		return "", err
	}

	// Create temp file with output
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("resource-%d.yaml", time.Now().UnixNano()))
	if err := ioutil.WriteFile(tmpFile, []byte(output), 0644); err != nil {
		return "", err
	}

	// Apply the processed template
	e2e.Logf("Applying processed template from file: %s", tmpFile)
	cmd := exec.Command(oc.execPath, "apply", "-f", tmpFile)
	if oc.namespace != "" {
		cmd.Args = append(cmd.Args, "-n", oc.namespace)
	}
	if oc.kubeconfig != "" {
		cmd.Env = append(os.Environ(), "KUBECONFIG="+oc.kubeconfig)
	}
	output2, err := cmd.CombinedOutput()
	if err != nil {
		e2e.Errorf("Failed to apply template: %v, output: %s", err, string(output2))
		return "", err
	}
	return tmpFile, nil
}

func nonAdminApplyResourceFromTemplate(oc *CLI, args ...string) error {
	return applyResourceFromTemplate(oc, args...)
}

func getScanNodesLabels(oc *CLI, nodeList []string, expected string) []string {
	var matchedLabelsNodeNames []string
	for _, nodeName := range nodeList {
		nodeLabels, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("node", nodeName, "-o=jsonpath={.metadata.labels}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if matched, _ := regexp.MatchString(expected, nodeLabels); matched {
			matchedLabelsNodeNames = append(matchedLabelsNodeNames, nodeName)
		}
	}
	return matchedLabelsNodeNames
}

func getCurrentRs(oc *CLI, namespace string, selector string, generation int) (int, string) {
	// Get the current ReplicaSet matching the selector
	output, err := oc.WithoutNamespace().Run("get").Args("-n", namespace, "rs", "-l", selector, "-o", "jsonpath={.items[?(@.metadata.annotations.deployment\\.kubernetes\\.io/revision==\""+strconv.Itoa(generation)+"\")].metadata.name}").Output()
	if err != nil {
		return 0, ""
	}
	rsName := strings.TrimSpace(output)
	if rsName == "" {
		// Fallback to getting latest rs
		output, err = oc.WithoutNamespace().Run("get").Args("-n", namespace, "rs", "-l", selector, "--sort-by=.metadata.creationTimestamp", "-o", "jsonpath={.items[-1].metadata.name}").Output()
		if err != nil {
			return 0, ""
		}
		rsName = strings.TrimSpace(output)
	}
	return generation, rsName
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func getWorkersList(oc *CLI) []string {
	e2e.Logf("Getting list of worker nodes")
	nodes, err := GetClusterNodesBy(oc, "worker")
	if err != nil {
		e2e.Warningf("Failed to get worker nodes: %v", err)
		return []string{}
	}
	e2e.Logf("Found %d worker node(s)", len(nodes))
	return nodes
}

// CreateSpecifiedNamespaceAsAdmin creates specified name namespace.
func (c *CLI) CreateSpecifiedNamespaceAsAdmin(namespace string) {
	err := c.AsAdmin().WithoutNamespace().Run("create").Args("namespace", namespace).Execute()
	o.Expect(err).NotTo(o.HaveOccurred(), fmt.Sprintf("Failed to create namespace/%s", namespace))
}

// DeleteSpecifiedNamespaceAsAdmin deletes specified name namespace.
func (c *CLI) DeleteSpecifiedNamespaceAsAdmin(namespace string) {
	err := c.AsAdmin().WithoutNamespace().Run("delete").Args("namespace", namespace).Execute()
	o.Expect(err).NotTo(o.HaveOccurred(), fmt.Sprintf("Failed to delete namespace/%s", namespace))
}
