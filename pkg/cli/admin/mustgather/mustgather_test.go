package mustgather

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/printers"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/diff"

	configv1 "github.com/openshift/api/config/v1"
	imagev1 "github.com/openshift/api/image/v1"
	configv1fake "github.com/openshift/client-go/config/clientset/versioned/fake"
	imageclient "github.com/openshift/client-go/image/clientset/versioned/fake"
)

func TestImagesAndImageStreams(t *testing.T) {

	testCases := []struct {
		name                string
		images              []string
		imageStreams        []string
		expectedImages      []string
		imageObjects        []runtime.Object
		unstructuredObjects []runtime.Object
		coObjects           []runtime.Object
		allImages           bool
	}{
		{
			name: "Default",
			imageObjects: []runtime.Object{
				newImageStream("openshift", "must-gather", withTag("latest", "registry.test/must-gather:1.0.0")),
			},
			expectedImages: []string{"registry.test/must-gather:1.0.0"},
		},
		{
			name:           "DefaultNoMustGatherImageStream",
			expectedImages: []string{"registry.redhat.io/openshift4/ose-must-gather:latest"},
		},
		{
			name:           "MultipleImages",
			images:         []string{"one", "two", "three"},
			expectedImages: []string{"one", "two", "three"},
		},
		{
			name:           "MultipleImageStreams",
			imageStreams:   []string{"test/one:a", "test/two:a", "test/three:a", "test/three:b"},
			expectedImages: []string{"one@a", "two@a", "three@a", "three@b"},
			imageObjects: []runtime.Object{
				newImageStream("test", "one", withTag("a", "one@a")),
				newImageStream("test", "two", withTag("a", "two@a")),
				newImageStream("test", "three",
					withTag("a", "three@a"),
					withTag("b", "three@b"),
				),
			},
		},
		{
			name:           "ImagesAndImageStreams",
			images:         []string{"one", "two"},
			imageStreams:   []string{"test/three:a", "test/four:a"},
			expectedImages: []string{"one", "two", "three@a", "four@a"},
			imageObjects: []runtime.Object{
				newImageStream("test", "three", withTag("a", "three@a")),
				newImageStream("test", "four", withTag("a", "four@a")),
			},
		},
		{
			name:      "AllImagesOnlyCSVs",
			allImages: true,
			unstructuredObjects: []runtime.Object{
				newClusterServiceVersion("csva", "test/csv:a", true),
				newClusterServiceVersion("csvb", "test/csv:b", false),
			},
			expectedImages: []string{"registry.redhat.io/openshift4/ose-must-gather:latest", "test/csv:a"},
		},
		{
			name:      "AllImagesOnlyCOs",
			allImages: true,
			coObjects: []runtime.Object{
				newClusterOperator("coa", "test/co:a", true),
				newClusterOperator("cob", "test/co:b", false),
			},
			expectedImages: []string{"registry.redhat.io/openshift4/ose-must-gather:latest", "test/co:a"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			options := MustGatherOptions{
				IOStreams:    genericiooptions.NewTestIOStreamsDiscard(),
				ConfigClient: configv1fake.NewSimpleClientset(tc.coObjects...),
				Client:       fake.NewSimpleClientset(),
				DynamicClient: dynamicfake.NewSimpleDynamicClient(func() *runtime.Scheme {
					s := runtime.NewScheme()
					s.AddKnownTypeWithName(schema.GroupVersionKind{
						Group:   "operators.coreos.com",
						Version: "v1alpha1",
						Kind:    "ClusterServiceVersionList",
					}, &unstructured.UnstructuredList{})
					return s
				}(), tc.unstructuredObjects...),
				ImageClient:  imageclient.NewSimpleClientset(tc.imageObjects...).ImageV1(),
				Images:       tc.images,
				ImageStreams: tc.imageStreams,
				LogOut:       genericiooptions.NewTestIOStreamsDiscard().Out,
				AllImages:    tc.allImages,
			}
			err := options.completeImages(context.TODO())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(options.Images, tc.expectedImages) {
				t.Fatal(diff.ObjectDiff(options.Images, tc.expectedImages))
			}
		})
	}

}

