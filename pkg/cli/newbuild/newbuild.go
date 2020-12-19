package newbuild

import (
	"fmt"
	"io/ioutil"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	buildv1 "github.com/openshift/api/build/v1"
	ocnewapp "github.com/openshift/oc/pkg/cli/newapp"
	configcmd "github.com/openshift/oc/pkg/helpers/bulk"
	newapp "github.com/openshift/oc/pkg/helpers/newapp/app"
	newcmd "github.com/openshift/oc/pkg/helpers/newapp/cmd"
)

// NewBuildRecommendedCommandName is the recommended command name.
const NewBuildRecommendedCommandName = "new-build"

var (
	newBuildLong = templates.LongDesc(`
		Create a new build by specifying source code

		This command will try to create a build configuration for your application using images and
		code that has a public repository. It will lookup the images on the local Docker installation
		(if available), a container image registry, or an image stream.

		If you specify a source code URL, it will set up a build that takes your source code and converts
		it into an image that can run inside of a pod. Local source must be in a git repository that has a
		remote repository that the server can see.

		Once the build configuration is created a new build will be automatically triggered.
		You can use '%[1]s status' to check the progress.`)

	newBuildExample = templates.Examples(`
		# Create a build config based on the source code in the current git repository (with a public
		# remote) and a Docker image
		arvan paas new-build . --docker-image=repo/langimage

		# Create a NodeJS build config based on the provided [image]~[source code] combination
		arvan paas new-build centos/nodejs-8-centos7~https://github.com/sclorg/nodejs-ex.git

		# Create a build config from a remote repository using its beta2 branch
		arvan paas new-build https://github.com/openshift/ruby-hello-world#beta2

		# Create a build config using a Dockerfile specified as an argument
		arvan paas new-build -D $'FROM centos:7\nRUN yum install -y httpd'

		# Create a build config from a remote repository and add custom environment variables
		arvan paas new-build https://github.com/openshift/ruby-hello-world -e RACK_ENV=development

		# Create a build config from a remote private repository and specify which existing secret to use
		arvan paas new-build https://github.com/youruser/yourgitrepo --source-secret=yoursecret

		# Create a build config from a remote repository and inject the npmrc into a build
		arvan paas new-build https://github.com/openshift/ruby-hello-world --build-secret npmrc:.npmrc

		# Create a build config from a remote repository and inject environment data into a build
		arvan paas new-build https://github.com/openshift/ruby-hello-world --build-config-map env:config

		# Create a build config that gets its input from a remote repository and another Docker image
		arvan paas new-build https://github.com/openshift/ruby-hello-world --source-image=openshift/jenkins-1-centos7 --source-image-path=/var/lib/jenkins:tmp
	`)

	newBuildNoInput = `You must specify one or more images, image streams, or source code locations to create a build.

To build from an existing image stream tag or Docker image, provide the name of the image and
the source code location:

  arvan paas new-build centos/nodejs-8-centos7~https://github.com/sclorg/nodejs-ex.git

If you only specify the source repository location (local or remote), the command will look at
the repo to determine the type, and then look for a matching image on your server or on the
default container image registry.

  arvan paas new-build https://github.com/sclorg/nodejs-ex.git

will look for an image called "nodejs" in your current project, the 'openshift' project, or
on the Docker Hub.
`
)

type BuildOptions struct {
	*ocnewapp.ObjectGeneratorOptions
	genericclioptions.IOStreams
}

func NewBuildOptions(streams genericclioptions.IOStreams) *BuildOptions {
	config := newcmd.NewAppConfig()
	config.ExpectToBuild = true

	return &BuildOptions{
		IOStreams: streams,
		ObjectGeneratorOptions: &ocnewapp.ObjectGeneratorOptions{
			PrintFlags: genericclioptions.NewPrintFlags("created"),
			IOStreams:  streams,
			Config:     config,
		},
	}
}

