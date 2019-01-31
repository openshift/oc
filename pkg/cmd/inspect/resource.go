package inspect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions/resource"
	"k8s.io/client-go/rest"

	ocpappsv1 "github.com/openshift/api/apps/v1"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/api/route"
	"github.com/openshift/must-gather/pkg/util"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	clusterScopedResourcesDirname = "cluster-scoped-resources"
	namespaceResourcesDirname     = "namespaces"

	configResourceDataKey   = "/cluster-scoped-resources/config.openshift.io"
	operatorResourceDataKey = "/cluster-scoped-resources/operator.openshift.io"
)

// InspectResource receives an object to gather debugging data for, and a context to keep track of
// already-seen objects when following related-object reference chains.
func InspectResource(info *resource.Info, context *resourceContext, o *InspectOptions) error {
	if context.visited.Has(infoToContextKey(info)) {
		log.Printf("Skipping previously-inspected resource: %q ...", infoToContextKey(info))
		return nil
	}
	context.visited.Insert(infoToContextKey(info))

	unstr, ok := info.Object.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("unexpected type. Expecting %q but got %T", "*unstructured.Unstructured", info.Object)
	}

	switch info.ResourceMapping().Resource.GroupResource() {
	case configv1.GroupVersion.WithResource("clusteroperators").GroupResource():
		// first, gather config.openshift.io resource data
		errs := []error{}
		if err := o.gatherConfigResourceData(path.Join(o.baseDir, "/cluster-scoped-resources/config.openshift.io"), context); err != nil {
			errs = append(errs, err)
		}

		// then, gather operator.openshift.io resource data
		if err := o.gatherOperatorResourceData(path.Join(o.baseDir, "/cluster-scoped-resources/operator.openshift.io"), context); err != nil {
			errs = append(errs, err)
		}

		// save clusteroperator resources to disk
		if err := gatherClusterOperatorResource(o.baseDir, unstr, o.fileWriter); err != nil {
			return err
		}

		// obtain associated objects for the current clusteroperator resources
		relatedObjReferences, err := obtainClusterOperatorRelatedObjects(unstr)
		if err != nil {
			return err
		}

		for _, relatedRef := range relatedObjReferences {
			if context.visited.Has(objectRefToContextKey(relatedRef)) {
				continue
			}

			relatedInfo, err := objectReferenceToResourceInfo(o.configFlags, relatedRef)
			if err != nil {
				errs = append(errs, err)
				continue
			}

			if err := InspectResource(relatedInfo, context, o); err != nil {
				errs = append(errs, err)
				continue
			}
		}

		if len(errs) > 0 {
			return errors.NewAggregate(errs)
		}
	case corev1.SchemeGroupVersion.WithResource("namespaces").GroupResource():
		if err := o.gatherNamespaceData(o.baseDir, info.Name); err != nil {
			return err
		}
	default:
		// save the current object to disk
		filename := fmt.Sprintf("%s.yaml", unstr.GetName())
		dirPath := dirPathForInfo(o.baseDir, info)
		// ensure destination path exists
		if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
			return err
		}

		return o.fileWriter.WriteFromResource(path.Join(dirPath, "/"+filename), info.Object)
	}

	return nil
}

func gatherClusterOperatorResource(baseDir string, obj *unstructured.Unstructured, fileWriter *util.MultiSourceFileWriter) error {
	log.Printf("Gathering cluster operator resource data...\n")

	// ensure destination path exists
	destDir := path.Join(baseDir, "/"+clusterScopedResourcesDirname, "/"+obj.GroupVersionKind().Group, "/clusteroperators")
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.yaml", obj.GetName())
	return fileWriter.WriteFromResource(path.Join(destDir, "/"+filename), obj)
}

func obtainClusterOperatorRelatedObjects(obj *unstructured.Unstructured) ([]*configv1.ObjectReference, error) {
	// obtain related namespace info for the current clusteroperator
	log.Printf("    Gathering related object reference information for ClusterOperator %q...\n", obj.GetName())

	structuredCO := &configv1.ClusterOperator{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, structuredCO); err != nil {
		return nil, err
	}

	relatedObjs := []*configv1.ObjectReference{}
	for idx, relatedObj := range structuredCO.Status.RelatedObjects {
		relatedObjs = append(relatedObjs, &structuredCO.Status.RelatedObjects[idx])
		log.Printf("    Found related object %q for ClusterOperator %q...\n", relatedObj.Name, structuredCO.Name)
	}

	return relatedObjs, nil
}

func (o *InspectOptions) gatherNamespaceData(baseDir, namespace string) error {
	log.Printf("Gathering data for ns/%s...\n", namespace)

	destDir := path.Join(baseDir, namespaceResourcesDirname, namespace)

	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	ns, err := o.kubeClient.CoreV1().Namespaces().Get(namespace, metav1.GetOptions{})
	if err != nil { // If we can't get the namespace we need to exit out
		return err
	}
	ns.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Namespace"))

	errs := []error{}
	// write namespace.yaml file
	filename := fmt.Sprintf("%s.yaml", namespace)
	if err := o.fileWriter.WriteFromResource(path.Join(destDir, "/"+filename), ns); err != nil {
		errs = append(errs, err)
	}

	log.Printf("    Collecting resources for namespace %q...\n", namespace)

	resourcesTypesToStore := map[schema.GroupVersionResource]bool{
		corev1.SchemeGroupVersion.WithResource("events"):               true,
		corev1.SchemeGroupVersion.WithResource("pods"):                 true,
		corev1.SchemeGroupVersion.WithResource("configmaps"):           true,
		corev1.SchemeGroupVersion.WithResource("services"):             true,
		appsv1.SchemeGroupVersion.WithResource("deployments"):          true,
		ocpappsv1.SchemeGroupVersion.WithResource("deploymentconfigs"): true,
		appsv1.SchemeGroupVersion.WithResource("daemonsets"):           true,
		appsv1.SchemeGroupVersion.WithResource("statefulsets"):         true,
		{Group: route.GroupName, Version: "v1", Resource: "routes"}:    true,
	}
	resourcesToStore := map[schema.GroupVersionResource]runtime.Object{}

	// collect resource information for namespace
	for gvr := range resourcesTypesToStore {
		list, err := o.dynamicClient.Resource(gvr).Namespace(namespace).List(metav1.ListOptions{})
		if err != nil {
			errs = append(errs, err)
		}
		resourcesToStore[gvr] = list

	}

	// store redacted secrets
	secrets, err := o.dynamicClient.Resource(corev1.SchemeGroupVersion.WithResource("secrets")).Namespace(namespace).List(metav1.ListOptions{})
	if err != nil {
		errs = append(errs, err)
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
		errs = append(errs, err)
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

	for gvr, obj := range resourcesToStore {
		filename := gvr.Resource
		if len(gvr.Group) > 0 {
			filename += "." + gvr.Group
		}
		filename += ".yaml"
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
	if running, err := util.PodRunningReady(pod); err != nil {
		return err
	} else if !running {
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
