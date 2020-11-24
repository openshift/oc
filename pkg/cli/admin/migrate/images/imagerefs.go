package images

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	"k8s.io/kubectl/pkg/util/templates"

	buildv1 "github.com/openshift/api/build/v1"
	imagev1 "github.com/openshift/api/image/v1"
	imagev1typedclient "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	"github.com/openshift/library-go/pkg/image/reference"
	imageref "github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/oc/pkg/cli/admin/migrate"
	"github.com/openshift/oc/pkg/helpers/image/credentialprovider"
)

var (
	internalMigrateImagesLong = templates.LongDesc(`
		Migrate references to Docker images

		This command updates embedded Docker image references on the server in place. By default it
		will update image streams and images, and may be used to update resources with a pod template
		(deployments, replication controllers, daemon sets).

		References are changed by providing a mapping between a source registry and name and the
		desired registry and name. Either name or registry can be set to '*' to change all values.
		The registry value "docker.io" is special and will handle any image reference that refers to
		the DockerHub. You may pass multiple mappings - the first matching mapping will be applied
		per resource.

		The following resource types may be migrated by this command:

		* buildconfigs
		* daemonsets
		* deploymentconfigs
		* images
		* imagestreams
		* jobs
		* pods
		* replicationcontrollers
		* secrets (docker)

		Only images, imagestreams, and secrets are updated by default. Updating images and image
		streams requires administrative privileges.
	`)

	internalMigrateImagesExample = templates.Examples(`
		# Perform a dry-run of migrating all "docker.io" references to "myregistry.com"
		oc adm migrate image-references docker.io/*=myregistry.com/*

		# To actually perform the migration, the confirm flag must be appended
		oc adm migrate image-references docker.io/*=myregistry.com/* --confirm

		# To see more details of what will be migrated, use the loglevel and output flags
		oc adm migrate image-references docker.io/*=myregistry.com/* --loglevel=2 -o yaml

		# Migrate from a service IP to an internal service DNS name
		oc adm migrate image-references 172.30.1.54/*=registry.openshift.svc.cluster.local/*

		# Migrate from a service IP to an internal service DNS name for all deployment configs and builds
		oc adm migrate image-references 172.30.1.54/*=registry.openshift.svc.cluster.local/* --include=buildconfigs,deploymentconfigs
	`)
)

type MigrateImageReferenceOptions struct {
	migrate.ResourceOptions

	Client          imagev1typedclient.ImageStreamsGetter
	Mappings        ImageReferenceMappings
	UpdatePodSpecFn polymorphichelpers.UpdatePodSpecForObjectFunc
}

func NewMigrateImageReferenceOptions(streams genericclioptions.IOStreams) *MigrateImageReferenceOptions {
	return &MigrateImageReferenceOptions{
		ResourceOptions: *migrate.NewResourceOptions(streams).WithIncludes([]string{"imagestream", "image", "secrets"}),
	}
}

// NewCmdMigrateImageReferences implements a MigrateImages command
func NewCmdMigrateImageReferences(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewMigrateImageReferenceOptions(streams)
	cmd := &cobra.Command{
		Use:        "image-references REGISTRY/NAME=REGISTRY/NAME [...]",
		Short:      "Update embedded Docker image references",
		Long:       internalMigrateImagesLong,
		Example:    internalMigrateImagesExample,
		Deprecated: "migration of content is managed automatically in OpenShift 4.x",
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}

	o.ResourceOptions.Bind(cmd)

	return cmd
}

func (o *MigrateImageReferenceOptions) Complete(f kcmdutil.Factory, c *cobra.Command, args []string) error {
	var remainingArgs []string
	for _, s := range args {
		if !strings.Contains(s, "=") {
			remainingArgs = append(remainingArgs, s)
			continue
		}
		mapping, err := ParseMapping(s)
		if err != nil {
			return err
		}
		o.Mappings = append(o.Mappings, mapping)
	}

	o.UpdatePodSpecFn = polymorphichelpers.UpdatePodSpecForObjectFn

	if len(remainingArgs) > 0 {
		return fmt.Errorf("all arguments must be valid FROM=TO mappings")
	}

	o.ResourceOptions.SaveFn = o.save
	if err := o.ResourceOptions.Complete(f, c); err != nil {
		return err
	}

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.Client, err = imagev1typedclient.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	return nil
}