func newImageStream(namespace, name string, options ...func(*imagev1.ImageStream) *imagev1.ImageStream) *imagev1.ImageStream {
	imageStream := &imagev1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	for _, f := range options {
		imageStream = f(imageStream)
	}
	return imageStream
}

func withTag(tag, reference string) func(*imagev1.ImageStream) *imagev1.ImageStream {
	return func(imageStream *imagev1.ImageStream) *imagev1.ImageStream {
		imageStream.Status.Tags = append(imageStream.Status.Tags, imagev1.NamedTagEventList{
			Tag:   tag,
			Items: []imagev1.TagEvent{{DockerImageReference: reference}},
		})
		return imageStream
	}
}

func newClusterServiceVersion(name, image string, annotate bool) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "ClusterServiceVersion",
	})
	if annotate {
		u.SetAnnotations(map[string]string{"operators.openshift.io/must-gather-image": image})
	}
	return u
}

func newClusterOperator(name, image string, annotate bool) *configv1.ClusterOperator {
	c := &configv1.ClusterOperator{}
	c.SetName(name)
	if annotate {
		c.SetAnnotations(map[string]string{"operators.openshift.io/must-gather-image": image})
	}
	return c
}

func TestGetNamespace(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		Options          *MustGatherOptions
		ShouldBeRetained bool
		ShouldFail       bool
	}{
		"no namespace given": {
			Options: &MustGatherOptions{
				Client: fake.NewSimpleClientset(),
			},
			ShouldBeRetained: false,
		},
		"namespace given": {
			Options: &MustGatherOptions{
				Client: fake.NewSimpleClientset(
					&corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-namespace",
						},
					},
				),
				RunNamespace: "test-namespace",
			},
			ShouldBeRetained: true,
		},
		"namespace given, but does not exist": {
			Options: &MustGatherOptions{
				Client:       fake.NewSimpleClientset(),
				RunNamespace: "test-namespace",
			},
			ShouldBeRetained: true,
			ShouldFail:       true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			tc.Options.PrinterCreated = printers.NewDiscardingPrinter()
			tc.Options.PrinterDeleted = printers.NewDiscardingPrinter()

			ns, cleanup, err := tc.Options.getNamespace(context.TODO())
			if err != nil {
				if tc.ShouldFail {
					return
				}

				t.Fatal(err)
			}

			if _, err = tc.Options.Client.CoreV1().Namespaces().Get(context.TODO(), ns.Name, metav1.GetOptions{}); err != nil {
				if !k8sapierrors.IsNotFound(err) {
					t.Fatal(err)
				}

				t.Error("namespace should exist")
			}

			cleanup()

			if _, err = tc.Options.Client.CoreV1().Namespaces().Get(context.TODO(), ns.Name, metav1.GetOptions{}); err != nil {
				if !k8sapierrors.IsNotFound(err) {
					t.Fatal(err)
				} else if tc.ShouldBeRetained {
					t.Error("namespace should still exist")
				}
			}
		})
	}
}

func buildTestNode(name string, apply func(*corev1.Node)) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:     name,
			SelfLink: fmt.Sprintf("/api/v1/nodes/%s", name),
			Labels:   map[string]string{},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}
	if apply != nil {
		apply(node)
	}
	return node
}

func buildTestControlPlaneNode(name string, apply func(*corev1.Node)) *corev1.Node {
	node := buildTestNode(name, apply)
	node.ObjectMeta.Labels[controlPlaneNodeRoleLabel] = ""
	return node
}

