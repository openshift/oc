package mustgather

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

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/cmd/logs"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/templates"
	admissionapi "k8s.io/pod-security-admission/api"
	"k8s.io/utils/exec"

	configclient "github.com/openshift/client-go/config/clientset/versioned"
	imagev1client "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	"github.com/openshift/library-go/pkg/image/imageutil"
	imagereference "github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/library-go/pkg/operator/resource/retry"
	"github.com/openshift/oc/pkg/cli/admin/inspect"
	"github.com/openshift/oc/pkg/cli/rsync"
	ocmdhelpers "github.com/openshift/oc/pkg/helpers/cmd"
)

const (
	gatherContainerName = "gather"
)

var (
	mustGatherLong = templates.LongDesc(`
		Launch a pod to gather debugging information.

		This command will launch a pod in a temporary namespace on your cluster that gathers
		debugging information and then downloads the gathered information.
	`)

	mustGatherExample = templates.Examples(`
		# Gather information using the default plug-in image and command, writing into ./must-gather.local.<rand>
		  oc adm must-gather

		# Gather information with a specific local folder to copy to
		  oc adm must-gather --dest-dir=/local/directory

		# Gather audit information
		  oc adm must-gather -- /usr/bin/gather_audit_logs

		# Gather information using multiple plug-in images
		  oc adm must-gather --image=quay.io/kubevirt/must-gather --image=quay.io/openshift/origin-must-gather

		# Gather information using a specific image stream plug-in
		  oc adm must-gather --image-stream=openshift/must-gather:latest

		# Gather information using a specific image, command, and pod directory
		  oc adm must-gather --image=my/image:tag --source-dir=/pod/directory -- myspecial-command.sh
	`)

	volumeUsageCheckerScript = `
echo "volume percentage checker started....."
while true; do
disk_usage=$(du -s "%s" | awk '{print $1}')
disk_space=$(df -P "%s" | awk 'NR==2 {print $2}')
usage_percentage=$(( (disk_usage * 100) / disk_space ))
echo "volume usage percentage $usage_percentage"
if [ "$usage_percentage" -gt "%d" ]; then
	echo "Disk usage exceeds the volume percentage of %d for mounted directory. Exiting..."
	# kill gathering process in gather container to prevent disk to use more.
	pkill --signal SIGKILL -f %s
	exit 1
fi
sleep 5
done`
)

const (
	// number of concurrent must-gather Pods to run if --all-images or multiple --image are provided
	concurrentMG = 4
	// annotation to look for in ClusterServiceVersions and ClusterOperators when using --all-images
	mgAnnotation = "operators.openshift.io/must-gather-image"
)

