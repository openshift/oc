package inspect

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	secret "k8s.io/api/core/v1"
)

func TestElideSecret(t *testing.T) {
	tests := []struct {
		input    secret.Secret
		expected secret.Secret
	}{
		{
			input: secret.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"openshift.io/token-secret.value":                  "test",
						"kubectl.kubernetes.io/last-applied-configuration": "test",
						"another-annotation":                               "another-annotation",
					},
				},
				Data: map[string][]byte{
					"tls.crt":        []byte("test"),
					"ca.crt":         []byte("test"),
					"service-ca.crt": []byte("test"),
					"tls.key":        []byte("test"),
					"another.key":    []byte("test"),
				},
			},
			expected: secret.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"openshift.io/token-secret.value":                  "",
						"kubectl.kubernetes.io/last-applied-configuration": "",
						"another-annotation":                               "another-annotation",
					},
				},
				Data: map[string][]byte{
					"tls.crt":        []byte("test"),
					"ca.crt":         []byte("test"),
					"service-ca.crt": []byte("test"),
					"tls.key":        []byte("4 bytes long"),
					"another.key":    []byte("4 bytes long"),
				},
			},
		},
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			elideSecret(&test.input)
			if !reflect.DeepEqual(test.input.Annotations, test.expected.Annotations) {
				t.Errorf("expected %v, got %v", test.expected.Annotations, test.input.Annotations)
			}
			if !reflect.DeepEqual(test.input.Data, test.expected.Data) {
				t.Errorf("expected %v, got %v", test.expected.Data, test.input.Data)
			}
		})
	}
}
