package nodeimage

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"
	"k8s.io/klog/v2"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/cmd/exec"
	"sigs.k8s.io/yaml"

	ocpv1 "github.com/openshift/api/config/v1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	operatorclient "github.com/openshift/client-go/operator/clientset/versioned"
	"github.com/openshift/library-go/pkg/operator/resource/retry"
	ocrelease "github.com/openshift/oc/pkg/cli/admin/release"
	imagemanifest "github.com/openshift/oc/pkg/cli/image/manifest"
)

type BaseNodeImageCommand struct {
	genericiooptions.IOStreams
	SecurityOptions imagemanifest.SecurityOptions
	LogOut          io.Writer

	Config                   *rest.Config
	remoteExecutor           exec.RemoteExecutor
	ConfigClient             configclient.Interface
	OpenshiftOperatorClient  operatorclient.Interface
	Client                   kubernetes.Interface
	nodeJoinerImage          string
	nodeJoinerNamespace      *corev1.Namespace
	nodeJoinerServiceAccount *corev1.ServiceAccount
	nodeJoinerRole           *rbacv1.ClusterRole
	RESTClientGetter         genericclioptions.RESTClientGetter
	nodeJoinerPod            *corev1.Pod
	command                  string
}

func newBaseNodeImageCommand(streams genericiooptions.IOStreams, command, prefix string) *BaseNodeImageCommand {
	cmd := &BaseNodeImageCommand{
		IOStreams: streams,
		command:   command,
	}
	cmd.LogOut = cmd.newPrefixWriter(streams.Out, prefix)
	return cmd
}

func (c *BaseNodeImageCommand) newPrefixWriter(out io.Writer, prefix string) io.Writer {
	reader, writer := io.Pipe()
	scanner := bufio.NewScanner(reader)
	go func() {
		for scanner.Scan() {
			text := scanner.Text()
			ts := time.Now().UTC().Format(time.RFC3339)
			// In case of pod logs, capture only the relevant message portion
			if idx := strings.Index(text, "msg="); idx != -1 {
				text = text[idx+len("msg="):]
			}
			fmt.Fprintf(out, "%s [node-image %s] %s\n", ts, prefix, text)
		}
	}()
	return writer
}

func (c *BaseNodeImageCommand) log(format string, a ...interface{}) {
	fmt.Fprintf(c.LogOut, format+"\n", a...)
}

func (c *BaseNodeImageCommand) getNodeJoinerPullSpec(ctx context.Context) error {
	// Get the current cluster release version.
	releaseImage, err := c.fetchClusterReleaseImage(ctx)
	if err != nil {
		return err
	}

	// Extract the baremetal-installer image pullspec, since it
	// provides the node-joiner tool.

	// First attempt to get the installer image from the configMap created by installer. This will work in disconnected environments.
	installerImagesConfigMap, err := c.Client.CoreV1().ConfigMaps("openshift-config").Get(ctx, "installer-images", metav1.GetOptions{})
	if err == nil {
		images := installerImagesConfigMap.Data["images.json"]
		if len(images) > 0 {
			imageMap := make(map[string]string)
			err := yaml.Unmarshal([]byte(images), &imageMap)
			if err == nil {
				installer, exists := imageMap["installer"]
				if exists {
					// Found pullSpec from configMap
					c.log("installer pullspec obtained from installer-images configMap %s", installer)
					c.nodeJoinerImage = installer
					return nil
				}
			}
		}
	}

	// If cannot obtain installer image from configMap, get it from the releaseImage.
	// Note that after OCP 4.20, this should not be necessary since the configMap was added to installer in 4.19.
	c.log("configMap containing installer-images is not available, trying to get image from registry")
	opts := ocrelease.NewInfoOptions(c.IOStreams)
	opts.SecurityOptions = c.SecurityOptions
	idmsFile, err := c.getIdmsFile(ctx)
	if err != nil {
		return err
	}
	opts.IDMSFile = idmsFile
	defer func() {
		if opts.IDMSFile != "" {
			os.Remove(opts.IDMSFile)
		}
	}()

	release, err := opts.LoadReleaseInfo(releaseImage, false)
	if err != nil {
		return err
	}

	tagName := "baremetal-installer"
	for _, tag := range release.References.Spec.Tags {
		if tag.Name == tagName {
			c.nodeJoinerImage = tag.From.Name
			return nil
		}
	}

	return fmt.Errorf("no image tag %q exists in the release image %s", tagName, releaseImage)
}

func (c *BaseNodeImageCommand) fetchClusterReleaseImage(ctx context.Context) (string, error) {
	cv, err := c.getCurrentClusterVersion(ctx)
	if err != nil {
		return "", err
	}

	image := cv.Status.Desired.Image
	if len(image) == 0 && cv.Spec.DesiredUpdate != nil {
		image = cv.Spec.DesiredUpdate.Image
	}
	if len(image) == 0 {
		return "", fmt.Errorf("the server is not reporting a release image at this time")
	}

	return image, nil
}