func TestGetCandidateNodeNames(t *testing.T) {
	tests := []struct {
		description string
		nodeList    *corev1.NodeList
		hasMaster   bool
		nodeNames   []string
	}{
		{
			description: "No nodes are ready and reacheable (hasMaster=true)",
			nodeList:    &corev1.NodeList{},
			hasMaster:   true,
			nodeNames:   []string{},
		},
		{
			description: "No nodes are ready and reacheable (hasMaster=false)",
			nodeList:    &corev1.NodeList{},
			hasMaster:   false,
			nodeNames:   []string{},
		},
		{
			description: "Control plane nodes are ready and reacheable",
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					*buildTestControlPlaneNode("controlplane1", nil),
					*buildTestControlPlaneNode("controlplane2", nil),
				},
			},
			hasMaster: true,
			nodeNames: []string{"controlplane2", "controlplane1"},
		},
		{
			description: "Some control plane nodes are not ready or reacheable",
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					*buildTestControlPlaneNode("controlplane1", func(node *corev1.Node) {
						node.Status.Conditions = []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						}
					}),
					*buildTestControlPlaneNode("controlplane2", nil),
					*buildTestControlPlaneNode("controlplane3", func(node *corev1.Node) {
						node.Spec.Taints = []corev1.Taint{
							{
								Key: unreachableTaintKey,
							},
						}
					}),
					*buildTestControlPlaneNode("controlplane4", func(node *corev1.Node) {
						node.Spec.Taints = []corev1.Taint{
							{
								Key: notReadyTaintKey,
							},
						}
					}),
				},
			},
			hasMaster: true,
			nodeNames: []string{"controlplane2"},
		},
		{
			description: "Mix of control plane and worker nodes (at least one reachable and ready control plane node)",
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					*buildTestControlPlaneNode("controlplane1", func(node *corev1.Node) {
						node.Status.Conditions = []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						}
					}),
					*buildTestControlPlaneNode("controlplane2", nil),
					*buildTestNode("controlplane3", nil),
				},
			},
			hasMaster: true,
			nodeNames: []string{"controlplane2"},
		},
		{
			description: "Mix of control plane and worker nodes (no ready and reachable control plane node)",
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					*buildTestControlPlaneNode("controlplane1", func(node *corev1.Node) {
						node.Status.Conditions = []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						}
					}),
					*buildTestControlPlaneNode("controlplane2", func(node *corev1.Node) {
						node.Spec.Taints = []corev1.Taint{
							{
								Key: unreachableTaintKey,
							},
						}
					}),
					*buildTestControlPlaneNode("controlplane3", func(node *corev1.Node) {
						node.Spec.Taints = []corev1.Taint{
							{
								Key: notReadyTaintKey,
							},
						}
					}),
					*buildTestControlPlaneNode("worker1", nil),
				},
			},
			hasMaster: true,
			nodeNames: []string{"worker1"},
		},
		{
			description: "Mix of control plane and worker nodes (no ready and not reachable nodes with unschedulable)",
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					*buildTestControlPlaneNode("controlplane1", func(node *corev1.Node) {
						node.Status.Conditions = []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						}
					}),
					*buildTestControlPlaneNode("controlplane2", func(node *corev1.Node) {
						node.Spec.Taints = []corev1.Taint{
							{
								Key: unreachableTaintKey,
							},
						}
					}),
					*buildTestControlPlaneNode("controlplane3", func(node *corev1.Node) {
						node.Spec.Taints = []corev1.Taint{
							{
								Key: notReadyTaintKey,
							},
						}
					}),
					*buildTestControlPlaneNode("worker1", func(node *corev1.Node) {
						node.Spec.Unschedulable = true
					}),
				},
			},
			nodeNames: []string{"worker1"},
		},
		{
			description: "No ready and reachable nodes (nodes with the recent teartbeat time are sorted left)",
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					*buildTestControlPlaneNode("controlplane1", func(node *corev1.Node) {
						node.Status.Conditions = []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						}
					}),
					*buildTestControlPlaneNode("controlplane2", func(node *corev1.Node) {
						node.Status.Conditions = []corev1.NodeCondition{}
					}),
					*buildTestNode("worker1", func(node *corev1.Node) {
						node.Status.Conditions = []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse, LastHeartbeatTime: metav1.Time{Time: time.Date(2025, time.July, 10, 10, 20, 0, 0, time.UTC)}},
						}
					}),
					*buildTestNode("other1", func(node *corev1.Node) {
						node.Status.Conditions = []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse, LastHeartbeatTime: metav1.Time{Time: time.Date(2025, time.July, 10, 10, 30, 0, 0, time.UTC)}},
						}
					}),
				},
			},
			hasMaster: false,
			nodeNames: []string{"other1", "worker1", "controlplane2", "controlplane1"},
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			names := getCandidateNodeNames(test.nodeList, test.hasMaster)
			if !reflect.DeepEqual(test.nodeNames, names) {
				t.Fatalf("Expected and computed list of node names differ. Expected %#v. Got %#v.", test.nodeNames, names)
			}
		})
	}
}

