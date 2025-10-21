package set

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/kubectl/pkg/cmd/set"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	ktemplates "k8s.io/kubectl/pkg/util/templates"

	imageclient "github.com/openshift/client-go/image/clientset/versioned"
	imagetypedclient "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	"github.com/openshift/oc/pkg/helpers/clientcmd"
)

var (
	setLong = ktemplates.LongDesc(`
		Configure application resources

		These commands help you make changes to existing application resources.`)
)

// NewCmdSet exposes commands for modifying objects.
func NewCmdSet(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	set := &cobra.Command{
		Use:   "set COMMAND",
		Short: "Commands that help set specific features on objects",
		Long:  setLong,
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	groups := ktemplates.CommandGroups{
		{
			Message: "Manage workloads:",
			Commands: []*cobra.Command{
				NewCmdDeploymentHook(f, streams),
				NewCmdEnv(f, streams),
				NewCmdImage(f, streams),
				// TODO: this seems reasonable to upstream
				NewCmdProbe(f, streams),
				NewCmdResources(f, streams),
				NewCmdSelector(f, streams),
				NewCmdServiceAccount(f, streams),
				NewCmdVolume(f, streams),
			},
		},
		{
			Message: "Manage secrets and config:",
			Commands: []*cobra.Command{
				NewCmdData(f, streams),
				NewCmdBuildSecret(f, streams),
			},
		},
		{
			Message: "Manage application flows:",
			Commands: []*cobra.Command{
				NewCmdBuildHook(f, streams),
				NewCmdImageLookup(f, streams),
				NewCmdTriggers(f, streams),
			},
		},
		{
			Message: "Manage load balancing:",
			Commands: []*cobra.Command{
				NewCmdRouteBackends(f, streams),
			},
		},
		{
			Message: "Manage authorization policy:",
			Commands: []*cobra.Command{
				NewCmdSubject(f, streams),
			},
		},
	}
	groups.Add(set)
	return set
}

var (
	setImageLong = ktemplates.LongDesc(`
Update existing container image(s) of resources.`)

	setImageExample = ktemplates.Examples(`
	  # Set a deployment config's nginx container image to 'nginx:1.9.1', and its busybox container image to 'busybox'.
	  oc set image dc/nginx busybox=busybox nginx=nginx:1.9.1

	  # Set a deployment config's app container image to the image referenced by the imagestream tag 'openshift/ruby:2.3'.
	  oc set image dc/myapp app=openshift/ruby:2.3 --source=imagestreamtag

	  # Update all deployments' and rc's nginx container's image to 'nginx:1.9.1'
	  oc set image deployments,rc nginx=nginx:1.9.1 --all

	  # Update image of all containers of daemonset abc to 'nginx:1.9.1'
	  oc set image daemonset abc *=nginx:1.9.1

	  # Print result (in YAML format) of updating nginx container image from local file, without hitting the server
	  oc set image -f path/to/file.yaml nginx=nginx:1.9.1 --local -o yaml`)
)

// NewCmdImage is a wrapper for the Kubernetes CLI set image command
func NewCmdImage(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := set.NewCmdImage(f, streams)
	cmd.Long = setImageLong
	cmd.Example = setImageExample
	cmd.Flags().String("source", "docker", "The image source type; valid types are 'imagestreamtag', 'istag', 'imagestreamimage', 'isimage', and 'docker'")
	set.ImageResolver = resolveImageFactory(f, cmd)

	return cmd
}

var (
	setResourcesLong = ktemplates.LongDesc(`
Specify compute resource requirements (cpu, memory) for any resource that defines a pod template. If a pod is successfully scheduled, it is guaranteed the amount of resource requested, but may burst up to its specified limits.

For each compute resource, if a limit is specified and a request is omitted, the request will default to the limit.

Possible resources include (case insensitive):
"ReplicationController", "Deployment", "DaemonSet", "Job", "ReplicaSet", "DeploymentConfigs"`)

	setResourcesExample = ktemplates.Examples(`
# Set a deployments nginx container CPU limits to "200m and memory to 512Mi"
oc set resources deployment nginx -c=nginx --limits=cpu=200m,memory=512Mi

# Set the resource request and limits for all containers in nginx
oc set resources deployment nginx --limits=cpu=200m,memory=512Mi --requests=cpu=100m,memory=256Mi

# Remove the resource requests for resources on containers in nginx
oc set resources deployment nginx --limits=cpu=0,memory=0 --requests=cpu=0,memory=0

# Print the result (in YAML format) of updating nginx container limits locally, without hitting the server
oc set resources -f path/to/file.yaml --limits=cpu=200m,memory=512Mi --local -o yaml`)
)

// NewCmdResources is a wrapper for the Kubernetes CLI set resources command
func NewCmdResources(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := set.NewCmdResources(f, streams)
	cmd.Long = setResourcesLong
	cmd.Example = setResourcesExample

	return cmd
}

var (
	setSelectorLong = ktemplates.LongDesc(`
Set the selector on a resource. Note that the new selector will overwrite the old selector if the resource had one prior to the invocation
of 'set selector'.

A selector must begin with a letter or number, and may contain letters, numbers, hyphens, dots, and underscores, up to oc characters.
If --resource-version is specified, then updates will use this resource version, otherwise the existing resource-version will be used.
Note: currently selectors can only be set on service objects.`)

	setSelectorExample = ktemplates.Examples(`
# Set the labels and selector before creating a deployment/service pair.
oc create service clusterip my-svc --clusterip="None" -o yaml --dry-run | oc set selector --local -f - 'environment=qa' -o yaml | oc create -f -
oc create deployment my-dep -o yaml --dry-run | oc label --local -f - environment=qa -o yaml | oc create -f -`)
)

// NewCmdSelector is a wrapper for the Kubernetes CLI set selector command
func NewCmdSelector(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := set.NewCmdSelector(f, streams)
	cmd.Long = setSelectorLong
	cmd.Example = setSelectorExample

	return cmd
}

var (
	setServiceaccountLong = ktemplates.LongDesc(`
Update ServiceAccount of pod template resources.
`)

	setServiceaccountExample = ktemplates.Examples(`
# Set deployment nginx-deployment's service account to serviceaccount1
oc set serviceaccount deployment nginx-deployment serviceaccount1

# Print the result (in YAML format) of updated nginx deployment with service account from a local file, without hitting the API server
oc set sa -f nginx-deployment.yaml serviceaccount1 --local --dry-run -o yaml
`)
)

// NewCmdServiceAccount is a wrapper for the Kubernetes CLI set serviceaccount command
func NewCmdServiceAccount(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := set.NewCmdServiceAccount(f, streams)
	cmd.Long = setServiceaccountLong
	cmd.Example = setServiceaccountExample

	return cmd
}

var (
	setSubjectLong = ktemplates.LongDesc(`
Update user, group or service account in a role binding or cluster role binding.`)

	setSubjectExample = ktemplates.Examples(`
# Update a cluster role binding for serviceaccount1
oc set subject clusterrolebinding admin --serviceaccount=namespace:serviceaccount1

# Update a role binding for user1, user2, and group1
oc set subject rolebinding admin --user=user1 --user=user2 --group=group1

# Print the result (in YAML format) of updating role binding subjects locally, without hitting the server
oc create rolebinding admin --role=admin --user=admin -o yaml --dry-run | oc set subject --local -f - --user=foo -o yaml`)
)

// NewCmdSubject is a wrapper for the Kubernetes CLI set subject command
func NewCmdSubject(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := set.NewCmdSubject(f, streams)
	cmd.Long = setSubjectLong
	cmd.Example = setSubjectExample

	return cmd
}

func resolveImageFactory(f kcmdutil.Factory, cmd *cobra.Command) set.ImageResolverFunc {
	resolveImageFn := func(in string) (string, error) {
		return in, nil
	}
	return func(image string) (string, error) {
		source, err := cmd.Flags().GetString("source")
		if err != nil {
			return resolveImageFn(source)
		}
		if isDockerImageSource(source) {
			return resolveImageFn(image)
		}
		config, err := f.ToRESTConfig()
		if err != nil {
			return "", err
		}
		imageClient, err := imageclient.NewForConfig(config)
		if err != nil {
			return "", err
		}
		namespace, _, err := f.ToRawKubeConfigLoader().Namespace()
		if err != nil {
			return "", err
		}

		return resolveImagePullSpec(imageClient.ImageV1(), source, image, namespace)
	}
}

// resolveImagePullSpec resolves the provided source which can be "docker", "istag" or
// "isimage" and returns the full Docker pull spec.
func resolveImagePullSpec(imageClient imagetypedclient.ImageV1Interface, source, name, defaultNamespace string) (string, error) {
	// for Docker source, just passtrough the image name
	if isDockerImageSource(source) {
		return name, nil
	}
	// parse the namespace from the provided image
	namespace, image := splitNamespaceAndImage(name)
	if len(namespace) == 0 {
		namespace = defaultNamespace
	}

	dockerImageReference := ""

	if isImageStreamTag(source) {
		if resolved, err := imageClient.ImageStreamTags(namespace).Get(context.TODO(), image, metav1.GetOptions{}); err != nil {
			return "", fmt.Errorf("failed to get image stream tag %q: %v", image, err)
		} else {
			dockerImageReference = resolved.Image.DockerImageReference
		}
	}

	if isImageStreamImage(source) {
		if resolved, err := imageClient.ImageStreamImages(namespace).Get(context.TODO(), image, metav1.GetOptions{}); err != nil {
			return "", fmt.Errorf("failed to get image stream image %q: %v", image, err)
		} else {
			dockerImageReference = resolved.Image.DockerImageReference
		}
	}

	if len(dockerImageReference) == 0 {
		return "", fmt.Errorf("unable to resolve %s %q", source, name)
	}

	return clientcmd.ParseDockerImageReferenceToStringFunc(dockerImageReference)
}

func isDockerImageSource(source string) bool {
	return source == "docker"
}

func isImageStreamTag(source string) bool {
	return source == "istag" || source == "imagestreamtag"
}

func isImageStreamImage(source string) bool {
	return source == "isimage" || source == "imagestreamimage"
}

func splitNamespaceAndImage(name string) (string, string) {
	namespace := ""
	imageName := ""
	if parts := strings.Split(name, "/"); len(parts) == 2 {
		namespace, imageName = parts[0], parts[1]
	} else if len(parts) == 1 {
		imageName = parts[0]
	}
	return namespace, imageName
}
