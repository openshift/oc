package imageprune

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/docker/distribution/manifest/schema2"
	"github.com/docker/distribution/registry/api/errcode"
	imagespecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"k8s.io/klog/v2"

	kappsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerrapi "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/retry"

	appsv1 "github.com/openshift/api/apps/v1"
	buildv1 "github.com/openshift/api/build/v1"
	dockerv10 "github.com/openshift/api/image/docker10"
	imagev1 "github.com/openshift/api/image/v1"
	imagev1client "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	"github.com/openshift/library-go/pkg/build/buildutil"
	"github.com/openshift/library-go/pkg/image/imageutil"
	"github.com/openshift/library-go/pkg/image/reference"
)

const defaultPruneImageWorkerCount = 5

type imageStreamTagReference struct {
	Namespace string
	Name      string
	Tag       string
}

func (r imageStreamTagReference) String() string {
	return fmt.Sprintf("%s/%s:%s", r.Namespace, r.Name, r.Tag)
}

type imageStreamImageReference struct {
	Namespace string
	Name      string
	Digest    string
}

func (r imageStreamImageReference) String() string {
	return fmt.Sprintf("%s/%s@%s", r.Namespace, r.Name, r.Digest)
}

type resourceReference struct {
	Resource  string
	Namespace string
	Name      string
}

func (r resourceReference) String() string {
	if r.Namespace == "" {
		return fmt.Sprintf("%s/%s", r.Resource, r.Name)
	}
	return fmt.Sprintf("%s/%s namespace=%s", r.Resource, r.Name, r.Namespace)
}

func referencesSample(refs []resourceReference) string {
	if len(refs) == 0 {
		return ""
	}

	result := ""
	suffix := ""
	limit := len(refs)
	if limit >= 5 {
		limit = 3
		suffix = fmt.Sprintf(", and %d more", len(refs)-limit)
	}
	result += refs[0].String()
	for i := 1; i < limit; i++ {
		result += ", "
		result += refs[i].String()
	}
	return result + suffix
}

type PruneStats struct {
	mutex                      sync.Mutex
	DeletedImages              int
	DeletedImageStreamTagItems int
	UpdatedImageStreams        int
	DeletedLayerLinks          int
	DeletedManifestLinks       int
	DeletedBlobs               int
}

func (s *PruneStats) Copy() *PruneStats {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return &PruneStats{
		DeletedImages:              s.DeletedImages,
		DeletedImageStreamTagItems: s.DeletedImageStreamTagItems,
		UpdatedImageStreams:        s.UpdatedImageStreams,
		DeletedLayerLinks:          s.DeletedLayerLinks,
		DeletedManifestLinks:       s.DeletedManifestLinks,
		DeletedBlobs:               s.DeletedBlobs,
	}
}

func (s *PruneStats) Add(other *PruneStats) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	other = other.Copy() // make a local copy to avoid races

	s.DeletedImages += other.DeletedImages
	s.DeletedImageStreamTagItems += other.DeletedImageStreamTagItems
	s.UpdatedImageStreams += other.UpdatedImageStreams
	s.DeletedLayerLinks += other.DeletedLayerLinks
	s.DeletedManifestLinks += other.DeletedManifestLinks
	s.DeletedBlobs += other.DeletedBlobs
}

func (s *PruneStats) String() string {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var parts []string
	if s.DeletedImages != 0 {
		parts = append(parts, fmt.Sprintf("deleted %d image object(s)", s.DeletedImages))
	}
	if s.DeletedImageStreamTagItems != 0 {
		parts = append(parts, fmt.Sprintf("deleted %d image stream tag item(s)", s.DeletedImageStreamTagItems))
	}
	if s.UpdatedImageStreams != 0 {
		parts = append(parts, fmt.Sprintf("updated %d image stream(s)", s.UpdatedImageStreams))
	}
	if s.DeletedLayerLinks != 0 {
		parts = append(parts, fmt.Sprintf("deleted %d layer link(s)", s.DeletedLayerLinks))
	}
	if s.DeletedManifestLinks != 0 {
		parts = append(parts, fmt.Sprintf("deleted %d manifest link(s)", s.DeletedManifestLinks))
	}
	if s.DeletedBlobs != 0 {
		parts = append(parts, fmt.Sprintf("deleted %d blob(s)", s.DeletedBlobs))
	}
	if len(parts) == 0 {
		return "deleted 0 objects"
	}
	return strings.Join(parts, ", ")
}

// pruneAlgorithm contains the various settings to use when evaluating images
// and layers for pruning.
type pruneAlgorithm struct {
	keepYoungerThan    time.Time
	keepTagRevisions   int
	pruneOverSizeLimit bool
	namespace          string
	allImages          bool
	pruneRegistry      bool
}

// ImageDeleter knows how to remove images from OpenShift.
type ImageDeleter interface {
	// DeleteImage removes the image from OpenShift's storage.
	DeleteImage(image *imagev1.Image) error
}

// ImageStreamDeleter knows how to remove an image reference from an image stream.
type ImageStreamDeleter interface {
	// GetImageStream returns a fresh copy of an image stream.
	GetImageStream(stream *imagev1.ImageStream) (*imagev1.ImageStream, error)
	// UpdateImageStream updates the image stream's status. The updated image
	// stream is returned.
	UpdateImageStream(stream *imagev1.ImageStream, deletedItems int) (*imagev1.ImageStream, error)
}

// BlobDeleter knows how to delete a blob from the container image registry.
type BlobDeleter interface {
	// DeleteBlob uses registryClient to ask the registry at registryURL
	// to remove the blob.
	DeleteBlob(blob string) error
}

// LayerLinkDeleter knows how to delete a repository layer link from the container image registry.
type LayerLinkDeleter interface {
	// DeleteLayerLink uses registryClient to ask the registry at registryURL to
	// delete the repository layer link.
	DeleteLayerLink(repo, linkName string) error
}

// ManifestDeleter knows how to delete image manifest data for a repository from
// the container image registry.
type ManifestDeleter interface {
	// DeleteManifest uses registryClient to ask the registry at registryURL to
	// delete the repository's image manifest data.
	DeleteManifest(repo, manifest string) error
}

