package newapp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"sort"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	corev1 "k8s.io/api/core/v1"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	corev1typedclient "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/cmd/logs"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/generate"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/templates"

	appsv1 "github.com/openshift/api/apps/v1"
	"github.com/openshift/api/build"
	buildv1 "github.com/openshift/api/build/v1"
	imagev1 "github.com/openshift/api/image/v1"
	routev1 "github.com/openshift/api/route/v1"
	imagev1typedclient "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	routev1typedclient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	templatev1typedclient "github.com/openshift/client-go/template/clientset/versioned/typed/template/v1"
	"github.com/openshift/library-go/pkg/git"
	"github.com/openshift/library-go/pkg/image/imageutil"
	"github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/oc/pkg/helpers/bulk"
	cmdutil "github.com/openshift/oc/pkg/helpers/cmd"
	imagehelpers "github.com/openshift/oc/pkg/helpers/image"
	"github.com/openshift/oc/pkg/helpers/newapp"
	newappapp "github.com/openshift/oc/pkg/helpers/newapp/app"
	newcmd "github.com/openshift/oc/pkg/helpers/newapp/cmd"
	dockerutil "github.com/openshift/oc/pkg/helpers/newapp/docker"
)

// RoutePollTimoutSeconds sets how long new-app command waits for route host to be prepopulated
const RoutePollTimeout = 5 * time.Second

var (
	newAppLong = templates.LongDesc(`
		Create a new application by specifying source code, templates, and/or images

		This command will try to build up the components of an application using images, templates,
		or code that has a public repository. It will lookup the images on the local Docker installation
		(if available), a container image registry, an integrated image stream, or stored templates.

		If you specify a source code URL, it will set up a build that takes your source code and converts
		it into an image that can run inside of a pod. Local source must be in a git repository that has a
		remote repository that the server can see. The images will be deployed via a deployment or
		deployment configuration, and a service will be connected to the first public port of the app.
		You may either specify components using the various existing flags or let arvan paas new-app autodetect
		what kind of components you have provided.

		If you provide source code, a new build will be automatically triggered.
		You can use 'arvan paas status' to check the progress.`)

	newAppExample = templates.Examples(`
		# List all local templates and image streams that can be used to create an app
		arvan paas new-app --list

		# Create an application based on the source code in the current git repository (with a public remote) and a Docker image
		arvan paas new-app . --docker-image=repo/langimage

		# Create an application myapp with Docker based build strategy expecting binary input
		arvan paas new-app  --strategy=docker --binary --name myapp

		# Create a Ruby application based on the provided [image]~[source code] combination
		arvan paas new-app centos/ruby-25-centos7~https://github.com/sclorg/ruby-ex.git

		# Use the public Docker Hub MySQL image to create an app. Generated artifacts will be labeled with db=mysql
		arvan paas new-app mysql MYSQL_USER=user MYSQL_PASSWORD=pass MYSQL_DATABASE=testdb -l db=mysql

		# Use a MySQL image in a private registry to create an app and override application artifacts' names
		arvan paas new-app --docker-image=myregistry.com/mycompany/mysql --name=private

		# Create an application from a remote repository using its beta4 branch
		arvan paas new-app https://github.com/openshift/ruby-hello-world#beta4

		# Create an application based on a stored template, explicitly setting a parameter value
		arvan paas new-app --template=ruby-helloworld-sample --param=MYSQL_USER=admin

		# Create an application from a remote repository and specify a context directory
		arvan paas new-app https://github.com/youruser/yourgitrepo --context-dir=src/build

		# Create an application from a remote private repository and specify which existing secret to use
		arvan paas new-app https://github.com/youruser/yourgitrepo --source-secret=yoursecret

		# Create an application based on a template file, explicitly setting a parameter value
		arvan paas new-app --file=./example/myapp/template.json --param=MYSQL_USER=admin

		# Search all templates, image streams, and Docker images for the ones that match "ruby"
		arvan paas new-app --search ruby

		# Search for "ruby", but only in stored templates (--template, --image-stream and --docker-image
		# can be used to filter search results)
		arvan paas new-app --search --template=ruby

		# Search for "ruby" in stored templates and print the output as an YAML
		arvan paas new-app --search --template=ruby --output=yaml
	`)

	newAppNoInput = `You must specify one or more images, image streams, templates, or source code locations to create an application.

To list all local templates and image streams, use:

  arvan paas new-app -L

To search templates, image streams, and Docker images that match the arguments provided, use:

  arvan paas new-app -S php
  arvan paas new-app -S --template=ruby
  arvan paas new-app -S --image-stream=mysql
  arvan paas new-app -S --docker-image=python
`
)

type ObjectGeneratorOptions struct {
	PrintFlags *genericclioptions.PrintFlags
	Action     bulk.BulkAction

	Config *newcmd.AppConfig

	CommandPath string
	CommandName string

	LogsForObject polymorphichelpers.LogsForObjectFunc
	Printer       printers.ResourcePrinter

	genericclioptions.IOStreams
}

type AppOptions struct {
	*ObjectGeneratorOptions

	RESTClientGetter genericclioptions.RESTClientGetter

	genericclioptions.IOStreams
}

