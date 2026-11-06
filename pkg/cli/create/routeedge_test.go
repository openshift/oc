package create

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	fakekubernetes "k8s.io/client-go/kubernetes/fake"

	routev1 "github.com/openshift/api/route/v1"
	routefake "github.com/openshift/client-go/route/clientset/versioned/fake"
)

func newTestRouteSubcommandOptions(t *testing.T) *CreateRouteSubcommandOptions {
	t.Helper()
	o := NewCreateRouteSubcommandOptions(genericiooptions.NewTestIOStreamsDiscard())
	printer, err := o.PrintFlags.ToPrinter()
	if err != nil {
		t.Fatal(err)
	}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "my-service", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}}},
	}
	o.Name = "my-route"
	o.Namespace = "default"
	o.Mapper = meta.NewDefaultRESTMapper(nil)
	o.Printer = printer
	o.Client = routefake.NewClientset().RouteV1()
	o.CoreClient = fakekubernetes.NewClientset(service).CoreV1()
	return o
}

func TestCreateEdgeRouteValidate(t *testing.T) {
	tests := []struct {
		name    string
		options CreateEdgeRouteOptions
		wantErr string
	}{
		{
			name:    "cert and key",
			options: CreateEdgeRouteOptions{Cert: "tls.crt", Key: "tls.key"},
		},
		{
			name:    "external certificate",
			options: CreateEdgeRouteOptions{ExternalCertificate: "my-secret"},
		},
		{
			name:    "cert with external certificate",
			options: CreateEdgeRouteOptions{Cert: "tls.crt", ExternalCertificate: "my-secret"},
			wantErr: "--cert and --external-certificate are mutually exclusive",
		},
		{
			name:    "key with external certificate",
			options: CreateEdgeRouteOptions{Key: "tls.key", ExternalCertificate: "my-secret"},
			wantErr: "--key and --external-certificate are mutually exclusive",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotErr string
			if err := tt.options.Validate(); err != nil {
				gotErr = err.Error()
			}
			if gotErr != tt.wantErr {
				t.Errorf("expected error %q, got %q", tt.wantErr, gotErr)
			}
		})
	}
}

func TestCreateEdgeRouteExternalCertificate(t *testing.T) {
	o := &CreateEdgeRouteOptions{
		CreateRouteSubcommandOptions: newTestRouteSubcommandOptions(t),
		Service:                      "my-service",
		ExternalCertificate:          "my-secret",
	}
	if err := o.Run(); err != nil {
		t.Fatal(err)
	}

	route, err := o.CreateRouteSubcommandOptions.Client.Routes("default").Get(context.TODO(), "my-route", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := &routev1.TLSConfig{
		Termination:         routev1.TLSTerminationEdge,
		ExternalCertificate: &routev1.LocalObjectReference{Name: "my-secret"},
	}
	if diff := cmp.Diff(want, route.Spec.TLS); diff != "" {
		t.Errorf("unexpected TLS config (-want +got):\n%s", diff)
	}
}