func NewMustGatherCommand(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewMustGatherOptions(streams)
	cmd := &cobra.Command{
		Use:     "must-gather",
		Short:   "Launch a new instance of a pod for gathering debug information",
		Long:    mustGatherLong,
		Example: mustGatherExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			ocmdhelpers.CheckPodSecurityErr(o.Run())
		},
	}

	cmd.Flags().StringVar(&o.NodeName, "node-name", o.NodeName, "Set a specific node to use - by default a random master will be used")
	cmd.Flags().StringVar(&o.NodeSelector, "node-selector", o.NodeSelector, "Set a specific node selector to use - only relevant when specifying a command and image which needs to capture data on a set of cluster nodes simultaneously")
	cmd.Flags().BoolVar(&o.HostNetwork, "host-network", o.HostNetwork, "Run must-gather pods as hostNetwork: true - relevant if a specific command and image needs to capture host-level data")
	cmd.Flags().StringSliceVar(&o.Images, "image", o.Images, "Specify a must-gather plugin image to run. If not specified, OpenShift's default must-gather image will be used.")
	cmd.Flags().StringSliceVar(&o.ImageStreams, "image-stream", o.ImageStreams, "Specify an image stream (namespace/name:tag) containing a must-gather plugin image to run.")
	cmd.Flags().BoolVar(&o.AllImages, "all-images", o.AllImages, `Collect must-gather using the default image for all Operators on the cluster annotated with `+mgAnnotation)
	cmd.Flags().StringVar(&o.DestDir, "dest-dir", o.DestDir, "Set a specific directory on the local machine to write gathered data to.")
	cmd.Flags().StringVar(&o.SourceDir, "source-dir", o.SourceDir, "Set the specific directory on the pod copy the gathered data from.")
	cmd.Flags().StringVar(&o.timeoutStr, "timeout", "10m", "The length of time to gather data, like 5s, 2m, or 3h, higher than zero. Defaults to 10 minutes.")
	cmd.Flags().StringVar(&o.RunNamespace, "run-namespace", o.RunNamespace, "An existing privileged namespace where must-gather pods should run. If not specified a temporary namespace will be generated.")
	cmd.Flags().Uint8Var(&o.VolumePercentage, "volume-percentage", o.VolumePercentage, "Specify maximum percentage of must-gather pod's allocated volume that can be used. If this limit is exceeded, must-gather will stop gathering, but still copy gathered data.")
	cmd.Flags().BoolVar(&o.Keep, "keep", o.Keep, "Do not delete temporary resources when command completes.")
	cmd.Flags().MarkHidden("keep")
	cmd.Flags().StringVar(&o.SinceTime, "since-time", o.SinceTime, "Only return logs after a specific date (RFC3339). Defaults to all logs. Plugins are encouraged but not required to support this. Only one of since-time / since may be used.")
	cmd.Flags().DurationVar(&o.Since, "since", o.Since, "Only return logs newer than a relative duration like 5s, 2m, or 3h. Defaults to all logs. Plugins are encouraged but not required to support this. Only one of since-time / since may be used.")

	return cmd
}

func NewMustGatherOptions(streams genericiooptions.IOStreams) *MustGatherOptions {
	opts := &MustGatherOptions{
		SourceDir:        "/must-gather/",
		IOStreams:        streams,
		Timeout:          10 * time.Minute,
		VolumePercentage: 30,
	}
	opts.LogOut = opts.newPrefixWriter(streams.Out, "[must-gather      ] OUT", false, true)
	opts.RawOut = opts.newPrefixWriter(streams.Out, "", false, false)
	return opts
}

