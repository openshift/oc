package trustpurge

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/utils/diff"

	"github.com/openshift/library-go/pkg/operator/certrotation"
)

func TestRemoveOldTrustRuntime_purgeTrustFromConfigMap(t *testing.T) {
	testBundle := bundleCerts(t,
		makeCert(t, "testsub", time.Now()),
		makeCert(t, "testsub2", time.Now().Add(-10*time.Hour)),
	)
	testPruneTime := time.Now().Add(-5 * time.Minute)
	prunedTestBundle := testPruneCertBundle(t, testPruneTime, testBundle)

	tests := []struct {
		name           string
		inputCM        *corev1.ConfigMap
		createdBefore  time.Time
		excludeCMs     map[string]sets.Set[string]
		expectedUpdate bool
		expectedBundle string
		injectErrors   []error
		wantErr        bool
	}{
		{
			name:          "not a ca-bundle CM - no labels",
			inputCM:       withManagedCertTypeLabel(testCM(t), "-"),
			createdBefore: time.Now().Add(10 * time.Second),
		},
		{
			name:          "not a ca-bundle CM - different cert type",
			inputCM:       withManagedCertTypeLabel(testCM(t), "client-cert"),
			createdBefore: time.Now().Add(10 * time.Second),
		},
		{
			name:          "ca bundle already empty",
			inputCM:       withAdditionalData(testCM(t), map[string]string{"ca-bundle.crt": ""}),
			createdBefore: time.Now().Add(10 * time.Second),
		},
		{
			name:          "ca bundle with unparsable data",
			inputCM:       withAdditionalData(testCM(t), map[string]string{"ca-bundle.crt": "how did this get here"}),
			createdBefore: time.Now().Add(10 * time.Second),
			wantErr:       true,
		},
		{
			name:           "basic - remove the ca-bundle",
			inputCM:        testCM(t),
			expectedUpdate: true,
			createdBefore:  time.Now().Add(10 * time.Second),
		},
		{
			name:          "basic - the ca-bundle is supposed to be excluded",
			inputCM:       testCM(t),
			excludeCMs:    map[string]sets.Set[string]{"test-namespace": sets.New("test-configmap")},
			createdBefore: time.Now().Add(10 * time.Second),
		},
		{
			name:           "only the CA bundle gets updated",
			inputCM:        withAdditionalData(testCM(t), map[string]string{"key of fun": "funny value"}),
			createdBefore:  time.Now().Add(10 * time.Second),
			expectedUpdate: true,
		},
		{
			name: "created before - all certs remain",
			inputCM: withAdditionalData(testCM(t), map[string]string{
				"ca-bundle.crt": string(bundleCerts(t,
					makeCert(t, "testsub", time.Now()),
					makeCert(t, "testsub2", time.Now().Add(5*time.Second)),
					makeCert(t, "testsub3", time.Now().Add(-5*time.Hour)),
				)),
			}),
			createdBefore: time.Now().Add(-6 * time.Hour),
		},
		{
			name: "created before - all certs get pruned",
			inputCM: withAdditionalData(testCM(t), map[string]string{
				"ca-bundle.crt": string(bundleCerts(t,
					makeCert(t, "testsub", time.Now()),
				)),
			}),
			createdBefore:  time.Now().Add(10 * time.Second),
			expectedUpdate: true,
		},
		{
			name: "created before - some certs remain after the pruning",
			inputCM: withAdditionalData(
				testCM(t), map[string]string{
					"ca-bundle.crt": string(testBundle),
				},
			),
			createdBefore:  testPruneTime,
			expectedBundle: string(prunedTestBundle),
			expectedUpdate: true,
		},
		{
			name:           "first update fails on conflict",
			inputCM:        testCM(t),
			createdBefore:  time.Now().Add(10 * time.Second),
			expectedUpdate: true,
			injectErrors:   []error{apierrors.NewConflict(schema.GroupResource{}, "test-configmap", fmt.Errorf("oh no, a conflict"))},
		},
		{
			name:           "all updates fail on conflict",
			inputCM:        testCM(t),
			createdBefore:  time.Now().Add(10 * time.Second),
			expectedUpdate: true,
			injectErrors: []error{
				apierrors.NewConflict(schema.GroupResource{}, "test-configmap", fmt.Errorf("oh no, a conflict")),
				apierrors.NewConflict(schema.GroupResource{}, "test-configmap", fmt.Errorf("oh no, a conflict")),
				apierrors.NewConflict(schema.GroupResource{}, "test-configmap", fmt.Errorf("oh no, a conflict")),
				apierrors.NewConflict(schema.GroupResource{}, "test-configmap", fmt.Errorf("oh no, a conflict")),
				apierrors.NewConflict(schema.GroupResource{}, "test-configmap", fmt.Errorf("oh no, a conflict")),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset(tt.inputCM.DeepCopy())
			if len(tt.injectErrors) > 0 {
				i := 0
				fakeClient.PrependReactor("update", "configmaps", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
					if i < len(tt.injectErrors) {
						i++
						return true, nil, tt.injectErrors[i-1]
					}
					return false, nil, nil
				})
			}

			r := &RemoveOldTrustRuntime{
				KubeClient:     fakeClient,
				dryRun:         false,
				createdBefore:  tt.createdBefore,
				excludeBundles: tt.excludeCMs,
				Printer:        printers.NewDiscardingPrinter(),
				IOStreams:      genericiooptions.NewTestIOStreamsDiscard(),
			}
			if err := r.purgeTrustFromConfigMap(tt.inputCM.DeepCopy()); (err != nil) != tt.wantErr {
				t.Errorf("RemoveOldTrustRuntime.purgeTrustFromConfigMap() error = %v, wantErr %v", err, tt.wantErr)
			}

			var expectedCM *corev1.ConfigMap
			if tt.expectedUpdate {
				expectedCM = tt.inputCM.DeepCopy()
				expectedCM.Data["ca-bundle.crt"] = tt.expectedBundle
			}
			expectedActionsNum := len(tt.injectErrors)
			if tt.expectedUpdate {
				expectedActionsNum++
			}
			testActions(t, expectedCM, fakeClient.Actions())
		})
	}
}

