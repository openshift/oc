package app

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	kappsv1 "k8s.io/api/apps/v1"
	kappsv1beta2 "k8s.io/api/apps/v1beta2"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	apimachineryvalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	kuval "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog"
	"k8s.io/kubectl/pkg/scheme"

	"github.com/openshift/api/apps"
	appsv1 "github.com/openshift/api/apps/v1"
	"github.com/openshift/api/build"
	buildv1 "github.com/openshift/api/build/v1"
	"github.com/openshift/api/image"
	dockerv10 "github.com/openshift/api/image/docker10"
	imagev1 "github.com/openshift/api/image/v1"
	imagev1typedclient "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	"github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/oc/pkg/helpers/legacy"
	"github.com/openshift/oc/pkg/helpers/newapp"
	"github.com/openshift/oc/pkg/helpers/newapp/docker/dockerfile"
)

func init() {
	utilruntime.Must(apps.Install(scheme.Scheme))
	utilruntime.Must(build.Install(scheme.Scheme))
	utilruntime.Must(image.Install(scheme.Scheme))
}

// A PipelineBuilder creates Pipeline instances.
type PipelineBuilder interface {
	To(string) PipelineBuilder

	NewBuildPipeline(string, *ImageRef, *SourceRepository, bool) (*Pipeline, error)
	NewImagePipeline(string, *ImageRef) (*Pipeline, error)
}

// NewPipelineBuilder returns a PipelineBuilder using name as a base name. A
// PipelineBuilder always creates pipelines with unique names, so that the
// actual name of a pipeline (Pipeline.Name) might differ from the base name.
// The pipelines created with a PipelineBuilder will have access to the given
// environment. The boolean outputDocker controls whether builds will output to
// an image stream tag or container image reference.
func NewPipelineBuilder(name string, environment Environment, dockerStrategyOptions *buildv1.DockerStrategyOptions, outputDocker bool) PipelineBuilder {
	return &pipelineBuilder{
		nameGenerator:         NewUniqueNameGenerator(name),
		environment:           environment,
		outputDocker:          outputDocker,
		dockerStrategyOptions: dockerStrategyOptions,
	}
}

type pipelineBuilder struct {
	nameGenerator         UniqueNameGenerator
	environment           Environment
	outputDocker          bool
	to                    string
	dockerStrategyOptions *buildv1.DockerStrategyOptions
}

func (pb *pipelineBuilder) To(name string) PipelineBuilder {
	pb.to = name
	return pb
}

// NewBuildPipeline creates a new pipeline with components that are expected to
// be built.
func (pb *pipelineBuilder) NewBuildPipeline(from string, input *ImageRef, sourceRepository *SourceRepository, binary bool) (*Pipeline, error) {
	strategy, source, err := StrategyAndSourceForRepository(sourceRepository, input)
	if err != nil {
		return nil, fmt.Errorf("can't build %q: %v", from, err)
	}

	var name string
	output := &ImageRef{
		OutputImage:   true,
		AsImageStream: !pb.outputDocker,
	}
	if len(pb.to) > 0 {
		outputImageRef, err := reference.Parse(pb.to)
		if err != nil {
			return nil, err
		}
		output.Reference = outputImageRef
		name, err = pb.nameGenerator.Generate(NameSuggestions{source, output, input})
		if err != nil {
			return nil, err
		}
	} else {
		name, err = pb.nameGenerator.Generate(NameSuggestions{source, input})
		if err != nil {
			return nil, err
		}
		output.Reference = reference.DockerImageReference{
			Name: name,
			Tag:  imagev1.DefaultImageTag,
		}
	}
	source.Name = name

	// Append any exposed ports from Dockerfile to input image
	if sourceRepository.GetStrategy() == newapp.StrategyDocker && sourceRepository.Info() != nil {
		node := sourceRepository.Info().Dockerfile.AST()
		ports := dockerfile.LastExposedPorts(node)
		if len(ports) > 0 {
			if input.Info == nil {
				input.Info = &dockerv10.DockerImage{
					Config: &dockerv10.DockerConfig{},
				}
			}
			input.Info.Config.ExposedPorts = map[string]struct{}{}
			for _, p := range ports {
				input.Info.Config.ExposedPorts[p] = struct{}{}
			}
		}
	}

	if input != nil {
		// TODO: assumes that build doesn't change the image metadata. In the future
		// we could get away with deferred generation possibly.
		output.Info = input.Info
	}

	build := &BuildRef{
		Source:                source,
		Input:                 input,
		Strategy:              strategy,
		Output:                output,
		Env:                   pb.environment,
		DockerStrategyOptions: pb.dockerStrategyOptions,
		Binary:                binary,
	}

	return &Pipeline{
		Name:       name,
		From:       from,
		InputImage: input,
		Image:      output,
		Build:      build,
	}, nil
}

