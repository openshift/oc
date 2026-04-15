package create

import (
	"bytes"
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	routev1 "github.com/openshift/api/route/v1"
	routefake "github.com/openshift/client-go/route/clientset/versioned/fake"
)

func newTestReencryptRouteOptions(t *testing.T) (*CreateReencryptRouteOptions, *routefake.Clientset, *bytes.Buffer) {
	t.Helper()
	sub, fakeRouteClientset, out := newTestRouteSubcommandOptions(t)
	o := &CreateReencryptRouteOptions{
		CreateRouteSubcommandOptions: sub,
		Service:                      "my-service",
	}
	return o, fakeRouteClientset, out
}

func TestCreateReencryptRoute_MutualExclusivity(t *testing.T) {
	tests := []struct {
		name                string
		cert                string
		key                 string
		externalCertificate string
		expectError         string
	}{
		{
			name: "neither --cert nor --external-certificate set",
		},
		{
			name: "only --cert set",
			cert: "/path/to/tls.crt",
		},
		{
			name:                "only --external-certificate set",
			externalCertificate: "my-secret",
		},
		{
			name:                "both --cert and --external-certificate set",
			cert:                "/path/to/tls.crt",
			externalCertificate: "my-secret",
			expectError:         "--cert and --external-certificate are mutually exclusive",
		},
		{
			name:                "both --key and --external-certificate set",
			key:                 "/path/to/tls.key",
			externalCertificate: "my-secret",
			expectError:         "--key and --external-certificate are mutually exclusive",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, _, _ := newTestReencryptRouteOptions(t)
			o.Cert = tt.cert
			o.Key = tt.key
			o.ExternalCertificate = tt.externalCertificate

			err := o.Validate()
			if tt.expectError != "" {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				if !strings.Contains(err.Error(), tt.expectError) {
					t.Fatalf("expected error containing %q, got: %v", tt.expectError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCreateReencryptRoute_ExternalCertificateWiring(t *testing.T) {
	o, fakeRouteClientset, _ := newTestReencryptRouteOptions(t)
	o.ExternalCertificate = "my-cert-secret"

	if err := o.Run(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	routes, err := fakeRouteClientset.RouteV1().Routes("default").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list routes: %v", err)
	}
	if len(routes.Items) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes.Items))
	}

	route := routes.Items[0]
	if route.Name != "my-route" {
		t.Errorf("expected route name %q, got %q", "my-route", route.Name)
	}
	if route.Spec.TLS == nil {
		t.Fatal("expected TLS config to be set")
	}
	if route.Spec.TLS.ExternalCertificate == nil {
		t.Fatal("expected ExternalCertificate to be set")
	}
	if route.Spec.TLS.ExternalCertificate.Name != "my-cert-secret" {
		t.Errorf("expected ExternalCertificate.Name %q, got %q", "my-cert-secret", route.Spec.TLS.ExternalCertificate.Name)
	}
	if route.Spec.TLS.Certificate != "" {
		t.Errorf("expected no inline certificate, got %q", route.Spec.TLS.Certificate)
	}
	if route.Spec.TLS.Key != "" {
		t.Errorf("expected no inline key, got %q", route.Spec.TLS.Key)
	}
	if route.Spec.TLS.Termination != routev1.TLSTerminationReencrypt {
		t.Errorf("expected termination %q, got %q", routev1.TLSTerminationReencrypt, route.Spec.TLS.Termination)
	}
}