// PrunerOptions contains the fields used to initialize a new Pruner.
type PrunerOptions struct {
	// KeepYoungerThan indicates the minimum age an Image must be to be a
	// candidate for pruning.
	KeepYoungerThan *time.Duration
	// KeepTagRevisions is the minimum number of tag revisions to preserve;
	// revisions older than this value are candidates for pruning.
	KeepTagRevisions *int
	// PruneOverSizeLimit indicates that images exceeding defined limits (openshift.io/Image)
	// will be considered as candidates for pruning.
	PruneOverSizeLimit *bool
	// AllImages considers all images for pruning, not just those pushed directly to the registry.
	AllImages *bool
	// PruneRegistry controls whether to both prune the API Objects in etcd and corresponding
	// data in the registry, or just prune the API Object and defer on the corresponding data in
	// the registry
	PruneRegistry *bool
	// Namespace to be pruned, if specified it should never remove Images.
	Namespace string
	// Images is the entire list of images in OpenShift indexed by their name.
	// An image must be in this list to be a candidate for pruning.
	Images map[string]*imagev1.Image
	// Streams is the entire list of image streams across all namespaces in the
	// cluster indexed by "namespace/name" strings.
	Streams map[string]*imagev1.ImageStream
	// Pods is the entire list of pods across all namespaces in the cluster.
	Pods *corev1.PodList
	// RCs is the entire list of replication controllers across all namespaces in
	// the cluster.
	RCs *corev1.ReplicationControllerList
	// BCs is the entire list of build configs across all namespaces in the
	// cluster.
	BCs *buildv1.BuildConfigList
	// Builds is the entire list of builds across all namespaces in the cluster.
	Builds *buildv1.BuildList
	// DSs is the entire list of daemon sets across all namespaces in the cluster.
	DSs *kappsv1.DaemonSetList
	// Deployments is the entire list of kube's deployments across all namespaces in the cluster.
	Deployments *kappsv1.DeploymentList
	// DCs is the entire list of deployment configs across all namespaces in the cluster.
	DCs *appsv1.DeploymentConfigList
	// RSs is the entire list of replica sets across all namespaces in the cluster.
	RSs *kappsv1.ReplicaSetList
	// SSs is the entire list of statefulsets across all namespaces in the cluster.
	SSs *kappsv1.StatefulSetList
	// LimitRanges is a map of LimitRanges across namespaces, being keys in this map.
	LimitRanges map[string][]*corev1.LimitRange
	// DryRun indicates that no changes will be made to the cluster and nothing
	// will be removed.
	DryRun bool
	// IgnoreInvalidRefs indicates that all invalid references should be ignored.
	IgnoreInvalidRefs bool
	// NumWorkers is a desired number of workers concurrently handling image prune jobs. If less than 1, the
	// default number of workers will be spawned.
	NumWorkers int
}

// Pruner knows how to prune istags, images, manifest, layers, image configs and blobs.
type Pruner interface {
	// Prune uses deleters to remove images that have been identified as
	// candidates for pruning based on the Pruner's internal pruning algorithm.
	// Please see NewPruner for details on the algorithm.
	Prune(
		imageStreamDeleter ImageStreamDeleter,
		layerLinkDeleter LayerLinkDeleter,
		manifestDeleter ManifestDeleter,
		blobDeleter BlobDeleter,
		imageDeleter ImageDeleter,
	) (*PruneStats, kerrors.Aggregate)
}

// pruner is an object that knows how to prune a data set
type pruner struct {
	usedTags   map[imageStreamTagReference][]resourceReference
	usedImages map[imageStreamImageReference][]resourceReference

	images       map[string]*imagev1.Image
	imageStreams map[string]*imagev1.ImageStream

	algorithm         pruneAlgorithm
	ignoreInvalidRefs bool
	imageStreamLimits map[string][]*corev1.LimitRange
	numWorkers        int
}

var _ Pruner = &pruner{}

// NewPruner creates a Pruner.
//
// Images younger than keepYoungerThan and images referenced by image streams
// and/or pods younger than keepYoungerThan are preserved. All other images are
// candidates for pruning. For example, if keepYoungerThan is 60m, and an
// ImageStream is only 59 minutes old, none of the images it references are
// eligible for pruning.
//
// keepTagRevisions is the number of revisions per tag in an image stream's
// status.tags that are preserved and ineligible for pruning. Any revision older
// than keepTagRevisions is eligible for pruning.
//
// pruneOverSizeLimit is a boolean flag speyfing that all images exceeding limits
// defined in their namespace will be considered for pruning. Important to note is
// the fact that this flag does not work in any combination with the keep* flags.
//
// images, streams, pods, rcs, bcs, builds, daemonsets and dcs are the resources used to run
// the pruning algorithm. These should be the full list for each type from the
// cluster; otherwise, the pruning algorithm might result in incorrect
// calculations and premature pruning.
//
// The ImageDeleter performs the following logic:
//
// remove any image that was created at least *n* minutes ago and is *not*
// currently referenced by:
//
// - any pod created less than *n* minutes ago
// - any image stream created less than *n* minutes ago
// - any running pods
// - any pending pods
// - any replication controllers
// - any daemonsets
// - any kube deployments
// - any deployment configs
// - any replica sets
// - any build configs
// - any builds
// - the n most recent tag revisions in an image stream's status.tags
//
// including only images with the annotation openshift.io/image.managed=true
// unless allImages is true.
//
// When removing an image, remove all references to the image from all
// ImageStreams having a reference to the image in `status.tags`.
//
// Also automatically remove any image layer that is no longer referenced by any
// images.
func NewPruner(options PrunerOptions) (Pruner, kerrors.Aggregate) {
	klog.V(1).Infof("Creating image pruner with keepYoungerThan=%v, keepTagRevisions=%s, pruneOverSizeLimit=%s, allImages=%s",
		options.KeepYoungerThan, getValue(options.KeepTagRevisions), getValue(options.PruneOverSizeLimit), getValue(options.AllImages))

	algorithm := pruneAlgorithm{}
	if options.KeepYoungerThan != nil {
		algorithm.keepYoungerThan = metav1.Now().Add(-*options.KeepYoungerThan)
	}
	if options.KeepTagRevisions != nil {
		algorithm.keepTagRevisions = *options.KeepTagRevisions
	}
	if options.PruneOverSizeLimit != nil {
		algorithm.pruneOverSizeLimit = *options.PruneOverSizeLimit
	}
	algorithm.allImages = true
	if options.AllImages != nil {
		algorithm.allImages = *options.AllImages
	}
	algorithm.pruneRegistry = true
	if options.PruneRegistry != nil {
		algorithm.pruneRegistry = *options.PruneRegistry
	}
	algorithm.namespace = options.Namespace

	p := &pruner{
		images:            options.Images,
		imageStreams:      options.Streams,
		algorithm:         algorithm,
		ignoreInvalidRefs: options.IgnoreInvalidRefs,
		imageStreamLimits: options.LimitRanges,
		numWorkers:        options.NumWorkers,
	}

	if p.numWorkers < 1 {
		p.numWorkers = defaultPruneImageWorkerCount
	}

	for _, image := range p.images {
		if err := imageutil.ImageWithMetadata(image); err != nil {
			return nil, kerrors.NewAggregate([]error{
				fmt.Errorf("failed to read image metadata for %s: %v", image.Name, err),
			})
		}
	}

	if err := p.analyzeImageStreamsReferences(options); err != nil {
		return nil, err
	}

	return p, nil
}

