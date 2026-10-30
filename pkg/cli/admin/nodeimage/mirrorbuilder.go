package nodeimage

import (
	"fmt"
	"io"
	"strings"

	ocpv1 "github.com/openshift/api/config/v1"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
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
			skippedCount := 0
			totalCount := len(idms.Spec.ImageDigestMirrors)
			for _, digestMirror := range idms.Spec.ImageDigestMirrors {
				if strings.HasPrefix(digestMirror.Source, "*.") {
					skippedCount++
					continue
				}
				imageDigestSet.Spec.ImageDigestMirrors = append(imageDigestSet.Spec.ImageDigestMirrors, digestMirror)
			}
			if skippedCount > 0 && skippedCount == totalCount {
				return nil, fmt.Errorf("ImageDigestMirrorSet %q contains only wildcard sources which are not supported by node-image image resolution: all %d entries were skipped", idms.Name, totalCount)
			}
			if skippedCount > 0 {
				log(writer, "Warning: skipped %d wildcard source(s) from ImageDigestMirrorSet %q: wildcard sources are not supported by node-image image resolution. The remaining %d mirror(s) may not be sufficient for disconnected operations", skippedCount, idms.Name, totalCount-skippedCount)
			}
		}
	}

	// Add mirrors from ICSP
	if icspMirrorSetList != nil {
		log(writer, "Adding mirrors from ImageContentSourcePolicies")
		for _, icsp := range icspMirrorSetList.Items {
			skippedCount := 0
			totalCount := len(icsp.Spec.RepositoryDigestMirrors)
			for _, digestMirror := range icsp.Spec.RepositoryDigestMirrors {
				if strings.HasPrefix(digestMirror.Source, "*.") {
					skippedCount++
					continue
				}
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
			if skippedCount > 0 && skippedCount == totalCount {
				return nil, fmt.Errorf("ImageContentSourcePolicy %q contains only wildcard sources which are not supported by node-image image resolution: all %d entries were skipped", icsp.Name, totalCount)
			}
			if skippedCount > 0 {
				log(writer, "Warning: skipped %d wildcard source(s) from ImageContentSourcePolicy %q: wildcard sources are not supported by node-image image resolution. The remaining %d mirror(s) may not be sufficient for disconnected operations", skippedCount, icsp.Name, totalCount-skippedCount)
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
