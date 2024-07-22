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
	"k8s.io/kubectl/pkg/cmd/exec"
	"k8s.io/kubectl/pkg/cmd/logs"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	"k8s.io/kubectl/pkg/util/templates"
	utilsexec "k8s.io/utils/exec"

	configclient "github.com/openshift/client-go/config/clientset/versioned"
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

	monitorCommand = "oc adm node-image monitor"
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
			kcmdutil.CheckErr(o.Run(cmd.Context()))
		},
	}
	flags := cmd.Flags()
	o.SecurityOptions.Bind(flags)

	flags.StringVar(&o.IPAddressesToMonitor, "ip-addresses", "", "IP addresses of nodes to monitor.")
	return cmd
}

// NewMonitorOptions creates the options for the monitor command
func NewMonitorOptions(streams genericiooptions.IOStreams) *MonitorOptions {
	return &MonitorOptions{
		CommonOptions: CommonOptions{
			IOStreams: streams,
			command:   monitorCommand,
		},
	}
}

type MonitorOptions struct {
	CommonOptions

	IPAddressesToMonitor string
	updateLogsFn         func(*logs.LogsOptions) error
}

type ConsumeLog interface {
	Update(opts *logs.LogsOptions) error
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
	o.updateLogsFn = func(opts *logs.LogsOptions) error {
		return opts.RunLogs()
	}

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

	if err := o.waitForContainerRunning(ctx); err != nil {
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
			if err := o.updateLogsFn(opts); err != nil {
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

func (o *MonitorOptions) createRolesAndBindings(ctx context.Context) error {
	nodeJoinerRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-joiner-monitor-",
			Annotations: map[string]string{
				"oc.openshift.io/command": o.command,
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

	_, err = o.Client.RbacV1().ClusterRoleBindings().Create(ctx, o.clusterRoleBindings(), metav1.CreateOptions{})
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
						fmt.Sprintf("HOME=/assets node-joiner monitor-add-nodes --dir=/assets %s",
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