func TestBuildPodCommand(t *testing.T) {
	tests := []struct {
		name                     string
		volumeUsageCheckerScript string
		gatherCommand            string
		expectedCommand          string
	}{
		{
			name:                     "default gather command",
			volumeUsageCheckerScript: "sleep infinity",
			gatherCommand:            "/usr/bin/gather",
			expectedCommand: `if command -v setsid >/dev/null 2>&1 && command -v ps >/dev/null 2>&1 && command -v pkill >/dev/null 2>&1; then
  HAVE_SESSION_TOOLS=true
else
  HAVE_SESSION_TOOLS=false
fi

sleep infinity & if [ "$HAVE_SESSION_TOOLS" = "true" ]; then
  setsid -w bash <<-MUSTGATHER_EOF
/usr/bin/gather
MUSTGATHER_EOF
else
  /usr/bin/gather
fi; sync && echo 'Caches written to disk'`,
		},
		{
			name:                     "custom gather command",
			volumeUsageCheckerScript: "sleep infinity",
			gatherCommand:            "sed -i 's#--rotated-pod-logs# #g' /usr/bin/*gather* && /usr/bin/gather",
			expectedCommand: `if command -v setsid >/dev/null 2>&1 && command -v ps >/dev/null 2>&1 && command -v pkill >/dev/null 2>&1; then
  HAVE_SESSION_TOOLS=true
else
  HAVE_SESSION_TOOLS=false
fi

sleep infinity & if [ "$HAVE_SESSION_TOOLS" = "true" ]; then
  setsid -w bash <<-MUSTGATHER_EOF
sed -i 's#--rotated-pod-logs# #g' /usr/bin/*gather* && /usr/bin/gather
MUSTGATHER_EOF
else
  sed -i 's#--rotated-pod-logs# #g' /usr/bin/*gather* && /usr/bin/gather
fi; sync && echo 'Caches written to disk'`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := buildPodCommand(test.volumeUsageCheckerScript, test.gatherCommand)
			if cmd != test.expectedCommand {
				t.Errorf("Unexpected pod command was generated: \n%s\n", cmp.Diff(test.expectedCommand, cmd))
			}
		})
	}
}