func Test_pruneCertBundle(t *testing.T) {
	tests := []struct {
		name          string
		createdBefore time.Time
		bundlePEM     []*x509.Certificate
		expectCNs     []string
		expectPruned  bool
	}{
		{
			name:          "single cert, don't remove",
			createdBefore: time.Now().Add(-5 * time.Minute),
			bundlePEM: []*x509.Certificate{
				makeCert(t, "test1", time.Now()),
			},
			expectCNs:    []string{"test1"},
			expectPruned: false,
		},
		{
			name:          "single cert, remove it",
			createdBefore: time.Now().Add(5 * time.Minute),
			bundlePEM: []*x509.Certificate{
				makeCert(t, "test1", time.Now()),
			},
			expectPruned: true,
		},
		{
			name:          "mutliple certs, all good",
			createdBefore: time.Now().Add(-5 * time.Minute),
			bundlePEM: []*x509.Certificate{
				makeCert(t, "test1", time.Now()),
				makeCert(t, "test2", time.Now().Add(5*time.Hour)),
				makeCert(t, "test3", time.Now().Add(-5*time.Second)),
				makeCert(t, "test4", time.Now().Add(852*time.Hour*24)),
			},
			expectCNs:    []string{"test1", "test2", "test3", "test4"},
			expectPruned: false,
		},
		{
			name:          "mutliple certs, prune some",
			createdBefore: time.Now().Add(-5 * time.Minute),
			bundlePEM: []*x509.Certificate{
				makeCert(t, "test1", time.Now()),
				makeCert(t, "test2", time.Now().Add(-7*time.Minute)),
				makeCert(t, "test3", time.Now().Add(-5*time.Second)),
				makeCert(t, "test4", time.Now().Add(-4*time.Hour)),
			},
			expectCNs:    []string{"test1", "test3"},
			expectPruned: true,
		},
		{
			name:          "mutliple certs, prune all",
			createdBefore: time.Now().Add(10 * time.Minute),
			bundlePEM: []*x509.Certificate{
				makeCert(t, "test1", time.Now()),
				makeCert(t, "test2", time.Now().Add(-7*time.Minute)),
				makeCert(t, "test3", time.Now().Add(-5*time.Second)),
				makeCert(t, "test4", time.Now().Add(-4*time.Hour)),
			},
			expectPruned: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			got, gotPruned := pruneCertBundle(tt.createdBefore, tt.bundlePEM)

			var gotSubjects []string
			if len(got) > 0 {

				for _, c := range got {
					gotSubjects = append(gotSubjects, c.Subject.CommonName)
				}
			}

			if !reflect.DeepEqual(gotSubjects, tt.expectCNs) {
				t.Errorf("pruneCertBundle() got = %v, want %v", gotSubjects, tt.expectCNs)
			}
			if gotPruned != tt.expectPruned {
				t.Errorf("pruneCertBundle() gotPruned = %v, want %v", gotPruned, tt.expectPruned)
			}
		})
	}
}

