package release

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"

	imageapi "github.com/openshift/api/image/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMirrorImages(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		is                  *imageapi.ImageStream
		expectedWarningMsgs []string
		expectedErr         string
	}{
		{
			is:                  nil,
			expectedWarningMsgs: []string{},
			expectedErr:         "unable to retrieve release image info: must specify an image containing a release payload with --from",
		},
		{
			is: &imageapi.ImageStream{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       imageapi.ImageStreamSpec{},
				Status:     imageapi.ImageStreamStatus{},
			},
			expectedWarningMsgs: []string{
				"warning: No release authenticity verification is configured, all releases are considered unverified",
				"warning: An image was retrieved that failed verification: verification is not possible",
				"warning: Release image contains no image references - is this a valid release?",
			},
			expectedErr: "",
		},
		{
			is: &imageapi.ImageStream{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec: imageapi.ImageStreamSpec{
					LookupPolicy: imageapi.ImageLookupPolicy{},
					Tags: []imageapi.TagReference{
						{
							Name: "test",
							From: &corev1.ObjectReference{
								Name: "quay.io/test/other@sha256:0000000000000000000000000000000000000001",
								Kind: "DockerImage",
							},
						},
					},
				},
				Status: imageapi.ImageStreamStatus{},
			},
			expectedWarningMsgs: []string{
				"No release authenticity verification is configured, all releases are considered unverified",
				"warning: An image was retrieved that failed verification: verification is not possible",
				"warning: Release image contains no image references - is this a valid release?",
			},
			expectedErr: "release tag \"test\" is not valid: invalid checksum digest length",
		},
	}

	ioStream, _, _, errOut := genericiooptions.NewTestIOStreams()

	for _, tt := range tests {
		options := NewNewOptions(ioStream)
		err := options.mirrorImages(ctx, tt.is)

		if err != nil {
			if len(tt.expectedErr) == 0 {
				t.Fatalf("unexpected error occurred %v\n", err)
			}

			if err.Error() != tt.expectedErr {
				t.Fatalf("expected error %v but actual %v\n", tt.expectedErr, err.Error())
			}
		} else {
			if len(tt.expectedErr) > 0 {
				t.Fatalf("expected error %v but got none\n", tt.expectedErr)
			}
		}

		if len(tt.expectedWarningMsgs) == 0 && len(errOut.String()) > 0 {
			t.Fatalf("unexpected error %v fired\n", errOut.String())
		}

		for _, expectedErr := range tt.expectedWarningMsgs {
			if !strings.Contains(errOut.String(), expectedErr) {
				t.Fatalf("error %v expected but not fired\n", expectedErr)
			}
		}
	}
}
