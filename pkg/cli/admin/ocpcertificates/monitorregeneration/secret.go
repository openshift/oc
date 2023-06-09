package monitorregeneration

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/openshift/library-go/pkg/operator/certrotation"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (o *MonitorCertificatesRuntime) createSecret(obj interface{}, isFirstSync bool) {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		fmt.Fprintf(o.IOStreams.ErrOut, "unexpected create obj %T", obj)
		return
	}

	if oldObj, _ := o.interestingSecrets.get(secret.Namespace, secret.Name); oldObj != nil {
		o.updateSecret(obj, oldObj)
		return
	}

	// not all replaces are the same.  we only really want to skip this on the first attempt
	if !isFirstSync {
		if isSAToken(secret) {
			fmt.Fprintf(o.IOStreams.Out, "secrets/%v[%v] -- serviceaccount token created\n", secret.Name, secret.Namespace)
		}
		if isDockerPullSecret(secret) {
			fmt.Fprintf(o.IOStreams.Out, "secrets/%v[%v] -- docker pull secret created\n", secret.Name, secret.Namespace)
		}
	}

	o.interestingSecrets.upsert(secret.Namespace, secret.Name, secret)
}

func (o *MonitorCertificatesRuntime) updateSecret(obj, oldObj interface{}) {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		fmt.Fprintf(o.IOStreams.ErrOut, "unexpected update obj %T", obj)
		return
	}
	defer o.interestingSecrets.upsert(secret.Namespace, secret.Name, secret)

	oldSecret, ok := oldObj.(*corev1.Secret)
	if !ok {
		fmt.Fprintf(o.IOStreams.ErrOut, "unexpected update oldObj %T", oldObj)
		return
	}

	// we skip revisions because their information is not unique
	if isForRevision(secret.OwnerReferences) {
		return
	}

	o.handleTLSSecret(secret, oldSecret)
}

func (o *MonitorCertificatesRuntime) handleTLSSecret(secret, oldSecret *corev1.Secret) {
	oldTLS, oldHasTLS := oldSecret.Data["tls.crt"]
	newTLS, newHasTLS := secret.Data["tls.crt"]
	if oldHasTLS && newHasTLS {
		if !reflect.DeepEqual(oldTLS, newTLS) {
			fmt.Fprintf(o.IOStreams.Out, "secrets/%v[%v] -- updated certificate, now expires at %v\n", secret.Name, secret.Namespace, secret.Annotations[certrotation.CertificateNotAfterAnnotation])
		}
	}

	_, oldHasNotAfter := oldSecret.Annotations[certrotation.CertificateNotAfterAnnotation]
	_, newHasNotAfter := secret.Annotations[certrotation.CertificateNotAfterAnnotation]
	if oldHasNotAfter && newHasNotAfter {
		oldRegenerating := oldSecret.Annotations[certrotation.CertificateNotAfterAnnotation] == "force-regeneration"
		newRegenerating := secret.Annotations[certrotation.CertificateNotAfterAnnotation] == "force-regeneration"
		switch {
		case oldRegenerating && !newRegenerating:
			fmt.Fprintf(o.IOStreams.Out, "secrets/%v[%v] -- finished regeneration\n", secret.Name, secret.Namespace)
		case !oldRegenerating && newRegenerating:
			fmt.Fprintf(o.IOStreams.Out, "secrets/%v[%v] -- started regeneration\n", secret.Name, secret.Namespace)
		}
	}
}

func (o *MonitorCertificatesRuntime) handleSAToken(secret, oldSecret *corev1.Secret) {
	if isSAToken(secret) {
		return
	}

	oldToken, oldHasToken := oldSecret.Data["token"]
	newToken, newHasToken := secret.Data["token"]
	switch {
	case oldHasToken && !newHasToken:
		fmt.Fprintf(o.IOStreams.Out, "secrets/%v[%v] -- serviceaccount token started regeneration\n", secret.Name, secret.Namespace)
	case oldHasToken && newHasToken:
		if !reflect.DeepEqual(newToken, oldToken) {
			fmt.Fprintf(o.IOStreams.Out, "secrets/%v[%v] -- serviceaccount token updated\n", secret.Name, secret.Namespace)
		}
	case !oldHasToken && !newHasToken:
	case !oldHasToken && newHasToken && len(newToken) > 0:
		fmt.Fprintf(o.IOStreams.Out, "secrets/%v[%v] -- serviceaccount token finished regeneration\n", secret.Name, secret.Namespace)
	}

	oldCA := oldSecret.Data["ca.crt"]
	newCA := oldSecret.Data["ca.crt"]
	if !reflect.DeepEqual(oldCA, newCA) {
		fmt.Fprintf(o.IOStreams.Out, "secrets/%v[%v] -- serviceaccount kube-apiserver trust bundle updated\n", secret.Name, secret.Namespace)
	}
}

func isSAToken(secret *corev1.Secret) bool {
	return secret.Type == corev1.SecretTypeServiceAccountToken
}

func isDockerPullSecret(secret *corev1.Secret) bool {
	if _, hasDockerCfg := secret.Data[".dockercfg"]; hasDockerCfg {
		return true
	}
	if _, isDockerPullSecret := secret.Annotations["openshift.io/token-secret.name"]; isDockerPullSecret {
		return true
	}
	return false
}

func isForRevision(ownerReferences []metav1.OwnerReference) bool {
	for _, ownerReference := range ownerReferences {
		if strings.HasPrefix(ownerReference.Name, "revision-status-") {
			return true
		}
	}

	return false
}

func (o *MonitorCertificatesRuntime) deleteSecret(obj interface{}) {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		fmt.Fprintf(o.IOStreams.ErrOut, "unexpected create obj %T", obj)
		return
	}

	if isSAToken(secret) {
		fmt.Fprintf(o.IOStreams.Out, "secrets/%v[%v] -- serviceaccount token deleted\n", secret.Name, secret.Namespace)
	}
	if isDockerPullSecret(secret) {
		fmt.Fprintf(o.IOStreams.Out, "secrets/%v[%v] -- docker pull secret deleted\n", secret.Name, secret.Namespace)
	}

	o.interestingSecrets.remove(secret.Namespace, secret.Name)
}
