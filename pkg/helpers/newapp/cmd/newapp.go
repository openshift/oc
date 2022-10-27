package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	docker "github.com/fsouza/go-dockerclient"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/klog/v2"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	apimachineryvalidation "k8s.io/apimachinery/pkg/api/validation"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kutilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/scheme"

	appsv1 "github.com/openshift/api/apps/v1"
	authv1 "github.com/openshift/api/authorization/v1"
	buildv1 "github.com/openshift/api/build/v1"
	"github.com/openshift/api/image/docker10"
	imagev1 "github.com/openshift/api/image/v1"
	imagev1typedclient "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	routev1typedclient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	templatev1typedclient "github.com/openshift/client-go/template/clientset/versioned/typed/template/v1"
	"github.com/openshift/library-go/pkg/build/buildutil"
	"github.com/openshift/library-go/pkg/image/imageutil"
	"github.com/openshift/library-go/pkg/image/reference"
	ometa "github.com/openshift/library-go/pkg/image/referencemutator"
	"github.com/openshift/oc/pkg/cli/image/imagesource"
	"github.com/openshift/oc/pkg/cli/image/info"
	"github.com/openshift/oc/pkg/helpers/env"
	utilenv "github.com/openshift/oc/pkg/helpers/env"
	"github.com/openshift/oc/pkg/helpers/newapp"
	"github.com/openshift/oc/pkg/helpers/newapp/app"
	"github.com/openshift/oc/pkg/helpers/newapp/dockerfile"
	"github.com/openshift/oc/pkg/helpers/newapp/jenkinsfile"
	"github.com/openshift/oc/pkg/helpers/newapp/source"
	"github.com/openshift/oc/pkg/helpers/template/templateprocessorclient"
)

const (
	GeneratedByNamespace = "openshift.io/generated-by"
	GeneratedForJob      = "openshift.io/generated-job"
	GeneratedForJobFor   = "openshift.io/generated-job.for"
	GeneratedByNewApp    = "OpenShiftNewApp"
	GeneratedByNewBuild  = "OpenShiftNewBuild"
)

// GenerationInputs control how new-app creates output
// TODO: split these into finer grained structs
type GenerationInputs struct {
	TemplateParameters []string
	Environment        []string
	BuildEnvironment   []string
	BuildArgs          []string
	Labels             map[string]string

	TemplateParameterFiles []string
	EnvironmentFiles       []string
	BuildEnvironmentFiles  []string

	IgnoreUnknownParameters bool
	InsecureRegistry        bool

	Strategy newapp.Strategy

	Name     string
	To       string
	NoOutput bool

	OutputDocker  bool
	Dockerfile    string
	ExpectToBuild bool
	BinaryBuild   bool
	ContextDir    string

	SourceImage     string
	SourceImagePath string

	Secrets    []string
	ConfigMaps []string

	AllowMissingImageStreamTags bool

	Deploy           bool
	DeploymentConfig bool
	AsTestDeployment bool

	AllowGenerationErrors bool
}

// AppConfig contains all the necessary configuration for an application
type AppConfig struct {
	ComponentInputs
	GenerationInputs

	ResolvedComponents *ResolvedComponents

	SkipGeneration bool

	AllowSecretUse bool
	SourceSecret   string
	PushSecret     string
	SecretAccessor app.SecretAccessor

	AsSearch bool
	AsList   bool
	DryRun   bool

	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	KubeClient     kubernetes.Interface
	ImageClient    imagev1typedclient.ImageV1Interface
	RouteClient    routev1typedclient.RouteV1Interface
	TemplateClient templatev1typedclient.TemplateV1Interface

	Resolvers

	Builder *resource.Builder
	Typer   runtime.ObjectTyper
	Mapper  meta.RESTMapper

	OriginNamespace string

	EnvironmentClassificationErrors map[string]ArgumentClassificationError
	SourceClassificationErrors      map[string]ArgumentClassificationError
	TemplateClassificationErrors    map[string]ArgumentClassificationError
	ComponentClassificationErrors   map[string]ArgumentClassificationError
	ClassificationWinners           map[string]ArgumentClassificationWinner
}

type ArgumentClassificationError struct {
	Key   string
	Value error
}

type ArgumentClassificationWinner struct {
	Name             string
	Suffix           string
	IncludeGitErrors bool
}

func (w *ArgumentClassificationWinner) String() string {
	if len(w.Name) == 0 || len(w.Suffix) == 0 {
		return ""
	}
	return fmt.Sprintf("Argument '%s' was classified as %s.", w.Name, w.Suffix)
}

type ErrRequiresExplicitAccess struct {
	Match app.ComponentMatch
	Input app.GeneratorInput
}

func (e ErrRequiresExplicitAccess) Error() string {
	return fmt.Sprintf("the component %q is requesting access to run with your security credentials and install components - you must explicitly grant that access to continue", e.Match.String())
}

// ErrNoInputs is returned when no inputs are specified
var ErrNoInputs = errors.New("no inputs provided")

// AppResult contains the results of an application
type AppResult struct {
	List *metainternalversion.List

	Name      string
	HasSource bool
	Namespace string

	GeneratedJobs bool
}

// QueryResult contains the results of a query (search or list)
type QueryResult struct {
	Matches app.ComponentMatches
	List    *metainternalversion.List
}

// NewAppConfig returns a new AppConfig, but you must set your typer, mapper, and clientMapper after the command has been run
// and flags have been parsed.
func NewAppConfig() *AppConfig {
	return &AppConfig{
		Resolvers: Resolvers{
			Detector: app.SourceRepositoryEnumerator{
				Detectors:         source.DefaultDetectors,
				DockerfileTester:  dockerfile.NewTester(),
				JenkinsfileTester: jenkinsfile.NewTester(),
			},
		},
		EnvironmentClassificationErrors: map[string]ArgumentClassificationError{},
		SourceClassificationErrors:      map[string]ArgumentClassificationError{},
		TemplateClassificationErrors:    map[string]ArgumentClassificationError{},
		ComponentClassificationErrors:   map[string]ArgumentClassificationError{},
		ClassificationWinners:           map[string]ArgumentClassificationWinner{},
	}
}

func (c *AppConfig) DockerRegistrySearcher() app.Searcher {
	r := NewImageRegistrySearcher()
	r.ImageRetriever.SecurityOptions.Insecure = c.InsecureRegistry
	return app.DockerRegistrySearcher{Client: r}
}

type ImageRegistrySearcher struct {
	info.ImageRetriever
}

func NewImageRegistrySearcher() *ImageRegistrySearcher {
	return &ImageRegistrySearcher{}
}

