package debug

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/kubectl/pkg/cmd/attach"
	"k8s.io/kubectl/pkg/cmd/exec"
	"k8s.io/pod-security-admission/api"

	fakekubeclient "k8s.io/client-go/kubernetes/fake"
	fakecorev1client "k8s.io/client-go/kubernetes/typed/core/v1/fake"
	clientgotesting "k8s.io/client-go/testing"
)

func TestGetNamespace(t *testing.T) {
	tests := []struct {
		name                  string
		objectNamespace       string
		namespace             string
		toNamespace           string
		explicitNamespace     bool
		expectedNamespace     string
		isNode                bool
		clientNamespaceObject *corev1.Namespace
	}{
		{
			name:              "default namespace is used",
			objectNamespace:   "default",
			namespace:         "default",
			toNamespace:       "",
			explicitNamespace: false,
			expectedNamespace: "default",
			isNode:            false,
		},
		{
			name:              "--to-namespace is passed",
			objectNamespace:   "openshift-etcd",
			namespace:         "openshift-etcd",
			toNamespace:       "openshift-monitoring",
			explicitNamespace: false,
			expectedNamespace: "openshift-monitoring",
			isNode:            false,
		},
		{
			name:              "--to-namespace and --namespace are passed together",
			objectNamespace:   "openshift-etcd",
			namespace:         "openshift-etcd",
			toNamespace:       "openshift-monitoring",
			explicitNamespace: true,
			expectedNamespace: "openshift-monitoring",
			isNode:            false,
		},
		{
			name:              "--namespace is used",
			objectNamespace:   "",
			namespace:         "openshift-etcd",
			toNamespace:       "",
			explicitNamespace: true,
			expectedNamespace: "openshift-etcd",
			isNode:            false,
		},
		{
			name:              "--namespace is used also for nodes",
			objectNamespace:   "",
			namespace:         "openshift-etcd",
			toNamespace:       "",
			explicitNamespace: true,
			expectedNamespace: "openshift-etcd",
			isNode:            true,
		},
		{
			name:              "--to-namespace and --namespace are passed together also for nodes",
			objectNamespace:   "",
			namespace:         "openshift-etcd",
			toNamespace:       "openshift-monitoring",
			explicitNamespace: true,
			expectedNamespace: "openshift-monitoring",
			isNode:            true,
		},
		{
			name:              "--to-namespace is passed also for nodes",
			objectNamespace:   "",
			namespace:         "openshift-etcd",
			toNamespace:       "openshift-monitoring",
			explicitNamespace: false,
			expectedNamespace: "openshift-monitoring",
			isNode:            true,
		},
		{
			name:              "--to-namespace is used even if the current namespace is privileged",
			objectNamespace:   "",
			namespace:         "openshift-etcd",
			toNamespace:       "openshift-monitoring",
			explicitNamespace: false,
			expectedNamespace: "openshift-monitoring",
			isNode:            true,
			clientNamespaceObject: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "openshift-etcd",
					Labels: map[string]string{
						api.EnforceLevelLabel: string(api.LevelPrivileged),
						api.AuditLevelLabel:   string(api.LevelPrivileged),
						api.WarnLevelLabel:    string(api.LevelPrivileged),
					},
				},
			},
		},
		{
			name:              "default namespace should be used because it is privileged",
			objectNamespace:   "",
			namespace:         "openshift-monitoring",
			toNamespace:       "",
			explicitNamespace: false,
			expectedNamespace: "openshift-monitoring",
			isNode:            true,
			clientNamespaceObject: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "openshift-monitoring",
					Labels: map[string]string{
						api.EnforceLevelLabel: string(api.LevelPrivileged),
					},
				},
			},
		},
		{
			name:              "temporary namespace should be created if no label",
			objectNamespace:   "",
			namespace:         "openshift-monitoring",
			toNamespace:       "",
			explicitNamespace: false,
			expectedNamespace: "openshift-debug-1111",
			isNode:            true,
			clientNamespaceObject: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "openshift-monitoring",
					Labels: nil,
				},
			},
		},
		{
			name:              "temporary namespace should be created if not privileged",
			objectNamespace:   "",
			namespace:         "openshift-monitoring",
			toNamespace:       "",
			explicitNamespace: false,
			expectedNamespace: "openshift-debug-1111",
			isNode:            true,
			clientNamespaceObject: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "openshift-monitoring",
					Labels: map[string]string{
						api.EnforceLevelLabel: string(api.LevelRestricted),
					},
				},
			},
		},
	}

	for _, test := range tests {
		var fakeCoreClient *fakecorev1client.FakeCoreV1
		if test.clientNamespaceObject != nil {
			fakeCoreClient = &fakecorev1client.FakeCoreV1{Fake: &(fakekubeclient.NewSimpleClientset(test.clientNamespaceObject).Fake)}

			fakeCoreClient.PrependReactor("create", "namespaces", func(a clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
				return true, &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "openshift-debug-1111",
						Labels: map[string]string{
							api.EnforceLevelLabel: string(api.LevelPrivileged),
						},
					},
				}, nil
			})
		}

		debugOptions := &DebugOptions{
			IOStreams:         genericiooptions.NewTestIOStreamsDiscard(),
			IsNode:            test.isNode,
			ExplicitNamespace: test.explicitNamespace,
			ToNamespace:       test.toNamespace,
			Namespace:         test.namespace,
			CoreClient:        fakeCoreClient,
			Attach: attach.AttachOptions{
				StreamOptions: exec.StreamOptions{
					Quiet: false,
				},
			},
		}

		actualNS, _, err := debugOptions.getNamespace(test.objectNamespace)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if actualNS != test.expectedNamespace {
			t.Errorf("test %s: expected namespace %s, got %s", test.name, test.expectedNamespace, actualNS)
		}
	}
}