// NewImagePipeline creates a new pipeline with components that are not expected
// to be built.
func (pb *pipelineBuilder) NewImagePipeline(from string, input *ImageRef) (*Pipeline, error) {
	name, err := pb.nameGenerator.Generate(input)
	if err != nil {
		return nil, err
	}
	input.ObjectName = name

	return &Pipeline{
		Name:  name,
		From:  from,
		Image: input,
	}, nil
}

// Pipeline holds components.
type Pipeline struct {
	Name string
	From string

	InputImage *ImageRef
	Build      *BuildRef
	Image      *ImageRef
	Deployment *DeploymentConfigRef
	Labels     map[string]string
}

// NeedsDeployment sets the pipeline for deployment.
func (p *Pipeline) NeedsDeployment(env Environment, labels map[string]string, asTest bool) error {
	if p.Deployment != nil {
		return nil
	}
	p.Deployment = &DeploymentConfigRef{
		Name: p.Name,
		Images: []*ImageRef{
			p.Image,
		},
		Env:    env,
		Labels: labels,
		AsTest: asTest,
	}
	return nil
}

// Objects converts all the components in the pipeline into runtime objects.
func (p *Pipeline) Objects(accept, objectAccept Acceptor) (Objects, error) {
	objects := Objects{}
	if p.InputImage != nil && p.InputImage.AsImageStream && accept.Accept(p.InputImage) {
		repo, err := p.InputImage.ImageStream()
		if err != nil {
			return nil, err
		}
		if objectAccept.Accept(repo) {
			objects = append(objects, repo)
		} else {
			// if the image stream exists, try creating the image stream tag
			tag, err := p.InputImage.ImageStreamTag()
			if err != nil {
				return nil, err
			}
			if objectAccept.Accept(tag) && accept.Accept(tag) {
				objects = append(objects, tag)
			}
		}
	}
	if p.Image != nil && p.Image.AsImageStream && accept.Accept(p.Image) {
		repo, err := p.Image.ImageStream()
		if err != nil {
			return nil, err
		}
		if objectAccept.Accept(repo) {
			objects = append(objects, repo)
		} else {
			// if the image stream exists, try creating the image stream tag
			tag, err := p.Image.ImageStreamTag()
			if err != nil {
				return nil, err
			}
			if objectAccept.Accept(tag) {
				objects = append(objects, tag)
			}
		}
	}
	if p.Build != nil && accept.Accept(p.Build) {
		build, err := p.Build.BuildConfig()
		if err != nil {
			return nil, err
		}
		if objectAccept.Accept(build) {
			objects = append(objects, build)
		}
		if p.Build.Source != nil && p.Build.Source.SourceImage != nil && p.Build.Source.SourceImage.AsImageStream && accept.Accept(p.Build.Source.SourceImage) {
			srcImage, err := p.Build.Source.SourceImage.ImageStream()
			if err != nil {
				return nil, err
			}
			if objectAccept.Accept(srcImage) {
				objects = append(objects, srcImage)
			}
		}
	}
	if p.Deployment != nil && accept.Accept(p.Deployment) {
		dc, err := p.Deployment.DeploymentConfig()
		if err != nil {
			return nil, err
		}
		if objectAccept.Accept(dc) {
			objects = append(objects, dc)
		}
	}
	return objects, nil
}

// PipelineGroup is a group of Pipelines.
type PipelineGroup []*Pipeline