func (s *ImageRegistrySearcher) Image(ref reference.DockerImageReference) (*docker10.DockerImage, error) {
	info, err := s.ImageRetriever.Image(context.TODO(), imagesource.TypedImageReference{
		Type: imagesource.DestinationRegistry,
		Ref:  ref,
	})
	if err != nil {
		return nil, err
	}
	image := &docker10.DockerImage{
		ID:              info.Config.ID,
		Parent:          info.Config.Parent,
		Created:         metav1.Time{Time: info.Config.Created},
		Author:          info.Config.Author,
		Architecture:    info.Config.Architecture,
		Size:            info.Config.Size,
		Config:          info.Config.Config,
		ContainerConfig: info.Config.ContainerConfig,
	}
	return image, nil
}

func (c *AppConfig) ensureDockerSearch() {
	if c.DockerSearcher == nil {
		c.DockerSearcher = c.DockerRegistrySearcher()
	}
}

// SetOpenShiftClient sets the passed OpenShift client in the application configuration
func (c *AppConfig) SetOpenShiftClient(imageClient imagev1typedclient.ImageV1Interface, templateClient templatev1typedclient.TemplateV1Interface, routeClient routev1typedclient.RouteV1Interface, OriginNamespace string, dockerclient *docker.Client) {
	c.OriginNamespace = OriginNamespace
	namespaces := []string{OriginNamespace}
	if openshiftNamespace := "openshift"; OriginNamespace != openshiftNamespace {
		namespaces = append(namespaces, openshiftNamespace)
	}
	c.ImageClient = imageClient
	c.RouteClient = routeClient
	c.TemplateClient = templateClient
	c.ImageStreamSearcher = app.ImageStreamSearcher{
		Client:           c.ImageClient,
		Namespaces:       namespaces,
		AllowMissingTags: c.AllowMissingImageStreamTags,
	}
	c.ImageStreamByAnnotationSearcher = app.NewImageStreamByAnnotationSearcher(
		c.ImageClient,
		c.ImageClient,
		namespaces,
	)
	c.TemplateSearcher = app.TemplateSearcher{
		Client:     c.TemplateClient,
		Namespaces: namespaces,
	}
	c.TemplateFileSearcher = &app.TemplateFileSearcher{
		Builder:   c.Builder,
		Namespace: OriginNamespace,
	}
	// the hierarchy of docker searching is:
	// 1) if we have an openshift client - query container image registries via openshift,
	// if we're unable to query via openshift, query the container image registries directly(fallback),
	// if we don't find a match there and a local docker daemon exists, look in the local registry.
	// 2) if we don't have an openshift client - query the container image registries directly,
	// if we don't find a match there and a local docker daemon exists, look in the local registry.
	c.DockerSearcher = app.DockerClientSearcher{
		Client:             dockerclient,
		Insecure:           c.InsecureRegistry,
		AllowMissingImages: c.AllowMissingImages,
		RegistrySearcher: app.ImageImportSearcher{
			Client:        c.ImageClient.ImageStreamImports(OriginNamespace),
			AllowInsecure: c.InsecureRegistry,
			Fallback:      c.DockerRegistrySearcher(),
		},
	}
}

func (c *AppConfig) tryToAddEnvironmentArguments(s string) bool {
	rc := env.IsEnvironmentArgument(s)
	if rc {
		klog.V(2).Infof("treating %s as possible environment argument\n", s)
		c.Environment = append(c.Environment, s)
	} else {
		c.EnvironmentClassificationErrors[s] = ArgumentClassificationError{
			Key:   "is not an environment variable",
			Value: nil,
		}
	}
	return rc
}

func (c *AppConfig) tryToAddSourceArguments(s string) bool {
	remote, rerr := app.IsRemoteRepository(s)
	local, derr := app.IsDirectory(s)

	if remote || local {
		klog.V(2).Infof("treating %s as possible source repo\n", s)
		c.SourceRepositories = append(c.SourceRepositories, s)
		return true
	}

	// will combine multiple errors into one line / string
	errStr := ""
	if rerr != nil {
		errStr = fmt.Sprintf("git ls-remote failed with: %v", rerr)
	}

	if derr != nil {
		if len(errStr) > 0 {
			errStr = errStr + "; "
		}
		errStr = fmt.Sprintf("%s local file access failed with: %v", errStr, derr)
	}
	c.SourceClassificationErrors[s] = ArgumentClassificationError{
		Key:   "is not a Git repository",
		Value: fmt.Errorf(errStr),
	}

	return false
}

func (c *AppConfig) tryToAddComponentArguments(s string) bool {
	err := app.IsComponentReference(s)
	if err == nil {
		klog.V(2).Infof("treating %s as a component ref\n", s)
		c.Components = append(c.Components, s)
		return true
	}
	c.ComponentClassificationErrors[s] = ArgumentClassificationError{
		Key:   "is not an image reference, image~source reference, nor template loaded in an accessible project",
		Value: err,
	}

	return false
}

func (c *AppConfig) tryToAddTemplateArguments(s string) bool {
	rc, err := app.IsPossibleTemplateFile(s)
	if rc {
		klog.V(2).Infof("treating %s as possible template file\n", s)
		c.Components = append(c.Components, s)
		return true
	}
	if err != nil {
		c.TemplateClassificationErrors[s] = ArgumentClassificationError{
			Key:   "is not a template stored in a local file",
			Value: err,
		}
	}
	return false
}

// AddArguments converts command line arguments into the appropriate bucket based on what they look like
func (c *AppConfig) AddArguments(args []string) []string {
	unknown := []string{}
	for _, s := range args {
		if len(s) == 0 {
			continue
		}

		switch {
		case c.tryToAddEnvironmentArguments(s):
			c.ClassificationWinners[s] = ArgumentClassificationWinner{Name: s, Suffix: "an environment value"}
		case c.tryToAddSourceArguments(s):
			c.ClassificationWinners[s] = ArgumentClassificationWinner{Name: s, Suffix: "a source repository"}
			delete(c.EnvironmentClassificationErrors, s)
		case c.tryToAddTemplateArguments(s):
			c.ClassificationWinners[s] = ArgumentClassificationWinner{Name: s, Suffix: "a template"}
			delete(c.EnvironmentClassificationErrors, s)
			delete(c.SourceClassificationErrors, s)
		case c.tryToAddComponentArguments(s):
			// NOTE, component argument classification currently is the most lenient, so we save it for the end
			c.ClassificationWinners[s] = ArgumentClassificationWinner{Name: s, Suffix: "an image, image~source, or loaded template reference", IncludeGitErrors: true}
			delete(c.EnvironmentClassificationErrors, s)
			delete(c.TemplateClassificationErrors, s)
			// we are going to save the source errors in case this really was a source repo in the end
		default:
			klog.V(2).Infof("treating %s as unknown\n", s)
			unknown = append(unknown, s)
		}

	}
	return unknown
}