func (p *pruner) analyzeImageStreamsReferences(options PrunerOptions) kerrors.Aggregate {
	p.usedTags = map[imageStreamTagReference][]resourceReference{}
	p.usedImages = map[imageStreamImageReference][]resourceReference{}

	var errs []error
	errs = append(errs, p.analyzeReferencesFromPods(options.Pods)...)
	errs = append(errs, p.analyzeReferencesFromReplicationControllers(options.RCs)...)
	errs = append(errs, p.analyzeReferencesFromDeploymentConfigs(options.DCs)...)
	errs = append(errs, p.analyzeReferencesFromReplicaSets(options.RSs)...)
	errs = append(errs, p.analyzeReferencesFromDeployments(options.Deployments)...)
	errs = append(errs, p.analyzeReferencesFromDaemonSets(options.DSs)...)
	errs = append(errs, p.analyzeReferencesFromBuilds(options.Builds)...)
	errs = append(errs, p.analyzeReferencesFromBuildConfigs(options.BCs)...)
	errs = append(errs, p.analyzeReferencesFromStatefulSets(options.SSs)...)
	return kerrors.NewAggregate(errs)
}

func (p *pruner) analyzeReferencesFromStatefulSets(statefulsets *kappsv1.StatefulSetList) []error {
	var errs []error
	for _, sset := range statefulsets.Items {
		ref := resourceReference{
			Resource:  "statefulset",
			Namespace: sset.Namespace,
			Name:      sset.Name,
		}
		klog.V(4).Infof("Examining %s", ref)
		errs = append(errs, p.analyzeReferencesFromPodSpec(ref, &sset.Spec.Template.Spec)...)
	}
	return errs
}

// analyzeImageReference analyzes which ImageStreamImage or ImageStreamTag is
// referenced by imageReference, and associates it with referrer.
func (p *pruner) analyzeImageReference(referrer resourceReference, subreferrer string, imageReference string) error {
	logPrefix := subreferrer
	if logPrefix != "" {
		logPrefix += ": "
	}

	ref, err := reference.Parse(imageReference)
	if err != nil {
		err = newErrBadReferenceTo(referrer, subreferrer, "image", imageReference, err)
		if !p.ignoreInvalidRefs {
			return err
		}
		klog.V(1).Infof("%s - skipping", err)
		return nil
	}

	if ref.Registry == "" || ref.Namespace == "" || strings.Index(ref.Name, "/") != -1 {
		klog.V(4).Infof("%s: %simage reference %s does not match hostname/namespace/name pattern - skipping", referrer, logPrefix, imageReference)
		return nil
	}

	if len(ref.ID) == 0 {
		// Attempt to dereference istag. Since we cannot be sure whether the reference refers to the
		// integrated registry or not, we ignore the host part completely. As a consequence, we may keep
		// image otherwise sentenced for a removal just because its pull spec accidentally matches one of
		// our imagestreamtags.

		// set the tag if empty
		ref = ref.DockerClientDefaults()

		istag := imageStreamTagReference{
			Namespace: ref.Namespace,
			Name:      ref.Name,
			Tag:       ref.Tag,
		}
		klog.V(4).Infof("%s: %sgot reference to ImageStreamTag %s (registry name is %s)", referrer, logPrefix, istag, ref.Registry)
		p.usedTags[istag] = append(p.usedTags[istag], referrer)
		return nil
	}

	isimage := imageStreamImageReference{
		Namespace: ref.Namespace,
		Name:      ref.Name,
		Digest:    ref.ID,
	}
	klog.V(4).Infof("%s: %sgot reference to ImageStreamImage %s (registry name is %s)", referrer, logPrefix, isimage, ref.Registry)
	p.usedImages[isimage] = append(p.usedImages[isimage], referrer)
	return nil
}

