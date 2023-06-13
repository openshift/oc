package trustpurge

import (
	"context"
	"crypto/x509"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
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

	dryRun         bool
	createdBefore  time.Time
	excludeBundles map[string]sets.Set[string]

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
	var err error
	var finalObj *corev1.ConfigMap

	if excludedConfigMaps := r.excludeBundles[cm.Namespace]; excludedConfigMaps.Has(cm.Name) {
		return nil
	}

	for retriesLeft := 2; retriesLeft > 0; retriesLeft -= 1 {
		if cm.Labels[certrotation.ManagedCertificateTypeLabelName] != "ca-bundle" {
			return nil
		}

		caBundle := cm.Data["ca-bundle.crt"]
		if len(caBundle) == 0 {
			// somebody was faster
			return nil
		}

		cm.Data["ca-bundle.crt"] = ""
		if !r.createdBefore.IsZero() {
			newBundle, pruned, err := pruneCertBundle(r.createdBefore, caBundle)
			if err != nil {
				return fmt.Errorf("cert pruning failed for %s/%s", cm.Namespace, cm.Name)
			}
			if !pruned { // old == new
				return nil
			}
			cm.Data["ca-bundle.crt"] = newBundle
		}

		updateOptions := metav1.UpdateOptions{
			FieldManager: RemoveOldTrustFieldManager,
		}
		if r.dryRun {
			updateOptions.DryRun = []string{metav1.DryRunAll}
		}

		finalObj, err = r.KubeClient.CoreV1().ConfigMaps(cm.Namespace).Update(context.TODO(), cm, updateOptions)
		if err != nil {
			if apierrors.IsConflict(err) && retriesLeft > 0 {
				fmt.Fprintf(r.ErrOut, "error encountered applying configmap, retrying: %v", err)
				var getErr error
				cm, getErr = r.KubeClient.CoreV1().ConfigMaps(cm.Namespace).Get(context.TODO(), cm.Name, metav1.GetOptions{})
				if getErr != nil {
					return fmt.Errorf("failed to retrieve CM %s/%s upon reapplying: %v", cm.Namespace, cm.Name, getErr)
				}
				continue
			}
			return err
		}
		// no error, no need to repeat
		break
	}

	if err != nil {
		return fmt.Errorf("failed to apply changes to %s/%s: %v", cm.Namespace, cm.Name, err)
	}
	finalObj.GetObjectKind().SetGroupVersionKind(configMapKind)
	return r.Printer.PrintObj(finalObj, r.Out)
}

func pruneCertBundle(createdBefore time.Time, bundlePEM string) (string, bool, error) {
	certs, err := certutil.ParseCertsPEM([]byte(bundlePEM))
	if err != nil {
		return "", false, fmt.Errorf("failed to parse certificates: %v", err)
	}

	newCerts := make([]*x509.Certificate, 0, len(certs))
	for i, cert := range certs {
		if cert.NotBefore.After(createdBefore) {
			newCerts = append(newCerts, certs[i])
		}
	}

	if len(certs) == len(newCerts) {
		return bundlePEM, false, nil
	}
	newPEMBundle, err := certutil.EncodeCertificates(newCerts...)
	if err != nil {
		return "", false, fmt.Errorf("failed to PEM-encode certs: %v", err)
	}
	return string(newPEMBundle), true, nil
}