func TestNewPod(t *testing.T) {
	generateMustGatherPod := func(image string, update func(pod *corev1.Pod)) *corev1.Pod {
		pod := defaultMustGatherPod(image)
		update(pod)
		return pod
	}

	tests := []struct {
		name        string
		options     *MustGatherOptions
		hasMasters  bool
		node        string
		affinity    *corev1.Affinity
		expectedPod *corev1.Pod
	}{
		{
			name: "node set, affinity provided, affinity not set",
			options: &MustGatherOptions{
				VolumePercentage: defaultVolumePercentage,
				SourceDir:        defaultSourceDir,
			},
			node:     "node1",
			affinity: buildNodeAffinity([]string{"node2"}),
			expectedPod: generateMustGatherPod("image", func(pod *corev1.Pod) {
				pod.Spec.NodeName = "node1"
			}),
		},
		{
			name: "node not set, affinity provided, affinity set",
			options: &MustGatherOptions{
				VolumePercentage: defaultVolumePercentage,
				SourceDir:        defaultSourceDir,
			},
			affinity: buildNodeAffinity([]string{"node2"}),
			expectedPod: generateMustGatherPod("image", func(pod *corev1.Pod) {
				pod.Spec.Affinity = buildNodeAffinity([]string{"node2"})
			}),
		},
		{
			name: "custom source dir",
			options: &MustGatherOptions{
				VolumePercentage: defaultVolumePercentage,
				SourceDir:        "custom-source-dir",
			},
			expectedPod: generateMustGatherPod("image", func(pod *corev1.Pod) {
				volumeUsageChecker := fmt.Sprintf(volumeUsageCheckerScript, "custom-source-dir", defaultVolumePercentage)
				podCmd := buildPodCommand(volumeUsageChecker, defaultMustGatherCommand)
				pod.Spec.Containers[0].Command = []string{"/bin/bash", "-c", podCmd}
				pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{
					Name:      "must-gather-output",
					MountPath: "custom-source-dir",
				}}
				pod.Spec.Containers[1].VolumeMounts = []corev1.VolumeMount{{
					Name:      "must-gather-output",
					MountPath: "custom-source-dir",
				}}
			}),
		},
		{
			name: "host network",
			options: &MustGatherOptions{
				VolumePercentage: defaultVolumePercentage,
				SourceDir:        defaultSourceDir,
				HostNetwork:      true,
			},
			expectedPod: generateMustGatherPod("image", func(pod *corev1.Pod) {
				pod.Spec.HostNetwork = true
				pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{
						Add: []corev1.Capability{
							corev1.Capability("CAP_NET_RAW"),
						},
					},
				}
			}),
		},
		{
			name: "with control plane nodes",
			options: &MustGatherOptions{
				VolumePercentage: defaultVolumePercentage,
				SourceDir:        defaultSourceDir,
			},
			hasMasters: true,
			expectedPod: generateMustGatherPod("image", func(pod *corev1.Pod) {
				pod.Spec.NodeSelector[controlPlaneNodeRoleLabel] = ""
			}),
		},
		{
			name: "with custom command",
			options: &MustGatherOptions{
				VolumePercentage: defaultVolumePercentage,
				SourceDir:        defaultSourceDir,
				Command:          []string{"custom_command", "with_params"},
			},
			expectedPod: generateMustGatherPod("image", func(pod *corev1.Pod) {
				cleanSourceDir := path.Clean(defaultSourceDir)
				volumeUsageChecker := fmt.Sprintf(volumeUsageCheckerScript, cleanSourceDir, defaultVolumePercentage)
				podCmd := buildPodCommand(volumeUsageChecker, "custom_command with_params")
				pod.Spec.Containers[0].Command = []string{"/bin/bash", "-c", podCmd}
				pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{
					Name:      "must-gather-output",
					MountPath: cleanSourceDir,
				}}
				pod.Spec.Containers[1].VolumeMounts = []corev1.VolumeMount{{
					Name:      "must-gather-output",
					MountPath: cleanSourceDir,
				}}
			}),
		},
		{
			name: "with since",
			options: &MustGatherOptions{
				VolumePercentage: defaultVolumePercentage,
				SourceDir:        defaultSourceDir,
				Since:            2 * time.Minute,
			},
			expectedPod: generateMustGatherPod("image", func(pod *corev1.Pod) {
				pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env, corev1.EnvVar{
					Name:  "MUST_GATHER_SINCE",
					Value: "2m0s",
				})
			}),
		},
		{
			name: "with sincetime",
			options: &MustGatherOptions{
				VolumePercentage: defaultVolumePercentage,
				SourceDir:        defaultSourceDir,
				SinceTime:        "2023-09-24T15:30:00Z",
			},
			expectedPod: generateMustGatherPod("image", func(pod *corev1.Pod) {
				pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env, corev1.EnvVar{
					Name:  "MUST_GATHER_SINCE_TIME",
					Value: "2023-09-24T15:30:00Z",
				})
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tPod := test.options.newPod(test.node, "image", test.hasMasters, test.affinity)
			if !cmp.Equal(test.expectedPod, tPod) {
				t.Errorf("Unexpected pod command was generated: \n%s\n", cmp.Diff(test.expectedPod, tPod))
			}
		})
	}
}
