package certregen

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/certrotation"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/utils/diff"
)

func TestRootsRegen_forceRegenerationOnSecret(t *testing.T) {
	tests := []struct {
		name             string
		validBefore      *time.Time
		inputSecret      *corev1.Secret
		expectedUpdate   bool
		injectApplyError bool
		wantErr          string
	}{
		{
			name:        "no annotations",
			inputSecret: withAnnotationKeysRemoved(testSecret(t), certrotation.CertificateIssuer, certrotation.CertificateNotBeforeAnnotation),
		},
		{
			name:        "invalid cert", // TODO: should we force cert regen or do we assume the system fixes itself?
			inputSecret: withCertKey(testSecret(t), []byte("bogus")),
			wantErr:     "error interpretting content: data does not contain any valid RSA or ECDSA certificates",
		},
		{
			name:           "invalid key",
			inputSecret:    withKeyKey(testSecret(t), []byte("bogus key")),
			expectedUpdate: true,
		},
		{
			name:           "force rotation by time",
			inputSecret:    testSecret(t),
			validBefore:    ptime(time.Now().Add(20 * time.Second)),
			expectedUpdate: true,
		},
		{
			name:        "force rotation by time - cert was not valid at that time",
			inputSecret: testSecret(t),
			validBefore: ptime(time.Now().Add(-20 * time.Second)),
		},
		{
			name:           "should just rotate",
			inputSecret:    testSecret(t),
			expectedUpdate: true,
		},
		{
			name:        "cert is a leaf cert",
			inputSecret: withLeafCert(t, testSecret(t)),
		},
		{
			name:             "apply fails",
			inputSecret:      testSecret(t),
			injectApplyError: true,
			wantErr:          "ha, you failed!",
			expectedUpdate:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secrets := []runtime.Object{}
			if tt.inputSecret != nil {
				secrets = append(secrets, tt.inputSecret.DeepCopy())
			}
			fakeClient := fake.NewSimpleClientset(secrets...)
			if tt.injectApplyError {
				fakeClient.PrependReactor("patch", "secrets", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, fmt.Errorf("ha, you failed!")
				})
			}

			o := &RootsRegen{
				ValidBefore: tt.validBefore,
			}
			err := o.forceRegenerationOnSecret(
				&objectPrinter{
					out:     genericiooptions.NewTestIOStreamsDiscard().Out,
					printer: printers.ResourcePrinterFunc(testPrinter),
				},
				fakeClient, tt.inputSecret, false,
			)
			testErr(t, tt.wantErr, err)

			var expectedSecret *corev1.Secret
			if tt.expectedUpdate {
				expectedSecret = &corev1.Secret{
					TypeMeta: tt.inputSecret.TypeMeta,
					ObjectMeta: metav1.ObjectMeta{
						Name:      tt.inputSecret.Name,
						Namespace: tt.inputSecret.Namespace,
						Annotations: map[string]string{
							certrotation.CertificateNotAfterAnnotation: "force-regeneration",
						},
					},
				}

			}
			testActions(t, expectedSecret, fakeClient.Actions())
		})
	}
}

func testPrinter(runtime.Object, io.Writer) error {
	return nil
}

func testSecret(t *testing.T) *corev1.Secret {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key")
	}

	cert, err := certutil.NewSelfSignedCACert(
		certutil.Config{
			CommonName: "test-ca",
		},
		key,
	)

	if err != nil {
		t.Fatalf("failed to generate CA cert: %v", err)
	}

	certPEM, err := certutil.EncodeCertificates(cert)
	if err != nil {
		t.Fatalf("failed to encode cert into PEM: %v", err)
	}

	keyPEM := bytes.NewBuffer(make([]byte, 0))
	if err := pem.Encode(keyPEM, &pem.Block{Type: keyutil.RSAPrivateKeyBlockType, Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		t.Fatalf("failed to encode key into PEM: %v", err)
	}

	s := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				certrotation.CertificateIssuer:              "test-signer",
				certrotation.CertificateNotBeforeAnnotation: cert.NotBefore.Format(time.RFC3339),
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       certPEM,
			corev1.TLSPrivateKeyKey: keyPEM.Bytes(),
		},
	}

	return s
}

func withAnnotationKeysRemoved(s *corev1.Secret, keys ...string) *corev1.Secret {
	for _, k := range keys {
		delete(s.Annotations, k)
	}
	return s
}

func withCertKey(s *corev1.Secret, val []byte) *corev1.Secret {
	if len(val) == 0 {
		delete(s.Data, corev1.TLSCertKey)
	} else {
		s.Data[corev1.TLSCertKey] = val
	}
	return s
}

func withKeyKey(s *corev1.Secret, val []byte) *corev1.Secret {
	if len(val) == 0 {
		delete(s.Data, corev1.TLSPrivateKeyKey)
	} else {
		s.Data[corev1.TLSPrivateKeyKey] = val
	}
	return s
}

func withLeafCert(t *testing.T, s *corev1.Secret) *corev1.Secret {
	cert, key, err := certutil.GenerateSelfSignedCertKey("somehost.host", nil, nil)
	if err != nil {
		t.Fatalf("failed to generate certificate: %v", err)
	}
	s.Data[corev1.TLSCertKey] = cert
	s.Data[corev1.TLSPrivateKeyKey] = key
	return s
}

func testErr(t *testing.T, expectedError string, actualError error) {
	switch {
	case len(expectedError) == 0 && actualError == nil:
	case len(expectedError) == 0 && actualError != nil:
		t.Fatalf("no error expected, got %v", actualError)
	case len(expectedError) != 0 && actualError == nil:
		t.Fatalf("expected some errors: %v, got none", expectedError)
	case len(expectedError) != 0 && actualError != nil:
		if !reflect.DeepEqual(expectedError, actualError.Error()) {
			t.Fatalf("expected some error: %v, got different error: %v", expectedError, actualError.Error())
		}
	}
}

func testActions(t *testing.T, expectedSecret *corev1.Secret, clientActions []clienttesting.Action) {
	if len(clientActions) > 1 {
		t.Fatalf("too many actions: %v", clientActions)
	}

	if expectedSecret == nil {
		if len(clientActions) != 0 {
			t.Fatalf("no action expected, but got: %v", clientActions)
		}
		return
	}

	if len(clientActions) == 0 {
		t.Fatalf("missing expected action")
	}

	action := clientActions[0].(clienttesting.PatchAction)
	if action.GetPatchType() != types.ApplyPatchType {
		t.Fatalf("wrong patch type: %v", action.GetPatchType())
	}
	actualSecret := resourceread.ReadSecretV1OrDie(action.GetPatch())
	if !equality.Semantic.DeepEqual(expectedSecret, actualSecret) {
		t.Logf("actual %v", actualSecret)
		t.Fatalf("unexpected diff: %v", diff.ObjectDiff(expectedSecret, actualSecret))
	}
}

func ptime(t time.Time) *time.Time {
	return &t
}