// analyzeReferencesFromPodSpec extracts information about image streams that
// are specified by referrer's pod spec's containers.
func (p *pruner) analyzeReferencesFromPodSpec(referrer resourceReference, spec *corev1.PodSpec) []error {
	var errs []error

	for _, container := range spec.InitContainers {
		if len(strings.TrimSpace(container.Image)) == 0 {
			klog.V(4).Infof("%s: init container %s: ignoring container because it has no reference to image", referrer, container.Name)
			continue
		}

		err := p.analyzeImageReference(referrer, fmt.Sprintf("init container %s", container.Name), container.Image)
		if err != nil {
			errs = append(errs, err)
		}
	}

	for _, container := range spec.Containers {
		if len(strings.TrimSpace(container.Image)) == 0 {
			klog.V(4).Infof("%s: container %s: ignoring container because it has no reference to image", referrer, container.Name)
			continue
		}

		err := p.analyzeImageReference(referrer, fmt.Sprintf("container %s", container.Name), container.Image)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// analyzeReferencesFromBuildStrategy extracts information about image streams
// that are used by referrer's build strategy.
func (p *pruner) analyzeReferencesFromBuildStrategy(referrer resourceReference, strategy buildv1.BuildStrategy) []error {
	from := buildutil.GetInputReference(strategy)
	if from == nil {
		klog.V(4).Infof("%s: unable to determine 'from' reference - skipping", referrer)
		return nil
	}

	switch from.Kind {
	case "DockerImage":
		if len(strings.TrimSpace(from.Name)) == 0 {
			klog.V(4).Infof("%s: ignoring build strategy because it has no reference to image", referrer)
			return nil
		}

		err := p.analyzeImageReference(referrer, "", from.Name)
		if err != nil {
			return []error{err}
		}

	case "ImageStreamImage":
		name, id, err := imageutil.ParseImageStreamImageName(from.Name)
		if err != nil {
			if !p.ignoreInvalidRefs {
				return []error{newErrBadReferenceTo(referrer, "", "ImageStreamImage", from.Name, err)}
			}
			klog.V(1).Infof("%s: failed to parse ImageStreamImage name %q: %v - skipping", referrer, from.Name, err)
			return nil
		}

		isimage := imageStreamImageReference{
			Namespace: from.Namespace,
			Name:      name,
			Digest:    id,
		}
		if isimage.Namespace == "" {
			isimage.Namespace = referrer.Namespace
		}
		klog.V(4).Infof("%s: got reference to ImageStreamImage %s", referrer, isimage)
		p.usedImages[isimage] = append(p.usedImages[isimage], referrer)

	case "ImageStreamTag":
		name, tag, err := imageutil.ParseImageStreamTagName(from.Name)
		if err != nil {
			if !p.ignoreInvalidRefs {
				return []error{newErrBadReferenceTo(referrer, "", "ImageStreamTag", from.Name, err)}
			}
			klog.V(1).Infof("%s: failed to parse ImageStreamTag name %q: %v", referrer, from.Name, err)
			return nil
		}

		istag := imageStreamTagReference{
			Namespace: from.Namespace,
			Name:      name,
			Tag:       tag,
		}
		if istag.Namespace == "" {
			istag.Namespace = referrer.Namespace
		}
		klog.V(4).Infof("%s: got reference to ImageStreamTag %s", referrer, istag)
		p.usedTags[istag] = append(p.usedTags[istag], referrer)

	default:
		klog.Warningf("%s: ignoring unrecognized source location %#v", referrer, from)
	}

	return nil
}

// analyzeReferencesFromPods finds references to imagestreams from pods.
func (p *pruner) analyzeReferencesFromPods(pods *corev1.PodList) []error {
	var errs []error
	for _, pod := range pods.Items {
		ref := resourceReference{
			Resource:  "pod",
			Namespace: pod.Namespace,
			Name:      pod.Name,
		}
		klog.V(4).Infof("Examining %s", ref)

		// A pod is only *excluded* from being added to the graph if its phase is not
		// pending or running. Additionally, it has to be at least as old as the minimum
		// age threshold defined by the algorithm.
		if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending {
			if pod.CreationTimestamp.Time.Before(p.algorithm.keepYoungerThan) {
				klog.V(4).Infof("%s: ignoring pod because it's not running nor pending and is too old", ref)
				continue
			}
		}

		errs = append(errs, p.analyzeReferencesFromPodSpec(ref, &pod.Spec)...)
	}
	return errs
}

// analyzeReferencesFromReplicationControllers finds references to imagestreams
// from replication controllers.
func (p *pruner) analyzeReferencesFromReplicationControllers(rcs *corev1.ReplicationControllerList) []error {
	var errs []error
	for _, rc := range rcs.Items {
		ref := resourceReference{
			Resource:  "replicationcontroller",
			Namespace: rc.Namespace,
			Name:      rc.Name,
		}
		klog.V(4).Infof("Examining %s", ref)
		errs = append(errs, p.analyzeReferencesFromPodSpec(ref, &rc.Spec.Template.Spec)...)
	}
	return errs
}

// analyzeReferencesFromDeploymentConfigs finds references to imagestreams from
// deployment configs.
func (p *pruner) analyzeReferencesFromDeploymentConfigs(dcs *appsv1.DeploymentConfigList) []error {
	var errs []error
	for _, dc := range dcs.Items {
		ref := resourceReference{
			Resource:  "deploymentconfig",
			Namespace: dc.Namespace,
			Name:      dc.Name,
		}
		klog.V(4).Infof("Examining %s", ref)
		errs = append(errs, p.analyzeReferencesFromPodSpec(ref, &dc.Spec.Template.Spec)...)
	}
	return errs
}

// analyzeReferencesFromReplicaSets finds references to imagestreams from
// replica sets.
func (p *pruner) analyzeReferencesFromReplicaSets(rss *kappsv1.ReplicaSetList) []error {
	var errs []error
	for _, rs := range rss.Items {
		ref := resourceReference{
			Resource:  "replicaset",
			Namespace: rs.Namespace,
			Name:      rs.Name,
		}
		klog.V(4).Infof("Examining %s", ref)
		errs = append(errs, p.analyzeReferencesFromPodSpec(ref, &rs.Spec.Template.Spec)...)
	}
	return errs
}

// analyzeReferencesFromDeployments finds references to imagestreams from
// deployments.
func (p *pruner) analyzeReferencesFromDeployments(deploys *kappsv1.DeploymentList) []error {
	var errs []error
	for _, deploy := range deploys.Items {
		ref := resourceReference{
			Resource:  "deployment",
			Namespace: deploy.Namespace,
			Name:      deploy.Name,
		}
		klog.V(4).Infof("Examining %s", ref)
		errs = append(errs, p.analyzeReferencesFromPodSpec(ref, &deploy.Spec.Template.Spec)...)
	}
	return errs
}

// analyzeReferencesFromDaemonSets finds references to imagestreams from daemon
// sets.
func (p *pruner) analyzeReferencesFromDaemonSets(dss *kappsv1.DaemonSetList) []error {
	var errs []error
	for _, ds := range dss.Items {
		ref := resourceReference{
			Resource:  "daemonset",
			Namespace: ds.Namespace,
			Name:      ds.Name,
		}
		klog.V(4).Infof("Examining %s", ref)
		errs = append(errs, p.analyzeReferencesFromPodSpec(ref, &ds.Spec.Template.Spec)...)
	}
	return errs
}

// analyzeReferencesFromBuilds finds references to imagestreams from builds.
func (p *pruner) analyzeReferencesFromBuilds(builds *buildv1.BuildList) []error {
	var errs []error
	for _, build := range builds.Items {
		ref := resourceReference{
			Resource:  "build",
			Namespace: build.Namespace,
			Name:      build.Name,
		}
		klog.V(4).Infof("Examining %s", ref)
		errs = append(errs, p.analyzeReferencesFromBuildStrategy(ref, build.Spec.Strategy)...)
	}
	return errs
}

// analyzeReferencesFromBuildConfigs finds references to imagestreams from
// build configs.
func (p *pruner) analyzeReferencesFromBuildConfigs(bcs *buildv1.BuildConfigList) []error {
	var errs []error
	for _, bc := range bcs.Items {
		ref := resourceReference{
			Resource:  "buildconfig",
			Namespace: bc.Namespace,
			Name:      bc.Name,
		}
		klog.V(4).Infof("Examining %s", ref)
		errs = append(errs, p.analyzeReferencesFromBuildStrategy(ref, bc.Spec.Strategy)...)
	}
	return errs
}

func getValue(option interface{}) string {
	if v := reflect.ValueOf(option); !v.IsNil() {
		return fmt.Sprintf("%v", v.Elem())
	}
	return "<nil>"
}

// exceedsLimits checks if given image exceeds LimitRanges defined in ImageStream's namespace.
func exceedsLimits(is *imagev1.ImageStream, image *imagev1.Image, limits map[string][]*corev1.LimitRange) bool {
	limitRanges, ok := limits[is.Namespace]
	if !ok || len(limitRanges) == 0 {
		return false
	}

	dockerImage, ok := image.DockerImageMetadata.Object.(*dockerv10.DockerImage)
	if !ok {
		return false
	}
	imageSize := resource.NewQuantity(dockerImage.Size, resource.BinarySI)
	for _, limitRange := range limitRanges {
		if limitRange == nil {
			continue
		}
		for _, limit := range limitRange.Spec.Limits {
			if limit.Type != imagev1.LimitTypeImage {
				continue
			}

			limitQuantity, ok := limit.Max[corev1.ResourceStorage]
			if !ok {
				continue
			}
			if limitQuantity.Cmp(*imageSize) < 0 {
				// image size is larger than the permitted limit range max size
				klog.V(4).Infof("Image %s in stream %s/%s exceeds limit %s: %v vs %v",
					image.Name, is.Namespace, is.Name, limitRange.Name, *imageSize, limitQuantity)
				return true
			}
		}
	}
	return false
}

func getImageBlobs(image *imagev1.Image) ([]string, error) {
	blobs := make([]string, 0, len(image.DockerImageLayers)+1)

	for _, layer := range image.DockerImageLayers {
		blobs = append(blobs, layer.Name)
	}

	dockerImage, ok := image.DockerImageMetadata.Object.(*dockerv10.DockerImage)
	if !ok {
		return blobs, fmt.Errorf("failed to read metadata for image %s", image.Name)
	}

	mediaTypeHasConfig := image.DockerImageManifestMediaType == schema2.MediaTypeManifest ||
		image.DockerImageManifestMediaType == imagespecv1.MediaTypeImageManifest

	if mediaTypeHasConfig && len(dockerImage.ID) > 0 {
		configName := dockerImage.ID
		blobs = append(blobs, configName)
	}

	return blobs, nil
}

type stringsCounter struct {
	mutex  sync.Mutex
	counts map[string]int
}

func newStringsCounter() *stringsCounter {
	return &stringsCounter{
		counts: make(map[string]int),
	}
}

func (c *stringsCounter) Add(key string, delta int) int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.counts[key] += delta
	return c.counts[key]
}

type referenceCounts struct {
	Blobs     *stringsCounter
	Manifests *stringsCounter
}

func newReferenceCounts() referenceCounts {
	return referenceCounts{
		Blobs:     newStringsCounter(),
		Manifests: newStringsCounter(),
	}
}

func (p *pruner) getRepositoryReferenceCounts(is *imagev1.ImageStream) (referenceCounts, error) {
	counts := newReferenceCounts()
	for _, tagEventList := range is.Status.Tags {
		for _, tagEvent := range tagEventList.Items {
			image, ok := p.images[tagEvent.Image]
			if !ok {
				klog.Warningf("imagestream %s/%s: tag %s: image %s not found", is.Namespace, is.Name, tagEventList.Tag, tagEvent.Image)
				continue
			}

			counts.Manifests.Add(image.Name, 1)

			imageBlobs, err := getImageBlobs(image)
			if err != nil {
				return counts, fmt.Errorf("tag %s: image %s: %v", tagEventList.Tag, tagEvent.Image, err)
			}
			for _, blob := range imageBlobs {
				counts.Blobs.Add(blob, 1)
			}
		}
	}
	return counts, nil
}

func (p *pruner) getGlobalReferenceCounts(images map[string]*imagev1.Image) (referenceCounts, error) {
	counts := newReferenceCounts()
	for _, image := range images {
		counts.Manifests.Add(image.Name, 1)
		counts.Blobs.Add(image.Name, 1)

		imageBlobs, err := getImageBlobs(image)
		if err != nil {
			return counts, fmt.Errorf("image %s: %v", image.Name, err)
		}
		for _, blob := range imageBlobs {
			counts.Blobs.Add(blob, 1)
		}
	}
	return counts, nil
}

func (p *pruner) pruneImageStreamTag(is *imagev1.ImageStream, tagEventList imagev1.NamedTagEventList, counts referenceCounts, layerLinkDeleter LayerLinkDeleter) (imagev1.NamedTagEventList, int, []string, []error) {
	filteredItems := tagEventList.Items[:0]
	var manifestsToDelete []string
	var errs []error
	for rev, item := range tagEventList.Items {
		if item.Created.After(p.algorithm.keepYoungerThan) {
			klog.V(4).Infof("imagestream %s/%s: tag %s: revision %d: keeping %s because of --keep-younger-than", is.Namespace, is.Name, tagEventList.Tag, rev+1, item.Image)
			filteredItems = append(filteredItems, item)
			continue
		}

		if rev == 0 {
			istag := imageStreamTagReference{
				Namespace: is.Namespace,
				Name:      is.Name,
				Tag:       tagEventList.Tag,
			}
			if usedBy := p.usedTags[istag]; len(usedBy) > 0 {
				klog.V(4).Infof("imagestream %s/%s: tag %s: revision %d: keeping %s because tag is used by %s", is.Namespace, is.Name, tagEventList.Tag, rev+1, item.Image, referencesSample(usedBy))
				filteredItems = append(filteredItems, item)
				continue
			}
		}

		image, ok := p.images[item.Image]
		if !ok {
			// There are few options why the image may not be found:
			// 1. the image is deleted manually and this record is no longer valid
			// 2. the imagestream was observed before the image creation, i.e.
			//    this record was created recently and it should be protected
			//    by keepYoungerThan
			klog.Infof("imagestream %s/%s: tag %s: revision %d: image %s not found, deleting...", is.Namespace, is.Name, tagEventList.Tag, rev+1, item.Image)
			continue
		}

		if p.algorithm.pruneOverSizeLimit {
			if !exceedsLimits(is, image, p.imageStreamLimits) {
				klog.V(4).Infof("imagestream %s/%s: tag %s: revision %d: keeping %s because --prune-over-size-limit is used and image does not exceed limits", is.Namespace, is.Name, tagEventList.Tag, rev+1, item.Image)
				filteredItems = append(filteredItems, item)
				continue
			}
		} else {
			if rev < p.algorithm.keepTagRevisions {
				klog.V(4).Infof("imagestream %s/%s: tag %s: revision %d: keeping %s because of --keep-tag-revisions", is.Namespace, is.Name, tagEventList.Tag, rev+1, item.Image)
				filteredItems = append(filteredItems, item)
				continue
			}
		}

		isimage := imageStreamImageReference{
			Namespace: is.Namespace,
			Name:      is.Name,
			Digest:    item.Image,
		}
		if usedBy := p.usedImages[isimage]; len(usedBy) > 0 {
			klog.V(4).Infof("imagestream %s/%s: tag %s: revision %d: keeping %s because image is used by %s", is.Namespace, is.Name, tagEventList.Tag, rev+1, item.Image, referencesSample(usedBy))
			filteredItems = append(filteredItems, item)
			continue
		}

		klog.V(4).Infof("imagestream %s/%s: tag %s: revision %d: deleting repository links for %s...", is.Namespace, is.Name, tagEventList.Tag, rev+1, item.Image)

		if p.algorithm.pruneRegistry {
			if counts.Manifests.Add(image.Name, -1) == 0 {
				manifestsToDelete = append(manifestsToDelete, image.Name)
			}

			imageBlobs, err := getImageBlobs(image)
			if err != nil {
				klog.Warningf("imagestream %s/%s: tag %s: image %s: %s", is.Namespace, is.Name, tagEventList.Tag, item.Image, err)
			}
			for _, blob := range imageBlobs {
				if counts.Blobs.Add(blob, -1) == 0 {
					err := layerLinkDeleter.DeleteLayerLink(fmt.Sprintf("%s/%s", is.Namespace, is.Name), blob)
					if err != nil {
						errs = append(errs, fmt.Errorf("failed to delete layer link %s: %v", blob, err))
					}
				}
			}
		}
	}

	deletedItems := len(tagEventList.Items) - len(filteredItems)

	tagEventList.Items = filteredItems

	return tagEventList, deletedItems, manifestsToDelete, errs
}

func (p *pruner) pruneImageStream(stream *imagev1.ImageStream, imageStreamDeleter ImageStreamDeleter, layerLinkDeleter LayerLinkDeleter, manifestDeleter ManifestDeleter) (*imagev1.ImageStream, *PruneStats, []error) {
	klog.V(4).Infof("Examining ImageStream %s/%s", stream.Namespace, stream.Name)

	if !p.algorithm.pruneOverSizeLimit && stream.CreationTimestamp.Time.After(p.algorithm.keepYoungerThan) {
		klog.V(4).Infof("imagestream %s/%s: keeping all images because of --keep-younger-than", stream.Namespace, stream.Name)
		return stream, &PruneStats{}, nil
	}

	collectingLayerLinkDeleter := newCollectingLayerLinkDeleter(layerLinkDeleter)

	var manifestsToDelete []string
	var errs []error
	var deletedItems int
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		manifestsToDelete = nil

		is, err := imageStreamDeleter.GetImageStream(stream)
		if err != nil {
			if kerrapi.IsNotFound(err) {
				klog.V(4).Infof("imagestream %s/%s: skipping because it does not exist anymore", stream.Namespace, stream.Name)
				stream = nil
				return nil
			}
			return err
		}
		stream = is

		counts, err := p.getRepositoryReferenceCounts(is)
		if err != nil {
			klog.Warningf("imagestream %s/%s: %v", is.Namespace, is.Name, err)
			return nil
		}

		deletedItems = 0
		for i, tagEventList := range is.Status.Tags {
			updatedTagEventList, deletedTagItems, tagManifestsToDelete, tagErrs := p.pruneImageStreamTag(is, tagEventList, counts, collectingLayerLinkDeleter)
			is.Status.Tags[i] = updatedTagEventList
			deletedItems += deletedTagItems
			manifestsToDelete = append(manifestsToDelete, tagManifestsToDelete...)
			errs = append(errs, tagErrs...)
		}

		// deleting tags without items
		tags := is.Status.Tags[:0]
		for i := range is.Status.Tags {
			if len(is.Status.Tags[i].Items) > 0 {
				tags = append(tags, is.Status.Tags[i])
			}
		}
		is.Status.Tags = tags

		if deletedItems > 0 {
			updatedStream, err := imageStreamDeleter.UpdateImageStream(is, deletedItems)
			if kerrapi.IsNotFound(err) {
				klog.V(4).Infof("imagestream %s/%s: the image stream cannot be updated because it's gone", is.Namespace, is.Name)
				stream = nil
				return nil
			} else if err != nil {
				return err
			}
			stream = updatedStream
		}

		return nil
	})
	stats := &PruneStats{
		DeletedLayerLinks: collectingLayerLinkDeleter.DeletedLayerLinkCount(),
	}
	if err != nil {
		errs = append(errs, err)
		return stream, stats, errs
	}

	if deletedItems > 0 {
		stats.DeletedImageStreamTagItems = deletedItems
		stats.UpdatedImageStreams = 1
	}

	if p.algorithm.pruneRegistry {
		for _, digest := range manifestsToDelete {
			err := manifestDeleter.DeleteManifest(fmt.Sprintf("%s/%s", stream.Namespace, stream.Name), digest)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to delete manifest link %s: %v", digest, err))
			} else {
				stats.DeletedManifestLinks++
			}
		}
	}

	return stream, stats, errs
}

