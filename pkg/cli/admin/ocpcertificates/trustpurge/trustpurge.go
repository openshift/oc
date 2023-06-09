package trustpurge

import (
	"context"
	"crypto/x509"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/kubernetes"
	certutil "k8s.io/client-go/util/cert"

	"github.com/openshift/library-go/pkg/operator/certrotation"
)

var (
	configMapKind = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
)

const (
	RemoveOldTrustFieldManager = "remove-old-trust"
)

type RemoveOldTrustRuntime struct {
	ResourceFinder genericclioptions.ResourceFinder
	KubeClient     kubernetes.Interface

	dryRun        bool
	createdBefore time.Time

	Printer printers.ResourcePrinter

	genericclioptions.IOStreams
}

func (r *RemoveOldTrustRuntime) Run(ctx context.Context) error {
	visitor := r.ResourceFinder.Do()

	// TODO need to wire context through the visitorFns
	err := visitor.Visit(r.purgeTrustFromResourceInfo)
	if err != nil {
		return err
	}
	return nil
}

func (r *RemoveOldTrustRuntime) purgeTrustFromResourceInfo(info *resource.Info, err error) error {
	if err != nil {
		return err
	}
	// TODO: can we just hardcode the kind and not take it as an argument?
	if configMapKind != info.Object.GetObjectKind().GroupVersionKind() {
		return fmt.Errorf("command must only be pointed at configMaps")
	}

	uncastObj, ok := info.Object.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("not unstructured: %w", err)
	}
	configMap := &corev1.ConfigMap{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uncastObj.Object, configMap); err != nil {
		return fmt.Errorf("not a secret: %w", err)
	}

	return r.purgeTrustFromConfigMap(configMap)
}

func (r *RemoveOldTrustRuntime) purgeTrustFromConfigMap(cm *corev1.ConfigMap) error {
	if cm.Labels[certrotation.ManagedCertificateTypeLabelName] != "ca-bundle" {
		return nil
	}

	caBundle := cm.Data["ca-bundle.crt"]
	if len(caBundle) == 0 {
		// somebody was faster
		fmt.Fprintf(r.ErrOut, "the 'ca-bundle.crt' key of %s/%s was empty", cm.Namespace, cm.Name)
		return nil
	}

	newCMData := map[string]string{
		"ca-bundle.crt": "",
	}
	if !r.createdBefore.IsZero() {
		certs, err := certutil.ParseCertsPEM([]byte(caBundle))
		if err != nil {
			return fmt.Errorf("failed to parse certificates of the %s/%s configMap: %v", cm.Namespace, cm.Name, err)
		}

		newCerts := make([]*x509.Certificate, 0, len(certs))
		for i, cert := range certs {
			if cert.NotBefore.After(r.createdBefore) {
				newCerts = append(newCerts, certs[i])
			}
		}

		if len(certs) == len(newCerts) {
			return nil
		}
		newPEMBundle, err := certutil.EncodeCertificates(newCerts...)
		if err != nil {
			return fmt.Errorf("failed to PEM-encode certs for configMap %s/%s: %v", cm.Namespace, cm.Name, err)
		}
		newCMData["ca-bundle.crt"] = string(newPEMBundle)
	}

	applyOptions := metav1.ApplyOptions{
		Force:        true,
		FieldManager: RemoveOldTrustFieldManager,
	}

	if r.dryRun {
		applyOptions.DryRun = []string{metav1.DryRunAll}
	}

	applyCM := applycorev1.ConfigMap(cm.Name, cm.Namespace)
	applyCM = applyCM.WithData(newCMData)

	finalObj, err := r.KubeClient.CoreV1().ConfigMaps(cm.Namespace).Apply(context.TODO(), applyCM, applyOptions)
	if err != nil {
		return err
	}

	finalObj.GetObjectKind().SetGroupVersionKind(configMapKind)
	return r.Printer.PrintObj(finalObj, r.Out)
}
