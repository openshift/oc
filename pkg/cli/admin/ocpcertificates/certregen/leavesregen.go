package certregen

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/certs/cert-inspection/certgraphanalysis"
	"github.com/openshift/library-go/pkg/operator/certrotation"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	RegenerateLeafCertsFieldManager = "regenerate-leaf-certificates"
)

type LeavesRegen struct {
	ValidBefore *time.Time
}

// split here for convenience of unit testing
func (o *LeavesRegen) forceRegenerationOnSecret(objPrinter *objectPrinter, kubeClient kubernetes.Interface, secret *corev1.Secret, dryRun bool) error {
	if isLeaf, err := IsLeafCertSecret(secret); err != nil {
		return err
	} else if !isLeaf {
		return nil
	}

	if o.ValidBefore != nil {
		notBefore, err := time.Parse(time.RFC3339, secret.Annotations[certrotation.CertificateNotBeforeAnnotation])
		if err != nil {
			return fmt.Errorf("error parsing notBefore: %w", err)
		}
		if notBefore.After(*o.ValidBefore) {
			// not for us
			return nil
		}
	}

	applyOptions := metav1.ApplyOptions{
		Force:        true,
		FieldManager: RegenerateLeafCertsFieldManager,
	}
	if dryRun {
		applyOptions.DryRun = []string{metav1.DryRunAll}
	}

	secretToApply := applycorev1.Secret(secret.Name, secret.Namespace)
	secretToApply.WithAnnotations(map[string]string{
		certrotation.CertificateNotAfterAnnotation: "force-regeneration",
	})

	finalObject, err := kubeClient.CoreV1().Secrets(secret.Namespace).Apply(context.TODO(), secretToApply, applyOptions)
	if err != nil {
		return err
	}

	// required for printing
	finalObject.GetObjectKind().SetGroupVersionKind(secretKind)
	if err := objPrinter.printObject(finalObject); err != nil {
		return err
	}

	return err
}

func isRevisioned(meta metav1.ObjectMeta) bool {
	for _, ref := range meta.OwnerReferences {
		if ref.Kind == "ConfigMap" && strings.HasPrefix(ref.Name, "revision-status-") {
			return true
		}
	}

	return false
}

func IsPlatformCertSecret(s *corev1.Secret) bool {
	return len(s.Annotations[certrotation.CertificateIssuer]) != 0 &&
		len(s.Annotations[certrotation.CertificateNotBeforeAnnotation]) != 0 &&
		!isRevisioned(s.ObjectMeta)
}

func IsLeafCertSecret(s *corev1.Secret) (bool, error) {
	if !IsPlatformCertSecret(s) {
		return false, nil
	}

	keyPairInfos, err := certgraphanalysis.InspectSecret(s)
	if err != nil {
		return false, fmt.Errorf("error interpretting content: %w", err)
	}

	if len(keyPairInfos) == 0 {
		return false, fmt.Errorf("no key pair from secret found")
	}

	keyPairInfo := keyPairInfos[0]
	if keyPairInfo.Spec.Details.SignerDetails != nil {
		// not for this command.
		return false, nil
	}

	issuerInfo := keyPairInfo.Spec.CertMetadata.CertIdentifier.Issuer
	if issuerInfo == nil {
		// what are you??
		return false, nil
	}

	if issuerInfo.CommonName == keyPairInfo.Spec.CertMetadata.CertIdentifier.CommonName {
		// not for this command, we only want leaf certs
		return false, nil
	}

	return true, nil
}