func (o *MustGatherOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
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
	if o.DynamicClient, err = dynamic.NewForConfig(o.Config); err != nil {
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
		o.DestDir = fmt.Sprintf("must-gather.local.%06d", rand.Int63())
	}
	// TODO: this should be in Validate() method, but added here because of the call to o.completeImages() below
	if o.AllImages {
		errStr := fmt.Sprintf("and --all-images are mutually exclusive: please specify one or the other")
		if len(o.Images) != 0 {
			return fmt.Errorf("--image %s", errStr)
		}
		if o.HostNetwork {
			return fmt.Errorf("--host-network %s", errStr)
		}
		if len(o.ImageStreams) != 0 {
			return fmt.Errorf("--image-streams %s", errStr)
		}
		if o.NodeName != "" {
			return fmt.Errorf("--node-name %s", errStr)
		}
		if o.RunNamespace != "" {
			return fmt.Errorf("--run-namespace %s", errStr)
		}
	}
	if err := o.completeImages(); err != nil {
		return err
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

func (o *MustGatherOptions) completeImages() error {
	for _, imageStream := range o.ImageStreams {
		if image, err := o.resolveImageStreamTagString(imageStream); err == nil {
			o.Images = append(o.Images, image)
		} else {
			return fmt.Errorf("unable to resolve image stream '%v': %v", imageStream, err)
		}
	}
	if len(o.Images) == 0 || o.AllImages {
		var image string
		var err error
		if image, err = o.resolveImageStreamTag("openshift", "must-gather", "latest"); err != nil {
			o.log("%v\n", err)
			image = "registry.redhat.io/openshift4/ose-must-gather:latest"
		}
		o.Images = append(o.Images, image)
	}
	if o.AllImages {
		// find all csvs and clusteroperators with the annotation "operators.openshift.io/must-gather-image"
		pluginImages := make(map[string]struct{})
		var err error

		pluginImages, err = o.annotatedCSVs()
		if err != nil {
			return err
		}

		cos, err := o.ConfigClient.ConfigV1().ClusterOperators().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, item := range cos.Items {
			ann := item.GetAnnotations()
			if v, ok := ann[mgAnnotation]; ok {
				pluginImages[v] = struct{}{}
			}
		}
		// delete the default image to avoid duplication in case an Operator had it in its annotation
		delete(pluginImages, o.Images[0])
		for i := range pluginImages {
			o.Images = append(o.Images, i)
		}
	}
	o.log("Using must-gather plug-in image: %s", strings.Join(o.Images, ", "))

	return nil
}

func (o *MustGatherOptions) annotatedCSVs() (map[string]struct{}, error) {
	csvGVR := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "clusterserviceversions",
	}
	pluginImages := make(map[string]struct{})

	csvs, err := o.DynamicClient.Resource(csvGVR).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, item := range csvs.Items {
		ann := item.GetAnnotations()
		if v, ok := ann[mgAnnotation]; ok {
			pluginImages[v] = struct{}{}
		}
	}
	return pluginImages, nil
}

func (o *MustGatherOptions) resolveImageStreamTagString(s string) (string, error) {
	namespace, name, tag := parseImageStreamTagString(s)
	if len(namespace) == 0 {
		return "", fmt.Errorf("expected namespace/name:tag")
	}
	return o.resolveImageStreamTag(namespace, name, tag)
}

func parseImageStreamTagString(s string) (string, string, string) {
	var namespace, nameAndTag string
	parts := strings.SplitN(s, "/", 2)
	switch len(parts) {
	case 2:
		namespace = parts[0]
		nameAndTag = parts[1]
	case 1:
		nameAndTag = parts[0]
	}
	name, tag, _ := imageutil.SplitImageStreamTag(nameAndTag)
	return namespace, name, tag
}

func (o *MustGatherOptions) resolveImageStreamTag(namespace, name, tag string) (string, error) {
	imageStream, err := o.ImageClient.ImageStreams(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	var image string
	if image, _, _, _, err = imageutil.ResolveRecentPullSpecForTag(imageStream, tag, false); err != nil {
		return "", fmt.Errorf("unable to resolve the imagestream tag %s/%s:%s: %v", namespace, name, tag, err)
	}
	return image, nil
}

type MustGatherOptions struct {
	genericiooptions.IOStreams

	Config           *rest.Config
	Client           kubernetes.Interface
	ConfigClient     configclient.Interface
	DynamicClient    dynamic.Interface
	ImageClient      imagev1client.ImageV1Interface
	RESTClientGetter genericclioptions.RESTClientGetter

	NodeName         string
	NodeSelector     string
	HostNetwork      bool
	DestDir          string
	SourceDir        string
	Images           []string
	AllImages        bool
	ImageStreams     []string
	Command          []string
	Timeout          time.Duration
	timeoutStr       string
	RunNamespace     string
	VolumePercentage uint8
	Keep             bool
	Since            time.Duration
	SinceTime        string

	RsyncRshCmd string

	PrinterCreated printers.ResourcePrinter
	PrinterDeleted printers.ResourcePrinter
	LogOut         io.Writer
	// RawOut is used for printing information we're looking to have copy/pasted into bugs
	RawOut io.Writer

	LogWriter    *os.File
	LogWriterMux sync.Mutex
}

func (o *MustGatherOptions) Validate() error {
	if len(o.Images) == 0 {
		return fmt.Errorf("missing an image")
	}
	if o.NodeName != "" && o.NodeSelector != "" {
		return fmt.Errorf("--node-name and --node-selector are mutually exclusive: please specify one or the other")
	}
	// validation from vendor/k8s.io/apimachinery/pkg/api/validation/path/name.go
	if len(o.NodeName) > 0 && strings.ContainsAny(o.NodeName, "/%") {
		return fmt.Errorf("--node-name may not contain '/' or '%%'")
	}
	if strings.Contains(o.DestDir, ":") {
		return fmt.Errorf("--dest-dir may not contain special characters such as colon(:)")
	}

	if o.VolumePercentage <= 0 || o.VolumePercentage > 100 {
		return fmt.Errorf("invalid volume usage percentage, please specify a value between 0 and 100")
	}
	if o.VolumePercentage >= 80 {
		klog.Warningf("volume percentage greater than or equal to 80 might cause filling up the disk space and have an impact on other components running on master")
	}

	if len(o.SinceTime) > 0 && o.Since != 0 {
		return fmt.Errorf("at most one of `--since-time` or `--since` may be specified")
	}

	if len(o.SinceTime) > 0 {
		if _, err := time.Parse(time.RFC3339, o.SinceTime); err != nil {
			return fmt.Errorf("--since-time only accepts times matching RFC3339, eg '2006-01-02T15:04:05Z'")
		}
	}

	return nil
}

// Run creates and runs a must-gather pod
func (o *MustGatherOptions) Run() error {
	var errs []error

	if err := os.MkdirAll(o.DestDir, os.ModePerm); err != nil {
		// ensure the errors bubble up to BackupGathering method for display
		errs = []error{err}
		return err
	}

	f, err := os.Create(path.Join(o.DestDir, "must-gather.logs"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to write must-gather logs: %v. It is possible the destination directory has not been created yet due to early termination\n", err)
	} else {
		o.LogWriter = f
		o.LogWriterMux.Lock()
		// gets invoked in Complete step before must-gather.logs is created
		o.LogWriter.WriteString(fmt.Sprintf("[must-gather      ] OUT %v Using must-gather plug-in image: %s\n", time.Now().UTC().Format(time.RFC3339Nano), strings.Join(o.Images, ", ")))
		o.LogWriterMux.Unlock()

		defer func() {
			o.LogWriter.Close()
		}()
	}

	// print at both the beginning and at the end.  This information is important enough to be in both spots.
	o.PrintBasicClusterState(context.TODO())
	defer func() {
		fmt.Fprintf(o.RawOut, "\n\n")
		fmt.Fprintf(o.RawOut, "Reprinting Cluster State:\n")
		o.PrintBasicClusterState(context.TODO())
	}()

	// Due to 'stack unwiding', this should happen after 'clusterState' printing, to ensure that we always
	//  print our ClusterState information.
	runBackCollection := true
	defer func() {
		if !runBackCollection {
			return
		}
		o.BackupGathering(context.TODO(), errs)
	}()

	// Get or create "working" namespace ...
	ns, cleanupNamespace, err := o.getNamespace()
	if err != nil {
		// ensure the errors bubble up to BackupGathering method for display
		errs = []error{err}
		return err
	}

	// ... ensure resource cleanup unless instructed otherwise ...
	if !o.Keep {
		defer cleanupNamespace()
	}

	// Prefer to run in master if there's any but don't be explicit otherwise.
	// This enables the command to run by default in hypershift where there's no masters.
	nodes, err := o.Client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: o.NodeSelector,
	})
	if err != nil {
		return err
	}
	var hasMaster bool
	for _, node := range nodes.Items {
		if _, ok := node.Labels["node-role.kubernetes.io/master"]; ok {
			hasMaster = true
			break
		}
	}

	// ... and create must-gather pod(s)
	var pods []*corev1.Pod
	for _, image := range o.Images {
		_, err := imagereference.Parse(image)
		if err != nil {
			line := fmt.Sprintf("unable to parse image reference %s: %v", image, err)
			o.log(line)
			// ensure the errors bubble up to BackupGathering method for display
			errs = []error{fmt.Errorf(line)}
			return err
		}
		if o.NodeSelector != "" {
			nodes, err := o.Client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
				LabelSelector: o.NodeSelector,
			})
			if err != nil {
				// ensure the errors bubble up to BackupGathering method for display
				errs = []error{err}
				return err
			}
			for _, node := range nodes.Items {
				pods = append(pods, o.newPod(node.Name, image, hasMaster))
			}
		} else {
			if o.NodeName != "" {
				if _, err := o.Client.CoreV1().Nodes().Get(context.TODO(), o.NodeName, metav1.GetOptions{}); err != nil {
					// ensure the errors bubble up to BackupGathering method for display
					errs = []error{err}
					return err
				}
			}
			pods = append(pods, o.newPod(o.NodeName, image, hasMaster))
		}
	}

	// log timestamps...
	if err := o.logTimestamp(); err != nil {
		// ensure the errors bubble up to BackupGathering method for display
		errs = []error{err}
		return err
	}
	defer o.logTimestamp()

	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	var wg sync.WaitGroup
	errCh := make(chan error, len(pods))

	for _, pod := range pods {
		queue.Add(pod)
	}
	queue.ShutDownWithDrain()

	wg.Add(concurrentMG)
	for i := 0; i < concurrentMG; i++ {
		go func() {
			defer wg.Done()
			for {
				pod, quit := queue.Get()
				if quit {
					return
				}
				defer queue.Done(pod)
				if err := o.processNextWorkItem(ns.Name, pod.(*corev1.Pod)); err != nil {
					errCh <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)

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

	// now gather all the events into a single file and produce a unified file
	if err := inspect.CreateEventFilterPage(o.DestDir); err != nil {
		errs = append(errs, err)
	}

	return errors.NewAggregate(errs)
}

// processNextWorkItem creates & processes the must-gather pod and returns error if any
func (o *MustGatherOptions) processNextWorkItem(ns string, pod *corev1.Pod) error {
	var err error
	pod, err = o.Client.CoreV1().Pods(ns).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	if o.NodeSelector != "" {
		o.log("pod: %s on node: %s for plug-in image %s created", pod.Name, pod.Spec.NodeName, pod.Spec.Containers[0].Image)
	} else {
		o.log("pod for plug-in image %s created", pod.Spec.Containers[0].Image)
	}
	if len(o.RunNamespace) > 0 && !o.Keep {
		defer func() {
			// must-gather runs in its own separate namespace as default , so after it is completed
			// it deletes this namespace and all the pods are removed by garbage collector.
			// However, if user specifies namespace via `run-namespace`, these pods need to
			// be deleted manually.
			err = o.Client.CoreV1().Pods(o.RunNamespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			if err != nil {
				klog.V(4).Infof("pod deletion error %v", err)
			}
		}()
	}

	log := o.newPodOutLogger(o.Out, pod.Name)

	// wait for gather container to be running (gather is running)
	if err := o.waitForGatherContainerRunning(pod); err != nil {
		log("gather did not start: %s", err)
		return fmt.Errorf("gather did not start for pod %s: %s", pod.Name, err)

	}
	// stream gather container logs
	if err := o.getGatherContainerLogs(pod); err != nil {
		log("gather logs unavailable: %v", err)
	}

	// wait for pod to be running (gather has completed)
	log("waiting for gather to complete")
	if err := o.waitForGatherToComplete(pod); err != nil {
		log("gather never finished: %v", err)
		if exiterr, ok := err.(*exec.CodeExitError); ok {
			return exiterr
		}
		return fmt.Errorf("gather never finished for pod %s: %s", pod.Name, err)
	}

	// copy the gathered files into the local destination dir
	log("downloading gather output")
	pod, err = o.Client.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
	if err != nil {
		log("gather output not downloaded: %v\n", err)
		return fmt.Errorf("unable to download output from pod %s: %s", pod.Name, err)
	}
	if err := o.copyFilesFromPod(pod); err != nil {
		log("gather output not downloaded: %v\n", err)
		return fmt.Errorf("unable to download output from pod %s: %s", pod.Name, err)
	}
	return nil
}

func (o *MustGatherOptions) newPodOutLogger(out io.Writer, podName string) func(string, ...interface{}) {
	writer := o.newPrefixWriter(out, fmt.Sprintf("[%s] OUT", podName), false, true)
	return func(format string, a ...interface{}) {
		fmt.Fprintf(writer, format+"\n", a...)
	}
}

func (o *MustGatherOptions) log(format string, a ...interface{}) {
	fmt.Fprintf(o.LogOut, format+"\n", a...)
}

func (o *MustGatherOptions) logTimestamp() error {
	f, err := os.OpenFile(path.Join(o.DestDir, "timestamp"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(fmt.Sprintf("%v\n", time.Now()))
	return err
}

func (o *MustGatherOptions) copyFilesFromPod(pod *corev1.Pod) error {
	streams := o.IOStreams
	streams.Out = o.newPrefixWriter(streams.Out, fmt.Sprintf("[%s] OUT", pod.Name), false, true)
	imageFolder := regexp.MustCompile("[^A-Za-z0-9]+").ReplaceAllString(pod.Status.ContainerStatuses[0].ImageID, "-")
	var destDir string
	if o.NodeSelector != "" {
		destDir = path.Join(o.DestDir, regexp.MustCompile("[^A-Za-z0-9]+").ReplaceAllString(pod.Spec.NodeName, "-"), imageFolder)
	} else {
		destDir = path.Join(o.DestDir, imageFolder)
	}
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	var errs []error

	// get must-gather gather container logs
	if err := func() error {
		dest, err := os.OpenFile(path.Join(destDir, "/gather.logs"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		defer dest.Close()

		logOptions := &corev1.PodLogOptions{
			Container:  gatherContainerName,
			Timestamps: true,
		}
		readCloser, err := o.Client.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOptions).Stream(context.TODO())
		if err != nil {
			return err
		}
		defer readCloser.Close()

		_, err = io.Copy(dest, readCloser)
		return err
	}(); err != nil {
		errs = append(errs, err)
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
		errs = append(errs, err)
	}
	return errors.NewAggregate(errs)
}

func (o *MustGatherOptions) getGatherContainerLogs(pod *corev1.Pod) error {
	since2s := int64(2)
	opts := &logs.LogsOptions{
		Namespace:   pod.Namespace,
		ResourceArg: pod.Name,
		Options: &corev1.PodLogOptions{
			Follow:     true,
			Container:  pod.Spec.Containers[0].Name,
			Timestamps: true,
		},
		RESTClientGetter: o.RESTClientGetter,
		Object:           pod,
		ConsumeRequestFn: logs.DefaultConsumeRequest,
		LogsForObject:    polymorphichelpers.LogsForObjectFn,
		IOStreams:        genericiooptions.IOStreams{Out: o.newPrefixWriter(o.Out, fmt.Sprintf("[%s] POD", pod.Name), true, false)},
	}

	for {
		// gather script might take longer than the default API server time,
		// so we should check if the gather script still runs and re-run logs
		// thus we run this in a loop
		if err := opts.RunLogs(); err != nil {
			return err
		}

		// to ensure we don't print all of history set since to past 2 seconds
		opts.Options.(*corev1.PodLogOptions).SinceSeconds = &since2s
		if done, _ := o.isGatherDone(pod); done {
			return nil
		}
		klog.V(4).Infof("lost logs, re-trying...")
	}
}

func (o *MustGatherOptions) newPrefixWriter(out io.Writer, prefix string, ignoreFileWriter bool, timestamp bool) io.Writer {
	reader, writer := io.Pipe()
	scanner := bufio.NewScanner(reader)
	if prefix != "" {
		prefix = prefix + " "
	}
	go func() {
		for scanner.Scan() {
			text := scanner.Text()
			ts := ""
			if timestamp {
				ts = time.Now().UTC().Format(time.RFC3339Nano) + " "
			}
			if !ignoreFileWriter && o.LogWriter != nil {
				o.LogWriterMux.Lock()
				o.LogWriter.WriteString(fmt.Sprintf("%s%s%s\n", prefix, ts, text))
				o.LogWriterMux.Unlock()
			}
			fmt.Fprintf(out, "%s%s%s\n", prefix, ts, text)
		}
	}()
	return writer
}

func (o *MustGatherOptions) waitForGatherToComplete(pod *corev1.Pod) error {
	return wait.PollUntilContextTimeout(context.TODO(), 10*time.Second, o.Timeout, true, func(ctx context.Context) (bool, error) {
		return o.isGatherDone(pod)
	})
}

func (o *MustGatherOptions) isGatherDone(pod *corev1.Pod) (bool, error) {
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
		if cstate.Name == gatherContainerName {
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
		return true, &exec.CodeExitError{
			Err:  fmt.Errorf("%s/%s unexpectedly terminated: exit code: %v, reason: %s, message: %s", pod.Namespace, pod.Name, state.Terminated.ExitCode, state.Terminated.Reason, state.Terminated.Message),
			Code: int(state.Terminated.ExitCode),
		}
	}
	return false, nil
}

func (o *MustGatherOptions) waitForGatherContainerRunning(pod *corev1.Pod) error {
	return wait.PollUntilContextTimeout(context.TODO(), 10*time.Second, o.Timeout, true, func(ctx context.Context) (bool, error) {
		var err error
		if pod, err = o.Client.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{}); err == nil {
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

func (o *MustGatherOptions) getNamespace() (*corev1.Namespace, func(), error) {
	if o.RunNamespace == "" {
		return o.createTempNamespace()
	}

	ns, err := o.Client.CoreV1().Namespaces().Get(context.TODO(), o.RunNamespace, metav1.GetOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("retrieving namespace %q: %w", o.RunNamespace, err)
	}

	return ns, func() {}, nil
}

func (o *MustGatherOptions) createTempNamespace() (*corev1.Namespace, func(), error) {
	ns, err := o.Client.CoreV1().Namespaces().Create(context.TODO(), newNamespace(), metav1.CreateOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("creating temp namespace: %w", err)
	}
	o.PrinterCreated.PrintObj(ns, o.LogOut)

	crb, err := o.Client.RbacV1().ClusterRoleBindings().Create(context.TODO(), newClusterRoleBinding(ns), metav1.CreateOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("creating temp clusterRoleBinding: %w", err)
	}
	o.PrinterCreated.PrintObj(crb, o.LogOut)

	cleanup := func() {
		if err := o.Client.CoreV1().Namespaces().Delete(context.TODO(), ns.Name, metav1.DeleteOptions{}); err != nil {
			fmt.Printf("%v\n", err)
		} else {
			o.PrinterDeleted.PrintObj(ns, o.LogOut)
		}
	}

	return ns, cleanup, nil
}

func newNamespace() *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "openshift-must-gather-",
			Labels: map[string]string{
				"openshift.io/run-level":                         "0",
				admissionapi.EnforceLevelLabel:                   string(admissionapi.LevelPrivileged),
				admissionapi.AuditLevelLabel:                     string(admissionapi.LevelPrivileged),
				admissionapi.WarnLevelLabel:                      string(admissionapi.LevelPrivileged),
				"security.openshift.io/scc.podSecurityLabelSync": "false",
			},
			Annotations: map[string]string{
				"oc.openshift.io/command":    "oc adm must-gather",
				"openshift.io/node-selector": "",
			},
		},
	}
}

func newClusterRoleBinding(ns *corev1.Namespace) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "must-gather-",
			Annotations: map[string]string{
				"oc.openshift.io/command": "oc adm must-gather",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Namespace",
					Name:       ns.GetName(),
					UID:        ns.GetUID(),
				},
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
				Namespace: ns.GetName(),
			},
		},
	}
}

// newPod creates a pod with 2 containers with a shared volume mount:
// - gather: init containers that run gather command
// - copy: no-op container we can exec into
func (o *MustGatherOptions) newPod(node, image string, hasMaster bool) *corev1.Pod {
	zero := int64(0)

	nodeSelector := map[string]string{
		corev1.LabelOSStable: "linux",
	}
	if node == "" && hasMaster {
		nodeSelector["node-role.kubernetes.io/master"] = ""
	}

	executedCommand := "/usr/bin/gather"
	if len(o.Command) > 0 {
		executedCommand = strings.Join(o.Command, " ")
	}

	cleanedSourceDir := path.Clean(o.SourceDir)
	volumeUsageChecker := fmt.Sprintf(volumeUsageCheckerScript, cleanedSourceDir, cleanedSourceDir, o.VolumePercentage, o.VolumePercentage, executedCommand)

	ret := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "must-gather-",
			Labels: map[string]string{
				"app": "must-gather",
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
					Name: "must-gather-output",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            gatherContainerName,
					Image:           image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					// always force disk flush to ensure that all data gathered is accessible in the copy container
					Command: []string{"/bin/bash", "-c", fmt.Sprintf("%s & %s; sync", volumeUsageChecker, executedCommand)},
					Env: []corev1.EnvVar{
						{
							Name: "NODE_NAME",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: "spec.nodeName",
								},
							},
						},
						{
							Name: "POD_NAME",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: "metadata.name",
								},
							},
						},
					},
					TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "must-gather-output",
							MountPath: cleanedSourceDir,
							ReadOnly:  false,
						},
					},
				},
				{
					Name:                     "copy",
					Image:                    image,
					ImagePullPolicy:          corev1.PullIfNotPresent,
					Command:                  []string{"/bin/bash", "-c", "trap : TERM INT; sleep infinity & wait"},
					TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "must-gather-output",
							MountPath: cleanedSourceDir,
							ReadOnly:  false,
						},
					},
				},
			},
			HostNetwork:                   o.HostNetwork,
			NodeSelector:                  nodeSelector,
			TerminationGracePeriodSeconds: &zero,
			Tolerations: []corev1.Toleration{
				{
					// An empty key with operator Exists matches all keys,
					// values and effects which means this will tolerate everything.
					// As noted in https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/
					Operator: "Exists",
				},
			},
		},
	}
	if o.HostNetwork {
		// If a user specified hostNetwork he might have intended to perform
		// packet captures on the host, for that we need to set the correct
		// capability. Limit the capability to CAP_NET_RAW though, as that's the
		// only capability which does not allow for more than what can be
		// considered "reading"
		ret.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					corev1.Capability("CAP_NET_RAW"),
				},
			},
		}
	}

	if o.Since != 0 {
		ret.Spec.Containers[0].Env = append(ret.Spec.Containers[0].Env, corev1.EnvVar{
			Name:  "MUST_GATHER_SINCE",
			Value: o.Since.String(),
		})
	}

	if o.SinceTime != "" {
		ret.Spec.Containers[0].Env = append(ret.Spec.Containers[0].Env, corev1.EnvVar{
			Name:  "MUST_GATHER_SINCE_TIME",
			Value: o.SinceTime,
		})
	}

	return ret
}