// NewCmdNewBuild implements the OpenShift cli new-build command
func NewCmdNewBuild(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewBuildOptions(streams)

	cmd := &cobra.Command{
		Use:        "new-build (IMAGE | IMAGESTREAM | PATH | URL ...)",
		Short:      "Create a new build configuration",
		Long:       newBuildLong,
		Example:    newBuildExample,
		SuggestFor: []string{"build", "builds"},
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.RunNewBuild())
		},
	}

	cmd.Flags().StringSliceVar(&o.Config.SourceRepositories, "code", o.Config.SourceRepositories, "Source code in the build configuration.")
	cmd.Flags().StringSliceVarP(&o.Config.ImageStreams, "image-stream", "i", o.Config.ImageStreams, "Name of an image stream to to use as a builder.")
	cmd.Flags().StringSliceVar(&o.Config.DockerImages, "docker-image", o.Config.DockerImages, "Name of a Docker image to use as a builder.")
	cmd.Flags().StringSliceVar(&o.Config.ConfigMaps, "build-config-map", o.Config.ConfigMaps, "ConfigMap and destination to use as an input for the build.")
	cmd.Flags().StringSliceVar(&o.Config.Secrets, "build-secret", o.Config.Secrets, "Secret and destination to use as an input for the build.")
	cmd.Flags().StringVar(&o.Config.SourceSecret, "source-secret", o.Config.SourceSecret, "The name of an existing secret that should be used for cloning a private git repository.")
	cmd.Flags().StringVar(&o.Config.PushSecret, "push-secret", o.Config.PushSecret, "The name of an existing secret that should be used for pushing the output image.")
	cmd.Flags().StringVar(&o.Config.Name, "name", o.Config.Name, "Set name to use for generated build artifacts.")
	cmd.Flags().StringVar(&o.Config.To, "to", o.Config.To, "Push built images to this image stream tag (or Docker image repository if --to-docker is set).")
	cmd.Flags().BoolVar(&o.Config.OutputDocker, "to-docker", o.Config.OutputDocker, "If true, have the build output push to a Docker repository.")
	cmd.Flags().StringArrayVar(&o.Config.BuildEnvironment, "build-env", o.Config.BuildEnvironment, "Specify a key-value pair for an environment variable to set into resulting image.")
	cmd.Flags().MarkHidden("build-env")
	cmd.Flags().StringArrayVarP(&o.Config.BuildEnvironment, "env", "e", o.Config.BuildEnvironment, "Specify a key-value pair for an environment variable to set into resulting image.")
	cmd.Flags().StringArrayVar(&o.Config.BuildEnvironmentFiles, "build-env-file", o.Config.BuildEnvironmentFiles, "File containing key-value pairs of environment variables to set into each container.")
	cmd.MarkFlagFilename("build-env-file")
	cmd.Flags().MarkHidden("build-env-file")
	cmd.Flags().StringArrayVar(&o.Config.BuildEnvironmentFiles, "env-file", o.Config.BuildEnvironmentFiles, "File containing key-value pairs of environment variables to set into each container.")
	cmd.MarkFlagFilename("env-file")
	cmd.Flags().Var(&o.Config.Strategy, "strategy", "Specify the build strategy to use if you don't want to detect (docker|pipeline|source). NOTICE: the pipeline strategy is deprecated; consider using Jenkinsfiles directly on Jenkins or OpenShift Pipelines.")
	cmd.Flags().StringVarP(&o.Config.Dockerfile, "dockerfile", "D", o.Config.Dockerfile, "Specify the contents of a Dockerfile to build directly, implies --strategy=docker. Pass '-' to read from STDIN.")
	cmd.Flags().StringArrayVar(&o.Config.BuildArgs, "build-arg", o.Config.BuildArgs, "Specify a key-value pair to pass to Docker during the build.")
	cmd.Flags().BoolVar(&o.Config.BinaryBuild, "binary", o.Config.BinaryBuild, "Instead of expecting a source URL, set the build to expect binary contents. Will disable triggers.")
	cmd.Flags().StringP("labels", "l", "", "Label to set in all generated resources.")
	cmd.Flags().BoolVar(&o.Config.InsecureRegistry, "insecure-registry", o.Config.InsecureRegistry, "If true, indicates that the referenced Docker images are on insecure registries and should bypass certificate checking")
	cmd.Flags().BoolVar(&o.Config.AllowMissingImages, "allow-missing-images", o.Config.AllowMissingImages, "If true, indicates that referenced Docker images that cannot be found locally or in a registry should still be used.")
	cmd.Flags().BoolVar(&o.Config.AllowMissingImageStreamTags, "allow-missing-imagestream-tags", o.Config.AllowMissingImageStreamTags, "If true, indicates that image stream tags that don't exist should still be used.")
	cmd.Flags().StringVar(&o.Config.ContextDir, "context-dir", o.Config.ContextDir, "Context directory to be used for the build.")
	cmd.Flags().BoolVar(&o.Config.NoOutput, "no-output", o.Config.NoOutput, "If true, the build output will not be pushed anywhere.")
	cmd.Flags().StringVar(&o.Config.SourceImage, "source-image", o.Config.SourceImage, "Specify an image to use as source for the build.  You must also specify --source-image-path.")
	cmd.Flags().StringVar(&o.Config.SourceImagePath, "source-image-path", o.Config.SourceImagePath, "Specify the file or directory to copy from the source image and its destination in the build directory. Format: [source]:[destination-dir].")

	o.Action.BindForOutput(cmd.Flags(), "output", "template", "sort-by")
	cmd.Flags().String("output-version", "", "The preferred API versions of the output objects")

	o.PrintFlags.AddFlags(cmd)
	return cmd
}