//Complete sets all common default options for commands (new-app and new-build)
func (o *ObjectGeneratorOptions) Complete(f kcmdutil.Factory, c *cobra.Command, args []string) error {
	cmdutil.WarnAboutCommaSeparation(o.ErrOut, o.Config.Environment, "--env")
	cmdutil.WarnAboutCommaSeparation(o.ErrOut, o.Config.BuildEnvironment, "--build-env")

	o.Action.IOStreams = o.IOStreams

	if o.PrintFlags.OutputFormat != nil {
		o.Action.Output = *o.PrintFlags.OutputFormat
	}

	// Only output="" should print descriptions of intermediate steps. Everything
	// else should print only some specific output (json, yaml, go-template, ...)
	o.Config.In = o.In
	if len(o.Action.Output) == 0 {
		o.Config.Out = o.Out
	} else {
		o.Config.Out = ioutil.Discard
	}
	o.Config.ErrOut = o.ErrOut

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	mapper, err := f.ToRESTMapper()
	if err != nil {
		return err
	}
	dynamicClient, err := dynamic.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	o.Action.Bulk.Scheme = newAppScheme
	o.Action.Bulk.Op = bulk.Creator{Client: dynamicClient, RESTMapper: mapper}.Create
	// Retry is used to support previous versions of the API server that will
	// consider the presence of an unknown trigger type to be an error.
	o.Action.Bulk.Retry = retryBuildConfig

	o.Config.DryRun = o.Action.DryRun
	o.CommandPath = c.CommandPath()

	o.LogsForObject = polymorphichelpers.LogsForObjectFn
	o.Printer, err = o.PrintFlags.ToPrinter()
	if err != nil {
		return err
	}

	if err := CompleteAppConfig(o.Config, f, c, args); err != nil {
		return err
	}
	if err := setAppConfigLabels(c, o.Config); err != nil {
		return err
	}
	return nil
}

func NewAppOptions(streams genericclioptions.IOStreams) *AppOptions {
	config := newcmd.NewAppConfig()
	config.Deploy = true

	// disable the --template printFlag, as it is shadowed by the existing --template printing
	// in this command - which is used to select existing app templates
	printFlags := genericclioptions.NewPrintFlags("created")
	printFlags.TemplatePrinterFlags.TemplateArgument = nil
	*printFlags.TemplatePrinterFlags.AllowMissingKeys = false

	return &AppOptions{
		IOStreams: streams,
		ObjectGeneratorOptions: &ObjectGeneratorOptions{
			PrintFlags: printFlags,
			IOStreams:  streams,
			Config:     config,
		},
	}
}

// NewCmdNewApplication implements the OpenShift cli new-app command.
func NewCmdNewApplication(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewAppOptions(streams)

	cmd := &cobra.Command{
		Use:        "new-app (IMAGE | IMAGESTREAM | TEMPLATE | PATH | URL ...)",
		Short:      "Create a new application",
		Long:       newAppLong,
		Example:    newAppExample,
		SuggestFor: []string{"app", "application"},
		Run: func(c *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, c, args))
			kcmdutil.CheckErr(o.RunNewApp())
		},
	}

	o.PrintFlags.AddFlags(cmd)

	cmd.Flags().BoolVar(&o.Config.AsTestDeployment, "as-test", o.Config.AsTestDeployment, "If true create this application as a test deployment, which validates that the deployment succeeds and then scales down.")
	cmd.Flags().BoolVar(&o.Config.DeploymentConfig, "as-deployment-config", o.Config.DeploymentConfig, "If true create this application as a deployment config, which allows for hooks and custom strategies.")
	cmd.Flags().StringSliceVar(&o.Config.SourceRepositories, "code", o.Config.SourceRepositories, "Source code to use to build this application.")
	cmd.Flags().StringVar(&o.Config.ContextDir, "context-dir", o.Config.ContextDir, "Context directory to be used for the build.")
	cmd.Flags().StringSliceVarP(&o.Config.ImageStreams, "image-stream", "i", o.Config.ImageStreams, "Name of an image stream to use in the app.")
	cmd.Flags().StringSliceVar(&o.Config.DockerImages, "docker-image", o.Config.DockerImages, "Name of a Docker image to include in the app.")
	cmd.Flags().StringSliceVar(&o.Config.Templates, "template", o.Config.Templates, "Name of a stored template to use in the app.")
	cmd.Flags().StringSliceVarP(&o.Config.TemplateFiles, "file", "f", o.Config.TemplateFiles, "Path to a template file to use for the app.")
	cmd.MarkFlagFilename("file", "yaml", "yml", "json")
	cmd.Flags().StringArrayVarP(&o.Config.TemplateParameters, "param", "p", o.Config.TemplateParameters, "Specify a key-value pair (e.g., -p FOO=BAR) to set/override a parameter value in the template.")
	cmd.Flags().StringArrayVar(&o.Config.TemplateParameterFiles, "param-file", o.Config.TemplateParameterFiles, "File containing parameter values to set/override in the template.")
	cmd.MarkFlagFilename("param-file")
	cmd.Flags().StringSliceVar(&o.Config.Groups, "group", o.Config.Groups, "Indicate components that should be grouped together as <comp1>+<comp2>.")
	cmd.Flags().StringArrayVarP(&o.Config.Environment, "env", "e", o.Config.Environment, "Specify a key-value pair for an environment variable to set into each container.")
	cmd.Flags().StringArrayVar(&o.Config.EnvironmentFiles, "env-file", o.Config.EnvironmentFiles, "File containing key-value pairs of environment variables to set into each container.")
	cmd.MarkFlagFilename("env-file")
	cmd.Flags().StringArrayVar(&o.Config.BuildEnvironment, "build-env", o.Config.BuildEnvironment, "Specify a key-value pair for an environment variable to set into each build image.")
	cmd.Flags().StringArrayVar(&o.Config.BuildEnvironmentFiles, "build-env-file", o.Config.BuildEnvironmentFiles, "File containing key-value pairs of environment variables to set into each build image.")
	cmd.MarkFlagFilename("build-env-file")
	cmd.Flags().StringVar(&o.Config.Name, "name", o.Config.Name, "Set name to use for generated application artifacts")
	cmd.Flags().Var(&o.Config.Strategy, "strategy", "Specify the build strategy to use if you don't want to detect (docker|pipeline|source). NOTICE: the pipeline strategy is deprecated; consider using Jenkinsfiles directly on Jenkins or OpenShift Pipelines.")
	cmd.Flags().StringP("labels", "l", "", "Label to set in all resources for this application.")
	cmd.Flags().BoolVar(&o.Config.IgnoreUnknownParameters, "ignore-unknown-parameters", o.Config.IgnoreUnknownParameters, "If true, will not stop processing if a provided parameter does not exist in the template.")
	cmd.Flags().BoolVar(&o.Config.InsecureRegistry, "insecure-registry", o.Config.InsecureRegistry, "If true, indicates that the referenced Docker images are on insecure registries and should bypass certificate checking")
	cmd.Flags().BoolVarP(&o.Config.AsList, "list", "L", o.Config.AsList, "List all local templates and image streams that can be used to create.")
	cmd.Flags().BoolVarP(&o.Config.AsSearch, "search", "S", o.Config.AsSearch, "Search all templates, image streams, and Docker images that match the arguments provided.")
	cmd.Flags().BoolVar(&o.Config.AllowMissingImages, "allow-missing-images", o.Config.AllowMissingImages, "If true, indicates that referenced Docker images that cannot be found locally or in a registry should still be used.")
	cmd.Flags().BoolVar(&o.Config.AllowMissingImageStreamTags, "allow-missing-imagestream-tags", o.Config.AllowMissingImageStreamTags, "If true, indicates that image stream tags that don't exist should still be used.")
	cmd.Flags().BoolVar(&o.Config.AllowSecretUse, "grant-install-rights", o.Config.AllowSecretUse, "If true, a component that requires access to your account may use your token to install software into your project. Only grant images you trust the right to run with your token.")
	cmd.Flags().StringVar(&o.Config.SourceSecret, "source-secret", o.Config.SourceSecret, "The name of an existing secret that should be used for cloning a private git repository.")
	cmd.Flags().BoolVar(&o.Config.SkipGeneration, "no-install", o.Config.SkipGeneration, "Do not attempt to run images that describe themselves as being installable")
	cmd.Flags().BoolVar(&o.Config.BinaryBuild, "binary", o.Config.BinaryBuild, "Instead of expecting a source URL, set the build to expect binary contents. Will disable triggers.")

	o.Action.BindForOutput(cmd.Flags(), "output", "template")
	cmd.Flags().String("output-version", "", "The preferred API versions of the output objects")

	return cmd
}