// validateBuilders confirms that all images associated with components that are to be built,
// are builders (or we're using a non-source strategy).
func (c *AppConfig) validateBuilders(components app.ComponentReferences) error {
	if c.Strategy != newapp.StrategyUnspecified {
		return nil
	}
	errs := []error{}
	for _, ref := range components {
		input := ref.Input()
		// if we're supposed to build this thing, and the image/imagestream we've matched it to did not come from an explicit CLI argument,
		// and the image/imagestream we matched to is not explicitly an s2i builder, and we're doing a source-type build, warn the user
		// that this probably won't work and force them to declare their intention explicitly.
		if input.ExpectToBuild && input.ResolvedMatch != nil && !app.IsBuilderMatch(input.ResolvedMatch) && input.Uses != nil && input.Uses.GetStrategy() == newapp.StrategySource {
			errs = append(errs, fmt.Errorf("the image match %q for source repository %q does not appear to be a source-to-image builder.\n\n- to attempt to use this image as a source builder, pass \"--strategy=source\"\n- to use it as a base image for a Docker build, pass \"--strategy=docker\"", input.ResolvedMatch.Name, input.Uses))
			continue
		}
	}
	return kutilerrors.NewAggregate(errs)
}

func validateEnforcedName(name string) error {
	// up to 63 characters is nominally possible, however "-1" gets added on the
	// end later for the deployment controller.  Deduct 5 from 63 to at least
	// cover us up to -9999.
	if reasons := apimachineryvalidation.NameIsDNS1035Label(name, false); (len(reasons) != 0 || len(name) > 58) && !app.IsParameterizableValue(name) {
		return fmt.Errorf("invalid name: %s. Must be an a lower case alphanumeric (a-z, and 0-9) string with a maximum length of 58 characters, where the first character is a letter (a-z), and the '-' character is allowed anywhere except the first or last character.", name)
	}
	return nil
}

func validateOutputImageReference(ref string) error {
	if _, err := reference.Parse(ref); err != nil {
		return fmt.Errorf("invalid output image reference: %s", ref)
	}
	return nil
}

// buildPipelines converts a set of resolved, valid references into pipelines.
func (c *AppConfig) buildPipelines(components app.ComponentReferences, environment app.Environment, buildEnvironment app.Environment) (app.PipelineGroup, error) {
	pipelines := app.PipelineGroup{}

	buildArgs, err := utilenv.ParseBuildArg(c.BuildArgs, c.In)
	if err != nil {
		return nil, err
	}

	var DockerStrategyOptions *buildv1.DockerStrategyOptions
	if len(c.BuildArgs) > 0 {
		DockerStrategyOptions = &buildv1.DockerStrategyOptions{
			BuildArgs: buildArgs,
		}
	}

	numDockerBuilds := 0

	pipelineBuilder := app.NewPipelineBuilder(c.Name, buildEnvironment, DockerStrategyOptions, c.OutputDocker).To(c.To)
	for _, group := range components.Group() {
		klog.V(4).Infof("found group: %v", group)
		common := app.PipelineGroup{}
		for _, ref := range group {
			refInput := ref.Input()
			from := refInput.String()
			var pipeline *app.Pipeline

			switch {
			case refInput.ExpectToBuild:
				klog.V(4).Infof("will add %q secrets into a build for a source build of %q", strings.Join(c.Secrets, ","), refInput.Uses)
				if err := refInput.Uses.AddBuildSecrets(c.Secrets); err != nil {
					return nil, fmt.Errorf("unable to add build secrets %q: %v", strings.Join(c.Secrets, ","), err)
				}

				klog.V(4).Infof("will add %q configMaps into a build for a source build of %q", strings.Join(c.ConfigMaps, ","), refInput.Uses)
				if err = refInput.Uses.AddBuildConfigMaps(c.ConfigMaps); err != nil {
					return nil, fmt.Errorf("unable to add build configMaps %q: %v", strings.Join(c.Secrets, ","), err)
				}

				if refInput.Uses.GetStrategy() == newapp.StrategyDocker {
					numDockerBuilds++
				}

				var (
					image *app.ImageRef
					err   error
				)
				if refInput.ResolvedMatch != nil {
					inputImage, err := app.InputImageFromMatch(refInput.ResolvedMatch)
					if err != nil {
						return nil, fmt.Errorf("can't build %q: %v", from, err)
					}
					if !inputImage.AsImageStream && from != "scratch" && (refInput.Uses == nil || refInput.Uses.GetStrategy() != newapp.StrategyPipeline) {
						msg := "Could not find an image stream match for %q. Make sure that a container image with that tag is available on the node for the build to succeed."
						klog.Warningf(msg, from)
					}
					image = inputImage
				}

				klog.V(4).Infof("will use %q as the base image for a source build of %q", ref, refInput.Uses)
				if pipeline, err = pipelineBuilder.NewBuildPipeline(from, image, refInput.Uses, c.BinaryBuild); err != nil {
					return nil, fmt.Errorf("can't build %q: %v", refInput.Uses, err)
				}
			default:
				inputImage, err := app.InputImageFromMatch(refInput.ResolvedMatch)
				if err != nil {
					return nil, fmt.Errorf("can't include %q: %v", from, err)
				}
				if !inputImage.AsImageStream {
					msg := "Could not find an image stream match for %q. Make sure that a container image with that tag is available on the node for the deployment to succeed."
					klog.Warningf(msg, from)
				}

				klog.V(4).Infof("will include %q", ref)
				if pipeline, err = pipelineBuilder.NewImagePipeline(from, inputImage); err != nil {
					return nil, fmt.Errorf("can't include %q: %v", refInput, err)
				}
			}
			if c.Deploy {
				if c.DeploymentConfig {
					if err := pipeline.NeedsDeploymentConfig(environment, c.Labels, c.AsTestDeployment); err != nil {
						return nil, fmt.Errorf("can't set up a deployment config for %q: %v", refInput, err)
					}
				} else {
					if err := pipeline.NeedsDeployment(environment, c.Labels, c.AsTestDeployment); err != nil {
						return nil, fmt.Errorf("can't set up a deployment for %q: %v", refInput, err)
					}
				}
			}
			if c.NoOutput {
				pipeline.Build.Output = nil
			}
			if refInput.Uses != nil && refInput.Uses.GetStrategy() == newapp.StrategyPipeline {
				pipeline.Build.Output = nil
				pipeline.Deployment = nil
				pipeline.DeploymentConfig = nil
				pipeline.Image = nil
				pipeline.InputImage = nil
			}
			common = append(common, pipeline)
			if err := common.Reduce(); err != nil {
				return nil, fmt.Errorf("can't create a pipeline from %s: %v", common, err)
			}
			describeBuildPipelineWithImage(c.Out, ref, pipeline, c.OriginNamespace)
		}
		pipelines = append(pipelines, common...)
	}

	if len(c.BuildArgs) > 0 {
		if numDockerBuilds == 0 {
			return nil, fmt.Errorf("Cannot use '--build-arg' without a Docker build")
		}
		if numDockerBuilds > 1 {
			fmt.Fprintf(c.ErrOut, "--> WARNING: Applying --build-arg to multiple Docker builds.\n")
		}
	}

	return pipelines, nil
}

