package images

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kutilerrors "k8s.io/apimachinery/pkg/util/errors"
	knet "k8s.io/apimachinery/pkg/util/net"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	apimachineryversion "k8s.io/apimachinery/pkg/version"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	kclientcmd "k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/pager"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	imagev1 "github.com/openshift/api/image/v1"
	appsv1client "github.com/openshift/client-go/apps/clientset/versioned/typed/apps/v1"
	buildv1client "github.com/openshift/client-go/build/clientset/versioned/typed/build/v1"
	imagev1client "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	"github.com/openshift/library-go/pkg/network/networkutils"

	"github.com/openshift/oc/pkg/cli/admin/prune/imageprune"
	"github.com/openshift/oc/pkg/version"
)

// PruneImagesRecommendedName is the recommended command name
const PruneImagesRecommendedName = "images"

var errNoToken = errors.New("you must use a client config with a token")

const registryURLNotReachable = `(?:operation|connection) timed out|no such host`

var (
	imagesLongDesc = templates.LongDesc(`
		Remove image stream tags, images, and image layers by age or usage.

		This command removes historical image stream tags, unused images, and unreferenced image
		layers from the integrated registry. By default, all images are considered as candidates.
		The command can be instructed to consider only images that have been directly pushed to the
		registry by supplying --all=false flag.

		By default, the prune operation performs a dry run making no changes to internal registry. A
		--confirm flag is needed for changes to be effective. The flag requires a valid route to the
		integrated container image registry. If this command is run outside of the cluster network, the route
		needs to be provided using --registry-url.

		Only a user with a cluster role %s or higher who is logged-in will be able to actually
		delete the images.

		If the registry is secured with a certificate signed by a self-signed root certificate
		authority other than the one present in current user's config, you may need to specify it
		using --certificate-authority flag.

		Insecure connection is allowed in the following cases unless certificate-authority is
		specified:

		 1. --force-insecure is given
		 2. provided registry-url is prefixed with http://
		 3. registry url is a private or link-local address
		 4. user's config allows for insecure connection (the user logged in to the cluster with
			--insecure-skip-tls-verify or allowed for insecure connection)`)

	imagesExample = templates.Examples(`
	  # See what the prune command would delete if only images and their referrers were more than an hour old
	  # and obsoleted by 3 newer revisions under the same tag were considered
	  oc adm prune images --keep-tag-revisions=3 --keep-younger-than=60m

	  # To actually perform the prune operation, the confirm flag must be appended
	  oc adm prune images --keep-tag-revisions=3 --keep-younger-than=60m --confirm

	  # See what the prune command would delete if we are interested in removing images
	  # exceeding currently set limit ranges ('openshift.io/Image')
	  oc adm prune images --prune-over-size-limit

	  # To actually perform the prune operation, the confirm flag must be appended
	  oc adm prune images --prune-over-size-limit --confirm

	  # Force the insecure HTTP protocol with the particular registry host name
	  oc adm prune images --registry-url=http://registry.example.org --confirm

	  # Force a secure connection with a custom certificate authority to the particular registry host name
	  oc adm prune images --registry-url=registry.example.org --certificate-authority=/path/to/custom/ca.crt --confirm`)
)

var (
	defaultKeepYoungerThan         = 60 * time.Minute
	defaultKeepTagRevisions        = 3
	defaultPruneImageOverSizeLimit = false
	defaultPruneRegistry           = true
	defaultNumWorkers              = 5
)

// PruneImagesOptions holds all the required options for pruning images.
type PruneImagesOptions struct {
	Confirm             bool
	KeepYoungerThan     *time.Duration
	KeepTagRevisions    *int
	PruneOverSizeLimit  *bool
	AllImages           *bool
	CABundle            string
	RegistryUrlOverride string
	Namespace           string
	ForceInsecure       bool
	PruneRegistry       *bool
	IgnoreInvalidRefs   bool
	NumWorkers          *int

	ClientConfig       *restclient.Config
	AppsClient         appsv1client.AppsV1Interface
	BuildClient        buildv1client.BuildV1Interface
	ImageClient        imagev1client.ImageV1Interface
	ImageClientFactory func() (imagev1client.ImageV1Interface, error)
	DiscoveryClient    discovery.DiscoveryInterface
	KubeClient         kubernetes.Interface
	Timeout            time.Duration
	Out                io.Writer
	ErrOut             io.Writer
}

