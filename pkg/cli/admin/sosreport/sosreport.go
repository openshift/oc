package sosreport

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	configclient "github.com/openshift/client-go/config/clientset/versioned"
	imagev1client "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	imagereference "github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/library-go/pkg/operator/resource/retry"
	"github.com/openshift/oc/pkg/cli/admin/inspect"
	"github.com/openshift/oc/pkg/cli/rsync"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/cmd/logs"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	sosReportLong = templates.LongDesc(`
		Launch a pod to collect a SOSReport from the provided node-name

		This command will launch a pod in a temporary namespace on your cluster that gathers
		a SOSReport from the provided node and then downloads the collected report.

		Experimental: This command is under active development and may change without notice.
	`)

	sosReportExample = templates.Examples(`
		# Collect a SOSReport using the default image configuration, writing the SOSReport into ./<host-name>.sosreport
		  oc adm sos
	`)
)

func NewSOSReportCommand(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewSOSReportOptions(streams)
	cmd := &cobra.Command{
		Use:     "sos",
		Short:   "Launch a new instance of a pod for collecting a SOSReport for a specific node.",
		Long:    sosReportLong,
		Example: sosReportExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().StringVar(&o.NodeName, "node-name", o.NodeName, "Set a specific node to use - by default a random master will be used")
	cmd.Flags().StringSliceVar(&o.Images, "image", o.Images, "Specify a must-gather plugin image to run. If not specified, OpenShift's default must-gather image will be used.")

	return cmd
}

func NewSOSReportOptions(streams genericclioptions.IOStreams) *SOSReportOptions {
	return &SOSReportOptions{
		SourceDir: "/tmp/sos-report",
		IOStreams: streams,
		LogOut:    newPrefixWriter(streams.Out, "[sosreport      ] OUT"),
		RawOut:    streams.Out,
		Timeout:   10 * time.Minute,
		Images:    []string{"registry.redhat.io/rhel8/support-tools:8.4-10"},
	}
}

func (o *SOSReportOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
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
	if o.ImageClient, err = imagev1client.NewForConfig(o.Config); err != nil {
		return err
	}
	if i := cmd.ArgsLenAtDash(); i != -1 && i < len(args) {
		o.Command = args[i:]
	} else {
		o.Command = args
	}
	if len(o.timeoutStr) > 0 {
		if strings.ContainsAny(o.timeoutStr, "smh") {
			o.Timeout, err = time.ParseDuration(o.timeoutStr)
			if err != nil {
				return fmt.Errorf(`invalid argument %q for "--timeout" flag: %v`, o.timeoutStr, err)
			}
		} else {
			fmt.Fprint(o.ErrOut, "Flag --timeout's value in seconds has been deprecated, use duration like 5s, 2m, or 3h, instead\n")
			i, err := strconv.ParseInt(o.timeoutStr, 0, 64)
			if err != nil {
				return fmt.Errorf(`invalid argument %q for "--timeout" flag: %v`, o.timeoutStr, err)
			}
			o.Timeout = time.Duration(i) * time.Second
		}
	}
	if len(o.DestDir) == 0 {
		// TODO: Add node-name to the directory-name
		o.DestDir = fmt.Sprintf("sosreport.%06d", rand.Int63())
	}
	o.PrinterCreated, err = printers.NewTypeSetter(scheme.Scheme).WrapToPrinter(&printers.NamePrinter{Operation: "created"}, nil)
	if err != nil {
		return err
	}
	o.PrinterDeleted, err = printers.NewTypeSetter(scheme.Scheme).WrapToPrinter(&printers.NamePrinter{Operation: "deleted"}, nil)
	if err != nil {
		return err
	}
	o.RsyncRshCmd = rsync.DefaultRsyncRemoteShellToUse(cmd)
	return nil
}

type SOSReportOptions struct {
	genericclioptions.IOStreams

	Config           *rest.Config
	Client           kubernetes.Interface
	ConfigClient     configclient.Interface
	ImageClient      imagev1client.ImageV1Interface
	RESTClientGetter genericclioptions.RESTClientGetter

	NodeName     string
	DestDir      string
	SourceDir    string
	Images       []string
	ImageStreams []string
	Command      []string
	Timeout      time.Duration
	timeoutStr   string
	Keep         bool

	RsyncRshCmd string

	PrinterCreated printers.ResourcePrinter
	PrinterDeleted printers.ResourcePrinter
	LogOut         io.Writer
	// RawOut is used for printing information we're looking to have copy/pasted into bugs
	RawOut io.Writer
}

func (o *SOSReportOptions) Validate() error {
	if len(o.Images) == 0 {
		return fmt.Errorf("missing an image")
	}
	return nil
}

// Run creates and runs a must-gather pod.d
func (o *SOSReportOptions) Run() error {
	var err error

	runBackCollection := true
	defer func() {
		if !runBackCollection {
			return
		}
		o.BackupGathering(context.TODO())
	}()

	// create namespace ...
	ns, err := o.Client.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "openshift-sosreport-",
			Labels: map[string]string{
				"openshift.io/run-level": "0",
			},
			Annotations: map[string]string{
				"oc.openshift.io/command":    "oc adm sos",
				"openshift.io/node-selector": "",
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	o.PrinterCreated.PrintObj(ns, o.LogOut)
	if !o.Keep {
		defer func() {
			if err := o.Client.CoreV1().Namespaces().Delete(context.TODO(), ns.Name, metav1.DeleteOptions{}); err != nil {
				fmt.Printf("%v\n", err)
				return
			}
			o.PrinterDeleted.PrintObj(ns, o.LogOut)
		}()
	}

	// ... and finally collection pod
	var pods []*corev1.Pod
	for _, image := range o.Images {
		_, err := imagereference.Parse(image)
		if err != nil {
			o.log("unable to parse image reference %s: %v", image, err)
			return err
		}

		pod, err := o.Client.CoreV1().Pods(ns.Name).Create(context.TODO(), o.newPod(o.NodeName, image), metav1.CreateOptions{})
		if err != nil {
			return err
		}
		o.log("pod for plug-in image %s created", image)
		pods = append(pods, pod)
	}

	// log timestamps...
	if err := os.MkdirAll(o.DestDir, os.ModePerm); err != nil {
		return err
	}
	if err := o.logTimestamp(); err != nil {
		return err
	}
	defer o.logTimestamp()

	var wg sync.WaitGroup
	wg.Add(len(pods))
	errCh := make(chan error, len(pods))
	for _, pod := range pods {
		go func(pod *corev1.Pod) {
			defer wg.Done()

			containerName := "sosreport"

			log := newPodOutLogger(o.Out, pod.Name)

			// wait for gather container to be running (gather is running)
			if err := o.waitForContainerRunning(pod, containerName); err != nil {
				log("gather did not start: %s", err)
				errCh <- fmt.Errorf("gather did not start for pod %s: %s", pod.Name, err)
				return
			}
			// stream gather container logs
			if err := o.getContainerLogs(pod, containerName); err != nil {
				log("gather logs unavailable: %v", err)
			}

			// wait for pod to be running (gather has completed)
			log("waiting for collection to complete")
			// TODO: Replace 'sosreport' with containername variable and pass to other waitForContainer function to clean up code
			if err := o.waitForCollectionToComplete(pod, containerName); err != nil {
				log("gather never finished: %v", err)
				errCh <- fmt.Errorf("gather never finished for pod %s: %s", pod.Name, err)
				return
			}

			// copy the gathered files into the local destination dir
			log("downloading sosreport output")
			pod, err = o.Client.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
			if err != nil {
				log("sosreport not downloaded: %v\n", err)
				errCh <- fmt.Errorf("unable to download output from pod %s: %s", pod.Name, err)
				return
			}
			if err := o.copyFilesFromPod(pod); err != nil {
				log("sosreport not downloaded: %v\n", err)
				errCh <- fmt.Errorf("unable to download output from pod %s: %s", pod.Name, err)
				return
			}
		}(pod)
	}
	wg.Wait()
	close(errCh)
	var errs []error
	for i := range errCh {
		errs = append(errs, i)
	}
	if len(errs) == 0 {
		// If we didn't have an error during collection, then we don't need to do our backup collection.
		runBackCollection = false

	} else if len(o.Command) > 0 {
		// If we had errors, but the user specified a command, he probably just typoed the command.
		// If the command was specified, don't run the backup collection.
		runBackCollection = false
	}

	return errors.NewAggregate(errs)

}

func newPodOutLogger(out io.Writer, podName string) func(string, ...interface{}) {
	writer := newPrefixWriter(out, fmt.Sprintf("[%s] OUT", podName))
	return func(format string, a ...interface{}) {
		fmt.Fprintf(writer, format+"\n", a...)
	}
}

func (o *SOSReportOptions) log(format string, a ...interface{}) {
	fmt.Fprintf(o.LogOut, format+"\n", a...)
}

func (o *SOSReportOptions) logTimestamp() error {
	f, err := os.OpenFile(path.Join(o.DestDir, "timestamp"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(fmt.Sprintf("%v\n", time.Now()))
	return err
}

func (o *SOSReportOptions) copyFilesFromPod(pod *corev1.Pod) error {
	streams := o.IOStreams
	streams.Out = newPrefixWriter(streams.Out, fmt.Sprintf("[%s] OUT", pod.Name))
	destDir := path.Join(o.DestDir, regexp.MustCompile("[^A-Za-z0-9]+").ReplaceAllString(pod.Status.ContainerStatuses[0].ImageID, "-"))
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}
	rsyncOptions := &rsync.RsyncOptions{
		Namespace:     pod.Namespace,
		Source:        &rsync.PathSpec{PodName: pod.Name, Path: path.Clean(o.SourceDir) + "/"},
		ContainerName: "copy",
		Destination:   &rsync.PathSpec{PodName: "", Path: destDir},
		Client:        o.Client,
		Config:        o.Config,
		Compress:      true,
		RshCmd:        fmt.Sprintf("%s --namespace=%s -c copy", o.RsyncRshCmd, pod.Namespace),
		IOStreams:     streams,
	}
	rsyncOptions.Strategy = rsync.NewDefaultCopyStrategy(rsyncOptions)
	err := rsyncOptions.RunRsync()
	if err != nil {
		klog.V(4).Infof("re-trying rsync after initial failure %v", err)
		// re-try copying data before letting it go
		err = rsyncOptions.RunRsync()
	}
	return err
}

func (o *SOSReportOptions) getContainerLogs(pod *corev1.Pod, containerName string) error {
	since2s := int64(2)
	opts := &logs.LogsOptions{
		Namespace:   pod.Namespace,
		ResourceArg: pod.Name,
		Options: &corev1.PodLogOptions{
			Follow:     true,
			Container:  containerName,
			Timestamps: true,
		},
		RESTClientGetter: o.RESTClientGetter,
		Object:           pod,
		ConsumeRequestFn: logs.DefaultConsumeRequest,
		LogsForObject:    polymorphichelpers.LogsForObjectFn,
		IOStreams:        genericclioptions.IOStreams{Out: newPrefixWriter(o.Out, fmt.Sprintf("[%s] POD", pod.Name))},
	}

	for {
		// collection script might take longer than the default API server time,
		// so we should check if the collection script still runs and re-run logs
		// thus we run this in a loop
		if err := opts.RunLogs(); err != nil {
			return err
		}

		// to ensure we don't print all of history set since to past 2 seconds
		opts.Options.(*corev1.PodLogOptions).SinceSeconds = &since2s
		if done, _ := o.isCollectionDone(pod, containerName); done {
			return nil
		}
		klog.V(4).Infof("lost logs, re-trying...")
	}
}

func newPrefixWriter(out io.Writer, prefix string) io.Writer {
	reader, writer := io.Pipe()
	scanner := bufio.NewScanner(reader)
	go func() {
		for scanner.Scan() {
			fmt.Fprintf(out, "%s %s\n", prefix, scanner.Text())
		}
	}()
	return writer
}

func (o *SOSReportOptions) waitForCollectionToComplete(pod *corev1.Pod, containerName string) error {
	return wait.PollImmediate(10*time.Second, o.Timeout, func() (bool, error) {
		return o.isCollectionDone(pod, containerName)
	})
}

func (o *SOSReportOptions) isCollectionDone(pod *corev1.Pod, containerName string) (bool, error) {
	var err error
	if pod, err = o.Client.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{}); err != nil {
		// at this stage pod should exist, we've been gathering container logs, so error if not found
		if kerrors.IsNotFound(err) {
			return true, err
		}
		return false, nil
	}
	var state *corev1.ContainerState
	for _, cstate := range pod.Status.ContainerStatuses {
		if cstate.Name == containerName {
			state = &cstate.State
			break
		}
	}

	// missing status for gather container => timeout in the worst case
	if state == nil {
		return false, nil
	}

	if state.Terminated != nil {
		if state.Terminated.ExitCode == 0 {
			return true, nil
		}
		return true, fmt.Errorf("%s/%s unexpectedly terminated: exit code: %v, reason: %s, message: %s", pod.Namespace, pod.Name, state.Terminated.ExitCode, state.Terminated.Reason, state.Terminated.Message)
	}
	return false, nil
}

func (o *SOSReportOptions) waitForContainerRunning(pod *corev1.Pod, containerName string) error {
	return wait.PollImmediate(10*time.Second, o.Timeout, func() (bool, error) {
		var err error
		if pod, err = o.Client.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{}); err == nil {
			if len(pod.Status.ContainerStatuses) == 0 {
				return false, nil
			}

			state, err := getContainerState(pod, containerName)
			if err != nil {
				return false, err
			}

			if state.Waiting != nil {
				switch state.Waiting.Reason {
				case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
					return true, fmt.Errorf("unable to pull image: %v: %v", state.Waiting.Reason, state.Waiting.Message)
				}
			}
			running := state.Running != nil
			terminated := state.Terminated != nil
			return running || terminated, nil
		}
		if retry.IsHTTPClientError(err) {
			return false, nil
		}
		return false, err
	})
}

func getContainerState(pod *corev1.Pod, containerName string) (state *corev1.ContainerState, err error) {

	for _, containerStatuses := range pod.Status.ContainerStatuses {
		if containerStatuses.Name == containerName {
			state = &containerStatuses.State
			break
		}
	}
	if state == nil {
		return nil, fmt.Errorf("no container [%s] found in pod [%s]", containerName, pod.Name)
	}
	return state, nil
}

func (o *SOSReportOptions) newClusterRoleBinding(ns string) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "sosreport-",
			Annotations: map[string]string{
				"oc.openshift.io/command": "oc adm sos",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "default",
				Namespace: ns,
			},
		},
	}
}

// newPod creates a pod with 2 containers with a shared volume mount:
// - sosreport: init containers that run gather command
// - copy: no-op container we can exec into
func (o *SOSReportOptions) newPod(node, image string) *corev1.Pod {
	zero := int64(0)

	nodeSelector := map[string]string{
		corev1.LabelOSStable: "linux",
	}
	if node == "" {
		nodeSelector["node-role.kubernetes.io/master"] = ""
	}
	// Reused values
	volumeName := "sosreport-output"
	sosreportAdditionalArgs := ""
	directoryType := corev1.HostPathDirectory
	privileged := true
	image = "registry.redhat.io/rhel8/support-tools:8.4-10"
	ret := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "sosreport-collect-",
			Labels: map[string]string{
				"app": "sosreport",
			},
		},
		Spec: corev1.PodSpec{

			NodeName: node,
			// This pod is ok to be OOMKilled but not preempted. Following the conventions mentioned at:
			// https://github.com/openshift/enhancements/blob/master/CONVENTIONS.md#priority-classes
			// so setting priority class to system-cluster-critical
			PriorityClassName: "system-cluster-critical",
			RestartPolicy:     corev1.RestartPolicyNever,
			Volumes: []corev1.Volume{
				{
					Name: volumeName,
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "run",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/run",
							Type: &directoryType,
						},
					},
				}, {
					Name: "var-log",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/var/log",
							Type: &directoryType,
						},
					},
				}, {
					Name: "machine-id",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/etc/machine-id",
						},
					},
				}, {
					Name: "local-time",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/etc/localtime",
						},
					},
				}, {
					Name: "root-dir",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/",
							Type: &directoryType,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "sosreport",
					Image:           image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					// TODO: Add optional SOSReport flags here. (such as -k crio.all=on -k crio.logs=on)
					// always force disk flush to ensure that all data gathered is accessible in the copy container
					Command: []string{"/bin/bash", "-c", fmt.Sprintf("/usr/sbin/sos report --batch --tmp-dir=%s %s; sync", o.SourceDir, sosreportAdditionalArgs)},

					// SOSReport requires additional securityContexts and mounts
					SecurityContext: &corev1.SecurityContext{
						Privileged: &privileged,
					},
					// Env: []corev1.EnvVar{
					// 	{
					// 		Name:  "HOST",
					// 		Value: "/host",
					// 	}, {
					// 		Name:  "IMAGE",
					// 		Value: image,
					// 	}, {
					// 		Name:  "NAME",
					// 		Value: fmt.Sprintf("%s-sosreport", node),
					// 	},
					// },
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      volumeName,
							MountPath: path.Clean(o.SourceDir),
							ReadOnly:  false,
						}, {
							// Mounth passthrough for access to Node
							Name:      "run",
							MountPath: "/run",
						}, {
							Name:      "var-log",
							MountPath: "/var/log",
						}, {
							Name:      "machine-id",
							MountPath: "/etc/machine-id",
						}, {
							Name:      "local-time",
							MountPath: "/etc/localtime",
						}, {
							Name:      "root-dir",
							MountPath: "/host",
						},
					},
				},
				{
					Name:            "copy",
					Image:           image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"/bin/bash", "-c", "trap : TERM INT; sleep infinity & wait"},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      volumeName,
							MountPath: path.Clean(o.SourceDir),
							ReadOnly:  false,
						},
					},
				},
			},
			NodeSelector:                  nodeSelector,
			TerminationGracePeriodSeconds: &zero,
			Tolerations: []corev1.Toleration{
				{
					Operator: "Exists",
				},
			},

			// Run the Pod in Host namespaces
			HostNetwork: true,
			HostPID:     true,
			HostIPC:     true,
		},
	}
	if len(o.Command) > 0 {
		// always force disk flush to ensure that all data gathered is accessible in the copy container
		ret.Spec.Containers[0].Command = []string{"/bin/bash", "-c", fmt.Sprintf("%s; sync", strings.Join(o.Command, " "))}
	}

	return ret
}

// BackupGathering is called if the full must-gather has an error.  This is useful for making sure we get *something*
// no matter what has failed.  It should be focused on universal openshift failures.
func (o *SOSReportOptions) BackupGathering(ctx context.Context) {
	inspectOptions := inspect.NewInspectOptions(o.IOStreams)
	inspectOptions.RESTConfig = rest.CopyConfig(o.Config)
	inspectOptions.DestDir = o.DestDir
	if err := inspectOptions.Complete([]string{"clusteroperators.v1.config.openshift.io"}); err != nil {
		fmt.Fprintf(o.ErrOut, "error completing backup collection: %v", err)
		return
	}
	if err := inspectOptions.Validate(); err != nil {
		fmt.Fprintf(o.ErrOut, "error validating backup collection: %v", err)
		return
	}
	if err := inspectOptions.Run(); err != nil {
		fmt.Fprintf(o.ErrOut, "error running backup collection: %v", err)
		return
	}
}