func (p *pruner) pruneImage(image *imagev1.Image, usedImages map[string]bool, counts referenceCounts, blobDeleter BlobDeleter, imageDeleter ImageDeleter) (*PruneStats, []error) {
	stats := &PruneStats{}

	if usedImages[image.Name] {
		klog.V(4).Infof("image %s: keeping because it is used by imagestreams", image.Name)
		return stats, nil
	}

	if !p.algorithm.allImages {
		if image.Annotations[imagev1.ManagedByOpenShiftAnnotation] != "true" {
			klog.V(4).Infof("image %s: keeping external image because --all=false", image.Name)
			return stats, nil
		}
	}

	if !p.algorithm.pruneOverSizeLimit && image.CreationTimestamp.Time.After(p.algorithm.keepYoungerThan) {
		klog.V(4).Infof("image %s: keeping because of --keep-younger-than", image.Name)
		return stats, nil
	}

	klog.V(4).Infof("image %s: deleting...", image.Name)

	var errs []error
	failures := 0

	if p.algorithm.pruneRegistry {
		imageBlobs, err := getImageBlobs(image)
		if err != nil {
			return stats, []error{err}
		}

		for _, blob := range imageBlobs {
			if counts.Blobs.Add(blob, -1) == 0 {
				err := blobDeleter.DeleteBlob(blob)
				if err != nil {
					failures++
					errs = append(errs, fmt.Errorf("failed to delete blob %s: %v", blob, err))
				} else {
					stats.DeletedBlobs++
				}
			}
		}
		if counts.Blobs.Add(image.Name, -1) == 0 {
			err := blobDeleter.DeleteBlob(image.Name)
			if err != nil {
				failures++
				errs = append(errs, fmt.Errorf("failed to delete manifest blob %s: %v", image.Name, err))
			} else {
				stats.DeletedBlobs++
			}
		}
	}

	if stats.DeletedBlobs > 0 || failures == 0 {
		if err := imageDeleter.DeleteImage(image); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete image %s: %v", image.Name, err))
		} else {
			stats.DeletedImages++
		}
	}

	return stats, errs
}

