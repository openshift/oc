package create

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	fakekubernetes "k8s.io/client-go/kubernetes/fake"

	routev1 "github.com/openshift/api/route/v1"
	routefake "github.com/openshift/client-go/route/clientset/versioned/fake"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file %s: %v", path, err)
	}
}

func newTestService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt32(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

func newTestRouteSubcommandOptions(t *testing.T) (*CreateRouteSubcommandOptions, *routefake.Clientset, *bytes.Buffer) {
	t.Helper()
	service := newTestService()
	streams, _, out, _ := genericiooptions.NewTestIOStreams()
	fakeKubeClient := fakekubernetes.NewClientset(service)
	fakeRouteClientset := routefake.NewClientset()
	mapper := meta.NewDefaultRESTMapper(nil)

	printFlags := genericclioptions.NewPrintFlags("created").WithTypeSetter(createCmdScheme)
	printer, err := printFlags.ToPrinter()
	if err != nil {
		t.Fatalf("failed to create printer: %v", err)
	}

	sub := &CreateRouteSubcommandOptions{
		PrintFlags: printFlags,
		Name:       "my-route",
		Namespace:  "default",
		Mapper:     mapper,
		Client:     fakeRouteClientset.RouteV1(),
		CoreClient: fakeKubeClient.CoreV1(),
		Printer:    printer,
		IOStreams:   streams,
	}

	return sub, fakeRouteClientset, out
}

func newTestEdgeRouteOptions(t *testing.T) (*CreateEdgeRouteOptions, *routefake.Clientset, *bytes.Buffer) {
	t.Helper()
	sub, fakeRouteClientset, out := newTestRouteSubcommandOptions(t)
	o := &CreateEdgeRouteOptions{
		CreateRouteSubcommandOptions: sub,
		Service:                      "my-service",
	}
	return o, fakeRouteClientset, out
}

func TestCreateEdgeRoute_MutualExclusivity(t *testing.T) {
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
			o, _, _ := newTestEdgeRouteOptions(t)
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

func TestCreateEdgeRoute_ExternalCertificateWiring(t *testing.T) {
	o, fakeRouteClientset, _ := newTestEdgeRouteOptions(t)
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
	if route.Spec.TLS.Termination != routev1.TLSTerminationEdge {
		t.Errorf("expected termination %q, got %q", routev1.TLSTerminationEdge, route.Spec.TLS.Termination)
	}
}

