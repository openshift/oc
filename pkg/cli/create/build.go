package create

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util"
	"k8s.io/kubectl/pkg/util/templates"

	buildv1 "github.com/openshift/api/build/v1"
	buildv1client "github.com/openshift/client-go/build/clientset/versioned/typed/build/v1"
	"github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/oc/pkg/helpers/env"
)

var (
	buildLong = templates.LongDesc(`
		Create a new build

		Builds create container images from source code or Dockerfiles. A build can pull source
		code from Git or accept a Dockerfile that pulls the source content.
	`)

	buildExample = templates.Examples(`
		# Create a new build
		oc create build myapp
	`)
)

type CreateBuildOptions struct {
	CreateSubcommandOptions *CreateSubcommandOptions

	Strategy                string
	SourceGit               string
	SourceRevision          string
	DockerfileContents      string
	DockerfilePath          string
	ContextDir              string
	FromImage               string
	ToImage                 string
	ToImageStreamTag        string
	ImageOptimizationPolicy string
	Env                     []string
	BuildLoglevel           int

	Client buildv1client.BuildsGetter
}

// NewCmdCreateBuild is a macro command to create a new build stream
func NewCmdCreateBuild(f genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := &CreateBuildOptions{
		CreateSubcommandOptions: NewCreateSubcommandOptions(streams),
	}
	cmd := &cobra.Command{
		Use:     "build NAME",
		Short:   "Create a new build",
		Long:    buildLong,
		Example: buildExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(cmd, f, args))
			cmdutil.CheckErr(o.Run())
		},
	}
	cmd.Flags().StringVar(&o.Strategy, "strategy", o.Strategy, "The build strategy to use: Docker, Source, or Custom. Can be defaulted by other arguments.")
	cmd.Flags().StringVar(&o.SourceGit, "source-git", o.SourceGit, "A URL or Git spec link to a Git repository.")
	cmd.Flags().StringVar(&o.SourceRevision, "source-revision", o.SourceRevision, "A commit, branch, or tag within the source repository.")
	cmd.Flags().StringVar(&o.ContextDir, "context-dir", o.ContextDir, "A relative path within the repository to use as the root of the build.")
	cmd.Flags().StringVar(&o.DockerfileContents, "dockerfile-contents", o.DockerfileContents, "The contents of a Dockerfile to build.")
	cmd.Flags().StringVar(&o.DockerfilePath, "dockerfile-path", o.DockerfilePath, "The relative path within the repository context that the Dockerfile is located at.")
	cmd.Flags().StringVar(&o.FromImage, "from-image", o.FromImage, "A container image pull spec to use as the basis for the image build.")
	cmd.Flags().StringVar(&o.ToImage, "to-image", o.ToImage, "A location to push the output image to.")
	cmd.Flags().StringVar(&o.ToImageStreamTag, "to-image-stream", o.ToImageStreamTag, "An image stream tag to push the output image to. Accepts [NAMESPACE/]STREAM:TAG")
	cmd.Flags().StringVar(&o.ImageOptimizationPolicy, "image-optimization-policy", o.ImageOptimizationPolicy, "Controls whether individual layers are created: SkipLayers, SkipLayersAndWarn, or None.")
	cmd.Flags().IntVar(&o.BuildLoglevel, "build-loglevel", o.BuildLoglevel, "Set the log level for builds (0-10, 0 default).")
	cmd.Flags().StringArrayVar(&o.Env, "env", o.Env, "Add enviroment variables to the build strategy.")

	o.CreateSubcommandOptions.AddFlags(cmd)
	cmdutil.AddDryRunFlag(cmd)

	return cmd
}

func (o *CreateBuildOptions) Complete(cmd *cobra.Command, f genericclioptions.RESTClientGetter, args []string) error {
	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.Client, err = buildv1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	return o.CreateSubcommandOptions.Complete(f, cmd, args)
}

func defaultDockerStrategy(build *buildv1.Build) *buildv1.Build {
	if build.Spec.CommonSpec.Strategy.DockerStrategy == nil {
		build.Spec.CommonSpec.Strategy.DockerStrategy = &buildv1.DockerBuildStrategy{}
	}
	return build
}

func stringToImageStreamTagRef(s string) (*corev1.ObjectReference, error) {
	ref, err := reference.Parse(s)
	if err != nil {
		return nil, err
	}
	if len(ref.Tag) == 0 || len(ref.ID) > 0 || len(ref.Name) == 0 || len(ref.Registry) > 0 {
		return nil, fmt.Errorf("must specify [NAMESPACE/]STREAM:TAG")
	}
	return &corev1.ObjectReference{
		Kind:      "ImageStreamTag",
		Name:      ref.Name + ":" + ref.Tag,
		Namespace: ref.Namespace,
	}, nil
}

