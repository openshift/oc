package debug

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
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
			IOStreams:         genericclioptions.NewTestIOStreamsDiscard(),
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

func TestSafeToDebug(t *testing.T) {
	tests := []struct {
		name          string
		pod           *corev1.Pod
		expectToDebug bool
	}{
		{
			name:          "when pod has one container and no volumes",
			pod:           getPod().pod,
			expectToDebug: true,
		},
		{
			name:          "when pod has multiple containers but no volumes",
			pod:           getPod().addContainer(1).pod,
			expectToDebug: true,
		},
		{
			name: "when pod has multiple containers but only configmap volumes",
			pod: getPod().addContainer(1).addVolume(corev1.Volume{
				Name: "c",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{},
				},
			}).pod,
			expectToDebug: true,
		},
		{
			name: "when pod has multiple containers but only secret volumes",
			pod: getPod().addContainer(1).addVolume(corev1.Volume{
				Name: "c",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{},
				},
			}).pod,
			expectToDebug: true,
		},
		{
			name: "when pod has multiple containers and one pvs",
			pod: getPod().addContainer(1).addVolume(corev1.Volume{
				Name: "c",
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{},
				},
			}).pod,
			expectToDebug: false,
		},
	}

	for _, test := range tests {
		_, debugFlag := safeToDebugPod(test.pod)
		if debugFlag != test.expectToDebug {
			t.Errorf("expected debug flag to be %v, got %v", test.expectToDebug, debugFlag)
		}
	}
}

type podBuilder struct {
	pod *corev1.Pod
}

func getPod() *podBuilder {
	return &podBuilder{
		pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "debug-test-pod",
				UID:       "debug-test-pod-uid",
				Namespace: "debug-test",
			},
			Spec: v1.PodSpec{
				NodeName: "debug-test-host",
				Containers: []corev1.Container{
					{
						Name:  "foobar",
						Image: "quay.io/foobar",
					},
				},
			},
		},
	}
}

func (pb *podBuilder) addVolume(v corev1.Volume) *podBuilder {
	pb.pod.Spec.Volumes = append(pb.pod.Spec.Volumes, v)
	return pb
}

func (pb *podBuilder) addContainer(index int) *podBuilder {
	pb.pod.Spec.Containers = append(pb.pod.Spec.Containers, corev1.Container{
		Name:  fmt.Sprintf("hello-%d", index),
		Image: "quay.io/hello",
	})
	return pb
}