// NewCmdPruneImages implements the OpenShift cli prune images command.
func NewCmdPruneImages(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	allImages := true
	opts := &PruneImagesOptions{
		Confirm:            false,
		KeepYoungerThan:    &defaultKeepYoungerThan,
		KeepTagRevisions:   &defaultKeepTagRevisions,
		PruneOverSizeLimit: &defaultPruneImageOverSizeLimit,
		PruneRegistry:      &defaultPruneRegistry,
		AllImages:          &allImages,
		NumWorkers:         &defaultNumWorkers,
	}

	cmd := &cobra.Command{
		Use:     "images",
		Short:   "Remove unreferenced images",
		Long:    fmt.Sprintf(imagesLongDesc, "system:image-pruner"),
		Example: imagesExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(opts.Complete(f, cmd, args, streams.Out))
			kcmdutil.CheckErr(opts.Validate())
			kcmdutil.CheckErr(opts.Run())
		},
	}

	cmd.Flags().BoolVar(&opts.Confirm, "confirm", opts.Confirm, "If true, specify that image pruning should proceed. Defaults to false, displaying what would be deleted but not actually deleting anything. Requires a valid route to the integrated container image registry (see --registry-url).")
	cmd.Flags().BoolVar(opts.AllImages, "all", *opts.AllImages, "Include images that were imported from external registries as candidates for pruning.  If pruned, all the mirrored objects associated with them will also be removed from the integrated registry.")
	cmd.Flags().DurationVar(opts.KeepYoungerThan, "keep-younger-than", *opts.KeepYoungerThan, "Specify the minimum age of an image and its referrers for it to be considered a candidate for pruning.")
	cmd.Flags().IntVar(opts.KeepTagRevisions, "keep-tag-revisions", *opts.KeepTagRevisions, "Specify the number of image revisions for a tag in an image stream that will be preserved.")
	cmd.Flags().BoolVar(opts.PruneOverSizeLimit, "prune-over-size-limit", *opts.PruneOverSizeLimit, "Specify if images which are exceeding LimitRanges (see 'openshift.io/Image'), specified in the same namespace, should be considered for pruning. This flag cannot be combined with --keep-younger-than nor --keep-tag-revisions.")
	cmd.Flags().StringVar(&opts.CABundle, "certificate-authority", opts.CABundle, "The path to a certificate authority bundle to use when communicating with the managed container image registries. Defaults to the certificate authority data from the current user's config file. It cannot be used together with --force-insecure.")
	cmd.Flags().StringVar(&opts.RegistryUrlOverride, "registry-url", opts.RegistryUrlOverride, "The address to use when contacting the registry, instead of using the default value. This is useful if you can't resolve or reach the registry (e.g.; the default is a cluster-internal URL) but you do have an alternative route that works. Particular transport protocol can be enforced using '<scheme>://' prefix.")
	cmd.Flags().BoolVar(&opts.ForceInsecure, "force-insecure", opts.ForceInsecure, "If true, allow an insecure connection to the container image registry that is hosted via HTTP or has an invalid HTTPS certificate. Whenever possible, use --certificate-authority instead of this dangerous option.")
	cmd.Flags().BoolVar(opts.PruneRegistry, "prune-registry", *opts.PruneRegistry, "If false, the prune operation will clean up image API objects, but the none of the associated content in the registry is removed.  Note, if only image API objects are cleaned up through use of this flag, the only means for subsequently cleaning up registry data corresponding to those image API objects is to employ the 'hard prune' administrative task.")
	cmd.Flags().BoolVar(&opts.IgnoreInvalidRefs, "ignore-invalid-refs", opts.IgnoreInvalidRefs, "If true, the pruning process will ignore all errors while parsing image references. This means that the pruning process will ignore the intended connection between the object and the referenced image. As a result an image may be incorrectly deleted as unused.")
	cmd.Flags().IntVar(opts.NumWorkers, "num-workers", *opts.NumWorkers, "Specify the number of parallel workers to use when running prune operations.")

	return cmd
}