func (o *CreateBuildOptions) Run() error {
	build := &buildv1.Build{
		// this is ok because we know exactly how we want to be serialized
		TypeMeta:   metav1.TypeMeta{APIVersion: buildv1.SchemeGroupVersion.String(), Kind: "Build"},
		ObjectMeta: metav1.ObjectMeta{Name: o.CreateSubcommandOptions.Name},
		Spec: buildv1.BuildSpec{
			CommonSpec: buildv1.CommonSpec{
				Source: buildv1.BuildSource{},
			},
		},
	}
	if len(o.Strategy) > 0 {
		switch build.Spec.Strategy.Type {
		case buildv1.DockerBuildStrategyType:
			build.Spec.Strategy.DockerStrategy = &buildv1.DockerBuildStrategy{}
		case buildv1.SourceBuildStrategyType:
			build.Spec.Strategy.SourceStrategy = &buildv1.SourceBuildStrategy{}
		case buildv1.CustomBuildStrategyType:
			build.Spec.Strategy.CustomStrategy = &buildv1.CustomBuildStrategy{}
		}
	}

	if len(o.SourceGit) > 0 {
		build.Spec.CommonSpec.Source.Git = &buildv1.GitBuildSource{
			URI: o.SourceGit,
			Ref: o.SourceRevision,
		}
	}
	if len(o.ContextDir) > 0 {
		build.Spec.CommonSpec.Source.ContextDir = o.ContextDir
	}
	if len(o.DockerfileContents) > 0 {
		build.Spec.CommonSpec.Source.Dockerfile = &o.DockerfileContents
		defaultDockerStrategy(build)
	}
	if len(o.DockerfilePath) > 0 {
		defaultDockerStrategy(build)
		build.Spec.CommonSpec.Strategy.DockerStrategy.DockerfilePath = o.DockerfilePath
	}
	if len(o.ImageOptimizationPolicy) > 0 {
		defaultDockerStrategy(build)
		policy := buildv1.ImageOptimizationPolicy(o.ImageOptimizationPolicy)
		build.Spec.CommonSpec.Strategy.DockerStrategy.ImageOptimizationPolicy = &policy
	}

	if len(o.FromImage) > 0 {
		ref := &corev1.ObjectReference{Kind: "DockerImage", Name: o.FromImage}
		switch t := build.Spec.CommonSpec.Strategy; {
		case t.DockerStrategy != nil:
			t.DockerStrategy.From = ref
		case t.SourceStrategy != nil:
			t.SourceStrategy.From = *ref
		case t.CustomStrategy != nil:
			t.CustomStrategy.From = *ref
		case t.JenkinsPipelineStrategy != nil:
		default:
			defaultDockerStrategy(build)
			build.Spec.CommonSpec.Strategy.DockerStrategy.From = ref
		}
	}

	if len(o.ToImage) > 0 {
		ref := &corev1.ObjectReference{Kind: "DockerImage", Name: o.ToImage}
		build.Spec.CommonSpec.Output.To = ref
	}
	if len(o.ToImageStreamTag) > 0 {
		ref, err := stringToImageStreamTagRef(o.ToImageStreamTag)
		if err != nil {
			return fmt.Errorf("invalid --to-image-stream: %v", err)
		}
		build.Spec.CommonSpec.Output.To = ref
	}
	add, _, err := env.ParseEnv(o.Env, nil)
	if err != nil {
		return fmt.Errorf("invalid --env: %v", err)
	}
	if o.BuildLoglevel > 0 {
		add = append(add, corev1.EnvVar{Name: "BUILD_LOGLEVEL", Value: strconv.Itoa(o.BuildLoglevel)})
	}
	if len(add) > 0 {
		switch {
		case build.Spec.CommonSpec.Strategy.DockerStrategy != nil:
			build.Spec.CommonSpec.Strategy.DockerStrategy.Env = add
		case build.Spec.CommonSpec.Strategy.CustomStrategy != nil:
			build.Spec.CommonSpec.Strategy.CustomStrategy.Env = add
		case build.Spec.CommonSpec.Strategy.SourceStrategy != nil:
			build.Spec.CommonSpec.Strategy.SourceStrategy.Env = add
		}
	}

	if err := util.CreateOrUpdateAnnotation(o.CreateSubcommandOptions.CreateAnnotation, build, scheme.DefaultJSONEncoder()); err != nil {
		return err
	}

	if o.CreateSubcommandOptions.DryRunStrategy != cmdutil.DryRunClient {
		var err error
		build, err = o.Client.Builds(o.CreateSubcommandOptions.Namespace).Create(context.TODO(), build, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}

	return o.CreateSubcommandOptions.Printer.PrintObj(build, o.CreateSubcommandOptions.Out)
}