// Reduce squashes all common components from the pipelines.
func (g PipelineGroup) Reduce() error {
	var deployment *DeploymentConfigRef
	for _, p := range g {
		if p.Deployment == nil || p.Deployment == deployment {
			continue
		}
		if deployment == nil {
			deployment = p.Deployment
		} else {
			deployment.Images = append(deployment.Images, p.Deployment.Images...)
			deployment.Env = NewEnvironment(deployment.Env, p.Deployment.Env)
			p.Deployment = deployment
		}
	}
	return nil
}

func (g PipelineGroup) String() string {
	s := []string{}
	for _, p := range g {
		s = append(s, p.From)
	}
	return strings.Join(s, "+")
}

// MakeSimpleName strips any non-alphanumeric characters out of a string and returns
// either an empty string or a string which is valid for most Kubernetes resources.
func MakeSimpleName(name string) string {
	name = strings.ToLower(name)
	name = invalidServiceChars.ReplaceAllString(name, "")
	name = strings.TrimFunc(name, func(r rune) bool { return r == '-' })
	if len(name) > kuval.DNS1035LabelMaxLength {
		name = name[:kuval.DNS1035LabelMaxLength]
	}
	return name
}

var invalidServiceChars = regexp.MustCompile("[^-a-z0-9]")

func makeValidServiceName(name string) (string, string) {
	if len(apimachineryvalidation.NameIsDNSSubdomain(name, false)) == 0 {
		return name, ""
	}
	name = MakeSimpleName(name)
	if len(name) == 0 {
		return "", "service-"
	}
	return name, ""
}

type sortablePorts []corev1.ContainerPort

func (s sortablePorts) Len() int      { return len(s) }
func (s sortablePorts) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s sortablePorts) Less(i, j int) bool {
	return s[i].ContainerPort < s[j].ContainerPort
}

// portName returns a unique key for the given port and protocol which can be
// used as a service port name.
func portName(port int, protocol corev1.Protocol) string {
	if protocol == "" {
		protocol = corev1.ProtocolTCP
	}
	return strings.ToLower(fmt.Sprintf("%d-%s", port, protocol))
}

func GenerateService(meta metav1.ObjectMeta, selector map[string]string) *corev1.Service {
	name, generateName := makeValidServiceName(meta.Name)
	svc := &corev1.Service{
		// this is ok because we know exactly how we want to be serialized
		TypeMeta: metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String(), Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:         name,
			GenerateName: generateName,
			Labels:       meta.Labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
		},
	}
	return svc
}

// AllContainerPorts creates a sorted list of all ports in all provided containers.
func AllContainerPorts(containers ...corev1.Container) []corev1.ContainerPort {
	var ports []corev1.ContainerPort
	for _, container := range containers {
		ports = append(ports, container.Ports...)
	}
	sort.Sort(sortablePorts(ports))
	return ports
}

// UniqueContainerToServicePorts creates one service port for each unique container port.
func UniqueContainerToServicePorts(ports []corev1.ContainerPort) []corev1.ServicePort {
	var result []corev1.ServicePort
	svcPorts := map[string]struct{}{}
	for _, p := range ports {
		name := portName(int(p.ContainerPort), p.Protocol)
		_, exists := svcPorts[name]
		if exists {
			continue
		}
		svcPorts[name] = struct{}{}
		result = append(result, corev1.ServicePort{
			Name:       name,
			Port:       p.ContainerPort,
			Protocol:   p.Protocol,
			TargetPort: intstr.FromInt(int(p.ContainerPort)),
		})
	}
	return result
}

// AddServices sets up services for the provided objects.
func AddServices(objects Objects, firstPortOnly bool) Objects {
	svcs := []runtime.Object{}
	for _, o := range objects {
		switch t := o.(type) {
		case *appsv1.DeploymentConfig:
			svc := addService(t.Spec.Template.Spec.Containers, t.ObjectMeta, t.Spec.Selector, firstPortOnly)
			if svc != nil {
				svcs = append(svcs, svc)
			}
		case *kappsv1.DaemonSet:
			svc := addService(t.Spec.Template.Spec.Containers, t.ObjectMeta, t.Spec.Template.Labels, firstPortOnly)
			if svc != nil {
				svcs = append(svcs, svc)
			}
		case *extensionsv1beta1.DaemonSet:
			svc := addService(t.Spec.Template.Spec.Containers, t.ObjectMeta, t.Spec.Template.Labels, firstPortOnly)
			if svc != nil {
				svcs = append(svcs, svc)
			}
		case *kappsv1beta2.DaemonSet:
			svc := addService(t.Spec.Template.Spec.Containers, t.ObjectMeta, t.Spec.Template.Labels, firstPortOnly)
			if svc != nil {
				svcs = append(svcs, svc)
			}
		}
	}
	return append(objects, svcs...)
}

