package networktools

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/diff"

	imagev1 "github.com/openshift/api/image/v1"
	imageclient "github.com/openshift/client-go/image/clientset/versioned/fake"
)

func TestImage(t *testing.T) {

	testCases := []struct {
		name          string
		image         string
		expectedImage string
		objects       []runtime.Object
	}{
		{
			name: "Default",
			objects: []runtime.Object{
				newImageStream("openshift", "network-tools", withTag("latest", "registry.test/network-tools:1.0.0")),
			},
			expectedImage: string("registry.test/network-tools:1.0.0"),
		},
		{
			name:          "DefaultNoNetworkToolsImageStream",
			expectedImage: string("quay.io/openshift/origin-network-tools:latest"),
		},
		{
			name:          "UserImage",
			image:         string("one"),
			expectedImage: string("one"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			options := Options{
				IOStreams:   genericclioptions.NewTestIOStreamsDiscard(),
				Client:      fake.NewSimpleClientset(),
				ImageClient: imageclient.NewSimpleClientset(tc.objects...).ImageV1(),
				Image:       tc.image,
				LogOut:      genericclioptions.NewTestIOStreamsDiscard().Out,
			}
			err := options.completeImage()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(options.Image, tc.expectedImage) {
				t.Fatal(diff.ObjectDiff(options.Image, tc.expectedImage))
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
