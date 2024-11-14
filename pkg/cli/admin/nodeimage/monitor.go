package nodeimage

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/resource/retry"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/kubectl/pkg/cmd/logs"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	"k8s.io/kubectl/pkg/util/templates"
	utilsexec "k8s.io/utils/exec"
)

const (
	nodeJoinerMonitorContainer = "node-joiner-monitor"
)

var (
	monitorLong = templates.LongDesc(`
		Monitor nodes being added to a cluster using an image generated from
		the "oc adm node-image create" command.

		After the node image ISO has been booted on the host, the monitor command
		reports any pre-flight validations that may have failed impeding the
		host from being added to the cluster. If validations are successful, the
		node installation starts.

		Before a node joins the cluster and becomes fully functional, two
		certificate signing requests (CSRs) need to be approved. The monitor
		command will display CSRs pending your approval.

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
		# IP address with a comma
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
			kcmdutil.CheckErr(o.Run())
		},
	}
	o.AddFlags(cmd)

	return cmd
}

// AddFlags defined the required command flags.
func (o *MonitorOptions) AddFlags(cmd *cobra.Command) {
	flags := o.addBaseFlags(cmd)

	flags.StringVar(&o.IPAddressesToMonitor, "ip-addresses", "", "IP addresses of nodes to monitor.")
}

// NewMonitorOptions creates the options for the monitor command
func NewMonitorOptions(streams genericiooptions.IOStreams) *MonitorOptions {
	return &MonitorOptions{
		BaseNodeImageCommand: BaseNodeImageCommand{
			IOStreams: streams,
			command:   monitorCommand,
		},
	}
}

type MonitorOptions struct {
	BaseNodeImageCommand

	IPAddressesToMonitor string
	updateLogsFn         func(*logs.LogsOptions) error
}

// Complete completes the required options for the monitor command.
func (o *MonitorOptions) Complete(f genericclioptions.RESTClientGetter, cmd *cobra.Command, args []string) error {
	err := o.baseComplete(f)
	if err != nil {
		return err
	}

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
func (o *MonitorOptions) Run() error {
	ctx := context.Background()
	defer o.cleanup(ctx)

	tasks := []func(context.Context) error{
		o.checkMinSupportedVersion,
		o.getNodeJoinerPullSpec,
		o.createNamespace,
		o.createServiceAccount,
		o.createRolesAndBindings,
		o.createPod,
	}

	err := o.runNodeJoinerPod(ctx, tasks)
	if err != nil {
		return err
	}

	podName := o.nodeJoinerPod.GetName()

	if err := o.waitForRunningPod(ctx); err != nil {
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
	var pod *corev1.Pod
	var err error
	retries := 5
	for i := 0; i < retries; i++ {
		pod, err = o.Client.CoreV1().Pods(o.nodeJoinerNamespace.Name).Get(ctx, o.nodeJoinerPod.Name, metav1.GetOptions{})
		if err == nil {
			break
		} else {
			klog.V(2).Infof("could not get node-joiner pod: %v", err)
		}
	}
	if err != nil {
		klog.Errorf("could not get %v pod after %v retries: %v", o.nodeJoinerPod.Name, retries, err)
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
				klog.V(2).Infof("log update failed: %v", err)
			}

			return o.isMonitoringDone(ctx)
		})
}

func (o *MonitorOptions) isMonitoringDone(ctx context.Context) (bool, error) {
	pod, err := o.Client.CoreV1().Pods(o.nodeJoinerNamespace.Name).Get(ctx, o.nodeJoinerPod.Name, metav1.GetOptions{})
	if err != nil {
		// at this stage pod should exist, return false to retry if client error
		if retry.IsHTTPClientError(err) {
			return false, nil
		}
		klog.V(2).Infof("pod should exist but is not found: %v", err)
		return false, err
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