// Complete sets any default behavior for the command
func (o *BuildOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	err := o.ObjectGeneratorOptions.Complete(f, cmd, args)
	if err != nil {
		return err
	}

	if o.ObjectGeneratorOptions.Config.Dockerfile == "-" {
		data, err := ioutil.ReadAll(o.In)
		if err != nil {
			return err
		}
		o.ObjectGeneratorOptions.Config.Dockerfile = string(data)
	}

	return nil
}

// RunNewBuild contains all the necessary functionality for the OpenShift cli new-build command
func (o *BuildOptions) RunNewBuild() error {
	config := o.Config
	out := o.Action.Out

	ocnewapp.CheckGitInstalled(out)

	result, err := config.Run()
	if err != nil {
		return ocnewapp.HandleError(err, o.CommandPath, config, transformBuildError)
	}

	if len(config.Labels) == 0 && len(result.Name) > 0 {
		config.Labels = map[string]string{"build": result.Name}
	}

	if err := ocnewapp.SetLabels(config.Labels, result); err != nil {
		return err
	}
	if err := ocnewapp.SetAnnotations(map[string]string{newcmd.GeneratedByNamespace: newcmd.GeneratedByNewBuild}, result); err != nil {
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

	if errs := o.Action.WithMessage(configcmd.CreateMessage(config.Labels), "created").Run(result.List, result.Namespace); len(errs) > 0 {
		return kcmdutil.ErrExit
	}

	if !o.Action.Verbose() || o.Action.DryRun {
		return nil
	}

	indent := o.Action.DefaultIndent()
	for _, item := range result.List.Items {
		switch t := item.(type) {
		case *buildv1.BuildConfig:
			if t.Spec.Strategy.Type == buildv1.JenkinsPipelineBuildStrategyType {
				fmt.Fprintln(o.Action.ErrOut, "JenkinsPipeline build strategy is deprecated. Use Jenkinsfiles directly on Jenkins or OpenShift Pipelines instead")
			}
			if len(t.Spec.Triggers) > 0 && t.Spec.Source.Binary == nil {
				fmt.Fprintf(out, "%sBuild configuration %q created and build triggered.\n", indent, t.Name)
				fmt.Fprintf(out, "%sRun 'arvan paas logs -f buildconfig/%s' to stream the build progress.\n", indent, t.Name)
			}
		}
	}

	return nil
}

func transformBuildError(err error, commandPath string, groups ocnewapp.ErrorGroups, config *newcmd.AppConfig) {
	switch t := err.(type) {
	case newapp.ErrNoMatch:
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
				  3. Git repository URLs or local paths that point to Git repositories

				--allow-missing-images can be used to force the use of an image that was not matched

				See '%[1]s -h' for examples.`, commandPath,
			),
			classification.String(),
			t,
			t.Errs...,
		)
		return
	}
	switch err {
	case newcmd.ErrNoInputs:
		groups.Add("", "", "", ocnewapp.UsageError(commandPath, newBuildNoInput))
		return
	}
	ocnewapp.TransformRunError(err, commandPath, groups, config)
	return
}
