package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericclioptions/resource"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/api/route"
	routev1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"

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
	routesClient    routev1.RouteV1Interface
	kubeClient      kubernetes.Interface
	discoveryClient discovery.CachedDiscoveryInterface
	dynamicClient   dynamic.Interface

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
		printFlags:  genericclioptions.NewPrintFlags("gathered").WithDefaultOutput("yaml"),
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

	o.routesClient, err = routev1.NewForConfig(o.restConfig)
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

		// save operator data for each clusteroperator
		if err := o.gatherClusterOperatorNamespaceData(path.Join(o.baseDir, "/"+info.Name), info.Name); err != nil {
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("One or more errors ocurred gathering cluster data:\n\n%v", errors.NewAggregate(errs))
	}
	return nil
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
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.yaml", info.Name)
	return o.fileWriter.WriteFromResource(path.Join(destDir, "/"+filename), info.Object)
}

func (o *InfoOptions) gatherConfigResourceData(destDir string) error {
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
		return fmt.Errorf("one or more errors ocurred while gathering config.openshift.io resource data:\n\n%v", errors.NewAggregate(errs))
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

	resourcesToStore := map[string]runtime.Object{}

	// collect resource information for namespace
	pods, err := o.kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	pods.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("PodList"))
	resourcesToStore["pods.yaml"] = pods

	configmaps, err := o.kubeClient.CoreV1().ConfigMaps(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	configmaps.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMapList"))
	resourcesToStore["configmaps.yaml"] = configmaps

	services, err := o.kubeClient.CoreV1().Services(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	services.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ServiceList"))
	resourcesToStore["services.yaml"] = services

	deployments, err := o.kubeClient.AppsV1().Deployments(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	deployments.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("DeploymentList"))
	resourcesToStore["deployments.yaml"] = deployments

	daemonsets, err := o.kubeClient.AppsV1().DaemonSets(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	daemonsets.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("DaemonSetList"))
	resourcesToStore["daemonsets.yaml"] = daemonsets

	statefulsets, err := o.kubeClient.AppsV1().StatefulSets(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	statefulsets.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("StatefulSetList"))
	resourcesToStore["statefulsets.yaml"] = statefulsets

	routes, err := o.routesClient.Routes(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	routes.SetGroupVersionKind(schema.GroupVersionKind{Group: route.GroupName, Kind: "RouteList"})
	resourcesToStore["routes.yaml"] = routes

	// store redacted secrets
	secrets, err := o.kubeClient.CoreV1().Secrets(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	secrets.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("SecretList"))
	resourcesToStore["secrets.yaml"] = secrets

	secretsToStore := []corev1.Secret{}
	for _, secret := range secrets.Items {
		secret.Data = nil
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
		return fmt.Errorf("errors ocurred storing resource information for namespace %q:\n\n%v", namespace, errors.NewAggregate(errs))
	}

	// gather specific pod data
	for _, pod := range pods.Items {
		pod.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Pod"))
		if err := o.gatherPodData(path.Join(destDir, "/pods/"+pod.Name), namespace, &pod); err != nil {
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("one or more errors ocurred while gathering pod-specific data for namespace: %s\n\n%v", namespace, errors.NewAggregate(errs))
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

	// gather data for each container in the given pod
	for _, container := range pod.Spec.Containers {
		if err := o.gatherContainerData(path.Join(destDir, "/"+container.Name), pod, &container); err != nil {
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("one or more errors ocurred while gathering container data for pod %s:\n\n%v", pod.Name, errors.NewAggregate(errs))
	}
	return nil
}

func (o *InfoOptions) gatherContainerData(destDir string, pod *corev1.Pod, container *corev1.Container) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	// gather logs
	if err := o.gatherContainerLogs(path.Join(destDir, "/logs"), pod, container); err != nil {
		return err
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
