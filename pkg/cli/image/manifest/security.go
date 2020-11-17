package manifest

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/spf13/pflag"

	"github.com/docker/distribution"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"

	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	operatorv1alpha1scheme "github.com/openshift/client-go/operator/clientset/versioned/scheme"
	operatorv1alpha1client "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1alpha1"
	imagereference "github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/library-go/pkg/image/registryclient"
	"github.com/openshift/oc/pkg/cli/image/imagesource"
	"github.com/openshift/oc/pkg/cli/image/manifest/dockercredentials"
)

type SecurityOptions struct {
	RegistryConfig   string
	Insecure         bool
	SkipVerification bool
	// ImageContentSourcePolicyFile to look up alternative sources
	ICSPFile              string
	LookupClusterICSP     bool
	ICSPList              []operatorv1alpha1.ImageContentSourcePolicy
	ICSPClientFn          func() (operatorv1alpha1client.ImageContentSourcePolicyInterface, error)
	TryAlternativeSources bool
	FileDir               string

	CachedContext *registryclient.Context
}

func (o *SecurityOptions) Bind(flags *pflag.FlagSet) {
	flags.StringVarP(&o.RegistryConfig, "registry-config", "a", o.RegistryConfig, "Path to your registry credentials (defaults to ~/.docker/config.json)")
	flags.BoolVar(&o.Insecure, "insecure", o.Insecure, "Allow push and pull operations to registries to be made over HTTP")
	flags.BoolVar(&o.SkipVerification, "skip-verification", o.SkipVerification, "Skip verifying the integrity of the retrieved content. This is not recommended, but may be necessary when importing images from older image registries. Only bypass verification if the registry is known to be trustworthy.")
	flags.BoolVar(&o.LookupClusterICSP, "lookup-cluster-icsp", o.LookupClusterICSP, "If set to true, look for alternative image sources from ImageContentSourcePolicy objects in cluster, honor the ordering of those sources, and fail if an ImageContentSourcePolicy is not found in cluster. Cannot be set to true with --icsp-file")
	flags.StringVar(&o.ICSPFile, "icsp-file", o.ICSPFile, "Path to an ImageContentSourcePolicy file.  If set, data from this file will be used to set alternative image sources. Cannot be set together with --lookup-cluster-icsp=true.")
}

// ReferentialHTTPClient returns an http.Client that is appropriate for accessing
// blobs referenced outside of the registry (due to the present of the URLs attribute
// in the manifest reference for a layer).
func (o *SecurityOptions) ReferentialHTTPClient() (*http.Client, error) {
	regContext, err := o.Context()
	if err != nil {
		return nil, err
	}
	client := &http.Client{}
	if o.Insecure {
		client.Transport = regContext.InsecureTransport
	} else {
		client.Transport = regContext.Transport
	}
	return client, nil
}

func (o *SecurityOptions) Complete(f kcmdutil.Factory) error {
	if o.LookupClusterICSP && len(o.ICSPFile) > 0 {
		return fmt.Errorf("cannot set both --lookup-cluster-icsp=true and --icsp-file")
	}
	o.ICSPClientFn = func() (operatorv1alpha1client.ImageContentSourcePolicyInterface, error) {
		// If ImageContentSourceFile is given, only add ImageContentSource from file, don't search cluster ICSP
		if len(o.ICSPFile) != 0 {
			return nil, nil
		}
		restConfig, err := f.ToRESTConfig()
		if err != nil {
			// may or may not be connected to a cluster
			// don't error if can't connect
			klog.V(4).Infof("did not connect to an OpenShift 4.x server, will not lookup ImageContentSourcePolicies: %v", err)
			return nil, nil
		}
		icspClient, err := operatorv1alpha1client.NewForConfig(restConfig)
		if err != nil {
			// may or may not be connected to a cluster
			// don't error if can't connect
			klog.V(4).Infof("did not connect to an OpenShift 4.x server, will not lookup ImageContentSourcePolicies: %v", err)
			return nil, nil
		}
		return icspClient.ImageContentSourcePolicies(), nil
	}
	return nil
}

func (o *SecurityOptions) Context() (*registryclient.Context, error) {
	if o.CachedContext != nil {
		return o.CachedContext, nil
	}
	context, err := o.NewContext()
	if err == nil {
		o.CachedContext = context
		o.CachedContext.Retries = 3
	}
	return context, err
}