// addServiceInternal utility used by AddServices to create services for multiple types.
func addService(containers []corev1.Container, objectMeta metav1.ObjectMeta, selector map[string]string, firstPortOnly bool) *corev1.Service {
	ports := UniqueContainerToServicePorts(AllContainerPorts(containers...))
	if len(ports) == 0 {
		return nil
	}
	if firstPortOnly {
		ports = ports[:1]
	}
	svc := GenerateService(objectMeta, selector)
	svc.Spec.Ports = ports
	return svc
}

type acceptNew struct{}

// AcceptNew only accepts runtime.Objects with an empty resource version.
var AcceptNew Acceptor = acceptNew{}

// Accept accepts any kind of object.
func (acceptNew) Accept(from interface{}) bool {
	_, meta, err := objectMetaData(from)
	if err != nil {
		return false
	}
	if len(meta.GetResourceVersion()) > 0 {
		return false
	}
	return true
}

type acceptUnique struct {
	typer   runtime.ObjectTyper
	objects map[string]struct{}
}

// Accept accepts any kind of object it hasn't accepted before.
func (a *acceptUnique) Accept(from interface{}) bool {
	obj, meta, err := objectMetaData(from)
	if err != nil {
		return false
	}
	gvk, _, err := a.typer.ObjectKinds(obj)
	if err != nil {
		return false
	}
	key := fmt.Sprintf("%s/%s/%s", gvk[0].Kind, meta.GetNamespace(), meta.GetName())
	_, exists := a.objects[key]
	if exists {
		return false
	}
	a.objects[key] = struct{}{}
	return true
}

// NewAcceptUnique creates an acceptor that only accepts unique objects by kind
// and name.
func NewAcceptUnique() Acceptor {
	return &acceptUnique{
		typer:   scheme.Scheme,
		objects: map[string]struct{}{},
	}
}

type acceptNonExistentImageStream struct {
	typer     runtime.ObjectTyper
	getter    imagev1typedclient.ImageV1Interface
	namespace string
}

// Accept accepts any non-ImageStream object or an ImageStream that does
// not exist in the api server
func (a *acceptNonExistentImageStream) Accept(from interface{}) bool {
	obj, _, err := objectMetaData(from)
	if err != nil {
		return false
	}
	gvk, _, err := a.typer.ObjectKinds(obj)
	if err != nil {
		return false
	}
	gk := gvk[0].GroupKind()
	if !(image.Kind("ImageStream") == gk || legacy.Kind("ImageStream") == gk) {
		return true
	}
	is, ok := from.(*imagev1.ImageStream)
	if !ok {
		klog.V(4).Infof("type cast to image stream %#v not right for an unanticipated reason", from)
		return true
	}
	namespace := a.namespace
	if len(is.Namespace) > 0 {
		namespace = is.Namespace
	}
	imgstrm, err := a.getter.ImageStreams(namespace).Get(context.TODO(), is.Name, metav1.GetOptions{})
	if err == nil && imgstrm != nil {
		klog.V(4).Infof("acceptor determined that imagestream %s in namespace %s exists so don't accept: %#v", is.Name, namespace, imgstrm)
		return false
	}
	return true
}

// NewAcceptNonExistentImageStream creates an acceptor that accepts an object
// if it is either a) not an ImageStream, or b) an ImageStream which does not
// yet exist in the api server
func NewAcceptNonExistentImageStream(typer runtime.ObjectTyper, getter imagev1typedclient.ImageV1Interface, namespace string) Acceptor {
	return &acceptNonExistentImageStream{
		typer:     typer,
		getter:    getter,
		namespace: namespace,
	}
}

type acceptNonExistentImageStreamTag struct {
	typer     runtime.ObjectTyper
	getter    imagev1typedclient.ImageV1Interface
	namespace string
}