func (p *pruner) pruneImageStreams(
	streamPruner ImageStreamDeleter,
	layerLinkDeleter LayerLinkDeleter,
	manifestDeleter ManifestDeleter,
) (*PruneStats, []error) {
	var keys []string
	for k := range p.imageStreams {
		keys = append(keys, k)
	}

	var wg sync.WaitGroup
	var mutex sync.Mutex
	workQueue := make(chan string)
	pruneStats := &PruneStats{}
	errorsCh := make(chan error)

	for i := 0; i < p.numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range workQueue {
				mutex.Lock()
				stream := p.imageStreams[k]
				mutex.Unlock()

				updatedStream, stats, errs := p.pruneImageStream(stream, streamPruner, layerLinkDeleter, manifestDeleter)

				if updatedStream == nil {
					mutex.Lock()
					delete(p.imageStreams, k)
					mutex.Unlock()
				} else if updatedStream != stream {
					mutex.Lock()
					p.imageStreams[k] = updatedStream
					mutex.Unlock()
				}

				pruneStats.Add(stats)

				for _, err := range errs {
					err = fmt.Errorf("imagestream %s/%s: %v", stream.Namespace, stream.Name, err)
					klog.V(4).Info(err)
					errorsCh <- err
				}
			}
		}()
	}

	go func() {
		for _, k := range keys {
			workQueue <- k
		}
		close(workQueue)
		wg.Wait()
		close(errorsCh)
	}()

	var errs []error
	for err := range errorsCh {
		errs = append(errs, err)
	}
	return pruneStats, errs
}

