package nodeimage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"
	"k8s.io/klog/v2"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	kutils "k8s.io/client-go/util/exec"
	"k8s.io/kubectl/pkg/cmd/exec"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
	"sigs.k8s.io/yaml"

	configclient "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/openshift/library-go/pkg/operator/resource/retry"
	ocrelease "github.com/openshift/oc/pkg/cli/admin/release"
	imagemanifest "github.com/openshift/oc/pkg/cli/image/manifest"
	"github.com/openshift/oc/pkg/cli/rsync"
)

const (
	nodeJoinerConfigurationFile       = "nodes-config.yaml"
	nodeJoinerContainer               = "node-joiner"
	nodeJoinerMinimumSupportedVersion = "4.17"
)

const (
	snFlagMacAddress        = "mac-address"
	snFlagCpuArch           = "cpu-architecture"
	snFlagSshKeyPath        = "ssh-key-path"
	snFlagHostname          = "hostname"
	snFlagRootDeviceHint    = "root-device-hint"
	snFlagNetworkConfigPath = "network-config-path"
)

var (
	createLong = templates.LongDesc(`
		Create an ISO image from an initial configuration for a given set of nodes,
		to add them to an existing on-prem cluster.

		This command creates a pod in a temporary namespace on the target cluster
		to retrieve the required information for creating a customized ISO image.
		The downloaded ISO image could then be used to boot a previously selected
		set of nodes, and add them to the target cluster in a fully automated way.

		The command also requires a connection to the target cluster, and a valid
		registry credentials to retrieve the required information from the target
		cluster release.

		A nodes-config.yaml config file must be created to provide the required
		initial configuration for the selected nodes.
		Alternatively, to support simpler configurations for adding just a single
		node, it's also possible to use a set of flags to configure the host. In
		such case the '--mac-address' is the only mandatory flag - while all the
		others will be optional (note: any eventual configuration file present
		will be ignored).
	`)

	createExample = templates.Examples(`
		# Create the ISO image and download it in the current folder
		  oc adm node-image create

		# Use a different assets folder
		  oc adm node-image create --dir=/tmp/assets

		# Specify a custom image name
		  oc adm node-image create -o=my-node.iso

		# In place of an ISO, creates files that can be used for PXE boot
		  oc adm node-image create --pxe

		# Create an ISO to add a single node without using the configuration file
		  oc adm node-image create --mac-address=00:d8:e7:c7:4b:bb

		# Create an ISO to add a single node with a root device hint and without
		# using the configuration file
		  oc adm node-image create --mac-address=00:d8:e7:c7:4b:bb --root-device-hint=deviceName:/dev/sda
	`)

	createCommand = "oc adm node-image create"
)

// NewCreate creates the command for generating the add nodes ISO.
func NewCreate(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewCreateOptions(streams)
	cmd := &cobra.Command{
		Use:     "create",
		Short:   "Create an ISO image for booting the nodes to be added to the target cluster",
		Long:    createLong,
		Example: createExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}
	o.AddFlags(cmd)

	return cmd
}

// NewCreateOptions creates the options for the create command
func NewCreateOptions(streams genericiooptions.IOStreams) *CreateOptions {
	return &CreateOptions{
		BaseNodeImageCommand: BaseNodeImageCommand{
			IOStreams: streams,
			command:   createCommand,
		},
	}
}

type CreateOptions struct {
	BaseNodeImageCommand

	FSys         fs.FS
	copyStrategy func(*rsync.RsyncOptions) rsync.CopyStrategy

	// AssetsDir it's used to specify the folder used to fetch the configuration
	// file, and to download the generated image.
	AssetsDir string
	// OutputName allows the user to specify the name of the generated image.
	OutputName string
	// GeneratePXEFiles generates files for PXE boot instead of an ISO
	GeneratePXEFiles bool

	// Simpler interface for creating a single node
	SingleNodeOpts *singleNodeCreateOptions

	nodeJoinerExitCode int
	rsyncRshCmd        string
}

type singleNodeCreateOptions struct {
	MacAddress        string
	CPUArchitecture   string
	SSHKeyPath        string
	Hostname          string
	RootDeviceHints   string
	NetworkConfigPath string
}

