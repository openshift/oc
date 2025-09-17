package mustgather

import (
	"context"
	"reflect"
	"testing"

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
			err := options.completeImages()
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

			ns, cleanup, err := tc.Options.getNamespace()
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
