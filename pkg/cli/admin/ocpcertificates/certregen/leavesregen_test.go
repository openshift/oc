package certregen

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/certrotation"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
)

func TestLeavesRegen_forceRegenerationOnSecret(t *testing.T) {
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
			inputSecret: withAnnotationKeysRemoved(testLeafCertSecret(t), certrotation.CertificateIssuer, certrotation.CertificateNotBeforeAnnotation),
		},
		{
			name:        "invalid cert", // TODO: should we force cert regen or do we assume the system fixes itself?
			inputSecret: withCertKey(testLeafCertSecret(t), []byte("bogus")),
			wantErr:     "error interpretting content: data does not contain any valid RSA or ECDSA certificates",
		},
		{
			name:           "invalid key",
			inputSecret:    withKeyKey(testLeafCertSecret(t), []byte("bogus key")),
			expectedUpdate: true,
		},
		{
			name:           "force rotation by time",
			inputSecret:    testLeafCertSecret(t),
			validBefore:    ptime(time.Now().Add(20 * time.Second)),
			expectedUpdate: true,
		},
		{
			name:        "force rotation by time - cert was not valid at that time",
			inputSecret: testLeafCertSecret(t),
			validBefore: ptime(time.Now().Add(-2 * time.Hour)), // certutil generates leaf certs with "NotBefore = now - 1*time.Hour"
		},
		{
			name:           "should just rotate",
			inputSecret:    testLeafCertSecret(t),
			expectedUpdate: true,
		},
		{
			name:        "cert is a CA cert",
			inputSecret: withCACert(t, testLeafCertSecret(t)),
		},
		{
			name:             "apply fails",
			inputSecret:      testLeafCertSecret(t),
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

			o := &LeavesRegen{
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

func testLeafCertSecret(t *testing.T) *corev1.Secret {
	certPEM, key, err := certutil.GenerateSelfSignedCertKey("somehost.host", nil, nil)
	if err != nil {
		t.Fatalf("failed to generate certificate: %v", err)
	}

	certs, err := certutil.ParseCertsPEM(certPEM)
	if err != nil {
		t.Fatalf("failed to parse PEM for a cert: %v", err)
	}
	cert := certs[0]

	s := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				certrotation.CertificateIssuer:              cert.Issuer.CommonName,
				certrotation.CertificateNotBeforeAnnotation: cert.NotBefore.Format(time.RFC3339),
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       certPEM,
			corev1.TLSPrivateKeyKey: key,
		},
	}

	return s
}

func withCACert(t *testing.T, s *corev1.Secret) *corev1.Secret {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
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

	s.Data[corev1.TLSCertKey] = certPEM
	s.Data[corev1.TLSPrivateKeyKey] = keyPEM.Bytes()

	return s
}
