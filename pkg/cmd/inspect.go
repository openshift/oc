package cmd

import (
	"bytes"
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
	"k8s.io/apimachinery/pkg/util/sets"
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
	inspectExample = `
	# Collect debugging data for the "openshift-apiserver-operator"
	%[1]s inspect clusteroperator/openshift-apiserver-operator

	# Collect debugging data for all clusteroperators
	%[1]s inspect clusteroperator
`
)

type InspectOptions struct {
	printFlags  *genericclioptions.PrintFlags
	configFlags *genericclioptions.ConfigFlags

	restConfig      *rest.Config
	kubeClient      kubernetes.Interface
	discoveryClient discovery.CachedDiscoveryInterface
	dynamicClient   dynamic.Interface

	podUrlGetter *util.PortForwardURLGetter

	fileWriter *util.MultiSourceFileWriter
	builder    *resource.Builder
	args       []string

	// directory where all gathered data will be stored
	baseDir string
	// whether or not to allow writes to an existing and populated base directory
	overwrite bool

	genericclioptions.IOStreams
}

func NewInspectOptions(streams genericclioptions.IOStreams) *InspectOptions {
	return &InspectOptions{
		printFlags:  genericclioptions.NewPrintFlags("gathered").WithDefaultOutput("yaml").WithTypeSetter(scheme.Scheme),
		configFlags: genericclioptions.NewConfigFlags(),
		overwrite:   true,
		IOStreams:   streams,
	}
}