// Generated by Claude
func TestCreateLabelMap(t *testing.T) {
	tests := []struct {
		name           string
		currentLabels  map[string]string
		keepLabels     bool
		expectedLabels map[string]string
	}{
		{
			name:           "keep labels with non-nil current labels",
			currentLabels:  map[string]string{"app": "test", debugPodLabelManagedBy: "oc-debug"},
			keepLabels:     true,
			expectedLabels: map[string]string{"app": "test", debugPodLabelManagedBy: "oc-debug"},
		},
		{
			name:           "keep labels with nil current labels",
			currentLabels:  nil,
			keepLabels:     true,
			expectedLabels: make(map[string]string),
		},
		{
			name:           "don't keep labels but preserve managed-by label",
			currentLabels:  map[string]string{"app": "test", debugPodLabelManagedBy: "oc-debug", "version": "v1"},
			keepLabels:     false,
			expectedLabels: map[string]string{debugPodLabelManagedBy: "oc-debug"},
		},
		{
			name:           "don't keep labels with no managed-by label",
			currentLabels:  map[string]string{"app": "test", "version": "v1"},
			keepLabels:     false,
			expectedLabels: make(map[string]string),
		},
		{
			name:           "don't keep labels with nil current labels",
			currentLabels:  nil,
			keepLabels:     false,
			expectedLabels: make(map[string]string),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := createLabelMap(test.currentLabels, test.keepLabels)

			if len(result) != len(test.expectedLabels) {
				t.Errorf("expected %d labels, got %d", len(test.expectedLabels), len(result))
				return
			}

			for key, expectedValue := range test.expectedLabels {
				if actualValue, exists := result[key]; !exists || actualValue != expectedValue {
					t.Errorf("expected label %s=%s, got %s=%s", key, expectedValue, key, actualValue)
				}
			}

			// Ensure no unexpected labels are present
			for key := range result {
				if _, expected := test.expectedLabels[key]; !expected {
					t.Errorf("unexpected label %s=%s", key, result[key])
				}
			}
		})
	}
}
