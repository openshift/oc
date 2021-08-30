package mustgather

import (
	"reflect"
	"testing"

	imagev1 "github.com/openshift/api/image/v1"
	imageclient "github.com/openshift/client-go/image/clientset/versioned/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/diff"
)

func TestImagesAndImageStreams(t *testing.T) {

	testCases := []struct {
		name           string
		images         []string
		imageStreams   []string
		expectedImages []string
		objects        []runtime.Object
	}{
		{
			name: "Default",
			objects: []runtime.Object{
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
			objects: []runtime.Object{
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
			objects: []runtime.Object{
				newImageStream("test", "three", withTag("a", "three@a")),
				newImageStream("test", "four", withTag("a", "four@a")),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			options := MustGatherOptions{
				IOStreams:    genericclioptions.NewTestIOStreamsDiscard(),
				Client:       fake.NewSimpleClientset(),
				ImageClient:  imageclient.NewSimpleClientset(tc.objects...).ImageV1(),
				Images:       tc.images,
				ImageStreams: tc.imageStreams,
				LogOut:       genericclioptions.NewTestIOStreamsDiscard().Out,
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
			Items: append([]imagev1.TagEvent{{DockerImageReference: reference}}),
		})
		return imageStream
	}
}

func TestEnvironmentVariables(t *testing.T) {

	testCases := []struct {
		name            string
		environmentStr  []string
		expectedEnviron []corev1.EnvVar
		objects         []runtime.Object
	}{
		// No Environ
		{
			name:            "DefaultNoEnvironmentVariable",
			environmentStr:  nil,
			expectedEnviron: []corev1.EnvVar{},
		},
		// Multiple Environ
		{
			name:           "MultipleEnvironmentVariables",
			environmentStr: []string{"FIELD1=VALUE1", "FIELD2=VALUE2"},
			expectedEnviron: []corev1.EnvVar{
				{Name: "FIELD1", Value: "VALUE1"},
				{Name: "FIELD2", Value: "VALUE2"},
			},
		},
		{
			name:           "EnvironmentVariableContainsEqualSign",
			environmentStr: []string{"FIELD1=VALUE1=VALUE2"},
			expectedEnviron: []corev1.EnvVar{
				{Name: "FIELD1", Value: "VALUE1=VALUE2"},
			},
		},
		{
			name:           "EmptyString",
			environmentStr: []string{"FIELD1="},
			expectedEnviron: []corev1.EnvVar{
				{Name: "FIELD1", Value: ""},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			options := MustGatherOptions{
				IOStreams:                  genericclioptions.NewTestIOStreamsDiscard(),
				Client:                     fake.NewSimpleClientset(),
				environmentVariablesString: tc.environmentStr,
				LogOut:                     genericclioptions.NewTestIOStreamsDiscard().Out,
			}
			err := options.completeEnvironmentVariables()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(options.EnvironmentVariables, tc.expectedEnviron) {
				t.Fatal(diff.ObjectDiff(options.EnvironmentVariables, tc.expectedEnviron))
			}
		})
	}
}