// AddFlags defined the required command flags.
func (o *CreateOptions) AddFlags(cmd *cobra.Command) {
	flags := o.addBaseFlags(cmd)

	flags.StringVar(&o.AssetsDir, "dir", o.AssetsDir, "The path containing the configuration file, used also to store the generated artifacts.")
	flags.StringVarP(&o.OutputName, "output-name", "o", "", "The name of the output image.")
	flags.BoolVarP(&o.GeneratePXEFiles, "pxe", "p", false, "Instead of an ISO, create files that can be used for PXE boot")

	flags.StringP(snFlagMacAddress, "m", "", "Single node flag. MAC address used to identify the host to apply the configuration. If specified, the nodes-config.yaml config file will not be used.")
	usageFmt := "Single node flag. %s. Valid only when `mac-address` is defined."
	flags.StringP(snFlagCpuArch, "c", "", fmt.Sprintf(usageFmt, "The CPU architecture to be used to install the node"))
	flags.StringP(snFlagSshKeyPath, "k", "", fmt.Sprintf(usageFmt, "Path to the SSH key used to access the node"))
	flags.String(snFlagHostname, "", fmt.Sprintf(usageFmt, "The hostname to be set for the node"))
	flags.String(snFlagRootDeviceHint, "", fmt.Sprintf(usageFmt, "Hint for specifying the storage location for the image root filesystem. Format accepted is <hint name>:<value>."))
	flags.String(snFlagNetworkConfigPath, "", fmt.Sprintf(usageFmt, "YAML file containing the NMState configuration to be applied for the node"))
}

// Complete completes the required options for the create command.
func (o *CreateOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	err := o.baseComplete(f)
	if err != nil {
		return nil
	}

	if o.AssetsDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		o.AssetsDir = cwd
	}
	o.FSys = os.DirFS(o.AssetsDir)
	o.remoteExecutor = &exec.DefaultRemoteExecutor{}
	o.rsyncRshCmd = rsync.DefaultRsyncRemoteShellToUse(cmd)
	o.copyStrategy = func(o *rsync.RsyncOptions) rsync.CopyStrategy {
		return rsync.NewDefaultCopyStrategy(o)
	}

	return o.completeSingleNodeOptions(cmd)
}

func (o *CreateOptions) completeSingleNodeOptions(cmd *cobra.Command) error {
	snOpts := &singleNodeCreateOptions{}

	if err := o.setSingleNodeFlag(cmd, snFlagMacAddress, &snOpts.MacAddress); err != nil {
		return err
	}

	for name, field := range map[string]*string{
		snFlagCpuArch:           &snOpts.CPUArchitecture,
		snFlagSshKeyPath:        &snOpts.SSHKeyPath,
		snFlagHostname:          &snOpts.Hostname,
		snFlagRootDeviceHint:    &snOpts.RootDeviceHints,
		snFlagNetworkConfigPath: &snOpts.NetworkConfigPath,
	} {
		if err := o.setSingleNodeFlag(cmd, name, field); err != nil {
			return err
		}
		if *field != "" && snOpts.MacAddress == "" {
			return fmt.Errorf("found flag `%s` configured, but it requires also flag `%s` to be set", name, snFlagMacAddress)
		}
	}

	if snOpts.MacAddress != "" {
		o.SingleNodeOpts = snOpts
	}
	return nil
}

func (o *CreateOptions) setSingleNodeFlag(cmd *cobra.Command, flagName string, dst *string) error {
	v, err := cmd.Flags().GetString(flagName)
	if err != nil {
		return err
	}
	*dst = v
	return nil
}

// Validate returns validation errors related to the create command.
func (o *CreateOptions) Validate() error {
	// Validate the configuration file only if there isn't any
	// single node flags set.
	if o.SingleNodeOpts == nil {
		err := o.validateConfigFile()
		if err != nil {
			return err
		}
	}

	return nil
}

func (o *CreateOptions) validateConfigFile() error {
	// Check if configuration file exists
	fi, err := fs.Stat(o.FSys, nodeJoinerConfigurationFile)
	if err != nil {
		return err
	}
	// Check if it's a valid yaml
	data, err := fs.ReadFile(o.FSys, nodeJoinerConfigurationFile)
	if err != nil {
		return err
	}
	var yamlData interface{}
	err = yaml.Unmarshal(data, &yamlData)
	if err != nil {
		return fmt.Errorf("config file %s is not valid: %w", fi.Name(), err)
	}
	return nil
}