// Complete sets any default behavior for the command
func (o *AppOptions) Complete(f kcmdutil.Factory, c *cobra.Command, args []string) error {
	o.RESTClientGetter = f

	cmdutil.WarnAboutCommaSeparation(o.ErrOut, o.ObjectGeneratorOptions.Config.TemplateParameters, "--param")
	err := o.ObjectGeneratorOptions.Complete(f, c, args)
	if err != nil {
		return err
	}

	return nil
}

// RunNewApp contains all the necessary functionality for the OpenShift cli new-app command
func (o *AppOptions) RunNewApp() error {
	config := o.Config
	out := o.Action.Out

	if config.Querying() {
		result, err := config.RunQuery()
		if err != nil {
			return HandleError(err, o.CommandPath, config, TransformRunError)
		}

		if o.Action.ShouldPrint() {
			list := &unstructured.UnstructuredList{
				Object: map[string]interface{}{
					"kind":       "List",
					"apiVersion": "v1",
					"metadata":   map[string]interface{}{},
				},
			}
			for _, item := range result.List.Items {
				unstructuredItem, err := runtime.DefaultUnstructuredConverter.ToUnstructured(item)
				if err != nil {
					return err
				}
				list.Items = append(list.Items, unstructured.Unstructured{Object: unstructuredItem})
			}

			return o.Printer.PrintObj(list, o.Out)
		}

		return printHumanReadableQueryResult(result, out)
	}

	CheckGitInstalled(out)

	result, err := config.Run()
	if err := HandleError(err, o.CommandPath, config, TransformRunError); err != nil {
		return err
	}

	// set labels explicitly supplied by the user on the command line
	if err := SetLabels(config.Labels, result); err != nil {
		return err
	}

	if len(result.Name) > 0 {
		// only set the computed implicit "app" label on objects if no object we've produced
		// already has the "app" label.
		appLabel := map[string]string{
			"app":                         result.Name,
			"app.kubernetes.io/instance":  result.Name,
			"app.kubernetes.io/component": result.Name,
		}

		for _, is := range o.Config.ImageStreams {
			if ref, _ := reference.Parse(is); len(ref.Name) > 0 {
				appLabel["app.kubernetes.io/name"] = ref.Name
			}
		}

		hasAppLabel, err := hasLabel(appLabel, result)
		if err != nil {
			return err
		}
		if !hasAppLabel {
			if err := SetLabels(appLabel, result); err != nil {
				return err
			}
		}
	}
	if err := SetAnnotations(map[string]string{newcmd.GeneratedByNamespace: newcmd.GeneratedByNewApp}, result); err != nil {
		return err
	}

	if o.Action.ShouldPrint() {
		// TODO(juanvallejo): this needs to be fixed by updating QueryResult.List to be of type corev1.List
		printableList := &corev1.List{
			// this is ok because we know exactly how we want to be serialized
			TypeMeta: metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String(), Kind: "List"},
		}
		for _, obj := range result.List.Items {
			printableList.Items = append(printableList.Items, runtime.RawExtension{
				Object: obj,
			})
		}
		return o.Printer.PrintObj(printableList, o.Out)
	}

	if result.GeneratedJobs {
		o.Action.Compact()
	}

	if errs := o.Action.WithMessage(bulk.CreateMessage(config.Labels), "created").Run(result.List, result.Namespace); len(errs) > 0 {
		return kcmdutil.ErrExit
	}

	if !o.Action.Verbose() || o.Action.DryRun {
		return nil
	}

	supportedTypes := map[schema.GroupVersionKind]bool{
		{Version: "v1", Kind: "Pod"}:                                   true,
		{Group: buildv1.GroupName, Version: "v1", Kind: "BuildConfig"}: true,
		{Group: imagev1.GroupName, Version: "v1", Kind: "ImageStream"}: true,
		{Group: routev1.GroupName, Version: "v1", Kind: "Route"}:       true,
	}

	hasMissingRepo := false
	installing := []*corev1.Pod{}
	indent := o.Action.DefaultIndent()
	containsRoute := false
	for _, item := range result.List.Items {
		// these are all unstructured
		unstructuredObj := item.(*unstructured.Unstructured)

		// Determine if dealing with a "known" resource, containing a switch case below.
		// If so, go through with a conversion attempt, and fail if necessary.
		if supported := supportedTypes[unstructuredObj.GroupVersionKind()]; !supported {
			continue
		}

		obj, err := scheme.Scheme.New(unstructuredObj.GroupVersionKind())
		if err != nil {
			return err
		}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, obj); err != nil {
			return err
		}

		switch t := obj.(type) {
		case *corev1.Pod:
			if t.Annotations[newcmd.GeneratedForJob] == "true" {
				installing = append(installing, t)
			}
		case *buildv1.BuildConfig:
			if t.Spec.Strategy.Type == buildv1.JenkinsPipelineBuildStrategyType {
				fmt.Fprintln(o.Action.ErrOut, "JenkinsPipeline build strategy is deprecated. Use Jenkinsfiles directly on Jenkins or OpenShift Pipelines instead")
			}
			triggered := false
			for _, trigger := range t.Spec.Triggers {
				switch trigger.Type {
				case buildv1.ImageChangeBuildTriggerType, buildv1.ConfigChangeBuildTriggerType:
					triggered = true
					break
				}
			}
			if triggered {
				fmt.Fprintf(out, "%[1]sBuild scheduled, use 'arvan paas logs -f buildconfig/%[2]s' to track its progress.\n", indent, t.Name)
			} else {
				fmt.Fprintf(out, "%[1]sUse 'arvan paas start-build %[2]s' to start a build.\n", indent, t.Name)
			}
		case *imagev1.ImageStream:
			if len(t.Status.DockerImageRepository) == 0 {
				if hasMissingRepo {
					continue
				}
				hasMissingRepo = true
				fmt.Fprintf(out, "%sWARNING: No container image registry has been configured with the server. Automatic builds and deployments may not function.\n", indent)
			}
		case *routev1.Route:
			containsRoute = true
			if len(t.Spec.Host) > 0 {
				var route *routev1.Route
				//check if route processing was completed and host field is prepopulated by router
				err := wait.PollImmediate(500*time.Millisecond, RoutePollTimeout, func() (bool, error) {
					route, err = config.RouteClient.Routes(t.Namespace).Get(context.TODO(), t.Name, metav1.GetOptions{})
					if err != nil {
						return false, fmt.Errorf("Error while polling route %s", t.Name)
					}
					if route.Spec.Host != "" {
						return true, nil
					}
					return false, nil
				})
				if err != nil {
					klog.V(4).Infof("Failed to poll route %s host field: %s", t.Name, err)
				} else {
					fmt.Fprintf(out, "%sAccess your application via route '%s' \n", indent, route.Spec.Host)
				}
			}

		}
	}
	switch {
	case len(installing) == 1:
		return followInstallation(config, o.RESTClientGetter, installing[0], o.LogsForObject)
	case len(installing) > 1:
		for i := range installing {
			fmt.Fprintf(out, "%sTrack installation of %s with 'arvan paas logs %s'.\n", indent, installing[i].Name, installing[i].Name)
		}
	case len(result.List.Items) > 0:
		//if we don't find a route we give a message to expose it
		if !containsRoute {
			//we if don't have any routes, but we have services - we suggest commands to expose those
			svc := getServices(result.List.Items)
			if len(svc) > 0 {
				fmt.Fprintf(out, "%sApplication is not exposed. You can expose services to the outside world by executing one or more of the commands below:\n", indent)
				for _, s := range svc {
					fmt.Fprintf(out, "%s 'arvan paas expose service/%s' \n", indent, s.Name)
				}
			}
		}
		fmt.Fprintf(out, "%sRun 'arvan paas status' to view your app.\n", indent)
	}
	return nil
}

