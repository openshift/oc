package nodeimage

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/spf13/cobra"
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
	"k8s.io/kubectl/pkg/cmd/exec"
	"k8s.io/kubectl/pkg/cmd/logs"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	"k8s.io/kubectl/pkg/util/templates"
	utilsexec "k8s.io/utils/exec"

	configclient "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/openshift/library-go/pkg/operator/resource/retry"
	ocrelease "github.com/openshift/oc/pkg/cli/admin/release"
	imagemanifest "github.com/openshift/oc/pkg/cli/image/manifest"
)

const (
	nodeJoinerMonitorContainer = "node-joiner-monitor"
)

var (
	monitorLong = templates.LongDesc(`
		Monitor nodes being added to a cluster using an image generated from
		the "oc adm node-image create" command.

		Each node being added to the cluster has two certificate signing requests
		(CSRs) that need to be approved before they join the cluster and become 
		fully functional. The monitor command will display CSRs pending your
		approval.

		The command ends when the nodes have successfully joined the cluster.

		The command creates a pod in a temporary namespace on the target cluster
		to monitor the nodes.

		The command also requires a connection to the target cluster, and a valid
		registry credentials to retrieve the required information from the target
		cluster release.
	`)

	monitorExample = templates.Examples(`
		# Monitor a single node being added to a cluster
		  oc adm node-image monitor --ip-addresses 192.168.111.83

		# Monitor multiple nodes being added to a cluster by separating each
		  IP address with a comma
		  oc adm node-image monitor --ip-addresses 192.168.111.83,192.168.111.84
	`)
)

// NewMonitor creates the command for monitoring nodes being added to a cluster.
func NewMonitor(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewMonitorOptions(streams)
	cmd := &cobra.Command{
		Use:     "monitor",
		Short:   "Monitor new nodes being added to an OpenShift cluster",
		Long:    monitorLong,
		Example: monitorExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}
	flags := cmd.Flags()
	o.SecurityOptions.Bind(flags)

	flags.StringVar(&o.IPAddressesToMonitor, "ip-addresses", "", "IP addresses of nodes to monitor.")
	flags.StringVar(&o.LogLevel, "log-level", "info", "log level (e.g. \"debug | info | warn | error\") (default \"info\")")
	return cmd
}

// NewMonitorOptions creates the options for the monitor command
func NewMonitorOptions(streams genericiooptions.IOStreams) *MonitorOptions {
	return &MonitorOptions{
		IOStreams: streams,
	}
}

type MonitorOptions struct {
	genericiooptions.IOStreams
	SecurityOptions imagemanifest.SecurityOptions

	Config         *rest.Config
	Client         kubernetes.Interface
	ConfigClient   configclient.Interface
	remoteExecutor exec.RemoteExecutor

	IPAddressesToMonitor string
	LogLevel             string

	RESTClientGetter         genericclioptions.RESTClientGetter
	nodeJoinerImage          string
	nodeJoinerNamespace      *corev1.Namespace
	nodeJoinerServiceAccount *corev1.ServiceAccount
	nodeJoinerRole           *rbacv1.ClusterRole
	nodeJoinerPod            *corev1.Pod
}

// Complete completes the required options for the monitor command.
func (o *MonitorOptions) Complete(f genericclioptions.RESTClientGetter, cmd *cobra.Command, args []string) error {
	o.RESTClientGetter = f

	var err error
	if o.Config, err = f.ToRESTConfig(); err != nil {
		return err
	}
	if o.Client, err = kubernetes.NewForConfig(o.Config); err != nil {
		return err
	}
	if o.ConfigClient, err = configclient.NewForConfig(o.Config); err != nil {
		return err
	}
	o.remoteExecutor = &exec.DefaultRemoteExecutor{}

	return nil
}

// Validate returns validation errors related to the monitor command.
func (o *MonitorOptions) Validate() error {
	// Check an IP address is provided
	if o.IPAddressesToMonitor == "" {
		return fmt.Errorf("--ip-addresses cannot be empty")
	}

	for _, ip := range strings.Split(o.IPAddressesToMonitor, ",") {
		parsedIPAddress := net.ParseIP(ip)
		if parsedIPAddress == nil {
			return fmt.Errorf("%s is not valid IP address", ip)
		}
	}

	return nil
}