// Run creates a temporary namespace to kick-off a pod for running the node-joiner
// cli tool. If the command is successfull, it will download the generated image
// from the pod.
func (o *CreateOptions) Run() error {
	ctx := context.Background()
	defer o.cleanup(ctx)

	tasks := []func(context.Context) error{
		o.getNodeJoinerPullSpec,
		o.createNamespace,
		o.createServiceAccount,
		o.createRolesAndBindings,
		o.createInputConfigMap,
		o.createPod,
	}
	err := o.runNodeJoinerPod(ctx, tasks)
	if err != nil {
		return err
	}

	err = o.waitForCompletion(ctx)
	// Something went wrong during the node-joiner tool execution,
	// let's show the logs and return an error
	if err != nil || o.nodeJoinerExitCode != 0 {
		printErr := o.printLogsInPod(ctx)
		if printErr != nil {
			return printErr
		}
		return fmt.Errorf("image generation error: %v (exit code: %d)", err, o.nodeJoinerExitCode)
	}

	err = o.copyArtifactsFromNodeJoinerPod()
	if err != nil {
		return err
	}

	err = o.renameImageIfOutputNameIsSpecified()
	if err != nil {
		return err
	}

	klog.V(1).Info("Command successfully completed")
	return nil
}

func (o *CreateOptions) printLogsInPod(ctx context.Context) error {
	klog.V(1).Info("Printing pod logs")
	logOptions := &corev1.PodLogOptions{
		Container:  nodeJoinerContainer,
		Timestamps: true,
	}
	readCloser, err := o.Client.CoreV1().Pods(o.nodeJoinerNamespace.GetName()).GetLogs(o.nodeJoinerPod.GetName(), logOptions).Stream(ctx)
	if err != nil {
		return err
	}
	defer readCloser.Close()

	_, err = io.Copy(o.IOStreams.ErrOut, readCloser)
	return err
}

func (o *CreateOptions) copyArtifactsFromNodeJoinerPod() error {
	klog.V(2).Infof("Copying artifacts from %s", o.nodeJoinerPod.GetName())
	rsyncOptions := &rsync.RsyncOptions{
		Namespace:     o.nodeJoinerNamespace.GetName(),
		Source:        &rsync.PathSpec{PodName: o.nodeJoinerPod.GetName(), Path: "/assets/"},
		ContainerName: nodeJoinerContainer,
		Destination:   &rsync.PathSpec{PodName: "", Path: o.AssetsDir},
		Client:        o.Client,
		Config:        o.Config,
		Compress:      true,
		RshCmd:        fmt.Sprintf("%s --namespace=%s -c %s", o.rsyncRshCmd, o.nodeJoinerNamespace.GetName(), nodeJoinerContainer),
		IOStreams:     o.IOStreams,
		Quiet:         true,
		RsyncInclude:  []string{"*.iso"},
		RsyncExclude:  []string{"*"},
	}
	if o.GeneratePXEFiles {
		rsyncOptions.RsyncInclude = []string{"boot-artifacts/*"}
		rsyncOptions.RsyncExclude = []string{}
	}
	rsyncOptions.Strategy = o.copyStrategy(rsyncOptions)
	return rsyncOptions.RunRsync()
}

func (o *CreateOptions) renameImageIfOutputNameIsSpecified() error {
	if o.OutputName == "" {
		return nil
	}
	// AssetDir doesn't exist in unit test fake filesystem
	_, err := os.Stat(o.AssetsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		} else {
			return err
		}
	}

	err = filepath.Walk(o.AssetsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && info.Name() != o.OutputName && strings.HasSuffix(info.Name(), ".iso") {
			newPath := filepath.Join(filepath.Dir(path), o.OutputName)

			// Check if another file has the same name
			if _, err := os.Stat(newPath); err == nil {
				return fmt.Errorf("file already exists: %s", newPath)
			}

			err := os.Rename(path, newPath)
			if err != nil {
				return err
			} else {
				return nil
			}
		}

		return nil
	})

	if err != nil {
		return err
	}
	return nil
}

func (o *CreateOptions) waitForCompletion(ctx context.Context) error {
	klog.V(2).Infof("Starting command in pod %s", o.nodeJoinerPod.GetName())
	// Wait for the node-joiner pod to come up
	err := o.waitForContainerRunning(ctx)
	if err != nil {
		return err
	}

	// Wait for the node-joiner cli tool to complete
	return wait.PollUntilContextTimeout(
		ctx,
		time.Second*5,
		time.Minute*15,
		true,
		func(ctx context.Context) (done bool, err error) {
			w := &bytes.Buffer{}
			wErr := &bytes.Buffer{}

			execOptions := &exec.ExecOptions{
				StreamOptions: exec.StreamOptions{
					Namespace:     o.nodeJoinerNamespace.GetName(),
					PodName:       o.nodeJoinerPod.GetName(),
					ContainerName: nodeJoinerContainer,
					IOStreams: genericiooptions.IOStreams{
						In:     nil,
						Out:    w,
						ErrOut: wErr,
					},
					Stdin: false,
					Quiet: false,
				},
				Executor:  o.remoteExecutor,
				PodClient: o.Client.CoreV1(),
				Config:    o.Config,
				Command: []string{
					"cat", "/assets/exit_code",
				},
			}

			err = execOptions.Validate()
			if err != nil {
				return false, err
			}

			klog.V(1).Info("Image generation in progress, please wait")
			err = execOptions.Run()
			if err != nil {
				var codeExitErr kutils.CodeExitError
				if !errors.As(err, &codeExitErr) {
					return false, err
				}
				if codeExitErr.Code != 1 {
					return false, fmt.Errorf("unexpected error code: %w", codeExitErr)
				}
				return false, nil
			}

			// Extract node-joiner tool exit code on completion
			o.nodeJoinerExitCode, err = strconv.Atoi(w.String())
			if err != nil {
				return false, err
			}
			return true, nil
		})
}