func getServices(items []runtime.Object) []*corev1.Service {
	var svc []*corev1.Service
	for _, i := range items {
		unstructuredObj := i.(*unstructured.Unstructured)
		obj, err := scheme.Scheme.New(unstructuredObj.GroupVersionKind())
		if err != nil {
			klog.V(1).Info(err)
			continue
		}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, obj); err != nil {
			klog.V(1).Info(err)
			continue
		}

		switch obj.(type) {
		case *corev1.Service:
			svc = append(svc, obj.(*corev1.Service))
		}
	}
	return svc
}

func followInstallation(config *newcmd.AppConfig, clientGetter genericclioptions.RESTClientGetter, pod *corev1.Pod, logsForObjectFn polymorphichelpers.LogsForObjectFunc) error {
	fmt.Fprintf(config.Out, "--> Installing ...\n")

	// we cannot retrieve logs until the pod is out of pending
	// TODO: move this to the server side
	podClient := config.KubeClient.CoreV1().Pods(pod.Namespace)
	if err := wait.PollImmediate(500*time.Millisecond, 60*time.Second, installationStarted(podClient, pod.Name, config.KubeClient.CoreV1().Secrets(pod.Namespace))); err != nil {
		return err
	}

	opts := &logs.LogsOptions{
		Namespace:   pod.Namespace,
		ResourceArg: pod.Name,
		Options: &corev1.PodLogOptions{
			Follow:    true,
			Container: pod.Spec.Containers[0].Name,
		},
		RESTClientGetter: clientGetter,
		ConsumeRequestFn: logs.DefaultConsumeRequest,
		LogsForObject:    logsForObjectFn,
		IOStreams:        genericclioptions.IOStreams{Out: config.Out},
	}
	logErr := opts.RunLogs()

	// status of the pod may take tens of seconds to propagate
	if err := wait.PollImmediate(500*time.Millisecond, 30*time.Second, installationComplete(podClient, pod.Name, config.Out)); err != nil {
		if err == wait.ErrWaitTimeout {
			if logErr != nil {
				// output the log error if one occurred
				err = logErr
			} else {
				err = fmt.Errorf("installation may not have completed, see logs for %q for more information", pod.Name)
			}
		}
		return err
	}

	return nil
}