func (o MigrateImageReferenceOptions) Validate() error {
	if len(o.Mappings) == 0 {
		return fmt.Errorf("at least one mapping argument must be specified: REGISTRY/NAME=REGISTRY/NAME")
	}
	return o.ResourceOptions.Validate()
}

func (o MigrateImageReferenceOptions) Run() error {
	return o.ResourceOptions.Visitor().Visit(func(info *resource.Info) (migrate.Reporter, error) {
		return o.transform(info.Object)
	})
}

// save invokes the API to alter an object. The reporter passed to this method is the same returned by
// the migration visitor method (for this type, transformImageReferences). It should return an error
// if the input type cannot be saved. It returns migrate.ErrRecalculate if migration should be re-run
// on the provided object.
func (o *MigrateImageReferenceOptions) save(info *resource.Info, reporter migrate.Reporter) error {
	switch t := info.Object.(type) {
	case *imagev1.ImageStream:
		// update status first so that a subsequent spec update won't pull incorrect values
		if reporter.(imageChangeInfo).status {
			updated, err := o.Client.ImageStreams(t.Namespace).UpdateStatus(context.TODO(), t, metav1.UpdateOptions{})
			if err != nil {
				return migrate.DefaultRetriable(info, err)
			}
			info.Refresh(updated, true)
			return migrate.ErrRecalculate
		}
		if reporter.(imageChangeInfo).spec {
			updated, err := o.Client.ImageStreams(t.Namespace).Update(context.TODO(), t, metav1.UpdateOptions{})
			if err != nil {
				return migrate.DefaultRetriable(info, err)
			}
			info.Refresh(updated, true)
		}
		return nil
	default:
		if _, err := resource.NewHelper(info.Client, info.Mapping).Replace(info.Namespace, info.Name, false, info.Object); err != nil {
			return migrate.DefaultRetriable(info, err)
		}
	}
	return nil
}

