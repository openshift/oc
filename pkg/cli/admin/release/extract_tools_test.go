package release

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	configv1 "github.com/openshift/api/config/v1"
	fakeconfigv1client "github.com/openshift/client-go/config/clientset/versioned/fake"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekubeclient "k8s.io/client-go/kubernetes/fake"
	appsv1client "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/utils/ptr"
)

func Test_copyAndReplace(t *testing.T) {
	buffer := 4
	tests := []struct {
		name         string
		input        string
		replacements []replacement
		expected     string
		error        string
	}{
		{
			name:  "buffer too small",
			input: "1234",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aaaaa"),
					value:  "A",
				},
			},
			error: "the buffer size must be greater than 5 bytes to find rep-A",
		},
		{
			name:     "buffer too large",
			input:    "123",
			expected: "123",
		},
		{
			name:  "value too large",
			input: "1234",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "AA",
				},
			},
			error: "the rep-A value has 2 bytes, but the maximum replacement length is 1",
		},
		{
			name:     "A beginning of file",
			input:    "aa345678",
			expected: "A\x00345678",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
			},
		},
		{
			name:     "A end of buffer",
			input:    "12aa5678",
			expected: "12A\x005678",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
			},
		},
		{
			name:     "A cross buffer",
			input:    "123aa678",
			expected: "123A\x00678",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
			},
		},
		{
			name:     "A beginning of buffer",
			input:    "1234aa78",
			expected: "1234A\x0078",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
			},
		},
		{
			name:     "A end of file",
			input:    "123456aa",
			expected: "123456A\x00",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
			},
		},
		{
			name:     "A buffer too large",
			input:    "12345aa",
			expected: "12345A\x00",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
			},
		},
		{
			name:     "AB beginning of file",
			input:    "aabb5678",
			expected: "A\x00B\x005678",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
				{
					name:   "rep-B",
					marker: []byte("bb"),
					value:  "B",
				},
			},
		},
		{
			name:     "BA beginning of file",
			input:    "bbaa5678",
			expected: "B\x00A\x005678",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
				{
					name:   "rep-B",
					marker: []byte("bb"),
					value:  "B",
				},
			},
		},
		{
			name:     "AB end of buffer",
			input:    "1234aabb",
			expected: "1234A\x00B\x00",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
				{
					name:   "rep-B",
					marker: []byte("bb"),
					value:  "B",
				},
			},
		},
		{
			name:     "AB cross buffer",
			input:    "123aa6bb",
			expected: "123A\x006B\x00",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
				{
					name:   "rep-B",
					marker: []byte("bb"),
					value:  "B",
				},
			},
		},
		{
			name:     "AB end of file",
			input:    "1234aabb",
			expected: "1234A\x00B\x00",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
				{
					name:   "rep-B",
					marker: []byte("bb"),
					value:  "B",
				},
			},
		},
		{
			name:     "BA end of file",
			input:    "1234bbaa",
			expected: "1234B\x00A\x00",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
				{
					name:   "rep-B",
					marker: []byte("bb"),
					value:  "B",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bytes.NewReader([]byte(tt.input))
			w := &bytes.Buffer{}
			err := copyAndReplace(nil, w, r, buffer, tt.replacements, "test")
			if (err == nil && tt.error != "") || (err != nil && err.Error() != tt.error) {
				t.Fatalf("unexpected error: %v != %v", err, tt.error)
			}
			actual := w.String()
			if actual != tt.expected {
				t.Fatalf("unexpected response body: %q != %q", actual, tt.expected)
			}
		})
	}
}

