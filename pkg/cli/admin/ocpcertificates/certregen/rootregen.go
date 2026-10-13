package certregen

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/certs/cert-inspection/certgraphanalysis"
	"github.com/openshift/library-go/pkg/operator/certrotation"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	RegenerateSignersFieldManager = "regenerate-signers"
)

type RootsRegen struct {
	ValidBefore *time.Time
}

// split here for convenience of unit testing
func (o *RootsRegen) forceRegenerationOnSecret(objPrinter *objectPrinter, kubeClient kubernetes.Interface, secret *corev1.Secret, dryRun bool) error {
	if !IsPlatformCertSecret(secret) {
		// TODO this should return an error if the name was specified.
		// otherwise, not for this command.
		return nil
	}

	keyPairInfos, err := certgraphanalysis.InspectSecret(secret)
	if err != nil {
		return fmt.Errorf("error interpretting content: %w", err)
	}

	if len(keyPairInfos) == 0 {
		return fmt.Errorf("no key pairs found for secret")
	}

	keyPairInfo := keyPairInfos[0]
	if keyPairInfo.Spec.Details.SignerDetails == nil {
		// not for this command.
		return nil
	}
	issuerInfo := keyPairInfo.Spec.CertMetadata.CertIdentifier.Issuer
	if issuerInfo == nil {
		// not for this command.
		return nil
	}

	if issuerInfo.CommonName != keyPairInfo.Spec.CertMetadata.CertIdentifier.CommonName {
		// not for this command, we only want self-signed signers.
		//fmt.Printf("#### SKIPPING ns/%v secret/%v issuer=%v\n", secret.Namespace, secret.Name, keyPairInfo.Spec.CertMetadata.CertIdentifier.Issuer)
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
		FieldManager: RegenerateSignersFieldManager,
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