func makeCert(t *testing.T, cn string, issuedOn time.Time) *x509.Certificate {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	tmpl := x509.Certificate{
		SerialNumber: new(big.Int).SetInt64(0),
		Subject: pkix.Name{
			CommonName: cn,
		},
		NotBefore:             issuedOn.UTC(),
		NotAfter:              issuedOn.Add(365 * 24 * time.Hour).UTC(),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDERBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, key.Public(), key)
	if err != nil {
		t.Fatalf("failed to create a certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDERBytes)
	if err != nil {
		t.Fatalf("failed to parse the freshly created certificate: %v", err)
	}

	return cert
}

func bundleCerts(t *testing.T, certs ...*x509.Certificate) []byte {
	var finalBundle []byte
	finalBundle, err := certutil.EncodeCertificates(certs...)
	if err != nil {
		t.Fatalf("failed to encode certs: %v", err)
	}
	return finalBundle
}

func testCM(t *testing.T) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "test-namespace",
			Labels: map[string]string{
				certrotation.ManagedCertificateTypeLabelName: "ca-bundle",
			},
		},
		Data: map[string]string{
			"ca-bundle.crt": string(bundleCerts(t,
				makeCert(t, "testingCA", time.Now()))),
		},
	}

	return cm
}

func withManagedCertTypeLabel(cm *corev1.ConfigMap, val string) *corev1.ConfigMap {
	if val == "-" {
		delete(cm.Labels, certrotation.ManagedCertificateTypeLabelName)
	} else {
		cm.Labels[certrotation.ManagedCertificateTypeLabelName] = val
	}
	return cm
}

func withAdditionalData(cm *corev1.ConfigMap, data map[string]string) *corev1.ConfigMap {
	for k, v := range data {
		cm.Data[k] = v
	}
	return cm
}

func testActions(t *testing.T, expectedCM *corev1.ConfigMap, clientActions []clienttesting.Action) {
	if expectedCM == nil {
		if len(clientActions) != 0 {
			t.Fatalf("no action expected, but got: %v", clientActions)
		}
		return
	}

	if len(clientActions) == 0 {
		t.Fatalf("missing expected action")
	}

	action, ok := clientActions[0].(clienttesting.UpdateAction)
	if !ok {
		t.Fatalf("the action was not update: %v", clientActions[0])
	}

	actualCM, ok := action.GetObject().(*corev1.ConfigMap)
	if !ok {
		t.Fatalf("the updated object was not a configMap: %v", actualCM)
	}
	if !equality.Semantic.DeepEqual(expectedCM, actualCM) {
		t.Logf("actual %v", actualCM)
		t.Fatalf("unexpected diff: %v", diff.ObjectDiff(expectedCM, actualCM))
	}
}

func testPruneCertBundle(t *testing.T, pruneBefore time.Time, bundlePEM []byte) []byte {
	certs, err := certutil.ParseCertsPEM(bundlePEM)
	if err != nil {
		t.Fatalf("failed to parse bundle: %v", err)
	}

	prunedCerts, pruned := pruneCertBundle(pruneBefore, certs)
	if !pruned {
		t.Fatalf("expected pruning to occur")
	}

	newBundlePEM, err := certutil.EncodeCertificates(prunedCerts...)
	if err != nil {
		t.Fatalf("failed to encode pruned certs: %v", err)
	}

	return newBundlePEM
}
