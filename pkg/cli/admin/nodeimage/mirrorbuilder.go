package nodeimage

import (
	"fmt"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	"io"

	ocpv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func getIdmsContents(writer io.Writer, idmsMirrorSetList *ocpv1.ImageDigestMirrorSetList, icspMirrorSetList *operatorv1alpha1.ImageContentSourcePolicyList) ([]byte, error) {
	imageDigestSet := ocpv1.ImageDigestMirrorSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: ocpv1.GroupVersion.String(),
			Kind:       "ImageDigestMirrorSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "image-digest",
			// not namespaced
		},
	}

	// Add mirrors from IDMS
	if idmsMirrorSetList != nil {
		log(writer, "Adding mirrors from ImageDigestMirrors")
		for _, idms := range idmsMirrorSetList.Items {
			for _, digestMirror := range idms.Spec.ImageDigestMirrors {
				imageDigestSet.Spec.ImageDigestMirrors = append(imageDigestSet.Spec.ImageDigestMirrors, digestMirror)
			}
		}
	}

	// Add mirrors from ICSP
	if icspMirrorSetList != nil {
		log(writer, "Adding mirrors from ImageContentSourcePolicies")
		for _, icsp := range icspMirrorSetList.Items {
			for _, digestMirror := range icsp.Spec.RepositoryDigestMirrors {
				// Skip adding the mirror if the source already exists
				if digestSourceExists(imageDigestSet.Spec.ImageDigestMirrors, digestMirror.Source) {
					continue
				}
				mirror := ocpv1.ImageDigestMirrors{
					Source: digestMirror.Source,
				}
				for _, m := range digestMirror.Mirrors {
					mirror.Mirrors = append(mirror.Mirrors, ocpv1.ImageMirror(m))
				}
				imageDigestSet.Spec.ImageDigestMirrors = append(imageDigestSet.Spec.ImageDigestMirrors, mirror)
			}
		}
	}

	contents, err := yaml.Marshal(imageDigestSet)
	if err != nil {
		return nil, err
	}

	return contents, nil
}

func digestSourceExists(mirrors []ocpv1.ImageDigestMirrors, source string) bool {
	for _, mirror := range mirrors {
		if mirror.Source == source {
			return true
		}
	}
	return false
}

func log(writer io.Writer, format string, a ...interface{}) {
	if writer != nil {
		fmt.Fprintf(writer, format+"\n", a...)
	}
}