// Complete turns a partially defined PruneImagesOptions into a solvent structure
// which can be validated and used for pruning images.
func (o *PruneImagesOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string, out io.Writer) error {
	if len(args) > 0 {
		return kcmdutil.UsageErrorf(cmd, "no arguments are allowed to this command")
	}

	if !cmd.Flags().Lookup("prune-over-size-limit").Changed {
		o.PruneOverSizeLimit = nil
	} else {
		if !cmd.Flags().Lookup("keep-younger-than").Changed {
			o.KeepYoungerThan = nil
		}
		if !cmd.Flags().Lookup("keep-tag-revisions").Changed {
			o.KeepTagRevisions = nil
		}
	}

	o.Namespace = metav1.NamespaceAll
	if cmd.Flags().Lookup("namespace").Changed {
		var err error
		o.Namespace, _, err = f.ToRawKubeConfigLoader().Namespace()
		if err != nil {
			return err
		}
	}
	o.Out = out
	o.ErrOut = os.Stderr

	var err error
	o.ClientConfig, err = f.ToRESTConfig()
	if err != nil {
		return err
	}
	if len(o.ClientConfig.BearerToken) == 0 {
		return errNoToken
	}
	o.ClientConfig.QPS = 100
	o.ClientConfig.Burst = 100
	o.KubeClient, err = kubernetes.NewForConfig(o.ClientConfig)
	if err != nil {
		return err
	}
	o.AppsClient, err = appsv1client.NewForConfig(o.ClientConfig)
	if err != nil {
		return err
	}
	o.BuildClient, err = buildv1client.NewForConfig(o.ClientConfig)
	if err != nil {
		return err
	}
	o.ImageClient, err = imagev1client.NewForConfig(o.ClientConfig)
	if err != nil {
		return err
	}
	o.DiscoveryClient, err = discovery.NewDiscoveryClientForConfig(o.ClientConfig)
	if err != nil {
		return err
	}

	o.ImageClientFactory = getImageClientFactory(f)

	o.Timeout = o.ClientConfig.Timeout
	if o.Timeout == 0 {
		o.Timeout = time.Duration(10 * time.Second)
	}

	return nil
}

// Validate ensures that a PruneImagesOptions is valid and can be used to execute pruning.
func (o PruneImagesOptions) Validate() error {
	if o.PruneOverSizeLimit != nil && (o.KeepYoungerThan != nil || o.KeepTagRevisions != nil) {
		return fmt.Errorf("--prune-over-size-limit cannot be specified with --keep-tag-revisions nor --keep-younger-than")
	}
	if o.KeepYoungerThan != nil && *o.KeepYoungerThan < 0 {
		return fmt.Errorf("--keep-younger-than must be greater than or equal to 0")
	}
	if o.KeepTagRevisions != nil && *o.KeepTagRevisions < 0 {
		return fmt.Errorf("--keep-tag-revisions must be greater than or equal to 0")
	}
	if err := validateRegistryURL(o.RegistryUrlOverride); len(o.RegistryUrlOverride) > 0 && err != nil {
		return fmt.Errorf("invalid --registry-url flag: %v", err)
	}
	if o.ForceInsecure && len(o.CABundle) > 0 {
		return fmt.Errorf("--certificate-authority cannot be specified with --force-insecure")
	}
	if len(o.CABundle) > 0 && strings.HasPrefix(o.RegistryUrlOverride, "http://") {
		return fmt.Errorf("--certificate-authority cannot be specified for insecure http protocol")
	}
	return nil
}

