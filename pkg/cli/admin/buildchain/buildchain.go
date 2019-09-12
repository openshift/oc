package buildchain

import (
	"fmt"
	"io"
	"strings"

	"github.com/openshift/library-go/pkg/image/imageutil"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/klog"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/api/image"
	buildv1client "github.com/openshift/client-go/build/clientset/versioned/typed/build/v1"
	imagev1client "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	projectv1client "github.com/openshift/client-go/project/clientset/versioned/typed/project/v1"
	osutil "github.com/openshift/oc/pkg/helpers/cmd"
	"github.com/openshift/oc/pkg/helpers/describe"
	imagegraph "github.com/openshift/oc/pkg/helpers/graph/imagegraph/nodes"
)

// BuildChainRecommendedCommandName is the recommended command name
const BuildChainRecommendedCommandName = "build-chain"

var (
	buildChainLong = templates.LongDesc(`
		Output the inputs and dependencies of your builds

		Supported formats for the generated graph are dot and a human-readable output.
		Tag and namespace are optional and if they are not specified, 'latest' and the
		default namespace will be used respectively.`)

	buildChainExample = templates.Examples(`
		# Build the dependency tree for the 'latest' tag in <image-stream>
	  %[1]s <image-stream>

	  # Build the dependency tree for 'v2' tag in dot format and visualize it via the dot utility
	  %[1]s <image-stream>:v2 -o dot | dot -T svg -o deps.svg

	  # Build the dependency tree across all namespaces for the specified image stream tag found in 'test' namespace
	  %[1]s <image-stream> -n test --all`)
)

// BuildChainOptions contains all the options needed for build-chain
type BuildChainOptions struct {
	name string

	defaultNamespace string
	namespaces       sets.String
	allNamespaces    bool
	triggerOnly      bool
	reverse          bool

	output string

	buildClient   buildv1client.BuildV1Interface
	imageClient   imagev1client.ImageV1Interface
	projectClient projectv1client.ProjectV1Interface
}

// NewCmdBuildChain implements the OpenShift experimental build-chain command
func NewCmdBuildChain(name, fullName string, f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	options := &BuildChainOptions{
		namespaces: sets.NewString(),
	}
	cmd := &cobra.Command{
		Use:     "build-chain IMAGESTREAMTAG",
		Short:   "Output the inputs and dependencies of your builds",
		Long:    buildChainLong,
		Example: fmt.Sprintf(buildChainExample, fullName),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(options.Complete(f, cmd, args, streams.Out))
			kcmdutil.CheckErr(options.Validate())
			kcmdutil.CheckErr(options.RunBuildChain())
		},
	}

	cmd.Flags().BoolVar(&options.allNamespaces, "all", false, "If true, build dependency tree for the specified image stream tag across all namespaces")
	cmd.Flags().BoolVar(&options.triggerOnly, "trigger-only", true, "If true, only include dependencies based on build triggers. If false, include all dependencies.")
	cmd.Flags().BoolVar(&options.reverse, "reverse", false, "If true, show the istags dependencies instead of its dependants.")
	cmd.Flags().StringVarP(&options.output, "output", "o", "", "Output format of dependency tree")
	return cmd
}

// Complete completes the required options for build-chain
func (o *BuildChainOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string, out io.Writer) error {
	if len(args) != 1 {
		return kcmdutil.UsageErrorf(cmd, "Must pass an image stream tag. If only an image stream name is specified, 'latest' will be used for the tag.")
	}

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.buildClient, err = buildv1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	o.imageClient, err = imagev1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	o.projectClient, err = projectv1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	resource := schema.GroupResource{}
	mapper, err := f.ToRESTMapper()
	if err != nil {
		return err
	}
	resource, o.name, err = osutil.ResolveResource(image.Resource("imagestreamtags"), args[0], mapper)
	if err != nil {
		return err
	}

	switch resource {
	case image.Resource("imagestreamtags"):
		o.name = normalizeImageStreamTag(o.name)
		klog.V(4).Infof("Using %q as the image stream tag to look dependencies for", o.name)
	default:
		return fmt.Errorf("invalid resource provided: %v", resource)
	}

	// Setup namespace
	if o.allNamespaces {
		// TODO: Handle different uses of build-chain; user and admin
		projectList, err := o.projectClient.Projects().List(metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, project := range projectList.Items {
			klog.V(4).Infof("Found namespace %q", project.Name)
			o.namespaces.Insert(project.Name)
		}
	}

	o.defaultNamespace, _, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}
	klog.V(4).Infof("Using %q as the namespace for %q", o.defaultNamespace, o.name)
	o.namespaces.Insert(o.defaultNamespace)
	klog.V(4).Infof("Will look for deps in %s", strings.Join(o.namespaces.List(), ","))

	return nil
}

// normalizeImageStreamTag normalizes an image stream tag by defaulting to 'latest'
// if no tag has been specified.
func normalizeImageStreamTag(name string) string {
	stripped, tag, ok := imageutil.SplitImageStreamTag(name)
	if !ok {
		// Default to latest
		return imageutil.JoinImageStreamTag(stripped, tag)
	}
	return name
}

// Validate returns validation errors regarding build-chain
func (o *BuildChainOptions) Validate() error {
	if len(o.name) == 0 {
		return fmt.Errorf("image stream tag cannot be empty")
	}
	if len(o.defaultNamespace) == 0 {
		return fmt.Errorf("default namespace cannot be empty")
	}
	if o.output != "" && o.output != "dot" {
		return fmt.Errorf("output must be either empty or 'dot'")
	}
	if o.buildClient == nil {
		return fmt.Errorf("buildConfig client must not be nil")
	}
	if o.imageClient == nil {
		return fmt.Errorf("imageStreamTag client must not be nil")
	}
	if o.projectClient == nil {
		return fmt.Errorf("project client must not be nil")
	}
	return nil
}

// RunBuildChain contains all the necessary functionality for the OpenShift
// experimental build-chain command
func (o *BuildChainOptions) RunBuildChain() error {
	ist := imagegraph.MakeImageStreamTagObjectMeta2(o.defaultNamespace, o.name)

	desc, err := describe.NewChainDescriber(o.buildClient, o.namespaces, o.output).Describe(ist, !o.triggerOnly, o.reverse)
	if err != nil {
		if _, isNotFoundErr := err.(describe.NotFoundErr); isNotFoundErr {
			// Try to get the imageStreamTag via a direct GET
			if _, getErr := o.imageClient.ImageStreamTags(o.defaultNamespace).Get(o.name, metav1.GetOptions{}); getErr != nil {
				return getErr
			}
			fmt.Printf("Image stream tag %q in %q doesn't have any dependencies.\n", o.name, o.defaultNamespace)
			return nil
		}
		return err
	}

	fmt.Println(desc)

	return nil
}