// buildTemplates converts a set of resolved, valid references into references to template objects.
func (c *AppConfig) buildTemplates(components app.ComponentReferences, parameters app.Environment, environment app.Environment, buildEnvironment app.Environment, templateProcessor templateprocessorclient.TemplateProcessorInterface) (string, []runtime.Object, error) {
	objects := []runtime.Object{}
	name := ""
	for _, ref := range components {
		tpl := ref.Input().ResolvedMatch.Template
		if len(tpl.Namespace) > 0 && tpl.Namespace != c.OriginNamespace {
			// if set, template namespace must match the namespace it will be processed in.
			tpl.Namespace = c.OriginNamespace
		}
		klog.V(4).Infof("processing template %s/%s", c.OriginNamespace, tpl.Name)
		if len(c.ContextDir) > 0 {
			return "", nil, fmt.Errorf("--context-dir is not supported when using a template")
		}
		result, err := TransformTemplate(tpl, templateProcessor, c.OriginNamespace, parameters, c.IgnoreUnknownParameters)
		if err != nil {
			return name, nil, err
		}
		if len(name) == 0 {
			name = tpl.Name
		}
		resultObjects, err := templateObjectsToAppObjects(result.Objects)
		if err != nil {
			return "", nil, err
		}

		objects = append(objects, resultObjects...)
		if len(resultObjects) > 0 {
			// if environment variables were passed in, let's apply the environment variables
			// to every pod template object
			for _, obj := range resultObjects {
				if bc, ok := obj.(*buildv1.BuildConfig); ok {
					buildEnv := getBuildConfigEnv(bc)
					buildEnv = app.JoinEnvironment(buildEnv, buildEnvironment.List())
					setBuildConfigEnv(bc, buildEnv)
				}
				podSpec, _, err := ometa.GetPodSpecV1(obj)
				if err == nil {
					for ii := range podSpec.Containers {
						if podSpec.Containers[ii].Env != nil {
							podSpec.Containers[ii].Env = app.JoinEnvironment(environment.List(), podSpec.Containers[ii].Env)
						} else {
							podSpec.Containers[ii].Env = environment.List()
						}
					}
				}
			}
		}

		DescribeGeneratedTemplate(c.Out, ref.Input().String(), result, c.OriginNamespace)
	}
	return name, objects, nil
}

// fakeSecretAccessor is used during dry runs of installation
type fakeSecretAccessor struct {
	token string
}

func (a *fakeSecretAccessor) Token() (string, error) {
	return a.token, nil
}
func (a *fakeSecretAccessor) CACert() (string, error) {
	return "", nil
}

// installComponents attempts to create pods to run installable images identified by the user. If an image
// is installable, we check whether it requires access to the user token. If so, the caller must have
// explicitly granted that access (because the token may be the user's).
func (c *AppConfig) installComponents(components app.ComponentReferences, env app.Environment) ([]runtime.Object, string, error) {
	if c.SkipGeneration {
		return nil, "", nil
	}

	jobs := components.InstallableComponentRefs()
	switch {
	case len(jobs) > 1:
		return nil, "", fmt.Errorf("only one installable component may be provided: %s", jobs.HumanString(", "))
	case len(jobs) == 0:
		return nil, "", nil
	}

	job := jobs[0]
	if len(components) > 1 {
		return nil, "", fmt.Errorf("%q is installable and may not be specified with other components", job.Input().Value)
	}
	input := job.Input()

	imageRef, err := app.InputImageFromMatch(input.ResolvedMatch)
	if err != nil {
		return nil, "", fmt.Errorf("can't include %q: %v", input, err)
	}
	klog.V(4).Infof("Resolved match for installer %#v", input.ResolvedMatch)

	imageRef.AsImageStream = false
	imageRef.AsResolvedImage = true
	imageRef.Env = env

	name := c.Name
	if len(name) == 0 {
		var ok bool
		name, ok = imageRef.SuggestName()
		if !ok {
			return nil, "", errors.New("can't suggest a valid name, please specify a name with --name")
		}
	}
	imageRef.ObjectName = name
	klog.V(4).Infof("Proposed installable image %#v", imageRef)

	secretAccessor := c.SecretAccessor
	generatorInput := input.ResolvedMatch.GeneratorInput
	token := generatorInput.Token
	if token != nil && !c.AllowSecretUse || secretAccessor == nil {
		if !c.DryRun {
			return nil, "", ErrRequiresExplicitAccess{Match: *input.ResolvedMatch, Input: generatorInput}
		}
		secretAccessor = &fakeSecretAccessor{token: "FAKE_TOKEN"}
	}

	objects := []runtime.Object{}

	serviceAccountName := "installer"
	if token != nil && token.ServiceAccount {
		if _, err := c.KubeClient.CoreV1().ServiceAccounts(c.OriginNamespace).Get(context.TODO(), serviceAccountName, metav1.GetOptions{}); err != nil {
			if kerrors.IsNotFound(err) {
				objects = append(objects,
					// create a new service account
					&corev1.ServiceAccount{
						// this is ok because we know exactly how we want to be serialized
						TypeMeta:   metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String(), Kind: "ServiceAccount"},
						ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName},
					},
					// grant the service account the edit role on the project (TODO: installer)
					&authv1.RoleBinding{
						// this is ok because we know exactly how we want to be serialized
						TypeMeta:   metav1.TypeMeta{APIVersion: authv1.SchemeGroupVersion.String(), Kind: "RoleBinding"},
						ObjectMeta: metav1.ObjectMeta{Name: "installer-role-binding"},
						Subjects:   []corev1.ObjectReference{{Kind: "ServiceAccount", Name: serviceAccountName}},
						RoleRef:    corev1.ObjectReference{Name: "edit"},
					},
				)
			}
		}
	}

	pod, secret, err := imageRef.InstallablePod(generatorInput, secretAccessor, serviceAccountName)
	if err != nil {
		return nil, "", err
	}
	objects = append(objects, pod)
	if secret != nil {
		objects = append(objects, secret)
	}
	for i := range objects {
		AddObjectAnnotations(objects[i], map[string]string{
			GeneratedForJob:    "true",
			GeneratedForJobFor: input.String(),
		})
	}

	describeGeneratedJob(c.Out, job, pod, secret, c.OriginNamespace)

	return objects, name, nil
}