var (
	errNoRegistryURLPathAllowed  = errors.New("no path after <host>[:<port>] is allowed")
	errNoRegistryURLQueryAllowed = errors.New("no query arguments are allowed after <host>[:<port>]")
	errRegistryURLHostEmpty      = errors.New("no host name specified")
)

// validateRegistryURL returns error if the given input is not a valid registry URL. The url may be prefixed
// with http:// or https:// schema. It may not contain any path or query after the host:[port].
func validateRegistryURL(registryURL string) error {
	var (
		u     *url.URL
		err   error
		parts = strings.SplitN(registryURL, "://", 2)
	)

	switch len(parts) {
	case 2:
		u, err = url.Parse(registryURL)
		if err != nil {
			return err
		}
		switch u.Scheme {
		case "http", "https":
		default:
			return fmt.Errorf("unsupported scheme: %s", u.Scheme)
		}
	case 1:
		u, err = url.Parse("https://" + registryURL)
		if err != nil {
			return err
		}
	}
	if len(u.Path) > 0 && u.Path != "/" {
		return errNoRegistryURLPathAllowed
	}
	if len(u.RawQuery) > 0 {
		return errNoRegistryURLQueryAllowed
	}
	if len(u.Host) == 0 {
		return errRegistryURLHostEmpty
	}
	return nil
}