func NewCmdInspect(parentName string, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewInspectOptions(streams)

	cmd := &cobra.Command{
		Use:          "inspect <operator> [flags]",
		Short:        "Collect debugging data for a given cluster operator",
		Example:      fmt.Sprintf(inspectExample, parentName),
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

	o.printFlags.AddFlags(cmd)
	return cmd
}

func (o *InspectOptions) Complete(cmd *cobra.Command, args []string) error {
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
	o.podUrlGetter = &util.PortForwardURLGetter{
		Protocol:  "https",
		Host:      "localhost",
		LocalPort: "37587",
	}

	o.builder = resource.NewBuilder(o.configFlags)
	return nil
}

func (o *InspectOptions) Validate() error {
	if len(o.args) != 1 {
		return fmt.Errorf("exactly 1 argument (operator name) is supported")
	}
	if len(o.baseDir) == 0 {
		return fmt.Errorf("--base-dir must not be empty")
	}
	return nil
}

func (o *InspectOptions) Run() error {
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
	skippedNamespaces := []string{}
	errs := []error{}
	if err := o.gatherConfigResourceData(path.Join(o.baseDir, "/cluster-scoped-resources/config.openshift.io")); err != nil {
		errs = append(errs, err)
	}

	// gather operator.openshift.io resource data
	if err := o.gatherOperatorResourceData(path.Join(o.baseDir, "/cluster-scoped-resources/operator.openshift.io")); err != nil {
		errs = append(errs, err)
	}

	for _, info := range infos {
		// save clusteroperator resources
		if err := o.gatherClusterOperatorResource(path.Join(o.baseDir, "/clusteroperator"), info); err != nil {
			errs = append(errs, err)
			continue
		}

		namespaces, err := obtainClusterOperatorNamespaces(info.Object)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		// save operator data for each clusteroperator namespace
		for _, namespace := range namespaces {
			if err := o.gatherNamespaceData(path.Join(o.baseDir, "namespaces", namespace), namespace); err != nil {
				if kapierrs.IsNotFound(err) {
					skippedNamespaces = append(skippedNamespaces, namespace)
					continue
				}

				errs = append(errs, err)
				continue
			}
		}
	}

	if len(skippedNamespaces) > 0 {
		for _, namespace := range skippedNamespaces {
			log.Printf("Data collection skipped namespace %q. Unable to find namespace...\n", namespace)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("One or more errors ocurred gathering cluster data:\n\n    %v", errors.NewAggregate(errs))
	}

	log.Printf("Finished successfully with no errors.\n")
	return nil
}

func obtainClusterOperatorNamespaces(obj runtime.Object) ([]string, error) {
	// obtain related namespace info for the current clusteroperator
	unstructuredCO, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("invalid resource type, expecting clusteroperators but got %T", obj)
	}
	log.Printf("    Gathering namespace information for ClusterOperator %q...\n", unstructuredCO.GetName())

	structuredCO := &configv1.ClusterOperator{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredCO.Object, structuredCO); err != nil {
		return nil, err
	}

	namespaces := []string{}
	for _, related := range structuredCO.Status.RelatedObjects {
		if related.Resource != "namespaces" {
			continue
		}

		namespaces = append(namespaces, related.Name)
		log.Printf("    Found related namespace %q for ClusterOperator %q...\n", related.Name, structuredCO.Name)
	}
	if len(namespaces) == 0 {
		log.Printf("    Falling back to <operator> namespace %q for ClusterOperator %q. Unable to find any related namespaces in object status...\n", structuredCO.Name, structuredCO.Name)
		log.Printf("    Falling back to <operand> namespace %q for ClusterOperator %q. Unable to find any related namespaces in object status...\n", strings.TrimSuffix(structuredCO.Name, "-operator"), structuredCO.Name)
		namespaces = []string{structuredCO.Name, strings.TrimSuffix(structuredCO.Name, "-operator")}
	}

	return namespaces, nil
}

// ensureDirectoryViable returns an error if the given path:
// 1. already exists AND is a file (not a directory)
// 2. already exists AND is NOT empty
// 3. an IO error occurs
func (o *InspectOptions) ensureDirectoryViable(dirPath string, allowDataOverride bool) error {
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

func (o *InspectOptions) gatherClusterOperatorResource(destDir string, info *resource.Info) error {
	log.Printf("Gathering cluster operator resource data...\n")

	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.yaml", info.Name)
	return o.fileWriter.WriteFromResource(path.Join(destDir, "/"+filename), info.Object)
}

// gatherConfigResourceData gathers all config.openshift.io resources
func (o *InspectOptions) gatherConfigResourceData(destDir string) error {
	log.Printf("Gathering config.openshift.io resource data...\n")

	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	resources, err := retrieveAPIGroupVersionResourceNames(o.discoveryClient, configv1.GroupName)
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

// gatherOperatorResourceData gathers all kubeapiserver.operator.openshift.io resources
func (o *InspectOptions) gatherOperatorResourceData(destDir string) error {
	log.Printf("Gathering kubeapiserver.operator.openshift.io resource data...\n")

	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	resources, err := retrieveAPIGroupVersionResourceNames(o.discoveryClient, "kubeapiserver.operator.openshift.io")
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

func retrieveAPIGroupVersionResourceNames(discoveryClient discovery.CachedDiscoveryInterface, apiGroup string) ([]schema.GroupVersionResource, error) {
	lists, discoveryErr := discoveryClient.ServerPreferredResources()

	foundResources := sets.String{}
	resources := []schema.GroupVersionResource{}
	for _, list := range lists {
		if len(list.APIResources) == 0 {
			continue
		}
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			/// something went seriously wrong
			return nil, err
		}
		for _, resource := range list.APIResources {
			// filter groups outside of the provided apiGroup
			if !strings.HasSuffix(gv.Group, apiGroup) {
				continue
			}
			verbs := sets.NewString(([]string(resource.Verbs))...)
			if !verbs.Has("list") {
				continue
			}
			// if we've already seen this resource in another version, don't add it again
			if foundResources.Has(resource.Name) {
				foundResources.Insert(resource.Name)
			}
			resources = append(resources, schema.GroupVersionResource{Group: gv.Group, Version: gv.Version, Resource: resource.Name})
		}
	}
	// we only care about discovery errors if we don't find what we want
	if len(resources) == 0 {
		return nil, discoveryErr
	}

	return resources, nil
}

func (o *InspectOptions) gatherNamespaceData(destDir, namespace string) error {
	log.Printf("Gathering data for ns/%s...\n", namespace)

	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	ns, err := o.kubeClient.CoreV1().Namespaces().Get(namespace, metav1.GetOptions{})
	if err != nil {
		return err
	}
	ns.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Namespace"))

	// write namespace.yaml file
	filename := fmt.Sprintf("%s.yaml", namespace)
	if err := o.fileWriter.WriteFromResource(path.Join(destDir, "/"+filename), ns); err != nil {
		return err
	}

	log.Printf("    Collecting resources for namespace %q...\n", namespace)

	resourcesTypesToStore := map[schema.GroupVersionResource]bool{
		corev1.SchemeGroupVersion.WithResource("events"):            true,
		corev1.SchemeGroupVersion.WithResource("pods"):              true,
		corev1.SchemeGroupVersion.WithResource("configmaps"):        true,
		corev1.SchemeGroupVersion.WithResource("services"):          true,
		appsv1.SchemeGroupVersion.WithResource("deployments"):       true,
		appsv1.SchemeGroupVersion.WithResource("daemonsets"):        true,
		appsv1.SchemeGroupVersion.WithResource("statefulsets"):      true,
		{Group: route.GroupName, Version: "v1", Resource: "routes"}: true,
	}
	resourcesToStore := map[schema.GroupVersionResource]runtime.Object{}

	// collect resource information for namespace
	for gvr := range resourcesTypesToStore {
		list, err := o.dynamicClient.Resource(gvr).Namespace(namespace).List(metav1.ListOptions{})
		if err != nil {
			return err
		}
		resourcesToStore[gvr] = list

	}

	// store redacted secrets
	secrets, err := o.dynamicClient.Resource(corev1.SchemeGroupVersion.WithResource("secrets")).Namespace(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	secretsToStore := []unstructured.Unstructured{}
	for _, secret := range secrets.Items {
		if _, ok := secret.Object["data"]; ok {
			secret.Object["data"] = nil
		}
		secretsToStore = append(secretsToStore, secret)
	}
	secrets.Items = secretsToStore
	resourcesToStore[corev1.SchemeGroupVersion.WithResource("secrets")] = secrets

	// store redacted routes
	routes, err := o.dynamicClient.Resource(schema.GroupVersionResource{Group: route.GroupName, Version: "v1", Resource: "routes"}).Namespace(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	routesToStore := []unstructured.Unstructured{}
	for _, route := range routes.Items {
		// TODO, you only want to remove the key
		if _, ok := route.Object["tls"]; ok {
			route.Object["tls"] = nil
		}
		routesToStore = append(routesToStore, route)
	}
	routes.Items = routesToStore
	resourcesToStore[schema.GroupVersionResource{Group: route.GroupName, Version: "v1", Resource: "routes"}] = routes

	errs := []error{}
	for gvr, obj := range resourcesToStore {
		filename := fmt.Sprintf("%s.%s.yaml", gvr.Resource, gvr.Group)
		if err := o.fileWriter.WriteFromResource(path.Join(destDir, "/"+filename), obj); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors ocurred storing resource information for namespace %q:\n\n    %v", namespace, errors.NewAggregate(errs))
	}

	log.Printf("    Gathering pod data for namespace %q...\n", namespace)

	// gather specific pod data
	for _, pod := range resourcesToStore[corev1.SchemeGroupVersion.WithResource("pods")].(*unstructured.UnstructuredList).Items {
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

func (o *InspectOptions) gatherPodData(destDir, namespace string, pod *corev1.Pod) error {
	if pod.Status.Phase != corev1.PodRunning {
		log.Printf("        Skipping container data collection for pod %q: Pod not running\n", pod.Name)
		return nil
	}

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
		if err := o.gatherContainerInfo(path.Join(destDir, "/"+container.Name), pod, container); err != nil {
			errs = append(errs, err)
			continue
		}
	}
	for _, container := range pod.Spec.InitContainers {
		if err := o.gatherContainerInfo(path.Join(destDir, "/"+container.Name), pod, container); err != nil {
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("one or more errors ocurred while gathering container data for pod %s:\n\n    %v", pod.Name, errors.NewAggregate(errs))
	}
	return nil
}

func (o *InspectOptions) gatherContainerInfo(destDir string, pod *corev1.Pod, container corev1.Container) error {
	if err := o.gatherContainerAllLogs(path.Join(destDir, "/"+container.Name), pod, &container); err != nil {
		return err
	}

	if len(container.Ports) == 0 {
		log.Printf("        Skipping container endpoint collection for pod %q container %q: No ports\n", pod.Name, container.Name)
		return nil
	}
	port := &util.RemoteContainerPort{
		Protocol: "https",
		Port:     container.Ports[0].ContainerPort,
	}

	if err := o.gatherContainerEndpoints(path.Join(destDir, "/"+container.Name), pod, &container, port); err != nil {
		return err
	}

	return nil
}

func (o *InspectOptions) gatherContainerAllLogs(destDir string, pod *corev1.Pod, container *corev1.Container) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	errs := []error{}
	if err := o.gatherContainerLogs(path.Join(destDir, "/logs"), pod, container); err != nil {
		errs = append(errs, filterContainerLogsErrors(err))
	}

	if len(errs) > 0 {
		return errors.NewAggregate(errs)
	}
	return nil
}

func (o *InspectOptions) gatherContainerEndpoints(destDir string, pod *corev1.Pod, container *corev1.Container, metricsPort *util.RemoteContainerPort) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	errs := []error{}
	if err := o.gatherContainerHealthz(path.Join(destDir, "/healthz"), pod, metricsPort); err != nil {
		errs = append(errs, err)
	}
	if err := o.gatherContainerVersion(destDir, pod, metricsPort); err != nil {
		errs = append(errs, err)
	}
	if err := o.gatherContainerMetrics(destDir, pod, metricsPort); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errors.NewAggregate(errs)
	}
	return nil
}

func filterContainerLogsErrors(err error) error {
	if strings.Contains(err.Error(), "previous terminated container") && strings.HasSuffix(err.Error(), "not found") {
		log.Printf("        Unable to gather previous container logs: %v\n", err)
		return nil
	}
	return err
}

func (o *InspectOptions) gatherContainerVersion(destDir string, pod *corev1.Pod, metricsPort *util.RemoteContainerPort) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	hasVersionPath := false

	// determine if a /version endpoint exists
	paths, err := getAvailablePodEndpoints(o.podUrlGetter, pod, o.restConfig, metricsPort)
	if err != nil {
		return err
	}
	for _, p := range paths {
		if p != "/version" {
			continue
		}
		hasVersionPath = true
		break
	}
	if !hasVersionPath {
		log.Printf("        Skipping /version info gathering for pod %q. Endpoint not found...\n", pod.Name)
		return nil
	}

	result, err := o.podUrlGetter.Get("/version", pod, o.restConfig, metricsPort)

	filename := fmt.Sprintf("%s.json", "metrics")
	return o.fileWriter.WriteFromSource(path.Join(destDir, filename), &util.TextWriterSource{Text: result})
}

// gatherContainerMetrics invokes an asynchronous network call
func (o *InspectOptions) gatherContainerMetrics(destDir string, pod *corev1.Pod, metricsPort *util.RemoteContainerPort) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	// we need a token in order to access the /metrics endpoint
	result, err := o.podUrlGetter.Get("/metrics", pod, o.restConfig, metricsPort)
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.json", "metrics")
	return o.fileWriter.WriteFromSource(path.Join(destDir, filename), &util.TextWriterSource{Text: result})
}

func (o *InspectOptions) gatherContainerHealthz(destDir string, pod *corev1.Pod, metricsPort *util.RemoteContainerPort) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	paths, err := getAvailablePodEndpoints(o.podUrlGetter, pod, o.restConfig, metricsPort)
	if err != nil {
		return err
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
		result, err := o.podUrlGetter.Get(path.Join("/", healthzPath), pod, o.restConfig, metricsPort)
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

		filenameSegs := strings.Split(filename, "/")
		if len(filenameSegs) > 1 {
			// ensure directory structure for nested paths exists
			filenameSegs = filenameSegs[:len(filenameSegs)-1]
			if err := os.MkdirAll(path.Join(destDir, "/"+strings.Join(filenameSegs, "/")), os.ModePerm); err != nil {
				return err
			}
		}

		if err := o.fileWriter.WriteFromSource(path.Join(destDir, filename), &util.TextWriterSource{Text: result}); err != nil {
			return err
		}
	}
	return nil
}

func getAvailablePodEndpoints(urlGetter *util.PortForwardURLGetter, pod *corev1.Pod, config *rest.Config, port *util.RemoteContainerPort) ([]string, error) {
	result, err := urlGetter.Get("/", pod, config, port)
	if err != nil {
		return nil, err
	}

	resultBuffer := bytes.NewBuffer([]byte(result))
	pathInfo := map[string][]string{}

	// first, unmarshal result into json object and obtain all available /healthz endpoints
	if err := json.Unmarshal(resultBuffer.Bytes(), &pathInfo); err != nil {
		return nil, err
	}
	paths, ok := pathInfo["paths"]
	if !ok {
		return nil, fmt.Errorf("unable to extract path information for pod %q", pod.Name)
	}

	return paths, nil
}

func (o *InspectOptions) gatherContainerLogs(destDir string, pod *corev1.Pod, container *corev1.Container) error {
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