// RunQuery executes the provided config and returns the result of the resolution.
func (c *AppConfig) RunQuery() (*QueryResult, error) {
	environment, buildEnvironment, parameters, err := c.validate()
	if err != nil {
		return nil, err
	}
	// TODO: I don't belong here
	c.ensureDockerSearch()

	if c.AsList {
		if c.AsSearch {
			return nil, errors.New("--list and --search can't be used together")
		}
		if c.HasArguments() {
			return nil, errors.New("--list can't be used with arguments")
		}
		c.Components = append(c.Components, "*")
	}

	b := &app.ReferenceBuilder{}
	s := &c.SourceRepositories
	i := &c.ImageStreams
	if err := AddComponentInputsToRefBuilder(b, &c.Resolvers, &c.ComponentInputs, &c.GenerationInputs, s, i); err != nil {
		return nil, err
	}
	components, repositories, errs := b.Result()
	if len(errs) > 0 {
		return nil, kutilerrors.NewAggregate(errs)
	}

	if len(components) == 0 && !c.AsList {
		return nil, ErrNoInputs
	}

	if len(repositories) > 0 {
		errs = append(errs, errors.New("--search can't be used with source code"))
	}
	if len(environment) > 0 {
		errs = append(errs, errors.New("--search can't be used with --env"))
	}
	if len(buildEnvironment) > 0 {
		errs = append(errs, errors.New("--search can't be used with --build-env"))
	}
	if len(parameters) > 0 {
		errs = append(errs, errors.New("--search can't be used with --param"))
	}
	if len(errs) > 0 {
		return nil, kutilerrors.NewAggregate(errs)
	}

	if err := components.Search(); err != nil {
		return nil, err
	}

	klog.V(4).Infof("Code %v", repositories)
	klog.V(4).Infof("Components %v", components)

	matches := app.ComponentMatches{}
	objects := app.Objects{}
	for _, ref := range components {
		for _, match := range ref.Input().SearchMatches {
			matches = append(matches, match)
			if match.IsTemplate() {
				objects = append(objects, match.Template)
			} else if match.IsImage() {
				if match.ImageStream != nil {
					objects = append(objects, match.ImageStream)
				}
				if match.DockerImage != nil {
					objects = append(objects, match.DockerImage)
				}
			}
		}
	}

	return &QueryResult{
		Matches: matches,
		List:    &metainternalversion.List{Items: objects},
	}, nil
}

