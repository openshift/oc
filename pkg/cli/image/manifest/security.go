package manifest

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/spf13/pflag"

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
	"github.com/openshift/oc/pkg/cli/image/manifest/dockercredentials"
)

type SecurityOptions struct {
	RegistryConfig               string
	Insecure                     bool
	SkipVerification             bool
	ImageContentSourcePolicyFile string
	ImageContentSourcePolicyList []operatorv1alpha1.ImageContentSourcePolicy
	ICSPClientFn                 func() (operatorv1alpha1client.ImageContentSourcePolicyInterface, error)

	CachedContext *registryclient.Context
}

func (o *SecurityOptions) Bind(flags *pflag.FlagSet) {
	flags.StringVarP(&o.RegistryConfig, "registry-config", "a", o.RegistryConfig, "Path to your registry credentials (defaults to ~/.docker/config.json)")
	flags.BoolVar(&o.Insecure, "insecure", o.Insecure, "Allow push and pull operations to registries to be made over HTTP")
	flags.BoolVar(&o.SkipVerification, "skip-verification", o.SkipVerification, "Skip verifying the integrity of the retrieved content. This is not recommended, but may be necessary when importing images from older image registries. Only bypass verification if the registry is known to be trustworthy.")
	flags.StringVar(&o.ImageContentSourcePolicyFile, "icsp-file", o.ImageContentSourcePolicyFile, "Path to an ImageContentSourcePolicy file.  If set, data from this file will be used to set source release image.")
}

// ReferentialHTTPClient returns an http.Client that is appropriate for accessing
// blobs referenced outside of the registry (due to the present of the URLs attribute
// in the manifest reference for a layer).
func (o *SecurityOptions) ReferentialHTTPClient() (*http.Client, error) {
	ctx, err := o.Context(imagereference.DockerImageReference{})
	if err != nil {
		return nil, err
	}
	client := &http.Client{}
	if o.Insecure {
		client.Transport = ctx.InsecureTransport
	} else {
		client.Transport = ctx.Transport
	}
	return client, nil
}

func (o *SecurityOptions) AddImageSourcePoliciesFromFile(image string) error {
	if len(image) == 0 {
		return fmt.Errorf("expected image to find image sources")
	}
	icspData, err := ioutil.ReadFile(o.ImageContentSourcePolicyFile)
	if err != nil {
		return fmt.Errorf("unable to read ImageContentSourceFile %s: %v", o.ImageContentSourcePolicyFile, err)
	}
	if len(icspData) == 0 {
		return fmt.Errorf("no data found in ImageContentSourceFile %s", o.ImageContentSourcePolicyFile)
	}
	icspObj, err := runtime.Decode(operatorv1alpha1scheme.Codecs.UniversalDeserializer(), icspData)
	if err != nil {
		return fmt.Errorf("error decoding ImageContentSourcePolicy from %s: %v", o.ImageContentSourcePolicyFile, err)
	}
	var icsp *operatorv1alpha1.ImageContentSourcePolicy
	var ok bool
	if icsp, ok = icspObj.(*operatorv1alpha1.ImageContentSourcePolicy); !ok {
		return fmt.Errorf("could not decode ImageContentSourcePolicy from %s", o.ImageContentSourcePolicyFile)
	}
	o.ImageContentSourcePolicyList = append(o.ImageContentSourcePolicyList, *icsp)
	return nil

}

func (o *SecurityOptions) Complete(f kcmdutil.Factory, image string) error {
	o.ICSPClientFn = func() (operatorv1alpha1client.ImageContentSourcePolicyInterface, error) {
		// If ImageContentSourceFile is given, only add ImageContentSource from file, don't search cluster ICSP
		if len(o.ImageContentSourcePolicyFile) != 0 {
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

func (o *SecurityOptions) AddICSPsFromCluster() error {
	icspClient, err := o.ICSPClientFn()
	if err != nil {
		return err
	}
	if icspClient != nil {
		o.GetICSPs(icspClient)
	}
	return nil
}

// GetICSPs will lookup ICSPs from cluster. Since it's not a hard requirement to find ICSPs from cluster, GetICSPs logs errors rather than returning errors.
func (o *SecurityOptions) GetICSPs(icspClient operatorv1alpha1client.ImageContentSourcePolicyInterface) {
	icsps, err := icspClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		// may or may not have access to ICSPs in cluster
		// don't error if can't access ICSPs
		klog.V(4).Infof("did not access any ImageContentSourcePolicies in cluster: %v", err)
	}
	if len(icsps.Items) == 0 {
		klog.V(4).Info("no ImageContentSourcePolicies found in cluster")
	}
	o.ImageContentSourcePolicyList = append(o.ImageContentSourcePolicyList, icsps.Items...)
}

func (o *SecurityOptions) Context(image imagereference.DockerImageReference) (*registryclient.Context, error) {
	if o.CachedContext != nil {
		return o.CachedContext, nil
	}
	context, err := o.NewContext(image)
	if err == nil {
		o.CachedContext = context
		o.CachedContext.Retries = 3
	}
	return context, err
}

func (o *SecurityOptions) NewContext(image imagereference.DockerImageReference) (*registryclient.Context, error) {
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
	if len(image.String()) > 0 {
		context.AlternativeSources = &addAlternativeImageSources{}
		altSources, err := context.AlternativeSources.AddImageSources(image, o.ImageContentSourcePolicyList)
		if err != nil {
			return nil, err
		}
		context.ImageSources = altSources
	}
	return context, nil
}

type addAlternativeImageSources struct{}

func (a *addAlternativeImageSources) AddImageSources(imageRef imagereference.DockerImageReference, icspList []operatorv1alpha1.ImageContentSourcePolicy) ([]imagereference.DockerImageReference, error) {
	if len(icspList) == 0 {
		return nil, nil
	}
	var imageSources []imagereference.DockerImageReference
	for _, icsp := range icspList {
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
	// make sure at least 1 imagesource
	// ie, make sure the image passed is included in image sources
	if len(imageSources) == 0 {
		imageSources = append(imageSources, imageRef.AsRepository())
	}
	uniqueMirrors := make([]imagereference.DockerImageReference, 0, len(imageSources))
	uniqueMap := make(map[imagereference.DockerImageReference]bool)
	for _, imageSourceMirror := range imageSources {
		if _, ok := uniqueMap[imageSourceMirror]; !ok {
			uniqueMap[imageSourceMirror] = true
			uniqueMirrors = append(uniqueMirrors, imageSourceMirror)
		}
	}
	klog.V(2).Infof("Found sources: %v for image: %v", uniqueMirrors, imageRef)
	return uniqueMirrors, nil
}