func (o *CreateOptions) createConfigFileFromFlags() ([]byte, error) {
	host := map[string]interface{}{}

	if o.SingleNodeOpts.MacAddress != "" {
		host["interfaces"] = []map[string]interface{}{
			{
				"name":       "eth0",
				"macAddress": o.SingleNodeOpts.MacAddress,
			},
		}
	}
	if o.SingleNodeOpts.Hostname != "" {
		host["hostname"] = o.SingleNodeOpts.Hostname
	}
	if o.SingleNodeOpts.CPUArchitecture != "" {
		host["cpuArchitecture"] = o.SingleNodeOpts.CPUArchitecture
	}
	if o.SingleNodeOpts.SSHKeyPath != "" {
		sshKeyData, err := fs.ReadFile(o.FSys, o.SingleNodeOpts.SSHKeyPath)
		if err != nil {
			return nil, err
		}
		host["sshKey"] = string(sshKeyData)
	}
	if o.SingleNodeOpts.RootDeviceHints != "" {
		parts := strings.SplitN(o.SingleNodeOpts.RootDeviceHints, ":", 2)
		host["rootDeviceHints"] = map[string]interface{}{
			parts[0]: parts[1],
		}
	}
	if o.SingleNodeOpts.NetworkConfigPath != "" {
		networkConfigData, err := fs.ReadFile(o.FSys, o.SingleNodeOpts.NetworkConfigPath)
		if err != nil {
			return nil, err
		}
		var networkConfig map[string]interface{}
		err = yaml.Unmarshal(networkConfigData, &networkConfig)
		if err != nil {
			return nil, err
		}
		host["networkConfig"] = networkConfig
	}

	config := map[string]interface{}{
		"hosts": []map[string]interface{}{
			host,
		},
	}

	return yaml.Marshal(&config)
}

func (o *CreateOptions) createInputConfigMap(ctx context.Context) error {
	var data []byte
	var err error

	if o.SingleNodeOpts != nil {
		klog.V(2).Info("Single node flags found, ignoring configuration file.")
		data, err = o.createConfigFileFromFlags()
	} else {
		data, err = fs.ReadFile(o.FSys, nodeJoinerConfigurationFile)
	}
	if err != nil {
		return err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodes-config",
			Namespace: o.nodeJoinerNamespace.GetName(),
		},
		Data: map[string]string{
			nodeJoinerConfigurationFile: string(data),
		},
	}

	_, err = o.Client.CoreV1().ConfigMaps(o.nodeJoinerNamespace.GetName()).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create configmap: %w", err)
	}

	return nil
}

func (o *CreateOptions) nodeJoinerCommand() string {
	if o.GeneratePXEFiles {
		return "node-joiner add-nodes --pxe"
	}
	return "node-joiner add-nodes"
}

func (o *CreateOptions) createPod(ctx context.Context) error {
	assetsVolSize := resource.MustParse("4Gi")
	nodeJoinerPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-joiner-",
			Labels: map[string]string{
				"app": "node-joiner",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:      corev1.RestartPolicyNever,
			ServiceAccountName: o.nodeJoinerServiceAccount.GetName(),
			SecurityContext: &corev1.PodSecurityContext{
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "nodes-config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "nodes-config",
							},
						},
					},
				},
				{
					Name: "assets",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{
							SizeLimit: &assetsVolSize,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            nodeJoinerContainer,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Image:           o.nodeJoinerImage,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "nodes-config",
							MountPath: "/config",
						},
						{
							Name:      "assets",
							MountPath: "/assets",
						},
					},
					Command: []string{
						"/bin/bash", "-c",
						fmt.Sprintf("cp /config/%s /assets; HOME=/assets %s --dir=/assets --log-level=debug; sleep 600", nodeJoinerConfigurationFile, o.nodeJoinerCommand()),
					},
				},
			},
		},
	}

	err := o.configurePodProxySetting(ctx, nodeJoinerPod)
	if err != nil {
		return err
	}

	pod, err := o.Client.CoreV1().Pods(o.nodeJoinerNamespace.GetName()).Create(ctx, nodeJoinerPod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create pod: %w", err)
	}
	o.nodeJoinerPod = pod

	return nil
}

