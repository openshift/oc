package create

import (
	"strings"
	"testing"
)

func TestCreateEdgeRouteValidate(t *testing.T) {
	tests := []struct {
		name          string
		options       CreateEdgeRouteOptions
		expectedError string
	}{
		{
			name: "valid options without external certificate",
			options: CreateEdgeRouteOptions{
				Cert:   "cert-path",
				Key:    "key-path",
				CACert: "ca-cert-path",
			},
		},
		{
			name: "valid options with external certificate only",
			options: CreateEdgeRouteOptions{
				ExternalCertificate: "my-tls-secret",
			},
		},
		{
			name: "invalid options: external certificate with cert",
			options: CreateEdgeRouteOptions{
				ExternalCertificate: "my-tls-secret",
				Cert:                "cert-path",
			},
			expectedError: "--external-certificate is mutually exclusive with --cert, --key, and --ca-cert",
		},
		{
			name: "invalid options: external certificate with key",
			options: CreateEdgeRouteOptions{
				ExternalCertificate: "my-tls-secret",
				Key:                 "key-path",
			},
			expectedError: "--external-certificate is mutually exclusive with --cert, --key, and --ca-cert",
		},
		{
			name: "invalid options: external certificate with ca-cert",
			options: CreateEdgeRouteOptions{
				ExternalCertificate: "my-tls-secret",
				CACert:              "ca-cert-path",
			},
			expectedError: "--external-certificate is mutually exclusive with --cert, --key, and --ca-cert",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.options.Validate()
			if len(tc.expectedError) > 0 {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.expectedError)
				}
				if !strings.Contains(err.Error(), tc.expectedError) {
					t.Fatalf("expected error containing %q, got: %v", tc.expectedError, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestCreateReencryptRouteValidate(t *testing.T) {
	tests := []struct {
		name          string
		options       CreateReencryptRouteOptions
		expectedError string
	}{
		{
			name: "valid options without external certificate",
			options: CreateReencryptRouteOptions{
				Cert:       "cert-path",
				Key:        "key-path",
				CACert:     "ca-cert-path",
				DestCACert: "dest-ca-cert-path",
			},
		},
		{
			name: "valid options with external certificate and dest ca-cert",
			options: CreateReencryptRouteOptions{
				ExternalCertificate: "my-tls-secret",
				DestCACert:          "dest-ca-cert-path",
			},
		},
		{
			name: "invalid options: external certificate with cert",
			options: CreateReencryptRouteOptions{
				ExternalCertificate: "my-tls-secret",
				Cert:                "cert-path",
			},
			expectedError: "--external-certificate is mutually exclusive with --cert, --key, and --ca-cert",
		},
		{
			name: "invalid options: external certificate with key",
			options: CreateReencryptRouteOptions{
				ExternalCertificate: "my-tls-secret",
				Key:                 "key-path",
			},
			expectedError: "--external-certificate is mutually exclusive with --cert, --key, and --ca-cert",
		},
		{
			name: "invalid options: external certificate with ca-cert",
			options: CreateReencryptRouteOptions{
				ExternalCertificate: "my-tls-secret",
				CACert:              "ca-cert-path",
			},
			expectedError: "--external-certificate is mutually exclusive with --cert, --key, and --ca-cert",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.options.Validate()
			if len(tc.expectedError) > 0 {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.expectedError)
				}
				if !strings.Contains(err.Error(), tc.expectedError) {
					t.Fatalf("expected error containing %q, got: %v", tc.expectedError, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}