func (o *SecurityOptions) NewContext() (*registryclient.Context, error) {
	rt, err := rest.TransportFor(&rest.Config{})
	if err != nil {
		return nil, err
	}
	insecureRT, err := rest.TransportFor(&rest.Config{TLSClientConfig: rest.TLSClientConfig{Insecure: true}})
	if err != nil {
		return nil, err
	}
	creds := dockercredentials.NewLocal()
	if len(o.RegistryConfig) > 0 {
		creds, err = dockercredentials.NewFromFile(o.RegistryConfig)
		if err != nil {
			return nil, fmt.Errorf("unable to load --registry-config: %v", err)
		}
	}
	context := registryclient.NewContext(rt, insecureRT).WithCredentials(creds)
	context.DisableDigestVerification = o.SkipVerification
	return context, nil
}

// AddICSPsFromCluster will lookup ICSPs from cluster. Since it's not a hard requirement to find ICSPs from cluster, logs errors rather than returning errors.
func (a *SecurityOptions) AddICSPsFromCluster() error {
	icspClient, err := a.ICSPClientFn()
	if err != nil {
		return err
	}
	if a.LookupClusterICSP && icspClient == nil {
		return fmt.Errorf("flag --lookup-cluster-icsp was set to true, but no method was set to find ImageContentSourcePolicy objects in cluster")
	}
	if icspClient != nil {
		icsps, err := icspClient.List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			if a.LookupClusterICSP {
				return fmt.Errorf("--lookup-cluster-icsp was set to true, but did not access ImageContentSourcePolicy objects in cluster: %v", err)
			}
			// may or may not have access to ICSPs in cluster
			// don't error if can't access ICSPs
			klog.V(4).Infof("did not access any ImageContentSourcePolicies in cluster: %v", err)
		}
		if len(icsps.Items) == 0 {
			if a.LookupClusterICSP {
				return fmt.Errorf("--lookup-cluster-icsp was set to true, but no ImageContentSourcePolicy objects found in cluster: %v", err)
			}
			klog.V(4).Info("no ImageContentSourcePolicies found in cluster")
		}
		a.ICSPList = append(a.ICSPList, icsps.Items...)
	}
	return nil
}

func (a *SecurityOptions) AddImageSourcePoliciesFromFile() error {
	icspData, err := ioutil.ReadFile(a.ICSPFile)
	if err != nil {
		return fmt.Errorf("unable to read ImageContentSourceFile %s: %v", a.ICSPFile, err)
	}
	if len(icspData) == 0 {
		return fmt.Errorf("no data found in ImageContentSourceFile %s", a.ICSPFile)
	}
	icspObj, err := runtime.Decode(operatorv1alpha1scheme.Codecs.UniversalDeserializer(), icspData)
	if err != nil {
		return fmt.Errorf("error decoding ImageContentSourcePolicy from %s: %v", a.ICSPFile, err)
	}
	var icsp *operatorv1alpha1.ImageContentSourcePolicy
	var ok bool
	if icsp, ok = icspObj.(*operatorv1alpha1.ImageContentSourcePolicy); !ok {
		return fmt.Errorf("could not decode ImageContentSourcePolicy from %s", a.ICSPFile)
	}
	a.ICSPList = append(a.ICSPList, *icsp)
	return nil
}

func (a *SecurityOptions) AddImageSources(imageRef imagereference.DockerImageReference) ([]imagereference.DockerImageReference, error) {
	var imageSources []imagereference.DockerImageReference
	for _, icsp := range a.ICSPList {
		repoDigestMirrors := icsp.Spec.RepositoryDigestMirrors
		var sourceMatches bool
		for _, rdm := range repoDigestMirrors {
			rdmRef, err := imagereference.Parse(rdm.Source)
			if err != nil {
				return nil, err
			}
			if imageRef.AsRepository() == rdmRef.AsRepository() {
				klog.V(2).Infof("%v RepositoryDigestMirrors source matches given image", imageRef.AsRepository())
				sourceMatches = true
			}
			for _, m := range rdm.Mirrors {
				if sourceMatches {
					klog.V(2).Infof("%v RepositoryDigestMirrors mirror added to potential ImageSourcePrefixes from ImageContentSourcePolicy", m)
					mRef, err := imagereference.Parse(m)
					if err != nil {
						return nil, err
					}
					imageSources = append(imageSources, mRef)
				}
			}
		}
	}
	uniqueMirrors := make([]imagereference.DockerImageReference, 0, len(imageSources))
	uniqueMap := make(map[imagereference.DockerImageReference]bool)
	for _, imageSourceMirror := range imageSources {
		if _, ok := uniqueMap[imageSourceMirror]; !ok {
			uniqueMap[imageSourceMirror] = true
			uniqueMirrors = append(uniqueMirrors, imageSourceMirror)
		}
	}
	// make sure at least 1 imagesource
	// ie, make sure the image passed is included in image sources
	// this is so the user-given image ref will be tried
	if len(imageSources) == 0 {
		imageSources = append(imageSources, imageRef.AsRepository())
		return imageSources, nil
	}
	klog.V(2).Infof("Found sources: %v for image: %v", uniqueMirrors, imageRef)
	return uniqueMirrors, nil
}

