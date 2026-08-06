package inspect

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	oauthv1 "github.com/openshift/api/oauth/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"sigs.k8s.io/yaml"
)

// TestInspectResource verifies that InspectResource writes redacted output for
// each supported resource type. Actual output is compared against fixtures in
// testdata/; run with -update to regenerate them.
func TestInspectResource(t *testing.T) {
	tests := []struct {
		name       string
		info       func(t *testing.T) *resource.Info
		outputPath func(destDir string) string
		fixture    string
	}{
		{
			name: "OAuthClient secrets are redacted",
			info: func(t *testing.T) *resource.Info {
				client := &oauthv1.OAuthClient{
					TypeMeta:          metav1.TypeMeta{APIVersion: "oauth.openshift.io/v1", Kind: "OAuthClient"},
					ObjectMeta:        metav1.ObjectMeta{Name: "console"},
					Secret:            "supersecret",
					AdditionalSecrets: []string{"oldsecret"},
				}
				m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(client)
				if err != nil {
					t.Fatal("unable to convert to unstructured:", err)
				}
				return &resource.Info{
					Object: &unstructured.Unstructured{Object: m},
					Name:   "console",
					Mapping: &apimeta.RESTMapping{
						Resource:         schema.GroupVersionResource{Group: "oauth.openshift.io", Version: "v1", Resource: "oauthclients"},
						GroupVersionKind: oauthv1.GroupVersion.WithKind("OAuthClient"),
					},
				}
			},
			outputPath: func(destDir string) string {
				return filepath.Join(destDir, "cluster-scoped-resources", "oauth.openshift.io", "oauthclients", "console.yaml")
			},
			fixture: "testdata/oauthclient-redacted.yaml",
		},
		{
			name: "OAuthClientList secrets are redacted",
			info: func(t *testing.T) *resource.Info {
				makeItem := func(name, secret string, additional []string) unstructured.Unstructured {
					client := &oauthv1.OAuthClient{
						TypeMeta:          metav1.TypeMeta{APIVersion: "oauth.openshift.io/v1", Kind: "OAuthClient"},
						ObjectMeta:        metav1.ObjectMeta{Name: name},
						Secret:            secret,
						AdditionalSecrets: additional,
					}
					m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(client)
					if err != nil {
						t.Fatal("unable to convert to unstructured:", err)
					}
					return unstructured.Unstructured{Object: m}
				}
				return &resource.Info{
					Object: &unstructured.UnstructuredList{
						Object: map[string]any{
							"apiVersion": "oauth.openshift.io/v1",
							"kind":       "OAuthClientList",
						},
						Items: []unstructured.Unstructured{
							makeItem("console", "supersecret", []string{"oldsecret"}),
							makeItem("another", "anothersecret", nil),
						},
					},
					Mapping: &apimeta.RESTMapping{
						Resource:         schema.GroupVersionResource{Group: "oauth.openshift.io", Version: "v1", Resource: "oauthclients"},
						GroupVersionKind: oauthv1.GroupVersion.WithKind("OAuthClientList"),
					},
				}
			},
			outputPath: func(destDir string) string {
				return filepath.Join(destDir, "cluster-scoped-resources", "oauth.openshift.io", "oauthclients.yaml")
			},
			fixture: "testdata/oauthclientlist-redacted.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destDir := t.TempDir()
			o := &InspectOptions{
				DestDir:    destDir,
				fileWriter: NewMultiSourceWriter(&printers.YAMLPrinter{}),
			}

			if err := InspectResource(context.Background(), tt.info(t), NewResourceContext(nil), o); err != nil {
				t.Fatalf("InspectResource failed: %v", err)
			}

			actualBytes, err := os.ReadFile(tt.outputPath(destDir))
			if err != nil {
				t.Fatalf("failed to read output file: %v", err)
			}
			fixtureBytes, err := os.ReadFile(tt.fixture)
			if err != nil {
				t.Fatalf("failed to read fixture %s: %v", tt.fixture, err)
			}

			var actual, expected map[string]any
			if err := yaml.Unmarshal(actualBytes, &actual); err != nil {
				t.Fatalf("failed to parse actual output: %v", err)
			}
			if err := yaml.Unmarshal(fixtureBytes, &expected); err != nil {
				t.Fatalf("failed to parse fixture: %v", err)
			}

			if diff := cmp.Diff(expected, actual); diff != "" {
				t.Errorf("output mismatch with fixture %s (-want +got):\n%s", tt.fixture, diff)
			}
		})
	}
}