func (p *pruner) pruneImages(
	blobDeleter BlobDeleter,
	imageDeleter ImageDeleter,
) (*PruneStats, []error) {
	usedImages := map[string]bool{}
	for _, stream := range p.imageStreams {
		for _, tag := range stream.Status.Tags {
			for _, item := range tag.Items {
				usedImages[item.Image] = true
			}
		}
	}

	pruneStats := &PruneStats{}

	counts, err := p.getGlobalReferenceCounts(p.images)
	if err != nil {
		return pruneStats, []error{err}
	}

	var wg sync.WaitGroup
	imagesToDelete := make(chan *imagev1.Image)
	errorsCh := make(chan error)

	for i := 0; i < p.numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for image := range imagesToDelete {
				stats, errs := p.pruneImage(image, usedImages, counts, blobDeleter, imageDeleter)

				pruneStats.Add(stats)

				for _, err := range errs {
					err = fmt.Errorf("image %s: %v", image.Name, err)
					klog.V(4).Info(err)
					errorsCh <- err
				}
			}
		}()
	}

	go func() {
		for _, image := range p.images {
			imagesToDelete <- image
		}
		close(imagesToDelete)
		wg.Wait()
		close(errorsCh)
	}()

	var errs []error
	for err = range errorsCh {
		errs = append(errs, err)
	}
	return pruneStats, errs
}

// Prune deletes historical items from image streams (image stream tag
// revisions) that are not protected by the pruner options and not used by the
// cluster objects. After that, it deletes images that are not used by image
// streams.
func (p *pruner) Prune(
	streamPruner ImageStreamDeleter,
	layerLinkDeleter LayerLinkDeleter,
	manifestDeleter ManifestDeleter,
	blobDeleter BlobDeleter,
	imageDeleter ImageDeleter,
) (*PruneStats, kerrors.Aggregate) {
	pruneStats := &PruneStats{}
	var errs []error

	// Stage 1: delete history from image streams
	stats, errors := p.pruneImageStreams(streamPruner, layerLinkDeleter, manifestDeleter)
	pruneStats.Add(stats)
	errs = append(errs, errors...)

	// Stage 2: delete images
	if p.algorithm.namespace == "" {
		stats, errors := p.pruneImages(blobDeleter, imageDeleter)
		pruneStats.Add(stats)
		errs = append(errs, errors...)
	}

	return pruneStats, kerrors.NewAggregate(errs)
}

// imageDeleter removes an image from OpenShift.
type imageDeleter struct {
	images imagev1client.ImagesGetter
}