// Run creates a temporary namespace to kick-off a pod for running the node-joiner
// monitor cli tool. Logs from node-joiner monitor are streamed from the pod
// to stdout.
func (o *MonitorOptions) Run(ctx context.Context) error {
	defer o.cleanup(ctx)

	err := o.createNamespace(ctx)
	if err != nil {
		return nil
	}

	err = o.runNodeJoinerPod(ctx)
	if err != nil {
		return err
	}

	podName := o.nodeJoinerPod.GetName()

	if err := o.waitForMonitoringContainerRunning(ctx); err != nil {
		klog.Errorf("monitoring did not start: %s", err)
		return fmt.Errorf("monitoring did not start for pod %s: %s", podName, err)
	}

	if err := o.waitForMonitoringToComplete(ctx); err != nil {
		klog.Errorf("monitoring never finished: %v", err)
		if exiterr, ok := err.(*utilsexec.CodeExitError); ok {
			return exiterr
		}
		return fmt.Errorf("monitoring never finished for pod %s: %s", podName, err)
	}

	return nil
}

func (o *MonitorOptions) cleanup(ctx context.Context) {
	if o.nodeJoinerNamespace == nil {
		return
	}

	err := o.Client.CoreV1().Namespaces().Delete(ctx, o.nodeJoinerNamespace.GetName(), metav1.DeleteOptions{})
	if err != nil {
		klog.Errorf("cannot delete namespace %s: %v\n", o.nodeJoinerNamespace.GetName(), err)
	}
}