func (c *BaseNodeImageCommand) getCurrentClusterVersion(ctx context.Context) (*ocpv1.ClusterVersion, error) {
	cv, err := c.ConfigClient.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		if kapierrors.IsNotFound(err) || kapierrors.ReasonForError(err) == metav1.StatusReasonUnknown {
			klog.V(2).Infof("Unable to find cluster version object from cluster: %v", err)
			return nil, fmt.Errorf("command expects a connection to an OpenShift 4.x server")
		}
	}
	return cv, nil
}

func (c *BaseNodeImageCommand) isClusterVersionLessThan(ctx context.Context, version string) (bool, error) {
	cv, err := c.getCurrentClusterVersion(ctx)
	if err != nil {
		return false, err
	}

	currentVersion := cv.Status.Desired.Version
	matches := regexp.MustCompile(`^(\d+[.]\d+)[.].*`).FindStringSubmatch(currentVersion)
	if len(matches) < 2 {
		return false, fmt.Errorf("failed to parse major.minor version from ClusterVersion status.desired.version %q", currentVersion)
	}
	return matches[1] < version, nil
}

// Adds a guardrail for node-image commands which is supported only for Openshift version 4.17 and later
func (c *BaseNodeImageCommand) checkMinSupportedVersion(ctx context.Context) error {
	notSupported, err := c.isClusterVersionLessThan(ctx, nodeJoinerMinimumSupportedVersion)
	if err != nil {
		return err
	}
	if notSupported {
		return fmt.Errorf("the 'oc adm node-image' command is only available for OpenShift versions %s and later", nodeJoinerMinimumSupportedVersion)
	}
	return nil
}

func (c *BaseNodeImageCommand) createNamespace(ctx context.Context) error {
	nsNodeJoiner := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "openshift-node-joiner-",
			Annotations: map[string]string{
				"oc.openshift.io/command":    c.command,
				"openshift.io/node-selector": "",
			},
		},
	}

	ns, err := c.Client.CoreV1().Namespaces().Create(ctx, nsNodeJoiner, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create namespace: %w", err)
	}

	c.nodeJoinerNamespace = ns
	return nil
}

func (c *BaseNodeImageCommand) cleanup(ctx context.Context) {
	if c.nodeJoinerNamespace == nil {
		return
	}

	err := c.Client.CoreV1().Namespaces().Delete(ctx, c.nodeJoinerNamespace.GetName(), metav1.DeleteOptions{})
	if err != nil {
		klog.Errorf("cannot delete namespace %s: %v\n", c.nodeJoinerNamespace.GetName(), err)
	}
}

func (c *BaseNodeImageCommand) createServiceAccount(ctx context.Context) error {
	nodeJoinerServiceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-joiner-",
			Annotations: map[string]string{
				"oc.openshift.io/command": c.command,
			},
			Namespace: c.nodeJoinerNamespace.GetName(),
		},
	}

	sa, err := c.Client.CoreV1().ServiceAccounts(c.nodeJoinerNamespace.GetName()).Create(ctx, nodeJoinerServiceAccount, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create service account: %w", err)
	}

	c.nodeJoinerServiceAccount = sa
	return nil
}

func (c *BaseNodeImageCommand) clusterRoleBindings() *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-joiner-monitor-",
			Annotations: map[string]string{
				"oc.openshift.io/command": c.command,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Namespace",
					Name:       c.nodeJoinerNamespace.GetName(),
					UID:        c.nodeJoinerNamespace.GetUID(),
				},
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      c.nodeJoinerServiceAccount.GetName(),
				Namespace: c.nodeJoinerNamespace.GetName(),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     c.nodeJoinerRole.GetName(),
		},
	}
}

func (c *BaseNodeImageCommand) waitForRunningPod(ctx context.Context) error {
	klog.V(2).Infof("Starting command in pod %s", c.nodeJoinerPod.GetName())
	// Wait for the node-joiner pod to come up
	return wait.PollUntilContextTimeout(
		ctx,
		time.Second*5,
		time.Minute*15,
		true,
		func(ctx context.Context) (done bool, err error) {
			klog.V(2).Infof("Waiting for running pod %s/%s", c.nodeJoinerNamespace.GetName(), c.nodeJoinerPod.GetName())
			pod, err := c.Client.CoreV1().Pods(c.nodeJoinerNamespace.GetName()).Get(context.TODO(), c.nodeJoinerPod.GetName(), metav1.GetOptions{})
			if err == nil {
				if len(pod.Status.ContainerStatuses) == 0 {
					return false, nil
				}
				state := pod.Status.ContainerStatuses[0].State
				if state.Waiting != nil {
					switch state.Waiting.Reason {
					case "InvalidImageName":
						return true, fmt.Errorf("unable to pull image: %v: %v", state.Waiting.Reason, state.Waiting.Message)
					case "ErrImagePull", "ImagePullBackOff":
						klog.V(1).Infof("Unable to pull image (%s), retrying", state.Waiting.Reason)
						return false, nil
					}
				}
				return state.Running != nil || state.Terminated != nil, nil
			}
			if retry.IsHTTPClientError(err) {
				return false, nil
			}
			return false, err
		})
}