func (o *CreateOptions) configurePodProxySetting(ctx context.Context, pod *corev1.Pod) error {
	proxy, err := o.ConfigClient.ConfigV1().Proxies().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		if kapierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	proxyVars := []corev1.EnvVar{}
	if proxy.Status.HTTPProxy != "" {
		proxyVars = append(proxyVars, corev1.EnvVar{Name: "HTTP_PROXY", Value: proxy.Status.HTTPProxy})
	}
	if proxy.Status.HTTPSProxy != "" {
		proxyVars = append(proxyVars, corev1.EnvVar{Name: "HTTPS_PROXY", Value: proxy.Status.HTTPSProxy})
	}
	if proxy.Status.NoProxy != "" {
		proxyVars = append(proxyVars, corev1.EnvVar{Name: "NO_PROXY", Value: proxy.Status.NoProxy})
	}

	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, proxyVars...)
	}
	return nil
}

type BaseNodeImageCommand struct {
	genericiooptions.IOStreams
	SecurityOptions imagemanifest.SecurityOptions

	Config                   *rest.Config
	remoteExecutor           exec.RemoteExecutor
	ConfigClient             configclient.Interface
	Client                   kubernetes.Interface
	nodeJoinerImage          string
	nodeJoinerNamespace      *corev1.Namespace
	nodeJoinerServiceAccount *corev1.ServiceAccount
	nodeJoinerRole           *rbacv1.ClusterRole
	RESTClientGetter         genericclioptions.RESTClientGetter
	nodeJoinerPod            *corev1.Pod
	command                  string
}

func (c *BaseNodeImageCommand) getNodeJoinerPullSpec(ctx context.Context) error {
	// Get the current cluster release version.
	releaseImage, err := c.fetchClusterReleaseImage(ctx)
	if err != nil {
		return err
	}

	// Extract the baremetal-installer image pullspec, since it
	// provide the node-joiner tool.
	opts := ocrelease.NewInfoOptions(c.IOStreams)
	opts.SecurityOptions = c.SecurityOptions
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
	cv, err := c.ConfigClient.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		if kapierrors.IsNotFound(err) || kapierrors.ReasonForError(err) == metav1.StatusReasonUnknown {
			klog.V(2).Infof("Unable to find cluster version object from cluster: %v", err)
			return "", fmt.Errorf("command expects a connection to an OpenShift 4.x server")
		}
	}
	// Adds a guardrail for node-image commands which is supported only for Openshift version 4.17 and later
	currentVersion := cv.Status.Desired.Version
	matches := regexp.MustCompile(`^(\d+[.]\d+)[.].*`).FindStringSubmatch(currentVersion)
	if len(matches) < 2 {
		return "", fmt.Errorf("failed to parse major.minor version from ClusterVersion status.desired.version %q", currentVersion)
	} else if matches[1] < nodeJoinerMinimumSupportedVersion {
		return "", fmt.Errorf("the 'oc adm node-image' command is only available for OpenShift versions %s and later", nodeJoinerMinimumSupportedVersion)
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

func (c *BaseNodeImageCommand) waitForContainerRunning(ctx context.Context) error {
	// Wait for the node-joiner pod to come up
	return wait.PollUntilContextTimeout(
		ctx,
		time.Second*1,
		time.Minute*5,
		true,
		func(ctx context.Context) (done bool, err error) {
			pod, err := c.Client.CoreV1().Pods(c.nodeJoinerNamespace.GetName()).Get(context.TODO(), c.nodeJoinerPod.GetName(), metav1.GetOptions{})
			if err == nil {
				klog.V(2).Info("Waiting for pod")
				if len(pod.Status.ContainerStatuses) == 0 {
					return false, nil
				}
				state := pod.Status.ContainerStatuses[0].State
				if state.Waiting != nil {
					switch state.Waiting.Reason {
					case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
						return true, fmt.Errorf("unable to pull image: %v: %v", state.Waiting.Reason, state.Waiting.Message)
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
					"imagecontentpolicies",
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