// Run contains all the necessary functionality for the OpenShift cli prune images command.
func (o PruneImagesOptions) Run() error {
	allPods, err := o.KubeClient.CoreV1().Pods(o.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	allRCs, err := o.KubeClient.CoreV1().ReplicationControllers(o.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	allBCs, err := o.BuildClient.BuildConfigs(o.Namespace).List(context.TODO(), metav1.ListOptions{})
	// We need to tolerate 'not found' errors for buildConfigs since they may be disabled in Atomic
	if err != nil && !kerrors.IsNotFound(err) {
		return err
	}

	allBuilds, err := o.BuildClient.Builds(o.Namespace).List(context.TODO(), metav1.ListOptions{})
	// We need to tolerate 'not found' errors for builds since they may be disabled in Atomic
	if err != nil && !kerrors.IsNotFound(err) {
		return err
	}

	allDSs, err := o.KubeClient.AppsV1().DaemonSets(o.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		// TODO: remove in future (3.9) release
		if !kerrors.IsForbidden(err) {
			return err
		}
		fmt.Fprintf(o.ErrOut, "Failed to list daemonsets: %v\n - * Make sure to update clusterRoleBindings.\n", err)
	}

	allDeployments, err := o.KubeClient.AppsV1().Deployments(o.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		// TODO: remove in future (3.9) release
		if !kerrors.IsForbidden(err) {
			return err
		}
		fmt.Fprintf(o.ErrOut, "Failed to list deployments: %v\n - * Make sure to update clusterRoleBindings.\n", err)
	}

	allDCs, err := o.AppsClient.DeploymentConfigs(o.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return err
	}

	allRSs, err := o.KubeClient.AppsV1().ReplicaSets(o.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		// TODO: remove in future (3.9) release
		if !kerrors.IsForbidden(err) {
			return err
		}
		fmt.Fprintf(o.ErrOut, "Failed to list replicasets: %v\n - * Make sure to update clusterRoleBindings.\n", err)
	}

	allSSets, err := o.KubeClient.AppsV1().StatefulSets(o.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	allJobs, err := o.KubeClient.BatchV1().Jobs(o.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	allCronJobs, err := o.KubeClient.BatchV1().CronJobs(o.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	limitRangesList, err := o.KubeClient.CoreV1().LimitRanges(o.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	limitRangesMap := make(map[string][]*corev1.LimitRange)
	for i := range limitRangesList.Items {
		limit := limitRangesList.Items[i]
		limits, ok := limitRangesMap[limit.Namespace]
		if !ok {
			limits = []*corev1.LimitRange{}
		}
		limits = append(limits, &limit)
		limitRangesMap[limit.Namespace] = limits
	}

	allStreams, err := o.ImageClient.ImageStreams(o.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	allStreamsMap := map[string]*imagev1.ImageStream{}
	for i := range allStreams.Items {
		stream := &allStreams.Items[i]
		allStreamsMap[fmt.Sprintf("%s/%s", stream.Namespace, stream.Name)] = stream
	}

	ctx := context.TODO()
	allImages := map[string]*imagev1.Image{}
	err = pager.New(func(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
		return o.ImageClient.Images().List(ctx, opts)
	}).EachListItem(ctx, metav1.ListOptions{Limit: 5000}, func(obj runtime.Object) error {
		image := obj.(*imagev1.Image)
		allImages[image.Name] = image
		return nil
	})
	if err != nil {
		return err
	}

	var (
		registryHost   = o.RegistryUrlOverride
		registryClient *http.Client
		registryPinger imageprune.RegistryPinger
	)

	registryPinger = &imageprune.DryRunRegistryPinger{}
	if o.Confirm && o.PruneRegistry != nil && *o.PruneRegistry {
		if len(registryHost) == 0 {
			registryHost, err = imageprune.DetermineRegistryHost(allImages, allStreams)
			if err != nil {
				return fmt.Errorf("unable to find the remote registry host: %v", err)
			}
		}

		insecure := o.ForceInsecure
		if !insecure && len(o.CABundle) == 0 {
			insecure = o.ClientConfig.TLSClientConfig.Insecure || networkutils.IsPrivateAddress(registryHost) ||
				strings.HasPrefix(registryHost, "http://")
		}

		registryClient, err = getRegistryClient(o.ClientConfig, o.CABundle, insecure)
		if err != nil {
			return err
		}

		registryPinger = &imageprune.DefaultRegistryPinger{
			Client:   registryClient,
			Insecure: insecure,
		}
	}

	// verify the registy connection now to avoid future surprises
	registryURL, err := registryPinger.Ping(registryHost)
	if err != nil {
		if len(o.RegistryUrlOverride) == 0 && regexp.MustCompile(registryURLNotReachable).MatchString(err.Error()) {
			err = fmt.Errorf("%s\n* Please provide a reachable route to the integrated registry using --registry-url.", err.Error())
		}
		return fmt.Errorf("failed to ping registry %s: %v", registryHost, err)
	}

	options := imageprune.PrunerOptions{
		KeepYoungerThan:    o.KeepYoungerThan,
		KeepTagRevisions:   o.KeepTagRevisions,
		PruneOverSizeLimit: o.PruneOverSizeLimit,
		AllImages:          o.AllImages,
		Images:             allImages,
		Streams:            allStreamsMap,
		Pods:               allPods,
		RCs:                allRCs,
		BCs:                allBCs,
		Builds:             allBuilds,
		DSs:                allDSs,
		Deployments:        allDeployments,
		DCs:                allDCs,
		RSs:                allRSs,
		SSets:              allSSets,
		Jobs:               allJobs,
		CronJobs:           allCronJobs,
		LimitRanges:        limitRangesMap,
		DryRun:             o.Confirm == false,
		PruneRegistry:      o.PruneRegistry,
		IgnoreInvalidRefs:  o.IgnoreInvalidRefs,
	}
	if o.Namespace != metav1.NamespaceAll {
		options.Namespace = o.Namespace
	}
	if o.NumWorkers != nil {
		options.NumWorkers = *o.NumWorkers
	}
	pruner, errs := imageprune.NewPruner(options)
	if errs != nil {
		o.printGraphBuildErrors(errs)
		return fmt.Errorf("failed to build graph - no changes made")
	}

	imageStreamDeleter := &describingImageStreamDeleter{w: o.Out, errOut: o.ErrOut}
	layerLinkDeleter := &describingLayerLinkDeleter{w: o.Out, errOut: o.ErrOut}
	manifestDeleter := &describingManifestDeleter{w: o.Out, errOut: o.ErrOut}
	blobDeleter := &describingBlobDeleter{w: o.Out, errOut: o.ErrOut}
	imageDeleter := &describingImageDeleter{w: o.Out, errOut: o.ErrOut}

	if o.Confirm {
		imageStreamDeleter.delegate = imageprune.NewImageStreamDeleter(o.ImageClient)
		layerLinkDeleter.delegate = imageprune.NewLayerLinkDeleter(registryClient, registryURL)
		manifestDeleter.delegate = imageprune.NewManifestDeleter(registryClient, registryURL)
		blobDeleter.delegate = imageprune.NewBlobDeleter(registryClient, registryURL)
		imageDeleter.delegate = imageprune.NewImageDeleter(o.ImageClient)
	} else {
		fmt.Fprintln(o.ErrOut, "Dry run enabled - no modifications will be made. Add --confirm to remove images")
	}

	if o.PruneRegistry != nil && !*o.PruneRegistry {
		fmt.Fprintln(o.Out, "Only API objects will be removed.  No modifications to the image registry will be made.")
	}

	stats, errs := pruner.Prune(
		imageStreamDeleter,
		layerLinkDeleter,
		manifestDeleter,
		blobDeleter,
		imageDeleter,
	)
	fmt.Fprintf(o.Out, "Summary: %s\n", stats)
	return errs
}

func (o *PruneImagesOptions) printGraphBuildErrors(errs kutilerrors.Aggregate) {
	refErrors := []error{}

	fmt.Fprintf(o.ErrOut, "Failed to build graph!\n")

	for _, err := range errs.Errors() {
		if _, ok := err.(*imageprune.ErrBadReference); ok {
			refErrors = append(refErrors, err)
		} else {
			fmt.Fprintf(o.ErrOut, "%v\n", err)
		}
	}

	if len(refErrors) > 0 {
		clientVersion, masterVersion, err := getClientAndMasterVersions(o.DiscoveryClient, o.Timeout)
		if err != nil {
			fmt.Fprintf(o.ErrOut, "Failed to get master API version: %v\n", err)
		}
		fmt.Fprintf(o.ErrOut, "\nThe following objects have invalid references:\n\n")
		for _, err := range refErrors {
			fmt.Fprintf(o.ErrOut, "  %s\n", err)
		}
		fmt.Fprintf(o.ErrOut, "\nEither fix the references or delete the objects to make the pruner proceed.\n")

		if masterVersion != nil && (clientVersion.Major != masterVersion.Major || clientVersion.Minor != masterVersion.Minor) {
			fmt.Fprintf(o.ErrOut, "Client version (%s) doesn't match master (%s), which may allow for different image references. Try to re-run this binary with the same version.\n", clientVersion, masterVersion)
		}
	}
}

// describingImageStreamDeleter prints information about each image stream update.
// If a delegate exists, its DeleteImageStream function is invoked prior to returning.
type describingImageStreamDeleter struct {
	w        io.Writer
	delegate imageprune.ImageStreamDeleter
	errOut   io.Writer
}

var _ imageprune.ImageStreamDeleter = &describingImageStreamDeleter{}

func (p *describingImageStreamDeleter) GetImageStream(stream *imagev1.ImageStream) (*imagev1.ImageStream, error) {
	if p.delegate == nil {
		return stream, nil
	}

	return p.delegate.GetImageStream(stream)
}

func (p *describingImageStreamDeleter) UpdateImageStream(stream *imagev1.ImageStream, deletedItems int) (*imagev1.ImageStream, error) {
	fmt.Fprintf(p.w, "Deleting %d items from image stream %s/%s\n", deletedItems, stream.Namespace, stream.Name)

	if p.delegate == nil {
		return stream, nil
	}

	updatedStream, err := p.delegate.UpdateImageStream(stream, deletedItems)
	if err != nil {
		fmt.Fprintf(p.errOut, "error updating image stream %s/%s to remove image references: %v\n", stream.Namespace, stream.Name, err)
	}

	return updatedStream, err
}

// describingImageDeleter prints information about each image being deleted.
// If a delegate exists, its DeleteImage function is invoked prior to returning.
type describingImageDeleter struct {
	w        io.Writer
	delegate imageprune.ImageDeleter
	errOut   io.Writer
}

var _ imageprune.ImageDeleter = &describingImageDeleter{}

func (p *describingImageDeleter) DeleteImage(image *imagev1.Image) error {
	fmt.Fprintf(p.w, "Deleting image %s\n", image.Name)

	if p.delegate == nil {
		return nil
	}

	err := p.delegate.DeleteImage(image)
	if err != nil {
		fmt.Fprintf(p.errOut, "error deleting image %s from server: %v\n", image.Name, err)
	}

	return err
}

// describingLayerLinkDeleter prints information about each repo layer link being deleted. If a delegate
// exists, its DeleteLayerLink function is invoked prior to returning.
type describingLayerLinkDeleter struct {
	w        io.Writer
	delegate imageprune.LayerLinkDeleter
	errOut   io.Writer
}

var _ imageprune.LayerLinkDeleter = &describingLayerLinkDeleter{}

func (p *describingLayerLinkDeleter) DeleteLayerLink(repo, name string) error {
	fmt.Fprintf(p.w, "Deleting layer link %s in repository %s\n", name, repo)

	if p.delegate == nil {
		return nil
	}

	err := p.delegate.DeleteLayerLink(repo, name)
	if err != nil {
		fmt.Fprintf(p.errOut, "error deleting repository %s layer link %s from the registry: %v\n", repo, name, err)
	}

	return err
}

// describingBlobDeleter prints information about each blob being deleted. If a
// delegate exists, its DeleteBlob function is invoked prior to returning.
type describingBlobDeleter struct {
	w        io.Writer
	delegate imageprune.BlobDeleter
	errOut   io.Writer
}

var _ imageprune.BlobDeleter = &describingBlobDeleter{}

func (p *describingBlobDeleter) DeleteBlob(layer string) error {
	fmt.Fprintf(p.w, "Deleting blob %s\n", layer)

	if p.delegate == nil {
		return nil
	}

	err := p.delegate.DeleteBlob(layer)
	if err != nil {
		fmt.Fprintf(p.errOut, "error deleting blob %s from the registry: %v\n", layer, err)
	}

	return err
}

// describingManifestDeleter prints information about each repo manifest being
// deleted. If a delegate exists, its DeleteManifest function is invoked prior
// to returning.
type describingManifestDeleter struct {
	w        io.Writer
	delegate imageprune.ManifestDeleter
	errOut   io.Writer
}

var _ imageprune.ManifestDeleter = &describingManifestDeleter{}

func (p *describingManifestDeleter) DeleteManifest(repo, manifest string) error {
	fmt.Fprintf(p.w, "Deleting manifest link %s in repository %s\n", manifest, repo)

	if p.delegate == nil {
		return nil
	}

	err := p.delegate.DeleteManifest(repo, manifest)
	if err != nil {
		fmt.Fprintf(p.errOut, "error deleting manifest link %s from repository %s: %v\n", manifest, repo, err)
	}

	return err
}

func getImageClientFactory(f kcmdutil.Factory) func() (imagev1client.ImageV1Interface, error) {
	return func() (imagev1client.ImageV1Interface, error) {
		clientConfig, err := f.ToRESTConfig()
		if err != nil {
			return nil, err
		}

		return imagev1client.NewForConfig(clientConfig)
	}
}

// getRegistryClient returns a registry client. Note that registryCABundle and registryInsecure=true are
// mutually exclusive. If registryInsecure=true is specified, the ca bundle is ignored.
func getRegistryClient(clientConfig *restclient.Config, registryCABundle string, registryInsecure bool) (*http.Client, error) {
	var (
		err                      error
		cadata                   []byte
		registryCABundleIncluded = false
		token                    = clientConfig.BearerToken
	)

	if len(token) == 0 {
		return nil, errNoToken
	}

	if len(registryCABundle) > 0 {
		cadata, err = os.ReadFile(registryCABundle)
		if err != nil {
			return nil, fmt.Errorf("failed to read registry ca bundle: %v", err)
		}
	}

	// copy the config
	registryClientConfig := *clientConfig
	registryClientConfig.TLSClientConfig.Insecure = registryInsecure

	// zero out everything we don't want to use
	registryClientConfig.BearerToken = ""
	registryClientConfig.BearerTokenFile = ""
	registryClientConfig.CertFile = ""
	registryClientConfig.CertData = []byte{}
	registryClientConfig.KeyFile = ""
	registryClientConfig.KeyData = []byte{}

	if registryInsecure {
		// it's not allowed to specify insecure flag together with CAs
		registryClientConfig.CAFile = ""
		registryClientConfig.CAData = []byte{}

	} else if len(cadata) > 0 && len(registryClientConfig.CAData) == 0 {
		// If given, we want to append cabundle to the resulting tlsConfig.RootCAs. However, if we
		// leave CAData unset, tlsConfig may not be created. We could append the caBundle to the
		// CAData here directly if we were ok doing a binary magic, which is not the case.
		registryClientConfig.CAData = cadata
		registryCABundleIncluded = true
	}

	// we have to set a username to something for the Docker login but it's not actually used
	registryClientConfig.Username = utilrand.String(5)

	// set the "password" to be the token
	registryClientConfig.Password = token

	tlsConfig, err := restclient.TLSConfigFor(&registryClientConfig)
	if err != nil {
		return nil, err
	}

	// Add the CA bundle to the client config's CA roots if provided and we haven't done that already.
	// FIXME: handle registryCABundle on one place
	if tlsConfig != nil && len(cadata) > 0 && !registryCABundleIncluded && !registryInsecure {
		if tlsConfig.RootCAs == nil {
			tlsConfig.RootCAs = x509.NewCertPool()
		}
		tlsConfig.RootCAs.AppendCertsFromPEM(cadata)
	}

	transport := knet.SetTransportDefaults(&http.Transport{
		TLSClientConfig: tlsConfig,
	})

	wrappedTransport, err := restclient.HTTPWrappersForConfig(&registryClientConfig, transport)
	if err != nil {
		return nil, err
	}

	return &http.Client{
		Transport: wrappedTransport,
	}, nil
}

// getClientAndMasterVersions returns version info for client and master binaries. If it takes too long to get
// a response from the master, timeout error is returned.
func getClientAndMasterVersions(client discovery.DiscoveryInterface, timeout time.Duration) (clientVersion, masterVersion *apimachineryversion.Info, err error) {
	done := make(chan error)

	go func() {
		defer close(done)

		ocVersionBody, err := client.RESTClient().Get().AbsPath("/version/openshift").Do(context.TODO()).Raw()
		switch {
		case err == nil:
			var ocServerInfo apimachineryversion.Info
			err = json.Unmarshal(ocVersionBody, &ocServerInfo)
			if err != nil && len(ocVersionBody) > 0 {
				done <- err
				return
			}
			masterVersion = &ocServerInfo

		case kerrors.IsNotFound(err) || kerrors.IsUnauthorized(err) || kerrors.IsForbidden(err):
		default:
			done <- err
			return
		}
	}()

	select {
	case err, closed := <-done:
		if strings.HasSuffix(fmt.Sprintf("%v", err), "connection refused") || kclientcmd.IsEmptyConfig(err) || kclientcmd.IsConfigurationInvalid(err) {
			return nil, nil, err
		}
		if closed && err != nil {
			return nil, nil, err
		}
	// do not block error printing if the master is busy
	case <-time.After(timeout):
		return nil, nil, fmt.Errorf("error: server took too long to respond with version information.")
	}

	v := version.Get()
	clientVersion = &v

	return
}