// Accept accepts any non-ImageStreamTag object or an ImageStreamTag that does
// not exist in the api server
func (a *acceptNonExistentImageStreamTag) Accept(from interface{}) bool {
	obj, _, err := objectMetaData(from)
	if err != nil {
		return false
	}
	gvk, _, err := a.typer.ObjectKinds(obj)
	if err != nil {
		return false
	}
	gk := gvk[0].GroupKind()
	if !(image.Kind("ImageStreamTag") == gk || legacy.Kind("ImageStreamTag") == gk) {
		return true
	}
	ist, ok := from.(*imagev1.ImageStreamTag)
	if !ok {
		klog.V(4).Infof("type cast to imagestreamtag %#v not right for an unanticipated reason", from)
		return true
	}
	namespace := a.namespace
	if len(ist.Namespace) > 0 {
		namespace = ist.Namespace
	}
	tag, err := a.getter.ImageStreamTags(namespace).Get(context.TODO(), ist.Name, metav1.GetOptions{})
	if err == nil && tag != nil {
		klog.V(4).Infof("acceptor determined that imagestreamtag %s in namespace %s exists so don't accept: %#v", ist.Name, namespace, tag)
		return false
	}
	return true
}

// NewAcceptNonExistentImageStreamTag creates an acceptor that accepts an object
// if it is either a) not an ImageStreamTag, or b) an ImageStreamTag which does not
// yet exist in the api server
func NewAcceptNonExistentImageStreamTag(typer runtime.ObjectTyper, getter imagev1typedclient.ImageV1Interface, namespace string) Acceptor {
	return &acceptNonExistentImageStreamTag{
		typer:     typer,
		getter:    getter,
		namespace: namespace,
	}
}

func objectMetaData(raw interface{}) (runtime.Object, metav1.Object, error) {
	obj, ok := raw.(runtime.Object)
	if !ok {
		return nil, nil, fmt.Errorf("%#v is not a runtime.Object", raw)
	}
	meta, err := meta.Accessor(obj)
	if err != nil {
		return nil, nil, err
	}
	return obj, meta, nil
}

type acceptBuildConfigs struct {
	typer runtime.ObjectTyper
}

// Accept accepts BuildConfigs and ImageStreams.
func (a *acceptBuildConfigs) Accept(from interface{}) bool {
	obj, _, err := objectMetaData(from)
	if err != nil {
		return false
	}
	gvk, _, err := a.typer.ObjectKinds(obj)
	if err != nil {
		return false
	}
	gk := gvk[0].GroupKind()
	return build.Kind("BuildConfig") == gk || image.Kind("ImageStream") == gk
}

// NewAcceptBuildConfigs creates an acceptor accepting BuildConfig objects
// and ImageStreams objects.
func NewAcceptBuildConfigs(typer runtime.ObjectTyper) Acceptor {
	return &acceptBuildConfigs{
		typer: typer,
	}
}

// Acceptors is a list of acceptors that behave like a single acceptor.
// All acceptors must accept an object for it to be accepted.
type Acceptors []Acceptor

// Accept iterates through all acceptors and determines whether the object
// should be accepted.
func (aa Acceptors) Accept(from interface{}) bool {
	for _, a := range aa {
		if !a.Accept(from) {
			return false
		}
	}
	return true
}

type acceptAll struct{}

// AcceptAll accepts all objects.
var AcceptAll Acceptor = acceptAll{}

// Accept accepts everything.
func (acceptAll) Accept(_ interface{}) bool {
	return true
}

// Objects is a set of runtime objects.
type Objects []runtime.Object

// Acceptor is an interface for accepting objects.
type Acceptor interface {
	Accept(from interface{}) bool
}

type acceptFirst struct {
	handled map[interface{}]struct{}
}

// NewAcceptFirst returns a new Acceptor.
func NewAcceptFirst() Acceptor {
	return &acceptFirst{make(map[interface{}]struct{})}
}

// Accept accepts any object it hasn't accepted before.
func (s *acceptFirst) Accept(from interface{}) bool {
	if _, ok := s.handled[from]; ok {
		return false
	}
	s.handled[from] = struct{}{}
	return true
}
