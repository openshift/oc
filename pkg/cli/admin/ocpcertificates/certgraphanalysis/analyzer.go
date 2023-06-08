package certgraphanalysis

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/oc/pkg/cli/admin/ocpcertificates/certgraphapi"
	"k8s.io/client-go/util/cert"
)

func InspectSecret(obj *corev1.Secret) (*certgraphapi.CertKeyPair, error) {
	resourceString := fmt.Sprintf("secrets/%s[%s]", obj.Name, obj.Namespace)
	tlsCrt, isTLS := obj.Data["tls.crt"]
	if !isTLS {
		return nil, nil
	}
	//fmt.Printf("%s - tls (%v)\n", resourceString, obj.CreationTimestamp.UTC())
	if len(tlsCrt) == 0 {
		return nil, fmt.Errorf("%s MISSING tls.crt content\n", resourceString)
	}

	certificates, err := cert.ParseCertsPEM([]byte(tlsCrt))
	if err != nil {
		return nil, err
	}
	for _, certificate := range certificates {
		detail, err := toCertKeyPair(certificate)
		if err != nil {
			return nil, err
		}
		detail = addSecretLocation(detail, obj.Namespace, obj.Name)
		return detail, nil
	}
	return nil, fmt.Errorf("didn't see that coming")
}