func installationStarted(c corev1typedclient.PodInterface, name string, s corev1typedclient.SecretInterface) wait.ConditionFunc {
	return func() (bool, error) {
		pod, err := c.Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if pod.Status.Phase == corev1.PodPending {
			return false, nil
		}
		// delete a secret named the same as the pod if it exists
		if secret, err := s.Get(context.TODO(), name, metav1.GetOptions{}); err == nil {
			if secret.Annotations[newcmd.GeneratedForJob] == "true" &&
				secret.Annotations[newcmd.GeneratedForJobFor] == pod.Annotations[newcmd.GeneratedForJobFor] {
				if err := s.Delete(context.TODO(), name, metav1.DeleteOptions{}); err != nil {
					klog.V(4).Infof("Failed to delete install secret %s: %v", name, err)
				}
			}
		}
		return true, nil
	}
}

func installationComplete(c corev1typedclient.PodInterface, name string, out io.Writer) wait.ConditionFunc {
	return func() (bool, error) {
		pod, err := c.Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			if kapierrors.IsNotFound(err) {
				return false, fmt.Errorf("installation pod was deleted; unable to determine whether it completed successfully")
			}
			return false, nil
		}
		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			fmt.Fprintf(out, "--> Success\n")
			if err := c.Delete(context.TODO(), name, metav1.DeleteOptions{}); err != nil {
				klog.V(4).Infof("Failed to delete install pod %s: %v", name, err)
			}
			return true, nil
		case corev1.PodFailed:
			return true, fmt.Errorf("installation of %q did not complete successfully", name)
		default:
			return false, nil
		}
	}
}

func setAppConfigLabels(c *cobra.Command, config *newcmd.AppConfig) error {
	labelStr := kcmdutil.GetFlagString(c, "labels")
	if len(labelStr) != 0 {
		var err error
		config.Labels, err = generate.ParseLabels(labelStr)
		if err != nil {
			return err
		}
	}
	return nil
}

// getDockerClient returns a client capable of communicating with the local
// docker daemon.  If an error occurs (such as no local daemon being available),
// it will return nil.
func getDockerClient() (*docker.Client, error) {
	dockerClient, _, err := dockerutil.NewHelper().GetClient()
	if err == nil {
		if err = dockerClient.Ping(); err != nil {
			klog.V(4).Infof("Docker client did not respond to a ping: %v", err)
			return nil, err
		}
		return dockerClient, nil
	}
	klog.V(2).Infof("No local Docker daemon detected: %v", err)
	return nil, err
}

