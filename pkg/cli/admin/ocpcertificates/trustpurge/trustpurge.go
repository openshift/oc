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
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/retry"

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
	var finalObj *corev1.ConfigMap

	cmNamespace, cmName := cm.Namespace, cm.Name
	if excludedConfigMaps := r.excludeBundles[cmNamespace]; excludedConfigMaps.Has(cmName) {
		return nil
	}

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var err error
		if cm == nil {
			cm, err = r.KubeClient.CoreV1().ConfigMaps(cmNamespace).Get(context.TODO(), cmName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to retrieve CM %s/%s upon reapplying: %v", cmNamespace, cmName, err)
			}
		}

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
				return fmt.Errorf("cert pruning failed for %s/%s", cmNamespace, cmName)
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

		finalObj, err = r.KubeClient.CoreV1().ConfigMaps(cmNamespace).Update(context.TODO(), cm, updateOptions)
		if err != nil {
			cm = nil
			return err
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to apply changes to %s/%s: %v", cmNamespace, cmName, err)
	}

	if finalObj == nil { // the CM was unchanged
		return nil
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