func (c *BaseNodeImageCommand) createRolesAndBindings(ctx context.Context) error {
	nodeJoinerRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-joiner-",
			Annotations: map[string]string{
				"oc.openshift.io/command": c.command,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Namespace",
					Name:       c.nodeJoinerNamespace.GetName(),
					UID:        c.nodeJoinerNamespace.GetUID(),
				},
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"config.openshift.io",
				},
				Resources: []string{
					"clusterversions",
					"infrastructures",
					"proxies",
					"imagedigestmirrorsets",
				},
				Verbs: []string{
					"get",
					"list",
				},
			},
			{
				APIGroups: []string{
					"operator.openshift.io",
				},
				Resources: []string{
					"imagecontentsourcepolicies",
				},
				Verbs: []string{
					"get",
					"list",
				},
			},
			{
				APIGroups: []string{
					"machineconfiguration.openshift.io",
				},
				Resources: []string{
					"machineconfigs",
				},
				Verbs: []string{
					"get",
					"list",
				},
			},
			{
				APIGroups: []string{
					"certificates.k8s.io",
				},
				Resources: []string{
					"certificatesigningrequests",
				},
				Verbs: []string{
					"get",
					"list",
				},
			},
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"configmaps",
					"nodes",
					"pods",
					"nodes",
				},
				Verbs: []string{
					"get",
					"list",
				},
			},
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"secrets",
				},
				Verbs: []string{
					"get",
					"list",
					"create",
					"update",
				},
			},
		},
	}
	cr, err := c.Client.RbacV1().ClusterRoles().Create(ctx, nodeJoinerRole, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create role: %w", err)
	}
	c.nodeJoinerRole = cr

	_, err = c.Client.RbacV1().ClusterRoleBindings().Create(ctx, c.clusterRoleBindings(), metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create role binding: %w", err)
	}

	return nil
}

func (c *BaseNodeImageCommand) baseComplete(f genericclioptions.RESTClientGetter) error {
	c.RESTClientGetter = f

	var err error
	if c.Config, err = f.ToRESTConfig(); err != nil {
		return err
	}
	if c.Client, err = kubernetes.NewForConfig(c.Config); err != nil {
		return err
	}
	if c.ConfigClient, err = configclient.NewForConfig(c.Config); err != nil {
		return err
	}
	if c.OpenshiftOperatorClient, err = operatorclient.NewForConfig(c.Config); err != nil {
		return err
	}
	c.remoteExecutor = &exec.DefaultRemoteExecutor{}
	return nil
}

func (c *BaseNodeImageCommand) addBaseFlags(cmd *cobra.Command) *flag.FlagSet {
	f := cmd.Flags()
	c.SecurityOptions.Bind(f)
	return f
}

func (o *BaseNodeImageCommand) runNodeJoinerPod(ctx context.Context, tasks []func(context.Context) error) error {
	for _, task := range tasks {
		if err := task(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (c *BaseNodeImageCommand) getIdmsFile(ctx context.Context) (string, error) {
	imageDigestMirrorSets, idmsErr := c.ConfigClient.ConfigV1().ImageDigestMirrorSets().List(ctx, metav1.ListOptions{})
	if idmsErr != nil && !kapierrors.IsNotFound(idmsErr) {
		return "", idmsErr
	}
	imageContentSourcePolicies, icspErr := c.OpenshiftOperatorClient.OperatorV1alpha1().ImageContentSourcePolicies().List(ctx, metav1.ListOptions{})
	if icspErr != nil && !kapierrors.IsNotFound(icspErr) {
		return "", icspErr
	}
	if idmsErr != nil || len(imageDigestMirrorSets.Items) == 0 {
		imageDigestMirrorSets = nil // handle empty idms
	}
	if icspErr != nil || len(imageContentSourcePolicies.Items) == 0 {
		imageContentSourcePolicies = nil // handle empty icsp
	}
	if imageDigestMirrorSets == nil && imageContentSourcePolicies == nil {
		return "", nil
	}

	// Build IDMS file from contents of the IDMS and ICSP mirror digests
	contents, err := getIdmsContents(c.LogOut, imageDigestMirrorSets, imageContentSourcePolicies)
	if err != nil {
		c.log("failure parsing imageDigestMirrorSet %s", err.Error())
		return "", err
	}

	// create temp file which will be removed by caller
	idmsFile, err := os.CreateTemp("", "idms-file")
	if err != nil {
		return "", err
	}

	c.log("Building IDMS file with contents %s", contents)
	if _, err := idmsFile.Write(contents); err != nil {
		idmsFile.Close()
		os.Remove(idmsFile.Name())
		return "", err
	}
	idmsFile.Close()
	return idmsFile.Name(), nil
}
