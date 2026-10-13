package trustpurge

import (
	"context"
	"crypto/x509"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/retry"

	"github.com/openshift/library-go/pkg/operator/certrotation"
	"github.com/openshift/oc/pkg/cli/admin/ocpcertificates/certregen"
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

	cachedSecretCerts map[string][]*cachedSecretCert

	genericiooptions.IOStreams
}

type cachedSecretCert struct {
	namespaceName string
	cert          *x509.Certificate
}

func newCachedSecretCert(namespace, name string, certPEM []byte) (*cachedSecretCert, error) {
	if len(certPEM) == 0 {
		return nil, nil
	}

	secretNamespaceName := fmt.Sprintf("secrets/%s[%s]", name, namespace)

	cert, err := certutil.ParseCertsPEM(certPEM)
	if err != nil {
		return nil, fmt.Errorf("failed parsing certificate of %s: %w", secretNamespaceName, err)
	}

	return &cachedSecretCert{
		namespaceName: secretNamespaceName,
		cert:          cert[0], // the 1st cert should always be the leaf
	}, nil
}

func (r *RemoveOldTrustRuntime) Run(ctx context.Context) error {
	if r.createdBefore.IsZero() {
		return fmt.Errorf("missing certificate validity borderline date")
	}

	namespaces, err := r.KubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, ns := range namespaces.Items {
		if nsName := ns.Name; strings.HasPrefix(nsName, "openshift-") {
			if err := r.cacheLeafSecretsForNS(ctx, nsName); err != nil {
				return err
			}
		}
	}

	visitor := r.ResourceFinder.Do()

	// TODO need to wire context through the visitorFns
	return visitor.Visit(r.purgeTrustFromResourceInfo)
}

func (r *RemoveOldTrustRuntime) cacheLeafSecretsForNS(ctx context.Context, nsName string) error {
	secretList, err := r.KubeClient.CoreV1().Secrets(nsName).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, s := range secretList.Items {
		if isLeaf, err := certregen.IsLeafCertSecret(&s); err != nil {
			return err
		} else if !isLeaf {
			continue
		}
		if cert := s.Data[corev1.TLSCertKey]; len(cert) == 0 {
			continue
		}

		issuer := s.Annotations[certrotation.CertificateIssuer]
		cachedS, err := newCachedSecretCert(s.Namespace, s.Name, s.Data[corev1.TLSCertKey])
		if err != nil {
			return err
		}
		if cachedS != nil {
			r.cachedSecretCerts[issuer] = append(r.cachedSecretCerts[issuer], cachedS)
		}
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

		caBundlePEM := cm.Data["ca-bundle.crt"]
		if len(caBundlePEM) == 0 {
			// somebody was faster
			return nil
		}

		caCerts, err := certutil.ParseCertsPEM([]byte(caBundlePEM))
		if err != nil {
			return fmt.Errorf("failed to parse certificates for %s/%s: %v", cmNamespace, cmName, err)
		}

		originalSecrets := secretsForBundle(caCerts, r.cachedSecretCerts)

		newBundle, pruned := pruneCertBundle(r.createdBefore, caCerts)
		if !pruned { // old == new
			return nil
		}

		newSecrets := secretsForBundle(newBundle, r.cachedSecretCerts)
		oldSecretNames, newSecretNames := sets.New[string](), sets.New[string]()
		for _, s := range originalSecrets {
			oldSecretNames.Insert(s.namespaceName)
		}
		for _, s := range newSecrets {
			newSecretNames.Insert(s.namespaceName)
		}

		if oDiffN := oldSecretNames.Difference(newSecretNames); oDiffN.Len() > 0 {
			return fmt.Errorf("secrets only trusted by the old bundle: %v", oDiffN.UnsortedList())
		}

		newBundlePEM, err := certutil.EncodeCertificates(newBundle...)
		if err != nil {
			return fmt.Errorf("failed to encode new cert bundle for %s/%s: %w", cmNamespace, cmName, err)
		}
		cm.Data["ca-bundle.crt"] = string(newBundlePEM)

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

func pruneCertBundle(createdBefore time.Time, certBundle []*x509.Certificate) ([]*x509.Certificate, bool) {
	newCerts := make([]*x509.Certificate, 0, len(certBundle))
	for i, cert := range certBundle {
		if cert.NotBefore.After(createdBefore) {
			newCerts = append(newCerts, certBundle[i])
		}
	}

	if len(certBundle) == len(newCerts) {
		return certBundle, false
	}

	return newCerts, true
}

func secretsForBundle(trustBundle []*x509.Certificate, cachedSecretsCerts map[string][]*cachedSecretCert) []*cachedSecretCert {
	var expectedValidSecrets []*cachedSecretCert
	trustPool := x509.NewCertPool()

	for _, cert := range trustBundle {
		cert := cert
		trustPool.AddCert(cert)

		if s := cachedSecretsCerts[cert.Issuer.CommonName]; s != nil {
			expectedValidSecrets = append(expectedValidSecrets, s...)
		}
	}

	verifyOpts := x509.VerifyOptions{
		Roots:     trustPool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}

	var actualValidSecrets []*cachedSecretCert
	for i, s := range expectedValidSecrets {
		_, err := s.cert.Verify(verifyOpts)
		if err == nil {
			actualValidSecrets = append(actualValidSecrets, expectedValidSecrets[i])
		}
	}

	return actualValidSecrets
}