func CompleteAppConfig(config *newcmd.AppConfig, f kcmdutil.Factory, c *cobra.Command, args []string) error {
	if config.Builder == nil {
		config.Builder = f.NewBuilder()
	}
	mapper, err := f.ToRESTMapper()
	if err != nil {
		return err
	}
	if config.Mapper == nil {
		config.Mapper = mapper
	}
	if config.Typer == nil {
		config.Typer = scheme.Scheme
	}

	namespace, _, err := f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}

	config.KubeClient, err = kubernetes.NewForConfig(clientConfig)

	dockerClient, _ := getDockerClient()

	imageClient, err := imagev1typedclient.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	templateClient, err := templatev1typedclient.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	routeClient, err := routev1typedclient.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	config.SetOpenShiftClient(imageClient, templateClient, routeClient, namespace, dockerClient)

	if config.AllowSecretUse {
		cfg, err := f.ToRESTConfig()
		if err != nil {
			return err
		}
		config.SecretAccessor = newConfigSecretRetriever(cfg)
	}

	unknown := config.AddArguments(args)
	if len(unknown) != 0 {
		buf := &bytes.Buffer{}
		fmt.Fprintf(buf, "Did not recognize the following arguments: %v\n\n", unknown)
		for _, argName := range unknown {
			fmt.Fprintf(buf, "%s:\n", argName)
			for _, classErr := range config.EnvironmentClassificationErrors {
				if classErr.Value != nil {
					fmt.Fprintf(buf, fmt.Sprintf("%s:  %v\n", classErr.Key, classErr.Value))
				} else {
					fmt.Fprintf(buf, fmt.Sprintf("%s\n", classErr.Key))
				}
			}
			for _, classErr := range config.SourceClassificationErrors {
				fmt.Fprintf(buf, fmt.Sprintf("%s:  %v\n", classErr.Key, classErr.Value))
			}
			for _, classErr := range config.TemplateClassificationErrors {
				fmt.Fprintf(buf, fmt.Sprintf("%s:  %v\n", classErr.Key, classErr.Value))
			}
			for _, classErr := range config.ComponentClassificationErrors {
				fmt.Fprintf(buf, fmt.Sprintf("%s:  %v\n", classErr.Key, classErr.Value))
			}
			fmt.Fprintln(buf)
		}
		return kcmdutil.UsageErrorf(c, heredoc.Docf(buf.String()))
	}

	if config.AllowMissingImages && config.AsSearch {
		return kcmdutil.UsageErrorf(c, "--allow-missing-images and --search are mutually exclusive.")
	}

	if len(config.SourceImage) != 0 && len(config.SourceImagePath) == 0 {
		return kcmdutil.UsageErrorf(c, "--source-image-path must be specified when --source-image is specified.")
	}
	if len(config.SourceImage) == 0 && len(config.SourceImagePath) != 0 {
		return kcmdutil.UsageErrorf(c, "--source-image must be specified when --source-image-path is specified.")
	}

	if config.BinaryBuild && config.Strategy == newapp.StrategyPipeline {
		return kcmdutil.UsageErrorf(c, "specifying binary builds and the pipeline strategy at the same time is not allowed.")
	}

	if len(config.BuildArgs) > 0 && config.Strategy != newapp.StrategyUnspecified && config.Strategy != newapp.StrategyDocker {
		return kcmdutil.UsageErrorf(c, "Cannot use '--build-arg' without a Docker build")
	}
	return nil
}

func SetAnnotations(annotations map[string]string, result *newcmd.AppResult) error {
	for _, object := range result.List.Items {
		err := newcmd.AddObjectAnnotations(object, annotations)
		if err != nil {
			return fmt.Errorf("failed to add annotation to object of type %q, this resource type is probably unsupported by your client version.", object.GetObjectKind().GroupVersionKind())
		}
	}
	return nil
}

func addObjectLabels(obj runtime.Object, labels labels.Set) error {
	if labels == nil {
		return nil
	}

	accessor, err := meta.Accessor(obj)
	if err != nil {
		return err
	}

	metaLabels := accessor.GetLabels()
	if metaLabels == nil {
		metaLabels = make(map[string]string)
	}
	for k, v := range labels {
		metaLabels[k] = v
	}
	accessor.SetLabels(metaLabels)

	return nil
}

func SetLabels(labels map[string]string, result *newcmd.AppResult) error {
	for _, object := range result.List.Items {
		err := addObjectLabels(object, labels)
		if err != nil {
			return fmt.Errorf("failed to add label to object of type %q, this resource type is probably unsupported by your client version.", object.GetObjectKind().GroupVersionKind())
		}
	}
	return nil
}