func (c *AppConfig) validate() (app.Environment, app.Environment, app.Environment, error) {
	env, err := app.ParseAndCombineEnvironment(c.Environment, c.EnvironmentFiles, c.In, func(key, file string) error {
		if file == "" {
			fmt.Fprintf(c.ErrOut, "warning: Environment variable %q was overwritten\n", key)
		} else {
			fmt.Fprintf(c.ErrOut, "warning: Environment variable %q already defined, ignoring value from file %q\n", key, file)
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	buildEnv, err := app.ParseAndCombineEnvironment(c.BuildEnvironment, c.BuildEnvironmentFiles, c.In, func(key, file string) error {
		if file == "" {
			fmt.Fprintf(c.ErrOut, "warning: Build Environment variable %q was overwritten\n", key)
		} else {
			fmt.Fprintf(c.ErrOut, "warning: Build Environment variable %q already defined, ignoring value from file %q\n", key, file)
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	params, err := app.ParseAndCombineEnvironment(c.TemplateParameters, c.TemplateParameterFiles, c.In, func(key, file string) error {
		if file == "" {
			fmt.Fprintf(c.ErrOut, "warning: Template parameter %q was overwritten\n", key)
		} else {
			fmt.Fprintf(c.ErrOut, "warning: Template parameter %q already defined, ignoring value from file %q\n", key, file)
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return env, buildEnv, params, nil
}

func templateObjectsToAppObjects(objs []runtime.RawExtension) (app.Objects, error) {
	converted := []runtime.Object{}

	for _, raw := range objs {
		if raw.Object != nil {
			continue
		}

		obj, err := runtime.Decode(scheme.Codecs.UniversalDeserializer(), raw.Raw)
		if err != nil {
			return nil, err
		}
		converted = append(converted, obj)
	}

	return converted, nil
}

// Run executes the provided config to generate objects.
func (c *AppConfig) Run() (*AppResult, error) {
	env, buildenv, parameters, err := c.validate()

	if err != nil {
		return nil, err
	}
	// TODO: I don't belong here
	c.ensureDockerSearch()

	resolved, err := Resolve(c)
	if err != nil {
		return nil, err
	}

	repositories := resolved.Repositories
	components := resolved.Components

	if len(repositories) == 0 && len(components) == 0 {
		return nil, ErrNoInputs
	}

	if err := c.validateBuilders(components); err != nil {
		return nil, err
	}

	if len(c.Name) > 0 {
		if err := validateEnforcedName(c.Name); err != nil {
			return nil, err
		}
	}

	if len(c.To) > 0 {
		if err := validateOutputImageReference(c.To); err != nil {
			return nil, err
		}
	}

	if len(components.ImageComponentRefs().Group()) > 1 && len(c.Name) > 0 {
		return nil, errors.New("only one component or source repository can be used when specifying a name")
	}
	if len(components.UseSource()) > 1 && len(c.To) > 0 {
		return nil, errors.New("only one component with source can be used when specifying an output image reference")
	}

	// identify if there are installable components in the input provided by the user
	installables, name, err := c.installComponents(components, env)
	if err != nil {
		return nil, err
	}
	if len(installables) > 0 {
		return &AppResult{
			List:      &metainternalversion.List{Items: installables},
			Name:      name,
			Namespace: c.OriginNamespace,

			GeneratedJobs: true,
		}, nil
	}

	pipelines, err := c.buildPipelines(components.ImageComponentRefs(), env, buildenv)
	if err != nil {
		return nil, err
	}

	acceptors := app.Acceptors{app.NewAcceptUnique(), app.AcceptNew,
		app.NewAcceptNonExistentImageStream(c.Typer, c.ImageClient, c.OriginNamespace), app.NewAcceptNonExistentImageStreamTag(c.Typer, c.ImageClient, c.OriginNamespace)}
	objects := app.Objects{}
	accept := app.NewAcceptFirst()
	for _, p := range pipelines {
		accepted, err := p.Objects(accept, acceptors)
		if err != nil {
			return nil, fmt.Errorf("can't setup %q: %v", p.From, err)
		}
		objects = append(objects, accepted...)
	}

	objects = app.AddServices(objects, false)

	templateProcessor := templateprocessorclient.NewTemplateProcessorClient(c.TemplateClient.RESTClient(), c.OriginNamespace)
	templateName, templateObjects, err := c.buildTemplates(components.TemplateComponentRefs(), parameters, env, buildenv, templateProcessor)
	if err != nil {
		return nil, err
	}

	// check for circular reference specifically from the template objects and print warnings if they exist
	err = c.checkCircularReferences(templateObjects)
	if err != nil {
		if err, ok := err.(app.CircularOutputReferenceError); ok {
			// templates only apply to `oc new-app`
			addOn := ""
			if len(c.Name) == 0 {
				addOn = ", override artifact names with --name"
			}
			fmt.Fprintf(c.ErrOut, "--> WARNING: %v\n%s", err, addOn)
		} else {
			return nil, err
		}
	}

	// Ensure that extra tags are not being created
	objects, err = c.removeRedundantTags(objects)
	if err != nil {
		return nil, err
	}

	// check for circular reference specifically from the newly generated objects, handling new-app vs. new-build nuances as needed
	err = c.checkCircularReferences(objects)
	if err != nil {
		if err, ok := err.(app.CircularOutputReferenceError); ok {
			if c.ExpectToBuild {
				// circular reference handling for `oc new-build`.
				if len(c.To) == 0 {
					// Output reference was generated, return error.
					return nil, fmt.Errorf("%v, set a different tag with --to", err)
				}
				// Output reference was explicitly provided, print warning.
				fmt.Fprintf(c.ErrOut, "--> WARNING: %v\n", err)
			} else {
				// circular reference handling for `oc new-app`
				if len(c.Name) == 0 {
					return nil, fmt.Errorf("%v, override artifact names with --name", err)
				}
				// Output reference was explicitly provided, print warning.
				fmt.Fprintf(c.ErrOut, "--> WARNING: %v\n", err)
			}
		} else {
			return nil, err
		}
	}

	objects = append(objects, templateObjects...)

	name = c.Name
	if len(name) == 0 {
		name = templateName
	}
	if len(name) == 0 {
		for _, pipeline := range pipelines {
			if pipeline.Deployment != nil {
				name = pipeline.Deployment.Name
				break
			}
			if pipeline.DeploymentConfig != nil {
				name = pipeline.DeploymentConfig.Name
				break
			}
		}
	}
	if len(name) == 0 {
		for _, obj := range objects {
			if bc, ok := obj.(*buildv1.BuildConfig); ok {
				name = bc.Name
				break
			}
		}
	}
	if len(c.SourceSecret) > 0 {
		if len(apimachineryvalidation.NameIsDNSSubdomain(c.SourceSecret, false)) != 0 {
			return nil, fmt.Errorf("source secret name %q is invalid", c.SourceSecret)
		}
		for _, obj := range objects {
			if bc, ok := obj.(*buildv1.BuildConfig); ok {
				klog.V(4).Infof("Setting source secret for build config to: %v", c.SourceSecret)
				bc.Spec.Source.SourceSecret = &corev1.LocalObjectReference{Name: c.SourceSecret}
				break
			}
		}
	}
	if len(c.PushSecret) > 0 {
		if len(apimachineryvalidation.NameIsDNSSubdomain(c.PushSecret, false)) != 0 {
			return nil, fmt.Errorf("push secret name %q is invalid", c.PushSecret)
		}
		for _, obj := range objects {
			if bc, ok := obj.(*buildv1.BuildConfig); ok {
				klog.V(4).Infof("Setting push secret for build config to: %v", c.SourceSecret)
				bc.Spec.Output.PushSecret = &corev1.LocalObjectReference{Name: c.PushSecret}
				break
			}
		}
	}

	return &AppResult{
		List:      &metainternalversion.List{Items: objects},
		Name:      name,
		HasSource: len(repositories) != 0,
		Namespace: c.OriginNamespace,
	}, nil
}

func (c *AppConfig) findImageStreamInObjectList(objects app.Objects, name, namespace string) *imagev1.ImageStream {
	for _, check := range objects {
		if is, ok := check.(*imagev1.ImageStream); ok {
			nsToCompare := is.Namespace
			if len(nsToCompare) == 0 {
				nsToCompare = c.OriginNamespace
			}
			if is.Name == name && nsToCompare == namespace {
				return is
			}
		}
	}
	return nil
}

// crossStreamCircularTagReference inherits some logic from imageapi.FollowTagReference, but differs in that a) it is only concerned
// with whether we can definitively say the IST chain is circular, and b) can cross image stream boundaries;
// not in imageapi pkg (see imports above) like other helpers cause of import cycle with the image client
func (c *AppConfig) crossStreamCircularTagReference(stream *imagev1.ImageStream, tag string, objects app.Objects) bool {
	if stream == nil {
		return false
	}
	seen := sets.NewString()
	for {
		if seen.Has(stream.ObjectMeta.Namespace + ":" + stream.ObjectMeta.Name + ":" + tag) {
			// circular reference
			return true
		}
		seen.Insert(stream.ObjectMeta.Namespace + ":" + stream.ObjectMeta.Name + ":" + tag)
		tagRef, ok := imageutil.SpecHasTag(stream, tag)
		if !ok {
			// no tag at the end of the rainbow
			return false
		}
		if tagRef.From == nil || tagRef.From.Kind != "ImageStreamTag" {
			// terminating tag
			return false
		}
		if strings.Contains(tagRef.From.Name, ":") {
			// another stream
			fromstream, fromtag, ok := imageutil.SplitImageStreamTag(tagRef.From.Name)
			if !ok {
				return false
			}
			stream, err := c.ImageClient.ImageStreams(tagRef.From.Namespace).Get(context.TODO(), fromstream, metav1.GetOptions{})
			if err != nil && !kerrors.IsNotFound(err) {
				return false
			}
			if stream == nil || kerrors.IsNotFound(err) {
				// in case we are creating with this generate run, check the list of objects provided before
				// giving up
				if stream = c.findImageStreamInObjectList(objects, fromstream, tagRef.From.Namespace); stream == nil {
					return false
				}
			}
			tag = fromtag
		} else {
			tag = tagRef.From.Name
		}
	}
}

// crossStreamInputToOutputTagReference inherits some logic from crossStreamCircularTagReference, but differs in that a) it is only concerned
// with whether we can definitively say the input IST from/follow chain lands at the output IST;
// this method currently assumes that crossStreamCircularTagReference was called on both in/out stream:tag combinations prior to entering this method
// not in imageapi pkg (see imports above) like other helpers cause of import cycle with the image client
func (c *AppConfig) crossStreamInputToOutputTagReference(instream, outstream *imagev1.ImageStream, intag, outtag string, objects app.Objects) bool {
	if instream == nil || outstream == nil {
		return false
	}
	for {
		if instream.ObjectMeta.Namespace == outstream.ObjectMeta.Namespace &&
			instream.ObjectMeta.Name == outstream.ObjectMeta.Name &&
			intag == outtag {
			return true
		}

		tagRef, ok := imageutil.SpecHasTag(instream, intag)
		if !ok {
			// no tag at the end of the rainbow
			return false
		}
		if tagRef.From == nil || tagRef.From.Kind != "ImageStreamTag" {
			// terminating tag
			return false
		}
		if strings.Contains(tagRef.From.Name, ":") {
			// another stream
			fromstream, fromtag, ok := imageutil.SplitImageStreamTag(tagRef.From.Name)
			if !ok {
				return false
			}
			instream, err := c.ImageClient.ImageStreams(tagRef.From.Namespace).Get(context.TODO(), fromstream, metav1.GetOptions{})
			if err != nil && !kerrors.IsNotFound(err) {
				return false
			}
			if instream == nil || kerrors.IsNotFound(err) {
				if instream = c.findImageStreamInObjectList(objects, fromstream, tagRef.From.Namespace); instream == nil {
					return false
				}
			}
			intag = fromtag
		} else {
			intag = tagRef.From.Name
		}
	}
}

// followRefToDockerImage follows a buildconfig...To/From reference until it
// terminates in container image information. This can include dereferencing chains
// of ImageStreamTag references that already exist or which are being created.
// ref is the reference to To/From to follow. If ref is an ImageStreamTag
// that is following another ImageStreamTag, isContext should be set to the
// parent IS. Finally, objects is the list of objects that new-app is creating
// to support the buildconfig. It returns a reference to a terminal DockerImage
// or nil if one could not be determined (a valid, non-error outcome). err
// is only used to indicate that the follow encountered a severe error
// (e.g malformed data).
func (c *AppConfig) followRefToDockerImage(ref *corev1.ObjectReference, isContext *imagev1.ImageStream, objects app.Objects) (*corev1.ObjectReference, error) {
	if ref == nil {
		return nil, errors.New("Unable to follow nil")
	}

	if ref.Kind == "DockerImage" {
		// Make a shallow copy so we don't modify the ObjectReference properties that
		// new-app/build created.
		copy := *ref
		// Namespace should not matter here. The DockerImage URL will include project
		// information if it is relevant.
		copy.Namespace = ""

		// DockerImage names may or may not have a tag suffix. Add :latest if there
		// is no tag so that string comparison will behave as expected.
		if !strings.Contains(copy.Name, ":") {
			copy.Name += ":" + imagev1.DefaultImageTag
		}
		return &copy, nil
	}

	if ref.Kind == "ImageStreamImage" {
		// even if the associated tag for this ImageStreamImage matches a output ImageStreamTag, when the image
		// is built it will have a new sha ... you are essentially using a single/unique older version of a image as the base
		// to build future images;  all this means we can leave the ref name as is and return with no error
		// also do shallow copy like above
		copy := *ref
		return &copy, nil
	}

	if ref.Kind != "ImageStreamTag" {
		return nil, fmt.Errorf("Unable to follow reference type: %q", ref.Kind)
	}

	isNS := ref.Namespace
	if len(isNS) == 0 {
		isNS = c.OriginNamespace
	}

	// Otherwise, we are tracing an IST reference
	isName, isTag, ok := imageutil.SplitImageStreamTag(ref.Name)
	if !ok {
		if isContext == nil {
			return nil, fmt.Errorf("Unable to parse ImageStreamTag reference: %q", ref.Name)
		}
		// Otherwise, we are following a tag that references another tag in the same ImageStream.
		isName = isContext.Name
		isTag = ref.Name
	} else {
		// Search for a new image stream context based on the name and tag
		isContext = nil

		// The imagestream is usually being created alongside the buildconfig
		// when new-build is being used, so scan objects being created for it.
		if isContext = c.findImageStreamInObjectList(objects, isName, isNS); isContext == nil {
			var err error
			isContext, err = c.ImageClient.ImageStreams(isNS).Get(context.TODO(), isName, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("Unable to check for circular build input/outputs: %v", err)
			}
			if isContext == nil {
				return nil, nil
			}
		}
	}

	// protect our recursion by leveraging a non-recursive IST -> IST traversal check
	// to warn us about circular problems with the tag so we can bail
	circular := c.crossStreamCircularTagReference(isContext, isTag, objects)
	if circular {
		return nil, app.CircularReferenceError{Reference: ref.Name}
	}

	// Dereference ImageStreamTag to see what it is pointing to
	var target *corev1.ObjectReference
	if tagRef, ok := imageutil.SpecHasTag(isContext, isTag); ok {
		target = tagRef.From
	}

	// From can still be nil even with the call to FollowTagReference returning OK
	if target == nil {
		if isContext.Spec.DockerImageRepository == "" {
			// Otherwise, this appears to be a new IS, created by new-app, with very little information
			// populated. We cannot resolve a DockerImage.
			return nil, nil
		}
		// Legacy InputStream without tag support? Spoof what we need.
		imageName := isContext.Spec.DockerImageRepository + ":" + isTag
		return &corev1.ObjectReference{
			Kind: "DockerImage",
			Name: imageName,
		}, nil
	}

	return c.followRefToDockerImage(target, isContext, objects)
}

// removeRedundantTags cycles through the supplied list of objects and removes
// any tags that are already being created in an imagestream
func (c *AppConfig) removeRedundantTags(objects app.Objects) (app.Objects, error) {
	objectsToRemove := map[string]struct{}{}
	for _, obj := range objects {
		if ist, ok := obj.(*imagev1.ImageStreamTag); ok {
			istNamespace := ist.Namespace
			if len(istNamespace) == 0 {
				istNamespace = c.OriginNamespace
			}
			streamName, tagName, ok := imageutil.SplitImageStreamTag(ist.Name)
			if !ok {
				return nil, fmt.Errorf("Unable to split ImageStreamTag: %s", ist.Name)
			}
			is := c.findImageStreamInObjectList(objects, streamName, istNamespace)
			if is != nil {
				if _, hasTag := imageutil.SpecHasTag(is, tagName); hasTag {
					objectsToRemove[fmt.Sprintf("%s/%s", istNamespace, ist.Name)] = struct{}{}

				}
			}

		}
	}

	objectsToKeep := app.Objects{}
	for _, obj := range objects {
		if ist, ok := obj.(*imagev1.ImageStreamTag); ok {
			istNamespace := ist.Namespace
			if len(istNamespace) == 0 {
				istNamespace = c.OriginNamespace
			}
			istName := fmt.Sprintf("%s/%s", istNamespace, ist.Name)
			if _, ok := objectsToRemove[istName]; ok {
				klog.V(4).Infof("Removing duplicate tag from object list: %s", istName)
				continue
			}
		}
		objectsToKeep = append(objectsToKeep, obj)
	}

	return objectsToKeep, nil
}

// checkCircularReferences ensures there are no builds that can trigger themselves
// due to an imagechangetrigger that matches the output destination of the image.
// objects is a list of api objects produced by new-app.
func (c *AppConfig) checkCircularReferences(objects app.Objects) error {
	for i, obj := range objects {

		if klog.V(5).Enabled() {
			json, _ := json.MarshalIndent(obj, "", "\t")
			klog.Infof("\n\nCycle check input object %v:\n%v\n", i, string(json))
		}

		if bc, ok := obj.(*buildv1.BuildConfig); ok {
			input := buildutil.GetInputReference(bc.Spec.Strategy)
			output := bc.Spec.Output.To

			if output == nil || input == nil {
				return nil
			}

			dockerInput, err := c.followRefToDockerImage(input, nil, objects)
			if err != nil {
				if _, ok := err.(app.CircularReferenceError); ok {
					return err
				}
				klog.Warningf("Unable to check for circular build input: %v", err)
				return nil
			}
			klog.V(5).Infof("Post follow input:\n%#v\n", dockerInput)

			dockerOutput, err := c.followRefToDockerImage(output, nil, objects)
			if err != nil {
				if _, ok := err.(app.CircularReferenceError); ok {
					return err
				}
				klog.Warningf("Unable to check for circular build output: %v", err)
				return nil
			}
			klog.V(5).Infof("Post follow:\n%#v\n", dockerOutput)

			// with reuse of existing image streams, some scenarios have arisen where
			// comparisons of input/output prior to following the ref to the container image
			// are necessary;
			// we still cite errors with circular IST chains;
			// however, if the output IST points to the same
			// image as the input IST because its referential chain takes it to the input IST (i.e. From refs),
			// where one of the ISTs has a direct docker ref, we are allowing that;
			// thus, invocations like `oc new-build --binary php`,
			// where "php" is a image stream with such a structure, do not require
			// the `--to` option
			// otherwise, we are deferring to the container image resolution path
			if output.Kind == "ImageStreamTag" && input.Kind == "ImageStreamTag" {
				iname, itag, iok := imageutil.SplitImageStreamTag(input.Name)
				oname, otag, ook := imageutil.SplitImageStreamTag(output.Name)
				if iok && ook {
					inamespace := input.Namespace
					if len(inamespace) == 0 {
						inamespace = c.OriginNamespace
					}
					onamespace := output.Namespace
					if len(onamespace) == 0 {
						onamespace = c.OriginNamespace
					}
					istream, err := c.ImageClient.ImageStreams(inamespace).Get(context.TODO(), iname, metav1.GetOptions{})
					if istream == nil || kerrors.IsNotFound(err) {
						istream = c.findImageStreamInObjectList(objects, iname, inamespace)
					}
					ostream, err := c.ImageClient.ImageStreams(onamespace).Get(context.TODO(), oname, metav1.GetOptions{})
					if ostream == nil || kerrors.IsNotFound(err) {
						ostream = c.findImageStreamInObjectList(objects, oname, onamespace)
					}
					if istream != nil && ostream != nil {
						if circularOutput := c.crossStreamInputToOutputTagReference(istream, ostream, itag, otag, objects); circularOutput {
							if dockerInput != nil {
								return app.CircularOutputReferenceError{Reference: fmt.Sprintf("%s", dockerInput.Name)}
							}
							return app.CircularOutputReferenceError{Reference: fmt.Sprintf("%s", input.Name)}
						}
						return nil
					}
				}
			}

			if dockerInput != nil && dockerOutput != nil {
				if reflect.DeepEqual(dockerInput, dockerOutput) {
					return app.CircularOutputReferenceError{Reference: fmt.Sprintf("%s", dockerInput.Name)}
				}
			}

			// If it is not possible to follow input and output out to DockerImages,
			// it is likely they are referencing newly created ImageStreams. Just
			// make sure they are not the same image stream.
			inCopy := *input
			outCopy := *output
			for _, ref := range []*corev1.ObjectReference{&inCopy, &outCopy} {
				// Some code paths add namespace and others don't. Make things
				// consistent.
				if len(ref.Namespace) == 0 {
					ref.Namespace = c.OriginNamespace
				}
			}

			if reflect.DeepEqual(inCopy, outCopy) {
				return app.CircularOutputReferenceError{Reference: fmt.Sprintf("%s/%s", inCopy.Namespace, inCopy.Name)}
			}
		}
	}
	return nil
}

func (c *AppConfig) Querying() bool {
	return c.AsList || c.AsSearch
}

func (c *AppConfig) HasArguments() bool {
	return len(c.Components) > 0 ||
		len(c.ImageStreams) > 0 ||
		len(c.DockerImages) > 0 ||
		len(c.Templates) > 0 ||
		len(c.TemplateFiles) > 0
}

// getBuildConfigEnv gets the buildconfig strategy environment
func getBuildConfigEnv(buildConfig *buildv1.BuildConfig) []corev1.EnvVar {
	switch {
	case buildConfig.Spec.Strategy.SourceStrategy != nil:
		return buildConfig.Spec.Strategy.SourceStrategy.Env
	case buildConfig.Spec.Strategy.DockerStrategy != nil:
		return buildConfig.Spec.Strategy.DockerStrategy.Env
	case buildConfig.Spec.Strategy.CustomStrategy != nil:
		return buildConfig.Spec.Strategy.CustomStrategy.Env
	case buildConfig.Spec.Strategy.JenkinsPipelineStrategy != nil:
		return buildConfig.Spec.Strategy.JenkinsPipelineStrategy.Env
	default:
		return nil
	}
}

// setBuildConfigEnv replaces the current buildconfig environment
func setBuildConfigEnv(buildConfig *buildv1.BuildConfig, env []corev1.EnvVar) {
	var oldEnv *[]corev1.EnvVar

	switch {
	case buildConfig.Spec.Strategy.SourceStrategy != nil:
		oldEnv = &buildConfig.Spec.Strategy.SourceStrategy.Env
	case buildConfig.Spec.Strategy.DockerStrategy != nil:
		oldEnv = &buildConfig.Spec.Strategy.DockerStrategy.Env
	case buildConfig.Spec.Strategy.CustomStrategy != nil:
		oldEnv = &buildConfig.Spec.Strategy.CustomStrategy.Env
	case buildConfig.Spec.Strategy.JenkinsPipelineStrategy != nil:
		oldEnv = &buildConfig.Spec.Strategy.JenkinsPipelineStrategy.Env
	default:
		return
	}
	*oldEnv = env
}

// AddObjectAnnotations adds new annotation(s) to a single runtime.Object
func AddObjectAnnotations(obj runtime.Object, annotations map[string]string) error {
	if len(annotations) == 0 {
		return nil
	}

	accessor, err := meta.Accessor(obj)
	if err != nil {
		return err
	}

	metaAnnotations := accessor.GetAnnotations()
	if metaAnnotations == nil {
		metaAnnotations = make(map[string]string)
	}
	for k, v := range annotations {
		metaAnnotations[k] = v
	}
	accessor.SetAnnotations(metaAnnotations)

	switch objType := obj.(type) {
	case *v1.Deployment:
		if err := addDeploymentNestedAnnotations(objType, annotations); err != nil {
			return fmt.Errorf("unable to add nested annotations to %s/%s: %v", obj.GetObjectKind().GroupVersionKind(), accessor.GetName(), err)
		}
	case *appsv1.DeploymentConfig:
		if err := addDeploymentConfigNestedAnnotations(objType, annotations); err != nil {
			return fmt.Errorf("unable to add nested annotations to %s/%s: %v", obj.GetObjectKind().GroupVersionKind(), accessor.GetName(), err)
		}
	}

	return nil
}

func addDeploymentNestedAnnotations(obj *v1.Deployment, annotations map[string]string) error {
	if obj.Spec.Template.Annotations == nil {
		obj.Spec.Template.Annotations = make(map[string]string)
	}
	for k, v := range annotations {
		obj.Spec.Template.Annotations[k] = v
	}
	return nil
}

func addDeploymentConfigNestedAnnotations(obj *appsv1.DeploymentConfig, annotations map[string]string) error {
	if obj.Spec.Template == nil {
		return nil
	}
	if obj.Spec.Template.Annotations == nil {
		obj.Spec.Template.Annotations = make(map[string]string)
	}
	for k, v := range annotations {
		obj.Spec.Template.Annotations[k] = v
	}
	return nil
}
