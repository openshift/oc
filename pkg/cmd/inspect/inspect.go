package inspect

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	fileWriter    *util.MultiSourceFileWriter
	builder       *resource.Builder
	args          []string
	namespace     string
	allNamespaces bool

	// directory where all gathered data will be stored
	baseDir string
	// whether or not to allow writes to an existing and populated base directory
	overwrite bool
	// flag to indicate GSS defaults should be used in deciding what to gather
	gssdefaults bool

	genericclioptions.IOStreams
}

func NewInspectOptions(streams genericclioptions.IOStreams) *InspectOptions {
	return &InspectOptions{
		printFlags:  genericclioptions.NewPrintFlags("gathered").WithDefaultOutput("yaml").WithTypeSetter(scheme.Scheme),
		configFlags: genericclioptions.NewConfigFlags(),
		overwrite:   true,
		gssdefaults: false,
		IOStreams:   streams,
	}
}

func NewCmdInspect(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewInspectOptions(streams)

	cmd := &cobra.Command{
		Use:          "inspect <operator> [flags]",
		Short:        "Collect debugging data for a given cluster operator",
		Example:      fmt.Sprintf(inspectExample, os.Args[0]),
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
	cmd.Flags().BoolVar(&o.allNamespaces, "all-namespaces", o.allNamespaces, "If present, list the requested object(s) across all namespaces. Namespace in current context is ignored even if specified with --namespace.")
	cmd.Flags().BoolVar(&o.gssdefaults, "default", false, "If true, use Red Hat Support suggested default options for data filtering.  This can save space.")

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

	o.namespace, _, err = o.configFlags.ToRawKubeConfigLoader().Namespace()
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
	if len(o.baseDir) == 0 {
		return fmt.Errorf("--base-dir must not be empty")
	}
	return nil
}

func (o *InspectOptions) Run() error {
	r := o.builder.
		Unstructured().
		NamespaceParam(o.namespace).DefaultNamespace().AllNamespaces(o.allNamespaces).
		ResourceTypeOrNameArgs(true, o.args...).
		Flatten().
		Latest().Do()

	infos, err := r.Infos()
	if err != nil {
		return err
	}

	// ensure we're able to proceed writing data to specified destination
	if err := o.ensureDirectoryViable(o.baseDir, o.overwrite); err != nil {
		return err
	}

	// finally, gather polymorphic resources specified by the user
	allErrs := []error{}
	ctx := NewResourceContext()
	for _, info := range infos {
		err := InspectResource(info, ctx, o)
		if err != nil {
			allErrs = append(allErrs, err)
		}
	}

	if len(allErrs) > 0 {
		return errors.NewAggregate(allErrs)
	}

	log.Printf("Finished successfully with no errors.\n")
	return nil
}

// gatherConfigResourceData gathers all config.openshift.io resources
func (o *InspectOptions) gatherConfigResourceData(destDir string, ctx *resourceContext) error {
	// determine if we've already collected configResourceData
	if ctx.visited.Has(configResourceDataKey) {
		log.Printf("Skipping previously-collected config.openshift.io resource data")
		return nil
	}
	ctx.visited.Insert(configResourceDataKey)

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

// gatherOperatorResourceData gathers all kubeapiserver.operator.openshift.io resources
func (o *InspectOptions) gatherOperatorResourceData(destDir string, ctx *resourceContext) error {
	// determine if we've already collected operatorResourceData
	if ctx.visited.Has(operatorResourceDataKey) {
		log.Printf("Skipping previously-collected operator.openshift.io resource data")
		return nil
	}
	ctx.visited.Insert(operatorResourceDataKey)

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
				continue
			}

			foundResources.Insert(resource.Name)
			resources = append(resources, schema.GroupVersionResource{Group: gv.Group, Version: gv.Version, Resource: resource.Name})
		}
	}
	// we only care about discovery errors if we don't find what we want
	if len(resources) == 0 {
		return nil, discoveryErr
	}

	return resources, nil
}
