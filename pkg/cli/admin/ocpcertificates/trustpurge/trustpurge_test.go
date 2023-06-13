package trustpurge

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
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
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/utils/diff"

	"github.com/openshift/library-go/pkg/operator/certrotation"
)

func TestRemoveOldTrustRuntime_purgeTrustFromConfigMap(t *testing.T) {
	testBundle := bundleCerts(
		makeCert(t, "testsub", time.Now()),
		makeCert(t, "testsub2", time.Now().Add(-10*time.Hour)),
	)
	testPruneTime := time.Now().Add(-5 * time.Minute)
	prunedTestBundle, pruned, err := pruneCertBundle(testPruneTime, string(testBundle))
	if err != nil {
		t.Fatalf("failed to prepare pruned cert bundle for tests: %v", err)
	}
	if !pruned {
		t.Fatalf("the test bundle should've gotten pruned")
	}

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
			name:    "not a ca-bundle CM - no labels",
			inputCM: withManagedCertTypeLabel(testCM(), "-"),
		},
		{
			name:    "not a ca-bundle CM - different cert type",
			inputCM: withManagedCertTypeLabel(testCM(), "client-cert"),
		},
		{
			name:           "basic - remove the ca-bundle",
			inputCM:        testCM(),
			expectedUpdate: true,
		},
		{
			name:       "basic - the ca-bundle is supposed to be excluded",
			inputCM:    testCM(),
			excludeCMs: map[string]sets.Set[string]{"test-namespace": sets.New("test-configmap")},
		},
		{
			name:           "only the CA bundle gets updated",
			inputCM:        withAdditionalData(testCM(), map[string]string{"key of fun": "funny value"}),
			expectedUpdate: true,
		},
		{
			name: "created before - all certs get pruned",
			inputCM: withAdditionalData(testCM(), map[string]string{
				"ca-bundle.crt": string(bundleCerts(
					makeCert(t, "testsub", time.Now()),
				)),
			}),
			createdBefore:  time.Now().Add(10 * time.Second),
			expectedUpdate: true,
		},
		{
			name: "created before - some certs remain after the pruning",
			inputCM: withAdditionalData(
				testCM(), map[string]string{
					"ca-bundle.crt": string(testBundle),
				},
			),
			createdBefore:  testPruneTime,
			expectedBundle: prunedTestBundle,
			expectedUpdate: true,
		},
		{
			name:           "first update fails on conflict",
			inputCM:        testCM(),
			expectedUpdate: true,
			injectErrors:   []error{apierrors.NewConflict(schema.GroupResource{}, "test-configmap", fmt.Errorf("oh no, a conflict"))},
		},
		{
			name:           "all updates fail on conflict",
			inputCM:        testCM(),
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
				IOStreams:      genericclioptions.NewTestIOStreamsDiscard(),
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
		bundlePEM     []byte
		expectCNs     []string
		expectPruned  bool
		wantErr       bool
	}{
		{
			name:          "bogus in the cert bundle",
			createdBefore: time.Now().Add(-5 * time.Second),
			bundlePEM: bundleCerts(
				[]byte("how did this get here"),
			),
			expectPruned: false,
			wantErr:      true,
		},
		{
			name:          "single cert, don't remove",
			createdBefore: time.Now().Add(-5 * time.Minute),
			bundlePEM: bundleCerts(
				makeCert(t, "test1", time.Now()),
			),
			expectCNs:    []string{"test1"},
			expectPruned: false,
			wantErr:      false,
		},
		{
			name:          "single cert, remove it",
			createdBefore: time.Now().Add(5 * time.Minute),
			bundlePEM: bundleCerts(
				makeCert(t, "test1", time.Now()),
			),
			expectPruned: true,
			wantErr:      false,
		},
		{
			name:          "mutliple certs, all good",
			createdBefore: time.Now().Add(-5 * time.Minute),
			bundlePEM: bundleCerts(
				makeCert(t, "test1", time.Now()),
				makeCert(t, "test2", time.Now().Add(5*time.Hour)),
				makeCert(t, "test3", time.Now().Add(-5*time.Second)),
				makeCert(t, "test4", time.Now().Add(852*time.Hour*24)),
			),
			expectCNs:    []string{"test1", "test2", "test3", "test4"},
			expectPruned: false,
			wantErr:      false,
		},
		{
			name:          "mutliple certs, prune some",
			createdBefore: time.Now().Add(-5 * time.Minute),
			bundlePEM: bundleCerts(
				makeCert(t, "test1", time.Now()),
				makeCert(t, "test2", time.Now().Add(-7*time.Minute)),
				makeCert(t, "test3", time.Now().Add(-5*time.Second)),
				makeCert(t, "test4", time.Now().Add(-4*time.Hour)),
			),
			expectCNs:    []string{"test1", "test3"},
			expectPruned: true,
			wantErr:      false,
		},
		{
			name:          "mutliple certs, prune all",
			createdBefore: time.Now().Add(10 * time.Minute),
			bundlePEM: bundleCerts(
				makeCert(t, "test1", time.Now()),
				makeCert(t, "test2", time.Now().Add(-7*time.Minute)),
				makeCert(t, "test3", time.Now().Add(-5*time.Second)),
				makeCert(t, "test4", time.Now().Add(-4*time.Hour)),
			),
			expectPruned: true,
			wantErr:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			got, gotPruned, err := pruneCertBundle(tt.createdBefore, string(tt.bundlePEM))
			if (err != nil) != tt.wantErr {
				t.Errorf("pruneCertBundle() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			var gotSubjects []string
			if len(got) > 0 {
				gotCerts, err := certutil.ParseCertsPEM([]byte(got))
				if err != nil {
					t.Fatalf("failed to parse returned PEM bundle: %v", err)
				}

				for _, c := range gotCerts {
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

func makeCert(t *testing.T, cn string, issuedOn time.Time) []byte {
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

	certBytes := bytes.NewBuffer([]byte{})
	if err := pem.Encode(certBytes, &pem.Block{Type: "CERTIFICATE", Bytes: certDERBytes}); err != nil {
		t.Fatalf("failed to PEM-encode certificate: %v", err)
	}

	return certBytes.Bytes()
}

func bundleCerts(certs ...[]byte) []byte {
	var finalBundle []byte
	for _, c := range certs {
		finalBundle = append(finalBundle, c...)
		finalBundle = append(finalBundle, '\n')
	}
	return finalBundle
}

func testCM() *corev1.ConfigMap {
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
			"ca-bundle.crt": "A bundled CA chain would normally live here",
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