func TestFindClusterIncludeConfig(t *testing.T) {
	tests := []struct {
		name           string
		configv1client configv1client.ConfigV1Interface
		appsv1client   appsv1client.AppsV1Interface
		expected       manifestInclusionConfiguration
		expectedErr    error
	}{
		{
			name: "no known disabled capabilities become enabled",
			configv1client: fakeconfigv1client.NewClientset(
				&configv1.ClusterVersion{
					ObjectMeta: metav1.ObjectMeta{Name: "version"},
					Spec: configv1.ClusterVersionSpec{
						Capabilities: &configv1.ClusterVersionCapabilitiesSpec{
							BaselineCapabilitySet: configv1.ClusterVersionCapabilitySetNone,
						},
					},
					Status: configv1.ClusterVersionStatus{
						Capabilities: configv1.ClusterVersionCapabilitiesStatus{
							// no capabilities are enabled
							KnownCapabilities: configv1.KnownClusterVersionCapabilities,
						},
					},
				},
				&configv1.FeatureGate{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
					Spec: configv1.FeatureGateSpec{
						FeatureGateSelection: configv1.FeatureGateSelection{
							FeatureSet: configv1.DevPreviewNoUpgrade,
						},
					},
				},
				&configv1.Infrastructure{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
					Status: configv1.InfrastructureStatus{
						PlatformStatus: &configv1.PlatformStatus{Type: configv1.AWSPlatformType},
					},
				},
			).ConfigV1(),
			appsv1client: fakekubeclient.NewClientset(
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-version-operator", Namespace: "openshift-cluster-version"},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{Env: []corev1.EnvVar{{Name: "CLUSTER_PROFILE", Value: "some-profile"}}}},
							},
						},
					},
				},
			).AppsV1(),
			expected: manifestInclusionConfiguration{
				Platform:           ptr.To[string]("aws"),
				Profile:            ptr.To[string]("some-profile"),
				RequiredFeatureSet: ptr.To[string](string(configv1.DevPreviewNoUpgrade)),
				Capabilities: &configv1.ClusterVersionCapabilitiesStatus{
					KnownCapabilities: configv1.KnownClusterVersionCapabilities,
				},
			},
		},
		{
			// no known capabilities at all, i.e., all capabilities are new
			name: "all new capabilities become enabled",
			configv1client: fakeconfigv1client.NewClientset(
				&configv1.ClusterVersion{
					ObjectMeta: metav1.ObjectMeta{Name: "version"},
					Spec: configv1.ClusterVersionSpec{
						Capabilities: &configv1.ClusterVersionCapabilitiesSpec{
							BaselineCapabilitySet: configv1.ClusterVersionCapabilitySetNone,
						},
					},
				},
				&configv1.FeatureGate{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
					Spec: configv1.FeatureGateSpec{
						FeatureGateSelection: configv1.FeatureGateSelection{
							FeatureSet: configv1.DevPreviewNoUpgrade,
						},
					},
				},
				&configv1.Infrastructure{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
					Status: configv1.InfrastructureStatus{
						PlatformStatus: &configv1.PlatformStatus{Type: configv1.AWSPlatformType},
					},
				},
			).ConfigV1(),
			appsv1client: fakekubeclient.NewClientset(
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-version-operator", Namespace: "openshift-cluster-version"},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{Env: []corev1.EnvVar{{Name: "CLUSTER_PROFILE", Value: "some-profile"}}}},
							},
						},
					},
				},
			).AppsV1(),
			expected: manifestInclusionConfiguration{
				Platform:           ptr.To[string]("aws"),
				Profile:            ptr.To[string]("some-profile"),
				RequiredFeatureSet: ptr.To[string](string(configv1.DevPreviewNoUpgrade)),
				Capabilities: &configv1.ClusterVersionCapabilitiesStatus{
					EnabledCapabilities: configv1.KnownClusterVersionCapabilities,
					KnownCapabilities:   configv1.KnownClusterVersionCapabilities,
				},
			},
		},
		{
			name: "default baseline capabilities become enabled",
			configv1client: fakeconfigv1client.NewClientset(
				&configv1.ClusterVersion{
					ObjectMeta: metav1.ObjectMeta{Name: "version"},
				},
				&configv1.FeatureGate{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
					Spec: configv1.FeatureGateSpec{
						FeatureGateSelection: configv1.FeatureGateSelection{
							FeatureSet: configv1.DevPreviewNoUpgrade,
						},
					},
				},
				&configv1.Infrastructure{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
					Status: configv1.InfrastructureStatus{
						PlatformStatus: &configv1.PlatformStatus{Type: configv1.AWSPlatformType},
					},
				},
			).ConfigV1(),
			appsv1client: fakekubeclient.NewClientset(
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-version-operator", Namespace: "openshift-cluster-version"},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{Env: []corev1.EnvVar{{Name: "CLUSTER_PROFILE", Value: "some-profile"}}}},
							},
						},
					},
				},
			).AppsV1(),
			expected: manifestInclusionConfiguration{
				Platform:           ptr.To[string]("aws"),
				Profile:            ptr.To[string]("some-profile"),
				RequiredFeatureSet: ptr.To[string](string(configv1.DevPreviewNoUpgrade)),
				Capabilities: &configv1.ClusterVersionCapabilitiesStatus{
					EnabledCapabilities: configv1.ClusterVersionCapabilitySets[configv1.ClusterVersionCapabilitySetCurrent],
					KnownCapabilities:   configv1.KnownClusterVersionCapabilities,
				},
			},
		},
		{
			name: "err on no cvo deployment",
			configv1client: fakeconfigv1client.NewClientset(
				&configv1.ClusterVersion{
					ObjectMeta: metav1.ObjectMeta{Name: "version"},
				},
				&configv1.FeatureGate{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
					Spec: configv1.FeatureGateSpec{
						FeatureGateSelection: configv1.FeatureGateSelection{
							FeatureSet: configv1.DevPreviewNoUpgrade,
						},
					},
				},
				&configv1.Infrastructure{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
					Status: configv1.InfrastructureStatus{
						PlatformStatus: &configv1.PlatformStatus{Type: configv1.AWSPlatformType},
					},
				},
			).ConfigV1(),
			appsv1client: fakekubeclient.NewClientset().AppsV1(),
			expectedErr:  &errors.StatusError{ErrStatus: metav1.Status{Message: `deployments.apps "cluster-version-operator" not found`}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, actualErr := findClusterIncludeConfig(context.TODO(), tt.configv1client, tt.appsv1client, "x.y.z")
			if diff := cmp.Diff(tt.expectedErr, actualErr, cmp.Comparer(func(x, y error) bool {
				if x == nil || y == nil {
					return x == nil && y == nil
				}
				return x.Error() == y.Error()
			})); diff != "" {
				t.Errorf("%s: actualErr differs from expected:\n%s", tt.name, diff)
			}
			if tt.expectedErr == nil {
				// Ignore the diff on nil and []v1.ClusterVersionCapability{}
				// Ignore the order on []configv1.ClusterVersionCapability
				if diff := cmp.Diff(tt.expected, actual, cmpopts.EquateEmpty(), cmpopts.SortSlices(func(a, b configv1.ClusterVersionCapability) bool { return a < b })); diff != "" {
					t.Errorf("%s: actual differs from expected:\n%s", tt.name, diff)
				}
			}
		})
	}
}