func (s *SecurityOptions) PreferredImageSource(image imagereference.DockerImageReference, regContext *registryclient.Context) (imagereference.DockerImageReference, distribution.Repository, distribution.ManifestService, error) {
	var (
		repo          distribution.Repository
		replacedImage imagereference.DockerImageReference
		manifests     distribution.ManifestService
		err           error
	)
	ctx := context.TODO()
	typedImageRef := imagesource.TypedImageReference{Ref: image, Type: imagesource.DestinationRegistry}

	sourceOpts := &imagesource.Options{
		FileDir:         s.FileDir,
		Insecure:        s.Insecure,
		RegistryContext: regContext,
	}
	if len(s.ICSPFile) == 0 && !s.LookupClusterICSP && !s.TryAlternativeSources {
		repo, manifests, err = sourceOpts.RepositoryWithManifests(ctx, typedImageRef)
		if err != nil {
			return imagereference.DockerImageReference{}, nil, nil, err
		}
		if repo == nil || manifests == nil {
			return image, nil, nil, fmt.Errorf("unable to retrieve image manifests for %v", image.String())
		}
		return image, repo, manifests, nil
	}

	var altSources []imagereference.DockerImageReference
	// always error if given an icsp file and don't successfully connect to image repository
	if len(s.ICSPFile) > 0 {
		if err = s.AddImageSourcePoliciesFromFile(); err != nil {
			return imagereference.DockerImageReference{}, nil, nil, err
		}
		altSources, err = s.AddImageSources(image)
		if err != nil {
			return imagereference.DockerImageReference{}, nil, nil, err
		}
		for _, icsRef := range altSources {
			replacedImage = replaceImage(icsRef, image)
			// if not successful, error will be handled below
			if repo, manifests, err = sourceOpts.RepositoryWithManifests(context.TODO(), imagesource.TypedImageReference{Ref: replacedImage, Type: imagesource.DestinationRegistry}); err == nil {
				if repo == nil || manifests == nil {
					return image, nil, nil, fmt.Errorf("unable to retrieve image manifests for %v", image.String())
				}
				return replacedImage, repo, manifests, nil
			}
		}
	}

	// always error if user passed flag to look in cluster and didn't connect to image repository
	if s.LookupClusterICSP {
		// now try to look for ICSPs from cluster
		if s.ICSPClientFn == nil {
			return imagereference.DockerImageReference{}, nil, nil, fmt.Errorf("unable to find ImageContentSourcePolicy object from cluster")
		}
		if err = s.AddICSPsFromCluster(); err != nil {
			return imagereference.DockerImageReference{}, nil, nil, err
		}
		altSources, err = s.AddImageSources(image)
		if err != nil {
			return imagereference.DockerImageReference{}, nil, nil, err
		}
		for _, icsRef := range altSources {
			replacedImage = replaceImage(icsRef, image)
			repo, manifests, err = sourceOpts.RepositoryWithManifests(context.TODO(), imagesource.TypedImageReference{Ref: replacedImage, Type: imagesource.DestinationRegistry})
			if err == nil && repo != nil && manifests != nil {
				return replacedImage, repo, manifests, nil
			}
		}
	}

	// now implicitly try other sources, only if TryAlternativeSources set
	if s.TryAlternativeSources {
		if s.ICSPClientFn == nil {
			repo, manifests, err = sourceOpts.RepositoryWithManifests(context.TODO(), typedImageRef)
			if err == nil && repo != nil && manifests != nil {
				return typedImageRef.Ref, repo, manifests, nil
			}
		}
		if err = s.AddICSPsFromCluster(); err != nil {
			return imagereference.DockerImageReference{}, nil, nil, err
		}
		altSources, err = s.AddImageSources(image)
		if err != nil {
			return imagereference.DockerImageReference{}, nil, nil, err
		}
		for _, icsRef := range altSources {
			replacedImage = replaceImage(icsRef, image)
			repo, manifests, err = sourceOpts.RepositoryWithManifests(context.TODO(), imagesource.TypedImageReference{Ref: replacedImage, Type: imagesource.DestinationRegistry})
			if err == nil && repo != nil && manifests != nil {
				return replacedImage, repo, manifests, nil
			}
		}
	}
	if err != nil {
		return imagereference.DockerImageReference{}, nil, nil, fmt.Errorf("unable to connect to imagerepository %s: %v", image.String(), err)
	}
	return replacedImage, repo, manifests, err
}

func replaceImage(icsRef imagereference.DockerImageReference, image imagereference.DockerImageReference) imagereference.DockerImageReference {
	icsRef.ID = image.ID
	icsRef.Tag = image.Tag
	return icsRef
}
