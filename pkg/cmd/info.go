package cmd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strings"

	"github.com/spf13/cobra"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kapierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericclioptions/resource"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/api/route"

	"github.com/openshift/must-gather/pkg/util"
)

var (
	infoExample = `
	# Collect debugging data for the "openshift-apiserver-operator"
	%[1]s info clusteroperator/openshift-apiserver-operator

	# Collect debugging data for all clusteroperators
	%[1]s info clusteroperator
`
)

type InfoOptions struct {
	printFlags  *genericclioptions.PrintFlags
	configFlags *genericclioptions.ConfigFlags

	restConfig      *rest.Config
	kubeClient      kubernetes.Interface
	discoveryClient discovery.CachedDiscoveryInterface
	dynamicClient   dynamic.Interface

	podUrlGetter *util.RemotePodURLGetter

	fileWriter *util.MultiSourceFileWriter
	builder    *resource.Builder
	args       []string

	// directory where all gathered data will be stored
	baseDir string
	// whether or not to allow writes to an existing and populated base directory
	overwrite bool

	genericclioptions.IOStreams
}

func NewInfoOptions(streams genericclioptions.IOStreams) *InfoOptions {
	return &InfoOptions{
		printFlags:  genericclioptions.NewPrintFlags("gathered").WithDefaultOutput("yaml").WithTypeSetter(scheme.Scheme),
		configFlags: genericclioptions.NewConfigFlags(),
		IOStreams:   streams,
	}
}