func hasLabel(labels map[string]string, result *newcmd.AppResult) (bool, error) {
	for _, obj := range result.List.Items {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return false, err
		}
		for k := range accessor.GetLabels() {
			if _, ok := labels[k]; ok {
				return true, nil
			}
		}

		switch objType := obj.(type) {
		case *appsv1.DeploymentConfig:
			if objType.Spec.Template == nil {
				continue
			}
			for k := range objType.Spec.Template.Labels {
				if _, ok := labels[k]; ok {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// isInvalidTriggerError returns true if the given error is
// a validation error that contains 'invalid trigger type' in its
// error message. This error is returned from older servers that
// consider the presence of unknown trigger types to be an error.
func isInvalidTriggerError(err error) bool {
	if !kapierrors.IsInvalid(err) {
		return false
	}
	statusErr, ok := err.(*kapierrors.StatusError)
	if !ok {
		return false
	}
	return strings.Contains(statusErr.Status().Message, "invalid trigger type")
}

// retryBuildConfig determines if the given error is caused by an invalid trigger
// error on a BuildConfig. If that is the case, it will remove all triggers with a
// type that is not in the whitelist for an older server.
func retryBuildConfig(obj *unstructured.Unstructured, err error) *unstructured.Unstructured {
	if obj == nil {
		return nil
	}
	triggerTypeWhiteList := map[buildv1.BuildTriggerType]struct{}{
		buildv1.GitHubWebHookBuildTriggerType:    {},
		buildv1.GenericWebHookBuildTriggerType:   {},
		buildv1.ImageChangeBuildTriggerType:      {},
		buildv1.GitLabWebHookBuildTriggerType:    {},
		buildv1.BitbucketWebHookBuildTriggerType: {},
	}
	if build.Kind("BuildConfig") == obj.GroupVersionKind().GroupKind() && isInvalidTriggerError(err) {
		var bc *buildv1.BuildConfig
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, bc)
		if err != nil {
			return nil
		}

		triggers := []buildv1.BuildTriggerPolicy{}
		for _, t := range bc.Spec.Triggers {
			if _, inList := triggerTypeWhiteList[t.Type]; inList {
				triggers = append(triggers, t)
			}
		}
		bc.Spec.Triggers = triggers

		retUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(bc)
		if err != nil {
			return nil
		}
		return &unstructured.Unstructured{Object: retUnstructured}
	}
	return nil
}

func HandleError(err error, commandPath string, config *newcmd.AppConfig, transformError func(err error, commandPath string, groups ErrorGroups, config *newcmd.AppConfig)) error {
	if err == nil {
		return nil
	}
	errs := []error{err}
	if agg, ok := err.(errors.Aggregate); ok {
		errs = agg.Errors()
	}
	groups := ErrorGroups{}
	for _, err := range errs {
		transformError(err, commandPath, groups, config)
	}
	buf := &bytes.Buffer{}
	for _, group := range groups {
		fmt.Fprint(buf, kcmdutil.MultipleErrors("error: ", group.errs))
		if len(group.classification) > 0 {
			fmt.Fprintln(buf)
		}
		fmt.Fprintf(buf, group.classification)
		if len(group.suggestion) > 0 {
			if len(group.classification) > 0 {
				fmt.Fprintln(buf)
			}
			fmt.Fprintln(buf)
		}
		fmt.Fprint(buf, group.suggestion)
	}
	return fmt.Errorf(buf.String())
}

type ErrorGroup struct {
	errs           []error
	suggestion     string
	classification string
}
type ErrorGroups map[string]ErrorGroup

func (g ErrorGroups) Add(group string, suggestion string, classification string, err error, errs ...error) {
	all := g[group]
	all.errs = append(all.errs, errs...)
	all.errs = append(all.errs, err)
	all.suggestion = suggestion
	all.classification = classification
	g[group] = all
}

func TransformRunError(err error, commandPath string, groups ErrorGroups, config *newcmd.AppConfig) {
	switch t := err.(type) {
	case newcmd.ErrRequiresExplicitAccess:
		if t.Input.Token != nil && t.Input.Token.ServiceAccount {
			groups.Add(
				"explicit-access-installer",
				heredoc.Doc(`
					WARNING: This will allow the pod to create and manage resources within your namespace -
					ensure you trust the image with those permissions before you continue.

					You can see more information about the image by adding the --dry-run flag.
					If you trust the provided image, include the flag --grant-install-rights.`,
				),
				"",
				fmt.Errorf("installing %q requires an 'installer' service account with project editor access", t.Match.Value),
			)
		} else {
			groups.Add(
				"explicit-access-you",
				heredoc.Doc(`
					WARNING: This will allow the pod to act as you across the entire cluster - ensure you
					trust the image with those permissions before you continue.

					You can see more information about the image by adding the --dry-run flag.
					If you trust the provided image, include the flag --grant-install-rights.`,
				),
				"",
				fmt.Errorf("installing %q requires that you grant the image access to run with your credentials", t.Match.Value),
			)
		}
		return
	case newappapp.ErrNoMatch:
		classification, _ := config.ClassificationWinners[t.Value]
		if classification.IncludeGitErrors {
			notGitRepo, ok := config.SourceClassificationErrors[t.Value]
			if ok {
				t.Errs = append(t.Errs, notGitRepo.Value)
			}
		}
		groups.Add(
			"no-matches",
			heredoc.Docf(`
				The '%[1]s' command will match arguments to the following types:

				  1. Images tagged into image streams in the current project or the 'openshift' project
				     - if you don't specify a tag, we'll add ':latest'
				  2. Images in the Docker Hub, on remote registries, or on the local Docker engine
				  3. Templates in the current project or the 'openshift' project
				  4. Git repository URLs or local paths that point to Git repositories

				--allow-missing-images can be used to point to an image that does not exist yet.

				See '%[1]s -h' for examples.`, commandPath,
			),
			classification.String(),
			t,
			t.Errs...,
		)
		return
	case newappapp.ErrMultipleMatches:
		classification, _ := config.ClassificationWinners[t.Value]
		buf := &bytes.Buffer{}
		for i, match := range t.Matches {

			// If we have more than 5 matches, stop output and recommend searching
			// after the fifth
			if i >= 5 {
				groups.Add(
					"multiple-matches",
					heredoc.Docf(`
						The argument %[1]q could apply to the following Docker images, OpenShift image streams, or templates:

						%[2]sTo view a full list of matches, use 'arvan paas new-app -S %[1]s'`, t.Value, buf.String(),
					),
					classification.String(),
					t,
					t.Errs...,
				)

				return
			}

			fmt.Fprintf(buf, "* %s\n", match.Description)
			fmt.Fprintf(buf, "  Use %[1]s to specify this image or template\n\n", match.Argument)
		}

		groups.Add(
			"multiple-matches",
			heredoc.Docf(`
					The argument %[1]q could apply to the following Docker images, OpenShift image streams, or templates:

					%[2]s`, t.Value, buf.String(),
			),
			classification.String(),
			t,
			t.Errs...,
		)
		return
	case newappapp.ErrPartialMatch:
		classification, _ := config.ClassificationWinners[t.Value]
		buf := &bytes.Buffer{}
		fmt.Fprintf(buf, "* %s\n", t.Match.Description)
		fmt.Fprintf(buf, "  Use %[1]s to specify this image or template\n\n", t.Match.Argument)

		groups.Add(
			"partial-match",
			heredoc.Docf(`
					The argument %[1]q only partially matched the following Docker image, OpenShift image stream, or template:

					%[2]s`, t.Value, buf.String(),
			),
			classification.String(),
			t,
			t.Errs...,
		)
		return
	case newappapp.ErrNoTagsFound:
		classification, _ := config.ClassificationWinners[t.Value]
		buf := &bytes.Buffer{}
		fmt.Fprintf(buf, "  Use --allow-missing-imagestream-tags to use this image stream\n\n")
		groups.Add(
			"no-tags",
			heredoc.Docf(`
					The image stream %[1]q exists, but it has no tags.

					%[2]s`, t.Match.Name, buf.String(),
			),
			classification.String(),
			t,
			t.Errs...,
		)
		return
	}
	switch err {
	case errNoTokenAvailable:
		// TODO: improve by allowing token generation
		groups.Add("", "", "", fmt.Errorf("to install components you must be logged in with an OAuth token (instead of only a certificate)"))
	case newcmd.ErrNoInputs:
		// TODO: suggest things to the user
		groups.Add("", "", "", UsageError(commandPath, newAppNoInput))
	default:
		if runtime.IsNotRegisteredError(err) {
			groups.Add("", "", "", fmt.Errorf(fmt.Sprintf("The template contained an object type unknown to `arvan paas new-app`.  Use `arvan paas process -f <template> | arvan paas create -f -` instead.  Error details: %v", err)))
		} else {
			groups.Add("", "", "", err)
		}
	}
	return
}

func UsageError(commandPath, format string, args ...interface{}) error {
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("%s\nSee '%s -h' for help and examples", msg, commandPath)
}

func printHumanReadableQueryResult(r *newcmd.QueryResult, out io.Writer) error {
	if len(r.Matches) == 0 {
		return fmt.Errorf("no matches found")
	}

	templates := newappapp.ComponentMatches{}
	imageStreams := newappapp.ComponentMatches{}
	dockerImages := newappapp.ComponentMatches{}

	for _, match := range r.Matches {
		switch {
		case match.IsTemplate():
			templates = append(templates, match)
		case match.IsImage() && match.ImageStream != nil:
			imageStreams = append(imageStreams, match)
		case match.IsImage() && match.DockerImage != nil:
			dockerImages = append(dockerImages, match)
		}
	}

	sort.Sort(newappapp.ScoredComponentMatches(templates))
	sort.Sort(newappapp.ScoredComponentMatches(imageStreams))
	sort.Sort(newappapp.ScoredComponentMatches(dockerImages))

	if len(templates) > 0 {
		fmt.Fprintf(out, "Templates (arvan paas new-app --template=<template>)\n")
		fmt.Fprintln(out, "-----")
		for _, match := range templates {
			template := match.Template
			description := template.ObjectMeta.Annotations["description"]

			fmt.Fprintln(out, template.Name)
			fmt.Fprintf(out, "  Project: %v\n", template.Namespace)
			if len(description) > 0 {
				fmt.Fprintf(out, "  %v\n", description)
			}
		}
		fmt.Fprintln(out)
	}

	if len(imageStreams) > 0 {
		fmt.Fprintf(out, "Image streams (arvan paas new-app --image-stream=<image-stream> [--code=<source>])\n")
		fmt.Fprintln(out, "-----")
		for _, match := range imageStreams {
			imageStream := match.ImageStream
			description := imageStream.ObjectMeta.Annotations["description"]
			tags := "<none>"
			if len(imageStream.Status.Tags) > 0 {
				set := sets.NewString()
				for _, tag := range imageStream.Status.Tags {
					if refTag, ok := imageutil.SpecHasTag(imageStream, tag.Tag); ok {
						if !imagehelpers.HasAnnotationTag(&refTag, imagehelpers.TagReferenceAnnotationTagHidden) {
							set.Insert(tag.Tag)
						}
					} else {
						set.Insert(tag.Tag)
					}
				}
				tags = strings.Join(set.List(), ", ")
			}

			fmt.Fprintln(out, imageStream.Name)
			fmt.Fprintf(out, "  Project: %v\n", imageStream.Namespace)
			if len(imageStream.Spec.DockerImageRepository) > 0 {
				fmt.Fprintf(out, "  Tracks:  %v\n", imageStream.Spec.DockerImageRepository)
			}
			fmt.Fprintf(out, "  Tags:    %v\n", tags)
			if len(description) > 0 {
				fmt.Fprintf(out, "  %v\n", description)
			}
		}
		fmt.Fprintln(out)
	}

	if len(dockerImages) > 0 {
		fmt.Fprintf(out, "Docker images (arvan paas new-app --docker-image=<docker-image> [--code=<source>])\n")
		fmt.Fprintln(out, "-----")
		for _, match := range dockerImages {
			image := match.DockerImage

			name, tag, ok := imageutil.SplitImageStreamTag(match.Name)
			if !ok {
				name = match.Name
				tag = match.ImageTag
			}

			fmt.Fprintln(out, name)
			fmt.Fprintf(out, "  Registry: %v\n", match.Meta["registry"])
			fmt.Fprintf(out, "  Tags:     %v\n", tag)

			if len(image.Comment) > 0 {
				fmt.Fprintf(out, "  %v\n", image.Comment)
			}
		}
		fmt.Fprintln(out)
	}

	return nil
}

type configSecretRetriever struct {
	config *restclient.Config
}

func newConfigSecretRetriever(config *restclient.Config) newappapp.SecretAccessor {
	return &configSecretRetriever{config}
}

var errNoTokenAvailable = fmt.Errorf("you are not logged in with a token - unable to provide a secret to the installable component")

func (r *configSecretRetriever) Token() (string, error) {
	if len(r.config.BearerToken) > 0 {
		return r.config.BearerToken, nil
	}
	return "", errNoTokenAvailable
}

func (r *configSecretRetriever) CACert() (string, error) {
	if len(r.config.CAData) > 0 {
		return string(r.config.CAData), nil
	}
	if len(r.config.CAFile) > 0 {
		data, err := ioutil.ReadFile(r.config.CAFile)
		if err != nil {
			return "", fmt.Errorf("unable to read CA cert from config %s: %v", r.config.CAFile, err)
		}
		return string(data), nil
	}
	return "", nil
}

func CheckGitInstalled(w io.Writer) {
	if !git.IsGitInstalled() {
		fmt.Fprintf(w, "warning: Cannot find git. Ensure that it is installed and in your path. Git is required to work with git repositories.\n")
	}
}