var _ ImageDeleter = &imageDeleter{}

// NewImageDeleter creates a new imageDeleter.
func NewImageDeleter(images imagev1client.ImagesGetter) ImageDeleter {
	return &imageDeleter{
		images: images,
	}
}

func (p *imageDeleter) DeleteImage(image *imagev1.Image) error {
	klog.V(4).Infof("Deleting image %q", image.Name)
	return p.images.Images().Delete(context.TODO(), image.Name, *metav1.NewDeleteOptions(0))
}

// imageStreamDeleter updates an image stream in OpenShift.
type imageStreamDeleter struct {
	streams imagev1client.ImageStreamsGetter
}

// NewImageStreamDeleter creates a new imageStreamDeleter.
func NewImageStreamDeleter(streams imagev1client.ImageStreamsGetter) ImageStreamDeleter {
	return &imageStreamDeleter{
		streams: streams,
	}
}

func (p *imageStreamDeleter) GetImageStream(stream *imagev1.ImageStream) (*imagev1.ImageStream, error) {
	return p.streams.ImageStreams(stream.Namespace).Get(context.TODO(), stream.Name, metav1.GetOptions{})
}

func (p *imageStreamDeleter) UpdateImageStream(stream *imagev1.ImageStream, deletedItems int) (*imagev1.ImageStream, error) {
	klog.V(4).Infof("Updating ImageStream %s/%s", stream.Namespace, stream.Name)
	is, err := p.streams.ImageStreams(stream.Namespace).UpdateStatus(context.TODO(), stream, metav1.UpdateOptions{})
	if err == nil {
		klog.V(5).Infof("Updated ImageStream: %#v", is)
	}
	return is, err
}

// deleteFromRegistry uses registryClient to send a DELETE request to the
// provided url.
func deleteFromRegistry(registryClient *http.Client, url string) error {
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	klog.V(5).Infof(`Sending request "%s %s" to the registry`, req.Method, req.URL.String())
	resp, err := registryClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		klog.V(4).Infof("Unable to delete layer %s, returned %v", url, resp.Status)
		return nil
	}

	// non-2xx/3xx response doesn't cause an error, so we need to check for it
	// manually and return it to caller
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf(resp.Status)
	}

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusAccepted {
		klog.V(1).Infof("Unexpected status code in response: %d", resp.StatusCode)
		var response errcode.Errors
		decoder := json.NewDecoder(resp.Body)
		if err := decoder.Decode(&response); err != nil {
			return err
		}
		klog.V(1).Infof("Response: %#v", response)
		return &response
	}

	return err
}

// remoteLayerLinkDeleter removes a repository layer link from the registry.
type remoteLayerLinkDeleter struct {
	registryClient *http.Client
	registryURL    *url.URL
}

func NewLayerLinkDeleter(registryClient *http.Client, registryURL *url.URL) LayerLinkDeleter {
	return &remoteLayerLinkDeleter{
		registryClient: registryClient,
		registryURL:    registryURL,
	}
}

func (p *remoteLayerLinkDeleter) DeleteLayerLink(repoName, linkName string) error {
	klog.V(4).Infof("Deleting layer link %s from repository %s/%s", linkName, p.registryURL.Host, repoName)
	return deleteFromRegistry(p.registryClient, fmt.Sprintf("%s/v2/%s/blobs/%s", p.registryURL.String(), repoName, linkName))
}

// collectingLayerLinkDeleter gathers information about which layers it has
// deleted and does not attempt to delete the same layer link twice.
type collectingLayerLinkDeleter struct {
	LayerLinkDeleter

	mutex   sync.Mutex
	deleted map[string]bool
	count   int
}

func newCollectingLayerLinkDeleter(deleter LayerLinkDeleter) *collectingLayerLinkDeleter {
	return &collectingLayerLinkDeleter{
		LayerLinkDeleter: deleter,
		deleted:          make(map[string]bool),
	}
}

func (p *collectingLayerLinkDeleter) isDeleted(repoName, linkName string) bool {
	key := fmt.Sprintf("%s@%s", repoName, linkName)

	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.deleted[key]
}

func (p *collectingLayerLinkDeleter) markAsDeleted(repoName, linkName string) {
	key := fmt.Sprintf("%s@%s", repoName, linkName)

	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.deleted[key] = true
	p.count++
}

func (p *collectingLayerLinkDeleter) DeletedLayerLinkCount() int {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.count
}

func (p *collectingLayerLinkDeleter) DeleteLayerLink(repoName, linkName string) error {
	if p.isDeleted(repoName, linkName) {
		return nil
	}

	err := p.LayerLinkDeleter.DeleteLayerLink(repoName, linkName)
	if err != nil {
		return err
	}

	p.markAsDeleted(repoName, linkName)

	return nil
}

// manifestDeleter deletes repository manifest data from the registry.
type remoteManifestDeleter struct {
	registryClient *http.Client
	registryURL    *url.URL
}

// NewManifestDeleter creates a new manifestDeleter.
func NewManifestDeleter(registryClient *http.Client, registryURL *url.URL) ManifestDeleter {
	return &remoteManifestDeleter{
		registryClient: registryClient,
		registryURL:    registryURL,
	}
}

func (p *remoteManifestDeleter) DeleteManifest(repoName, manifest string) error {
	klog.V(4).Infof("Deleting manifest %s from repository %s/%s", manifest, p.registryURL.Host, repoName)
	return deleteFromRegistry(p.registryClient, fmt.Sprintf("%s/v2/%s/manifests/%s", p.registryURL.String(), repoName, manifest))
}

// blobDeleter removes a blob from the registry.
type blobDeleter struct {
	registryClient *http.Client
	registryURL    *url.URL
}

// NewBlobDeleter creates a new blobDeleter.
func NewBlobDeleter(registryClient *http.Client, registryURL *url.URL) BlobDeleter {
	return &blobDeleter{
		registryClient: registryClient,
		registryURL:    registryURL,
	}
}

func (p *blobDeleter) DeleteBlob(blob string) error {
	klog.V(4).Infof("Deleting blob %s from registry %s", blob, p.registryURL.Host)
	return deleteFromRegistry(p.registryClient, fmt.Sprintf("%s/admin/blobs/%s", p.registryURL.String(), blob))
}