func NewCmdInfo(parentName string, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewInfoOptions(streams)

	cmd := &cobra.Command{
		Use:          "info <operator> [flags]",
		Short:        "Gather debugging data for a given cluster operator",
		Example:      fmt.Sprintf(infoExample, parentName),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(c, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			if err := o.Run(); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&o.baseDir, "base-dir", "must-gather", "Root directory used for storing all gathered cluster operator data. Defaults to $(PWD)/must-gather")
	cmd.Flags().BoolVar(&o.overwrite, "overwrite", false, "If true, allow this command to write to an existing location with previous data present")

	o.printFlags.AddFlags(cmd)
	return cmd
}

func (o *InfoOptions) Complete(cmd *cobra.Command, args []string) error {
	o.args = args

	var err error
	o.restConfig, err = o.configFlags.ToRESTConfig()
	if err != nil {
		return err
	}

	o.kubeClient, err = kubernetes.NewForConfig(o.restConfig)
	if err != nil {
		return err
	}

	o.dynamicClient, err = dynamic.NewForConfig(o.restConfig)
	if err != nil {
		return err
	}

	o.discoveryClient, err = o.configFlags.ToDiscoveryClient()
	if err != nil {
		return err
	}

	printer, err := o.printFlags.ToPrinter()
	if err != nil {
		return err
	}
	o.fileWriter = util.NewMultiSourceWriter(printer)
	o.podUrlGetter = &util.RemotePodURLGetter{
		Protocol: "https",
		Host:     "localhost",
		Port:     "8443",
	}

	// pre-fetch token while we perform other tasks
	if err := o.podUrlGetter.FetchToken(o.restConfig); err != nil {
		return err
	}

	o.builder = resource.NewBuilder(o.configFlags)
	return nil
}

func (o *InfoOptions) Validate() error {
	if len(o.args) != 1 {
		return fmt.Errorf("exactly 1 argument (operator name) is supported")
	}
	if len(o.baseDir) == 0 {
		return fmt.Errorf("--base-dir must not be empty")
	}
	return nil
}

func (o *InfoOptions) Run() error {
	r := o.builder.
		Unstructured().
		ResourceTypeOrNameArgs(true, o.args...).
		Flatten().
		Latest().Do()

	infos, err := r.Infos()
	if err != nil {
		return err
	}

	// first, ensure we're dealing with correct resource types
	for _, info := range infos {
		if configv1.GroupName != info.Mapping.GroupVersionKind.Group {
			return fmt.Errorf("unexpected resource API group %q. Expected %q", info.Mapping.GroupVersionKind.Group, configv1.GroupName)
		}
		if info.Mapping.Resource.Resource != "clusteroperators" {
			return fmt.Errorf("unsupported resource type, must be %q", "clusteroperators")
		}
	}

	// next, ensure we're able to proceed writing data to specified destination
	if err := o.ensureDirectoryViable(o.baseDir, o.overwrite); err != nil {
		return err
	}

	// gather config.openshift.io resource data
	errs := []error{}
	if err := o.gatherConfigResourceData(path.Join(o.baseDir, "/resources/config.openshift.io")); err != nil {
		errs = append(errs, err)
	}

	for _, info := range infos {
		// save clusteroperator resources
		if err := o.gatherClusterOperatorResource(path.Join(o.baseDir, "/resources"), info); err != nil {
			errs = append(errs, err)
			continue
		}

		namespace, err := obtainClusterOperatorNamespace(info.Object)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		// save operator data for each clusteroperator
		if err := o.gatherClusterOperatorNamespaceData(path.Join(o.baseDir, "/"+info.Name), namespace); err != nil {
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("One or more errors ocurred gathering cluster data:\n\n    %v", errors.NewAggregate(errs))
	}

	log.Printf("Finished successfully with no errors.\n")
	return nil
}

func obtainClusterOperatorNamespace(obj runtime.Object) (string, error) {
	// obtain related namespace info for the current clusteroperator
	unstructuredCO, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return "", fmt.Errorf("invalid resource type, expecting clusteroperators but got %T", obj)
	}
	log.Printf("    Gathering namespace information for ClusterOperator %q...\n", unstructuredCO.GetName())

	structuredCO := &configv1.ClusterOperator{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredCO.Object, structuredCO); err != nil {
		return "", err
	}

	for _, related := range structuredCO.Status.RelatedObjects {
		if related.Resource != "namespaces" {
			continue
		}
		log.Printf("    Found related namespace %q for ClusterOperator %q...\n", related.Name, structuredCO.Name)
		return related.Name, nil
	}

	log.Printf("    Falling back to namespace %q for ClusterOperator %q. Unable to find any related namespaces in object status...\n", structuredCO.Name, structuredCO.Name)
	return structuredCO.Name, nil
}

// ensureDirectoryViable returns an error if the given path:
// 1. already exists AND is a file (not a directory)
// 2. already exists AND is NOT empty
// 3. an IO error occurs
func (o *InfoOptions) ensureDirectoryViable(dirPath string, allowDataOverride bool) error {
	baseDirInfo, err := os.Stat(dirPath)
	if err != nil && os.IsNotExist(err) {
		// no error, directory simply does not exist yet
		return nil
	}
	if err != nil {
		return err
	}

	if !baseDirInfo.IsDir() {
		return fmt.Errorf("%q exists and is a file", dirPath)
	}
	files, err := ioutil.ReadDir(dirPath)
	if err != nil {
		return err
	}
	if len(files) > 0 && !allowDataOverride {
		return fmt.Errorf("%q exists and is not empty. Pass --overwrite to allow data overwrites", dirPath)
	}
	return nil
}

func (o *InfoOptions) gatherClusterOperatorResource(destDir string, info *resource.Info) error {
	log.Printf("Gathering cluster operator resource data...\n")

	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.yaml", info.Name)
	return o.fileWriter.WriteFromResource(path.Join(destDir, "/"+filename), info.Object)
}

func (o *InfoOptions) gatherConfigResourceData(destDir string) error {
	log.Printf("Gathering config.openshift.io resource data...\n")

	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	resources, err := retrieveConfigResourceNames(o.discoveryClient)
	if err != nil {
		return err
	}

	errs := []error{}
	for _, resource := range resources {
		resourceList, err := o.dynamicClient.Resource(resource).List(metav1.ListOptions{})
		if err != nil {
			errs = append(errs, err)
			continue
		}

		objToPrint := runtime.Object(resourceList)
		filename := fmt.Sprintf("%s.yaml", resource.Resource)
		if err := o.fileWriter.WriteFromResource(path.Join(destDir, "/"+filename), objToPrint); err != nil {
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("one or more errors ocurred while gathering config.openshift.io resource data:\n\n    %v", errors.NewAggregate(errs))
	}
	return nil
}

func retrieveConfigResourceNames(discoveryClient discovery.CachedDiscoveryInterface) ([]schema.GroupVersionResource, error) {
	lists, err := discoveryClient.ServerPreferredResources()
	if err != nil {
		return nil, err
	}

	resources := []schema.GroupVersionResource{}
	for _, list := range lists {
		if len(list.APIResources) == 0 {
			continue
		}
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		for _, resource := range list.APIResources {
			if len(resource.Verbs) == 0 {
				continue
			}
			// filter groups outside of config.openshift.io
			if gv.Group != configv1.GroupName {
				continue
			}
			resources = append(resources, schema.GroupVersionResource{Group: gv.Group, Version: gv.Version, Resource: resource.Name})
		}
	}

	return resources, nil
}

func (o *InfoOptions) gatherClusterOperatorNamespaceData(destDir, namespace string) error {
	log.Printf("Gathering cluster operator data for namespace %q...\n", namespace)

	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	ns, err := o.kubeClient.CoreV1().Namespaces().Get(namespace, metav1.GetOptions{})
	if err != nil {
		if kapierrs.IsNotFound(err) {
			log.Printf("Unable to find namespace %q. Skipping data collection for that namespace...\n", namespace)
			return nil
		}

		return err
	}
	ns.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Namespace"))

	// write namespace.yaml file
	filename := fmt.Sprintf("%s.yaml", namespace)
	if err := o.fileWriter.WriteFromResource(path.Join(destDir, "/"+filename), ns); err != nil {
		return err
	}

	log.Printf("    Collecting resources for namespace %q...\n", namespace)

	resourcesToStore := map[string]runtime.Object{}

	// collect resource information for namespace
	pods, err := o.dynamicClient.Resource(corev1.SchemeGroupVersion.WithResource("pods")).Namespace(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	resourcesToStore["pods.yaml"] = pods

	configmaps, err := o.dynamicClient.Resource(corev1.SchemeGroupVersion.WithResource("configmaps")).Namespace(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	resourcesToStore["configmaps.yaml"] = configmaps

	services, err := o.dynamicClient.Resource(corev1.SchemeGroupVersion.WithResource("services")).Namespace(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	resourcesToStore["services.yaml"] = services

	deployments, err := o.dynamicClient.Resource(appsv1.SchemeGroupVersion.WithResource("deployments")).Namespace(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	resourcesToStore["deployments.yaml"] = deployments

	daemonsets, err := o.dynamicClient.Resource(appsv1.SchemeGroupVersion.WithResource("daemonsets")).Namespace(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	resourcesToStore["daemonsets.yaml"] = daemonsets

	statefulsets, err := o.dynamicClient.Resource(appsv1.SchemeGroupVersion.WithResource("statefulsets")).Namespace(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	resourcesToStore["statefulsets.yaml"] = statefulsets

	routes, err := o.dynamicClient.Resource(schema.GroupVersionResource{Group: route.GroupName, Version: "v1", Resource: "routes"}).Namespace(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	resourcesToStore["routes.yaml"] = routes

	// store redacted secrets
	secrets, err := o.dynamicClient.Resource(corev1.SchemeGroupVersion.WithResource("secrets")).Namespace(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	resourcesToStore["secrets.yaml"] = secrets

	secretsToStore := []unstructured.Unstructured{}
	for _, secret := range secrets.Items {
		if _, ok := secret.Object["data"]; ok {
			secret.Object["data"] = nil
		}

		secretsToStore = append(secretsToStore, secret)
	}
	secrets.Items = secretsToStore

	errs := []error{}
	for filename, obj := range resourcesToStore {
		if err := o.fileWriter.WriteFromResource(path.Join(destDir, "/"+filename), obj); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors ocurred storing resource information for namespace %q:\n\n    %v", namespace, errors.NewAggregate(errs))
	}

	log.Printf("    Gathering pod data for namespace %q...\n", namespace)

	// gather specific pod data
	for _, pod := range pods.Items {
		log.Printf("        Gathering data for pod %q\n", pod.GetName())
		structuredPod := &corev1.Pod{}
		runtime.DefaultUnstructuredConverter.FromUnstructured(pod.Object, structuredPod)
		if err := o.gatherPodData(path.Join(destDir, "/pods/"+pod.GetName()), namespace, structuredPod); err != nil {
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("one or more errors ocurred while gathering pod-specific data for namespace: %s\n\n    %v", namespace, errors.NewAggregate(errs))
	}
	return nil
}

func (o *InfoOptions) gatherPodData(destDir, namespace string, pod *corev1.Pod) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.yaml", pod.Name)
	if err := o.fileWriter.WriteFromResource(path.Join(destDir, "/"+filename), pod); err != nil {
		return err
	}

	errs := []error{}

	// skip gathering container data if containers are no longer running
	running, err := util.PodRunningReady(pod)
	if err != nil {
		return err
	}
	if !running {
		log.Printf("        Skipping container data collection for pod %q: Pod not running\n", pod.Name)
		return nil
	}

	// gather data for each container in the given pod
	for _, container := range pod.Spec.Containers {
		if err := o.gatherContainerData(path.Join(destDir, "/"+container.Name), pod, &container); err != nil {
			errs = append(errs, err)
			continue
		}
	}
	for _, container := range pod.Spec.InitContainers {
		if err := o.gatherContainerData(path.Join(destDir, "/"+container.Name), pod, &container); err != nil {
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("one or more errors ocurred while gathering container data for pod %s:\n\n    %v", pod.Name, errors.NewAggregate(errs))
	}
	return nil
}

func (o *InfoOptions) gatherContainerData(destDir string, pod *corev1.Pod, container *corev1.Container) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	doneChan := make(chan error, 1)

	if err := o.gatherContainerLogs(path.Join(destDir, "/logs"), pod, container); err != nil {
		return filterContainerLogsErrors(err)
	}
	if err := o.gatherContainerHealthz(path.Join(destDir, "/healthz"), pod, container); err != nil {
		return err
	}
	if err := o.gatherContainerMetrics(destDir, pod, container, doneChan); err != nil {
		return err
	}

	<-doneChan
	return nil
}

func filterContainerLogsErrors(err error) error {
	if strings.Contains(err.Error(), "previous terminated container") && strings.HasSuffix(err.Error(), "not found") {
		log.Printf("        Unable to gather previous container logs: %v\n", err)
		return nil
	}
	return err
}

// gatherContainerMetrics invokes an asynchronous network call
func (o *InfoOptions) gatherContainerMetrics(destDir string, pod *corev1.Pod, container *corev1.Container, doneChan chan error) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	// we need a token in order to access the /metrics endpoint
	return o.podUrlGetter.EnsureGetWithTokenAsync("/metrics", pod, o.restConfig, func(result string, err error) {
		if err != nil {
			doneChan <- err
			return
		}

		filename := fmt.Sprintf("%s.json", "metrics")
		doneChan <- o.fileWriter.WriteFromSource(path.Join(destDir, filename), &util.TextWriterSource{Text: result})
	})
}

func (o *InfoOptions) gatherContainerHealthz(destDir string, pod *corev1.Pod, container *corev1.Container) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	result, err := o.podUrlGetter.Get("/", pod, o.restConfig)
	if err != nil {
		return err
	}

	pathInfo := map[string][]string{}

	// first, unmarshal result into json object and obtain all available /healthz endpoints
	if err := json.Unmarshal([]byte(result), &pathInfo); err != nil {
		return err
	}
	paths, ok := pathInfo["paths"]
	if !ok {
		return fmt.Errorf("unable to extract healthz path information for pod %q", pod.Name)
	}

	healthzSeparator := "/healthz"
	healthzPaths := []string{}
	for _, p := range paths {
		if !strings.HasPrefix(p, healthzSeparator) {
			continue
		}
		healthzPaths = append(healthzPaths, p)
	}
	if len(healthzPaths) == 0 {
		return fmt.Errorf("unable to find any available /healthz paths hosted in pod %q", pod.Name)
	}

	for _, healthzPath := range healthzPaths {
		result, err := o.podUrlGetter.Get(path.Join("/", healthzPath), pod, o.restConfig)
		if err != nil {
			// TODO: aggregate errors
			return err
		}

		if len(healthzSeparator) > len(healthzPath) {
			continue
		}
		filename := healthzPath[len(healthzSeparator):]
		if len(filename) == 0 {
			filename = "index"
		} else {
			filename = strings.TrimPrefix(filename, "/")
		}

		if err := o.fileWriter.WriteFromSource(path.Join(destDir, filename), &util.TextWriterSource{Text: result}); err != nil {
			return err
		}
	}
	return nil
}

func (o *InfoOptions) gatherContainerLogs(destDir string, pod *corev1.Pod, container *corev1.Container) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	logOptions := &corev1.PodLogOptions{
		Container:  container.Name,
		Follow:     false,
		Previous:   false,
		Timestamps: true,
	}
	// first, retrieve current logs
	logsReq := o.kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOptions)

	filename := fmt.Sprintf("%s.log", "current")

	if err := o.fileWriter.WriteFromSource(path.Join(destDir, "/"+filename), logsReq); err != nil {
		return err
	}

	// then, retrieve previous logs
	logOptions.Previous = true
	logsReqPrevious := o.kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOptions)

	filename = fmt.Sprintf("%s.log", "previous")
	return o.fileWriter.WriteFromSource(path.Join(destDir, "/"+filename), logsReqPrevious)
}