func (o *MonitorOptions) waitForMonitoringContainerRunning(ctx context.Context) error {
	// Wait for the node-joiner pod to come up
	return wait.PollUntilContextTimeout(
		ctx,
		time.Second*1,
		time.Minute*5,
		true,
		func(ctx context.Context) (done bool, err error) {
			pod, err := o.Client.CoreV1().Pods(o.nodeJoinerNamespace.GetName()).Get(context.TODO(), o.nodeJoinerPod.GetName(), metav1.GetOptions{})
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

func (o *MonitorOptions) waitForMonitoringToComplete(ctx context.Context) error {
	pod, err := o.Client.CoreV1().Pods(o.nodeJoinerNamespace.Name).Get(ctx, o.nodeJoinerPod.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	opts := &logs.LogsOptions{
		Options: &corev1.PodLogOptions{
			// tail the pod's log instead of printing the entire log every poll
			Follow: true,
		},
		RESTClientGetter: o.RESTClientGetter,
		Object:           pod,
		ConsumeRequestFn: logs.DefaultConsumeRequest,
		LogsForObject:    polymorphichelpers.LogsForObjectFn,
		IOStreams:        genericiooptions.IOStreams{Out: o.Out},
	}

	return wait.PollUntilContextTimeout(
		ctx,
		time.Second*5,
		time.Minute*90,
		true,
		func(ctx context.Context) (done bool, err error) {
			if err := opts.RunLogs(); err != nil {
				return false, err
			}

			return o.isMonitoringDone(ctx)
		})
}

func (o *MonitorOptions) isMonitoringDone(ctx context.Context) (bool, error) {
	pod, err := o.Client.CoreV1().Pods(o.nodeJoinerNamespace.Name).Get(ctx, o.nodeJoinerPod.Name, metav1.GetOptions{})
	if err != nil {
		// at this stage pod should exist, so error if not found
		if kapierrors.IsNotFound(err) {
			return true, err
		}
		return false, nil
	}
	var state *corev1.ContainerState
	for _, cstate := range pod.Status.ContainerStatuses {
		if cstate.Name == nodeJoinerMonitorContainer {
			state = &cstate.State
			break
		}
	}

	// missing status for monitor container => timeout in the worst case
	if state == nil {
		return false, nil
	}

	if state.Terminated != nil {
		if state.Terminated.ExitCode == 0 {
			return true, nil
		}
		return true, &utilsexec.CodeExitError{
			Err:  fmt.Errorf("%s/%s unexpectedly terminated: exit code: %v, reason: %s, message: %s", o.nodeJoinerNamespace.Name, o.nodeJoinerPod.Name, state.Terminated.ExitCode, state.Terminated.Reason, state.Terminated.Message),
			Code: int(state.Terminated.ExitCode),
		}
	}
	return false, nil
}

func (o *MonitorOptions) runNodeJoinerPod(ctx context.Context) error {
	tasks := []func(context.Context) error{
		o.getNodeJoinerPullSpec,
		o.createServiceAccount,
		o.createRolesAndBindings,
		o.createPod,
	}
	for _, task := range tasks {
		if err := task(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (o *MonitorOptions) getNodeJoinerPullSpec(ctx context.Context) error {
	// Get the current cluster release version.
	releaseImage, err := o.fetchClusterReleaseImage(ctx)
	if err != nil {
		return err
	}

	// Extract the baremetal-installer image pullspec, since it
	// provide the node-joiner tool.
	opts := ocrelease.NewInfoOptions(o.IOStreams)
	opts.SecurityOptions = o.SecurityOptions
	release, err := opts.LoadReleaseInfo(releaseImage, false)
	if err != nil {
		return err
	}

	tagName := "baremetal-installer"
	for _, tag := range release.References.Spec.Tags {
		if tag.Name == tagName {
			o.nodeJoinerImage = tag.From.Name
			return nil
		}
	}

	return fmt.Errorf("no image tag %q exists in the release image %s", tagName, releaseImage)
}

func (o *MonitorOptions) fetchClusterReleaseImage(ctx context.Context) (string, error) {
	cv, err := o.ConfigClient.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		if kapierrors.IsNotFound(err) || kapierrors.ReasonForError(err) == metav1.StatusReasonUnknown {
			klog.V(2).Infof("Unable to find cluster version object from cluster: %v", err)
			return "", fmt.Errorf("command expects a connection to an OpenShift 4.x server")
		}
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

func (o *MonitorOptions) createNamespace(ctx context.Context) error {
	nsNodeJoiner := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "openshift-node-joiner-monitor-",
			Annotations: map[string]string{
				"oc.openshift.io/command":    "oc adm node-image monitor",
				"openshift.io/node-selector": "",
			},
		},
	}

	ns, err := o.Client.CoreV1().Namespaces().Create(ctx, nsNodeJoiner, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create namespace: %w", err)
	}

	o.nodeJoinerNamespace = ns
	return nil
}

func (o *MonitorOptions) createServiceAccount(ctx context.Context) error {
	nodeJoinerServiceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-joiner-monitor-",
			Annotations: map[string]string{
				"oc.openshift.io/command": "oc adm node-image monitor",
			},
			Namespace: o.nodeJoinerNamespace.GetName(),
		},
	}

	sa, err := o.Client.CoreV1().ServiceAccounts(o.nodeJoinerNamespace.GetName()).Create(ctx, nodeJoinerServiceAccount, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create service account: %w", err)
	}

	o.nodeJoinerServiceAccount = sa
	return nil
}

func (o *MonitorOptions) createRolesAndBindings(ctx context.Context) error {
	nodeJoinerRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-joiner-monitor-",
			Annotations: map[string]string{
				"oc.openshift.io/command": "oc adm node-image monitor",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Namespace",
					Name:       o.nodeJoinerNamespace.GetName(),
					UID:        o.nodeJoinerNamespace.GetUID(),
				},
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"certificates.k8s.io",
				},
				Resources: []string{
					"certificatesigningrequests",
					"clusterversions",
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
					"pods",
					"nodes",
				},
				Verbs: []string{
					"get",
					"list",
				},
			},
		},
	}
	cr, err := o.Client.RbacV1().ClusterRoles().Create(ctx, nodeJoinerRole, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create role: %w", err)
	}
	o.nodeJoinerRole = cr

	nodeJoinerRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-joiner-monitor-",
			Annotations: map[string]string{
				"oc.openshift.io/command": "oc adm node-image monitor",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Namespace",
					Name:       o.nodeJoinerNamespace.GetName(),
					UID:        o.nodeJoinerNamespace.GetUID(),
				},
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      o.nodeJoinerServiceAccount.GetName(),
				Namespace: o.nodeJoinerNamespace.GetName(),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     o.nodeJoinerRole.GetName(),
		},
	}
	_, err = o.Client.RbacV1().ClusterRoleBindings().Create(ctx, nodeJoinerRoleBinding, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create role binding: %w", err)
	}

	return nil
}

func (o *MonitorOptions) createPod(ctx context.Context) error {
	assetsVolSize := resource.MustParse("4Gi")
	nodeJoinerPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-joiner-monitor-",
			Labels: map[string]string{
				"app": "node-joiner-monitor",
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
					Name:            nodeJoinerMonitorContainer,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Image:           o.nodeJoinerImage,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "assets",
							MountPath: "/assets",
						},
					},
					Command: []string{
						"/bin/bash", "-c",
						fmt.Sprintf("HOME=/assets node-joiner monitor-add-nodes --dir=/assets --log-level=%s %s",
							o.LogLevel,
							strings.ReplaceAll(o.IPAddressesToMonitor, ",", " ")),
					},
				},
			},
		},
	}
	pod, err := o.Client.CoreV1().Pods(o.nodeJoinerNamespace.GetName()).Create(ctx, nodeJoinerPod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create pod: %w", err)
	}
	o.nodeJoinerPod = pod

	return nil
}
