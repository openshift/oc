package clientcmd

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imagev1 "github.com/openshift/api/image/v1"
	fakeimagev1client "github.com/openshift/client-go/image/clientset/versioned/fake"
)

func image(pullSpec string) *imagev1.Image {
	return &imagev1.Image{
		ObjectMeta:           metav1.ObjectMeta{Name: "sha256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"},
		DockerImageReference: pullSpec,
	}
}

func isimage(name, pullSpec string) *imagev1.ImageStreamImage {
	i := image(pullSpec)
	return &imagev1.ImageStreamImage{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Image:      *i,
	}
}

func istag(name, namespace, pullSpec string) *imagev1.ImageStreamTag {
	i := image(pullSpec)
	return &imagev1.ImageStreamTag{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Image:      *i,
	}
}

func TestResolveImagePullSpec(t *testing.T) {
	testCases := []struct {
		client    *fakeimagev1client.Clientset
		source    string
		input     string
		expect    string
		expectErr bool
	}{
		{
			//client: imageclient.NewSimpleClientset(isimage("test@sha256:foo", "registry.url/image/test:latest")),
			client: fakeimagev1client.NewSimpleClientset(isimage("test@sha256:foo", "registry.url/image/test:latest")),
			source: "isimage",
			input:  "test@sha256:foo",
			expect: "registry.url/image/test:latest",
		},
		{
			client: fakeimagev1client.NewSimpleClientset(istag("test:1.1", "default", "registry.url/image/test:latest")),
			source: "istag",
			input:  "test:1.1",
			expect: "registry.url/image/test:latest",
		},
		{
			client: fakeimagev1client.NewSimpleClientset(istag("test:1.1", "user", "registry.url/image/test:latest")),
			source: "istag",
			input:  "user/test:1.1",
			expect: "registry.url/image/test:latest",
		},
		{
			client: fakeimagev1client.NewSimpleClientset(),
			source: "docker",
			input:  "test:latest",
			expect: "test:latest",
		},
		{
			client:    fakeimagev1client.NewSimpleClientset(),
			source:    "istag",
			input:     "test:1.2",
			expectErr: true,
		},
		{
			client:    fakeimagev1client.NewSimpleClientset(),
			source:    "istag",
			input:     "test:1.2",
			expectErr: true,
		},
		{
			client:    fakeimagev1client.NewSimpleClientset(),
			source:    "unknown",
			input:     "",
			expectErr: true,
		},
	}

	for i, test := range testCases {
		t.Logf("[%d] trying to resolve %q %s and expecting %q (expectErr=%t)", i, test.source, test.input, test.expect, test.expectErr)
		result, err := resolveImagePullSpec(context.TODO(), test.client.ImageV1(), test.source, test.input, "default")
		if err != nil && !test.expectErr {
			t.Errorf("[%d] unexpected error: %v", i, err)
		} else if err == nil && test.expectErr {
			t.Errorf("[%d] expected error but got none and result %q", i, result)
		}
		if test.expect != result {
			t.Errorf("[%d] expected %q, but got %q", i, test.expect, result)
		}
	}
}