// transform checks image references on the provided object and returns either a reporter (indicating
// that the object was recognized and whether it was updated) or an error.
func (o *MigrateImageReferenceOptions) transform(obj runtime.Object) (migrate.Reporter, error) {
	fn := o.Mappings.MapReference
	switch t := obj.(type) {
	case *imagev1.Image:
		var changed bool
		if updated := fn(t.DockerImageReference); updated != t.DockerImageReference {
			changed = true
			t.DockerImageReference = updated
		}
		return migrate.ReporterBool(changed), nil
	case *imagev1.ImageStream:
		var info imageChangeInfo
		if len(t.Spec.DockerImageRepository) > 0 {
			info.spec = updateString(&t.Spec.DockerImageRepository, fn)
		}
		for _, ref := range t.Spec.Tags {
			if ref.From == nil || ref.From.Kind != "DockerImage" {
				continue
			}
			info.spec = updateString(&ref.From.Name, fn) || info.spec
		}
		for _, events := range t.Status.Tags {
			for i := range events.Items {
				info.status = updateString(&events.Items[i].DockerImageReference, fn) || info.status
			}
		}
		return info, nil
	case *corev1.Secret:
		switch t.Type {
		case corev1.SecretTypeDockercfg:
			var v credentialprovider.DockerConfig
			if err := json.Unmarshal(t.Data[corev1.DockerConfigKey], &v); err != nil {
				return nil, err
			}
			if !updateDockerConfig(v, o.Mappings.MapDockerAuthKey) {
				return migrate.ReporterBool(false), nil
			}
			data, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			t.Data[corev1.DockerConfigKey] = data
			return migrate.ReporterBool(true), nil
		case corev1.SecretTypeDockerConfigJson:
			var v credentialprovider.DockerConfigJSON
			if err := json.Unmarshal(t.Data[corev1.DockerConfigJsonKey], &v); err != nil {
				return nil, err
			}
			if !updateDockerConfig(v.Auths, o.Mappings.MapDockerAuthKey) {
				return migrate.ReporterBool(false), nil
			}
			data, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			t.Data[corev1.DockerConfigJsonKey] = data
			return migrate.ReporterBool(true), nil
		default:
			return migrate.ReporterBool(false), nil
		}
	case *buildv1.BuildConfig:
		var changed bool
		if to := t.Spec.Output.To; to != nil && to.Kind == "DockerImage" {
			changed = updateString(&to.Name, fn) || changed
		}
		for i, image := range t.Spec.Source.Images {
			if image.From.Kind == "DockerImage" {
				changed = updateString(&t.Spec.Source.Images[i].From.Name, fn) || changed
			}
		}
		if c := t.Spec.Strategy.CustomStrategy; c != nil && c.From.Kind == "DockerImage" {
			changed = updateString(&c.From.Name, fn) || changed
		}
		if c := t.Spec.Strategy.DockerStrategy; c != nil && c.From != nil && c.From.Kind == "DockerImage" {
			changed = updateString(&c.From.Name, fn) || changed
		}
		if c := t.Spec.Strategy.SourceStrategy; c != nil && c.From.Kind == "DockerImage" {
			changed = updateString(&c.From.Name, fn) || changed
		}
		return migrate.ReporterBool(changed), nil
	default:
		if o.UpdatePodSpecFn != nil {
			var changed bool
			supports, err := o.UpdatePodSpecFn(obj, func(spec *corev1.PodSpec) error {
				changed = updatePodSpec(spec, fn)
				return nil
			})
			if !supports {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			return migrate.ReporterBool(changed), nil
		}
	}
	// TODO: implement use of the generic PodTemplate accessor from the factory to handle
	// any object with a pod template
	return nil, nil
}

// imageChangeInfo indicates whether the spec or status of an image stream was changed.
type imageChangeInfo struct {
	spec, status bool
}

func (i imageChangeInfo) Changed() bool {
	return i.spec || i.status
}

type TransformImageFunc func(in string) string

func updateString(value *string, fn TransformImageFunc) bool {
	result := fn(*value)
	if result != *value {
		*value = result
		return true
	}
	return false
}

func updatePodSpec(spec *corev1.PodSpec, fn TransformImageFunc) bool {
	var changed bool
	for i := range spec.Containers {
		changed = updateString(&spec.Containers[i].Image, fn) || changed
	}
	return changed
}

func updateDockerConfig(cfg credentialprovider.DockerConfig, fn TransformImageFunc) bool {
	var changed bool
	for k, v := range cfg {
		original := k
		if updateString(&k, fn) {
			changed = true
			delete(cfg, original)
			cfg[k] = v
		}
	}
	return changed
}

// ImageReferenceMapping represents a transformation of an image reference.
type ImageReferenceMapping struct {
	FromRegistry string
	FromName     string
	ToRegistry   string
	ToName       string
}

// ParseMapping converts a string in the form "(REGISTRY|*)/(NAME|*)" to an ImageReferenceMapping
// or returns a user-facing error. REGISTRY is the image registry value (hostname) or "docker.io".
// NAME is the full repository name (the path relative to the registry root).
// TODO: handle v2 repository names, which can have multiple segments (must fix
//   ParseDockerImageReference)
func ParseMapping(s string) (ImageReferenceMapping, error) {
	parts := strings.SplitN(s, "=", 2)
	from := strings.SplitN(parts[0], "/", 2)
	to := strings.SplitN(parts[1], "/", 2)
	if len(from) < 2 || len(to) < 2 {
		return ImageReferenceMapping{}, fmt.Errorf("all arguments must be of the form REGISTRY/NAME=REGISTRY/NAME, where registry or name may be '*' or a value")
	}
	if len(from[0]) == 0 {
		return ImageReferenceMapping{}, fmt.Errorf("%q is not a valid source: registry must be specified (may be '*')", parts[0])
	}
	if len(from[1]) == 0 {
		return ImageReferenceMapping{}, fmt.Errorf("%q is not a valid source: name must be specified (may be '*')", parts[0])
	}
	if len(to[0]) == 0 {
		return ImageReferenceMapping{}, fmt.Errorf("%q is not a valid target: registry must be specified (may be '*')", parts[1])
	}
	if len(to[1]) == 0 {
		return ImageReferenceMapping{}, fmt.Errorf("%q is not a valid target: name must be specified (may be '*')", parts[1])
	}
	if from[0] == "*" {
		from[0] = ""
	}
	if from[1] == "*" {
		from[1] = ""
	}
	if to[0] == "*" {
		to[0] = ""
	}
	if to[1] == "*" {
		to[1] = ""
	}
	if to[0] == "" && to[1] == "" {
		return ImageReferenceMapping{}, fmt.Errorf("%q is not a valid target: at least one change must be specified", parts[1])
	}
	if from[0] == to[0] && from[1] == to[1] {
		return ImageReferenceMapping{}, fmt.Errorf("%q is not valid: must target at least one field to change", s)
	}
	return ImageReferenceMapping{
		FromRegistry: from[0],
		FromName:     from[1],
		ToRegistry:   to[0],
		ToName:       to[1],
	}, nil
}

// ImageReferenceMappings provide a convenience method for transforming an input reference
type ImageReferenceMappings []ImageReferenceMapping

// MapReference transforms the provided Docker image reference if any mapping matches the
// input. If the reference cannot be parsed, it will not be modified.
func (m ImageReferenceMappings) MapReference(in string) string {
	ref, err := reference.Parse(in)
	if err != nil {
		return in
	}
	registry := ref.DockerClientDefaults().Registry
	name := ref.RepositoryName()
	for _, mapping := range m {
		if len(mapping.FromRegistry) > 0 && mapping.FromRegistry != registry {
			continue
		}
		if len(mapping.FromName) > 0 && mapping.FromName != name {
			continue
		}
		if len(mapping.ToRegistry) > 0 {
			ref.Registry = mapping.ToRegistry
		}
		if len(mapping.ToName) > 0 {
			ref.Namespace = ""
			ref.Name = mapping.ToName
		}
		return ref.Exact()
	}
	return in
}

// MapDockerAuthKey transforms the provided Docker Config host key if any mapping matches
// the input. If the reference cannot be parsed, it will not be modified.
func (m ImageReferenceMappings) MapDockerAuthKey(in string) string {
	value := in
	if len(value) == 0 {
		value = imageref.DockerDefaultV1Registry
	}
	if !strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "http://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return in
	}
	// The docker client allows exact matches:
	//    foo.bar.com/namespace
	// Or hostname matches:
	//    foo.bar.com
	// It also considers /v2/  and /v1/ equivalent to the hostname
	// See ResolveAuthConfig in docker/registry/auth.go.
	registry := parsed.Host
	name := parsed.Path
	switch {
	case name == "/":
		name = ""
	case strings.HasPrefix(name, "/v2/") || strings.HasPrefix(name, "/v1/"):
		name = name[4:]
	case strings.HasPrefix(name, "/"):
		name = name[1:]
	}
	for _, mapping := range m {
		if len(mapping.FromRegistry) > 0 && mapping.FromRegistry != registry {
			continue
		}
		if len(mapping.FromName) > 0 && mapping.FromName != name {
			continue
		}
		if len(mapping.ToRegistry) > 0 {
			registry = mapping.ToRegistry
		}
		if len(mapping.ToName) > 0 {
			name = mapping.ToName
		}
		if len(name) > 0 {
			return registry + "/" + name
		}
		return registry
	}
	return in
}
