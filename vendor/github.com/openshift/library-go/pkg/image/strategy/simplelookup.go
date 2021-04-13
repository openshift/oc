package strategy

import (
	"context"
	"fmt"
	"io/ioutil"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	operatorv1alpha1scheme "github.com/openshift/client-go/operator/clientset/versioned/scheme"
	operatorv1alpha1client "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1alpha1"
	reference "github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/library-go/pkg/image/registryclient"
)

// simpleLookupICSP holds ImageContentSourcePolicy variables to look up image sources
// satisfies *Context AlternativeBlobSourceStrategy interface
type simpleLookupICSP struct {
	lock sync.Mutex

	alternates []reference.DockerImageReference
	icspFile   string
	icspClient operatorv1alpha1client.ImageContentSourcePolicyInterface
}

func NewSimpleLookupICSPStrategy(file string, client operatorv1alpha1client.ImageContentSourcePolicyInterface) registryclient.AlternateBlobSourceStrategy {
	return &simpleLookupICSP{
		icspFile:   file,
		icspClient: client,
	}
}

func (s *simpleLookupICSP) FirstRequest(ctx context.Context, locator reference.DockerImageReference) (alternateRepositories []reference.DockerImageReference, err error) {
	return nil, nil
}

// OnFailure returns a list of possible image references for the locator image reference, gathered from image content source policies, or an error
func (s *simpleLookupICSP) OnFailure(ctx context.Context, locator reference.DockerImageReference) (alternateRepositories []reference.DockerImageReference, err error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if err := s.resolve(ctx, locator); err != nil {
		return nil, err
	}
	if len(s.alternates) == 0 {
		return nil, fmt.Errorf("no alternative image references found for image: %s", locator.String())
	}
	return s.alternates, nil
}

// addICSPsFromCluster will lookup ImageContentSourcePolicy resources in cluster.
func (s *simpleLookupICSP) addICSPsFromCluster(ctx context.Context) ([]operatorv1alpha1.ImageContentSourcePolicy, error) {
	if s.icspClient == nil {
		return nil, fmt.Errorf("no client to access ImageContentSourcePolicies in cluster")
	}
	icsps, err := s.icspClient.List(ctx, metav1.ListOptions{})
	if err != nil {
		// may or may not have access to ICSPs in cluster
		// don't error if can't access ICSPs
		return nil, fmt.Errorf("did not access any ImageContentSourcePolicies in cluster: %v", err)
	}
	if len(icsps.Items) == 0 {
		return nil, fmt.Errorf("no ImageContentSourcePolicies found in cluster")
	}
	return icsps.Items, nil
}

// addICSPsFromFile appends to list of alternative image sources from ICSP file
// returns error if no icsp object decoded from file data
func (s *simpleLookupICSP) addICSPsFromFile() ([]operatorv1alpha1.ImageContentSourcePolicy, error) {
	icspData, err := ioutil.ReadFile(s.icspFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read ImageContentSourceFile %s: %v", s.icspFile, err)
	}
	if len(icspData) == 0 {
		return nil, fmt.Errorf("no data found in ImageContentSourceFile %s", s.icspFile)
	}
	icspObj, err := runtime.Decode(operatorv1alpha1scheme.Codecs.UniversalDeserializer(), icspData)
	if err != nil {
		return nil, fmt.Errorf("error decoding ImageContentSourcePolicy from %s: %v", s.icspFile, err)
	}
	icsp, ok := icspObj.(*operatorv1alpha1.ImageContentSourcePolicy)
	if !ok {
		return nil, fmt.Errorf("could not decode ImageContentSourcePolicy from %s", s.icspFile)
	}
	return []operatorv1alpha1.ImageContentSourcePolicy{*icsp}, nil
}

// alternativeImageSources returns unique list of DockerImageReference objects from list of ImageContentSourcePolicy objects
func (s *simpleLookupICSP) alternativeImageSources(imageRef reference.DockerImageReference, icspList []operatorv1alpha1.ImageContentSourcePolicy) ([]reference.DockerImageReference, error) {
	var imageSources []reference.DockerImageReference
	// make sure at least 1 imagesource
	// ie, make sure the image passed is included in image sources
	// this is so the user-given image ref will be tried
	imageSources = append(imageSources, imageRef.AsRepository())
	klog.V(2).Infof("%v ImageReference added to potential ImageSourcePrefixes from ImageContentSourcePolicy", imageRef.AsRepository())
	for _, icsp := range icspList {
		repoDigestMirrors := icsp.Spec.RepositoryDigestMirrors
		for _, rdm := range repoDigestMirrors {
			var err error
			rdmSourceRef, err := reference.Parse(rdm.Source)
			if err != nil {
				return nil, err
			}
			if imageRef.AsRepository() != rdmSourceRef.AsRepository() {
				continue
			}
			klog.V(2).Infof("%v RepositoryDigestMirrors source matches given image", imageRef.AsRepository())
			for _, m := range rdm.Mirrors {
				mRef, err := reference.Parse(m)
				if err != nil {
					return nil, err
				}
				imageSources = append(imageSources, mRef)
				klog.V(2).Infof("%v RepositoryDigestMirrors mirror added to potential ImageSourcePrefixes from ImageContentSourcePolicy", m)
			}
		}
	}
	uniqueMirrors := make([]reference.DockerImageReference, 0, len(imageSources))
	uniqueMap := make(map[reference.DockerImageReference]bool)
	for _, imageSourceMirror := range imageSources {
		if _, ok := uniqueMap[imageSourceMirror]; !ok {
			uniqueMap[imageSourceMirror] = true
			uniqueMirrors = append(uniqueMirrors, imageSourceMirror)
		}
	}
	klog.V(2).Infof("Found sources: %v for image: %v", uniqueMirrors, imageRef)
	return uniqueMirrors, nil
}

// resolve gathers possible image sources for a given image
// gathered from ImageContentSourcePolicy objects and user-passed image.
// Will lookup from cluster or from ImageContentSourcePolicy file passed from user.
// Image reference of user-given image may be different from original in case of mirrored images.
func (s *simpleLookupICSP) resolve(ctx context.Context, imageRef reference.DockerImageReference) error {
	var icspList []operatorv1alpha1.ImageContentSourcePolicy
	var err error
	if len(s.icspFile) > 0 {
		icspList, err = s.addICSPsFromFile()
		if err != nil {
			return err
		}
	} else {
		// log errors from accessing cluster since this function has no way of knowing whether it should or should not succeed
		icspList, err = s.addICSPsFromCluster(ctx)
		if err != nil {
			klog.V(4).Infof("No alternative image sources gathered from cluster: %v", err)
		}
	}
	imageRefList, err := s.alternativeImageSources(imageRef, icspList)
	if err != nil {
		return err
	}
	s.alternates = imageRefList
	return nil
}
