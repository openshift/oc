package create

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	routev1 "github.com/openshift/api/route/v1"
)

func TestCreateReencryptRouteValidate(t *testing.T) {
	tests := []struct {
		name    string
		options CreateReencryptRouteOptions
		wantErr string
	}{
		{
			name:    "cert and key",
			options: CreateReencryptRouteOptions{Cert: "tls.crt", Key: "tls.key"},
		},
		{
			name:    "external certificate",
			options: CreateReencryptRouteOptions{ExternalCertificate: "my-secret"},
		},
		{
			name:    "cert with external certificate",
			options: CreateReencryptRouteOptions{Cert: "tls.crt", ExternalCertificate: "my-secret"},
			wantErr: "--cert and --external-certificate are mutually exclusive",
		},
		{
			name:    "key with external certificate",
			options: CreateReencryptRouteOptions{Key: "tls.key", ExternalCertificate: "my-secret"},
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

func TestCreateReencryptRouteExternalCertificate(t *testing.T) {
	o := &CreateReencryptRouteOptions{
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
		Termination:         routev1.TLSTerminationReencrypt,
		ExternalCertificate: &routev1.LocalObjectReference{Name: "my-secret"},
	}
	if diff := cmp.Diff(want, route.Spec.TLS); diff != "" {
		t.Errorf("unexpected TLS config (-want +got):\n%s", diff)
	}
}