// BackupGathering is called if the full must-gather has an error.  This is useful for making sure we get *something*
// no matter what has failed.  It should be focused on universal openshift failures.
func (o *MustGatherOptions) BackupGathering(ctx context.Context, errs []error) {
	fmt.Fprintf(o.ErrOut, "\n\n") // Space out the output
	fmt.Fprintf(o.ErrOut, "Error running must-gather collection:\n    %v\n\n", errors.NewAggregate(errs))
	fmt.Fprintf(o.ErrOut, "Falling back to `oc adm inspect clusteroperators.v1.config.openshift.io` to collect basic cluster information.\n")

	streams := o.IOStreams
	streams.Out = o.newPrefixWriter(streams.Out, fmt.Sprintf("[must-gather      ] OUT"), false, true)

	inspectOptions := inspect.NewInspectOptions(streams)
	inspectOptions.RESTConfig = rest.CopyConfig(o.Config)
	inspectOptions.DestDir = path.Join(o.DestDir, fmt.Sprintf("inspect.local.%06d", rand.Int63()))

	if err := inspectOptions.Complete([]string{"clusteroperators.v1.config.openshift.io"}); err != nil {
		fmt.Fprintf(o.ErrOut, "error completing backup collection: %v\n", err)
		return
	}
	if err := inspectOptions.Validate(); err != nil {
		fmt.Fprintf(o.ErrOut, "error validating backup collection: %v\n", err)
		return
	}
	if err := inspectOptions.Run(); err != nil {
		fmt.Fprintf(o.ErrOut, "error running backup collection: %v\n", err)
		return
	}
}
