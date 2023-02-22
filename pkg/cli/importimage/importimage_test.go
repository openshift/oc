package importimage

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imagev1 "github.com/openshift/api/image/v1"
	imagefake "github.com/openshift/client-go/image/clientset/versioned/fake"
	"github.com/openshift/oc/pkg/cli/tag"
)

func TestCreateImageImport(t *testing.T) {
	testCases := map[string]struct {
		name               string
		from               string
		stream             *imagev1.ImageStream
		all                bool
		confirm            bool
		scheduled          bool
		insecure           *bool
		referencePolicy    string
		err                string
		importMode         imagev1.ImportModeType
		expectedImages     []imagev1.ImageImportSpec
		expectedRepository *imagev1.RepositoryImportSpec
	}{
		"import from non-existing": {
			name: "nonexisting",
			err:  "pass --confirm to create and import",
		},
		"confirmed import from non-existing": {
			name:    "nonexisting",
			confirm: true,
			expectedImages: []imagev1.ImageImportSpec{{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "nonexisting"},
				To:              &corev1.LocalObjectReference{Name: "latest"},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				ImportPolicy:    imagev1.TagImportPolicy{ImportMode: imagev1.ImportModeLegacy},
			}},
		},
		"confirmed import all from non-existing": {
			name:    "nonexisting",
			all:     true,
			confirm: true,
			expectedRepository: &imagev1.RepositoryImportSpec{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "nonexisting"},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				ImportPolicy:    imagev1.TagImportPolicy{ImportMode: imagev1.ImportModeLegacy},
			},
		},
		"import from .spec.dockerImageRepository": {
			name: "testis",
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					DockerImageRepository: "repo.com/somens/someimage",
					Tags:                  []imagev1.TagReference{},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage"},
				To:              &corev1.LocalObjectReference{Name: "latest"},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				ImportPolicy:    imagev1.TagImportPolicy{ImportMode: imagev1.ImportModeLegacy},
			}},
		},
		"import from .spec.dockerImageRepository non-existing tag": {
			name: "testis:nonexisting",
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					DockerImageRepository: "repo.com/somens/someimage",
					Tags:                  []imagev1.TagReference{},
				},
			},
			err: `"nonexisting" does not exist on the image stream`,
		},
		"import all from .spec.dockerImageRepository": {
			name: "testis",
			all:  true,
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					DockerImageRepository: "repo.com/somens/someimage",
					Tags:                  []imagev1.TagReference{},
				},
			},
			expectedRepository: &imagev1.RepositoryImportSpec{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage"},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				ImportPolicy:    imagev1.TagImportPolicy{ImportMode: imagev1.ImportModeLegacy},
			},
		},
		"import all from .spec.dockerImageRepository with different from": {
			name: "testis",
			from: "totally_different_spec",
			all:  true,
			err:  "different import spec",
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					DockerImageRepository: "repo.com/somens/someimage",
					Tags:                  []imagev1.TagReference{},
				},
			},
		},
		"import all from .spec.dockerImageRepository with confirmed different from": {
			name:    "testis",
			from:    "totally/different/spec",
			all:     true,
			confirm: true,
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					DockerImageRepository: "repo.com/somens/someimage",
					Tags:                  []imagev1.TagReference{},
				},
			},
			expectedRepository: &imagev1.RepositoryImportSpec{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "totally/different/spec"},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				ImportPolicy:    imagev1.TagImportPolicy{ImportMode: imagev1.ImportModeLegacy},
			},
		},
		"import all from .spec.tags": {
			name: "testis",
			all:  true,
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "latest", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"}},
						{Name: "other", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:other"}},
					},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{
				{
					From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"},
					To:              &corev1.LocalObjectReference{Name: "latest"},
					ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
					ImportPolicy:    imagev1.TagImportPolicy{ImportMode: imagev1.ImportModeLegacy},
				},
				{
					From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:other"},
					To:              &corev1.LocalObjectReference{Name: "other"},
					ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
					ImportPolicy:    imagev1.TagImportPolicy{ImportMode: imagev1.ImportModeLegacy},
				},
			},
		},
		"import all from .spec.tags with insecure annotation": {
			name: "testis",
			all:  true,
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "testis",
					Namespace:   "other",
					Annotations: map[string]string{imagev1.InsecureRepositoryAnnotation: "true"},
				},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "latest", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"}},
						{Name: "other", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:other"}},
					},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{
				{
					From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"},
					To:              &corev1.LocalObjectReference{Name: "latest"},
					ImportPolicy:    imagev1.TagImportPolicy{Insecure: true, ImportMode: imagev1.ImportModeLegacy},
					ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				},
				{
					From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:other"},
					To:              &corev1.LocalObjectReference{Name: "other"},
					ImportPolicy:    imagev1.TagImportPolicy{Insecure: true, ImportMode: imagev1.ImportModeLegacy},
					ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				},
			},
		},
		"import all from .spec.tags with insecure flag": {
			name:     "testis",
			all:      true,
			insecure: newBool(true),
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "latest", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"}},
						{Name: "other", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:other"}},
					},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{
				{
					From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"},
					To:              &corev1.LocalObjectReference{Name: "latest"},
					ImportPolicy:    imagev1.TagImportPolicy{Insecure: true, ImportMode: imagev1.ImportModeLegacy},
					ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				},
				{
					From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:other"},
					To:              &corev1.LocalObjectReference{Name: "other"},
					ImportPolicy:    imagev1.TagImportPolicy{Insecure: true, ImportMode: imagev1.ImportModeLegacy},
					ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				},
			},
		},
		"import all from .spec.tags no DockerImage tags": {
			name: "testis",
			all:  true,
			err:  "does not have tags pointing to external container images",
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "latest", From: &corev1.ObjectReference{Kind: "ImageStreamTag", Name: "otheris:latest"}},
					},
				},
			},
		},
		"empty image stream": {
			name: "testis",
			err:  "the tag \"latest\" does not exist on the image stream - choose an existing tag to import or use the 'tag' command to create a new tag",
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
			},
		},
		"import latest tag": {
			name: "testis:latest",
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testis",
					Namespace: "other",
				},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "latest", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"}},
					},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"},
				To:              &corev1.LocalObjectReference{Name: "latest"},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				ImportPolicy:    imagev1.TagImportPolicy{ImportMode: imagev1.ImportModeLegacy},
			}},
		},
		"import existing tag": {
			name: "testis:existing",
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testis",
					Namespace: "other",
				},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "existing", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"}},
					},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"},
				To:              &corev1.LocalObjectReference{Name: "existing"},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				ImportPolicy:    imagev1.TagImportPolicy{ImportMode: imagev1.ImportModeLegacy},
			}},
		},
		"import non-existing tag": {
			name: "testis:latest",
			err:  "does not exist on the image stream",
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testis",
					Namespace: "other",
				},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "nonlatest", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"}},
					},
				},
			},
		},
		"import tag from .spec.tags": {
			name: "testis:mytag",
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testis",
					Namespace: "other",
				},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							Name: "mytag",
							From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"},
						},
					},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"},
				To:              &corev1.LocalObjectReference{Name: "mytag"},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				ImportPolicy:    imagev1.TagImportPolicy{ImportMode: imagev1.ImportModeLegacy},
			}},
		},
		"use tag aliases": {
			name: "testis:mytag",
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testis",
					Namespace: "other",
				},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "mytag", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"}},
						{Name: "other1", From: &corev1.ObjectReference{Kind: "ImageStreamTag", Name: "testis:mytag"}},
						{Name: "other2", From: &corev1.ObjectReference{Kind: "ImageStreamTag", Name: "mytag"}},
					},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"},
				To:              &corev1.LocalObjectReference{Name: "mytag"},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				ImportPolicy:    imagev1.TagImportPolicy{ImportMode: imagev1.ImportModeLegacy},
			}},
		},
		"import tag from alias of cross-image-stream": {
			name: "testis:mytag",
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testis",
					Namespace: "other",
				},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							Name: "mytag",
							From: &corev1.ObjectReference{Kind: "ImageStreamTag", Name: "otherimage:mytag"},
						},
					},
				},
			},
			err: "tag \"mytag\" points to an imagestreamtag from another ImageStream",
		},
		"import tag from alias of circular reference": {
			name: "testis:mytag",
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testis",
					Namespace: "other",
				},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							Name: "mytag",
							From: &corev1.ObjectReference{Kind: "ImageStreamTag", Name: "mytag"},
						},
					},
				},
			},
			err: "tag \"mytag\" on the image stream is a reference to same tag",
		},
		"import tag from non existing alias": {
			name: "testis:mytag",
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testis",
					Namespace: "other",
				},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							Name: "mytag",
							From: &corev1.ObjectReference{Kind: "ImageStreamTag", Name: "nonexisting"},
						},
					},
				},
			},
			err: "the tag \"mytag\" does not exist on the image stream - choose an existing tag to import or use the 'tag' command to create a new tag",
		},
		"use insecure annotation": {
			name: "testis",
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "testis",
					Namespace:   "other",
					Annotations: map[string]string{imagev1.InsecureRepositoryAnnotation: "true"},
				},
				Spec: imagev1.ImageStreamSpec{
					DockerImageRepository: "repo.com/somens/someimage",
					Tags:                  []imagev1.TagReference{},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage"},
				To:              &corev1.LocalObjectReference{Name: "latest"},
				ImportPolicy:    imagev1.TagImportPolicy{Insecure: true, ImportMode: imagev1.ImportModeLegacy},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
			}},
		},
		"insecure flag overrides insecure annotation": {
			name:     "testis",
			insecure: newBool(false),
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "testis",
					Namespace:   "other",
					Annotations: map[string]string{imagev1.InsecureRepositoryAnnotation: "true"},
				},
				Spec: imagev1.ImageStreamSpec{
					DockerImageRepository: "repo.com/somens/someimage",
					Tags:                  []imagev1.TagReference{},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage"},
				To:              &corev1.LocalObjectReference{Name: "latest"},
				ImportPolicy:    imagev1.TagImportPolicy{Insecure: false, ImportMode: imagev1.ImportModeLegacy},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
			}},
		},
		"import tag setting referencePolicy": {
			name:            "testis:mytag",
			referencePolicy: tag.LocalReferencePolicy,
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							Name: "mytag",
							From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"},
						},
					},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"},
				To:              &corev1.LocalObjectReference{Name: "mytag"},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.LocalTagReferencePolicy},
				ImportPolicy:    imagev1.TagImportPolicy{ImportMode: imagev1.ImportModeLegacy},
			}},
		},
		"import all from .spec.tags setting referencePolicy": {
			name:            "testis",
			all:             true,
			referencePolicy: tag.LocalReferencePolicy,
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "mytag", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"}},
						{Name: "other", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:other"}},
					},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{
				{
					From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"},
					To:              &corev1.LocalObjectReference{Name: "mytag"},
					ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.LocalTagReferencePolicy},
					ImportPolicy:    imagev1.TagImportPolicy{ImportMode: imagev1.ImportModeLegacy},
				},
				{
					From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:other"},
					To:              &corev1.LocalObjectReference{Name: "other"},
					ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.LocalTagReferencePolicy},
					ImportPolicy:    imagev1.TagImportPolicy{ImportMode: imagev1.ImportModeLegacy},
				},
			},
		},
		"import all from .spec.dockerImageRepository setting referencePolicy": {
			name:            "testis",
			all:             true,
			referencePolicy: tag.LocalReferencePolicy,
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					DockerImageRepository: "repo.com/somens/someimage",
					Tags:                  []imagev1.TagReference{},
				},
			},
			expectedRepository: &imagev1.RepositoryImportSpec{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage"},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.LocalTagReferencePolicy},
				ImportPolicy:    imagev1.TagImportPolicy{ImportMode: imagev1.ImportModeLegacy},
			},
		},
		"import tag setting scheduled": {
			name:      "testis:mytag",
			scheduled: true,
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							Name: "mytag",
							From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"},
						},
					},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"},
				To:              &corev1.LocalObjectReference{Name: "mytag"},
				ImportPolicy:    imagev1.TagImportPolicy{Scheduled: true, ImportMode: imagev1.ImportModeLegacy},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
			}},
		},
		"import already scheduled tag without setting scheduled": {
			name:      "testis:mytag",
			scheduled: false,
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							Name:         "mytag",
							From:         &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"},
							ImportPolicy: imagev1.TagImportPolicy{Scheduled: true},
						},
					},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"},
				To:              &corev1.LocalObjectReference{Name: "mytag"},
				ImportPolicy:    imagev1.TagImportPolicy{Scheduled: true, ImportMode: imagev1.ImportModeLegacy},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
			}},
		},
		"import all from .spec.tags setting scheduled": {
			name:      "testis",
			all:       true,
			scheduled: true,
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "mytag", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"}},
						{Name: "other", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:other"}},
					},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{
				{
					From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"},
					To:              &corev1.LocalObjectReference{Name: "mytag"},
					ImportPolicy:    imagev1.TagImportPolicy{Scheduled: true, ImportMode: imagev1.ImportModeLegacy},
					ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				},
				{
					From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:other"},
					To:              &corev1.LocalObjectReference{Name: "other"},
					ImportPolicy:    imagev1.TagImportPolicy{Scheduled: true, ImportMode: imagev1.ImportModeLegacy},
					ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				},
			},
		},
		"import all from .spec.tags, some already scheduled, without setting scheduled": {
			name:      "testis",
			all:       true,
			scheduled: false,
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							Name:         "mytag",
							From:         &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"},
							ImportPolicy: imagev1.TagImportPolicy{Scheduled: true},
						},
						{Name: "other", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:other"}},
					},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{
				{
					From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:mytag"},
					To:              &corev1.LocalObjectReference{Name: "mytag"},
					ImportPolicy:    imagev1.TagImportPolicy{Scheduled: true, ImportMode: imagev1.ImportModeLegacy},
					ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				},
				{
					From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:other"},
					To:              &corev1.LocalObjectReference{Name: "other"},
					ImportPolicy:    imagev1.TagImportPolicy{Scheduled: false, ImportMode: imagev1.ImportModeLegacy},
					ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
				},
			},
		},
		"import all from .spec.dockerImageRepository setting scheduled": {
			name:      "testis",
			all:       true,
			scheduled: true,
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					DockerImageRepository: "repo.com/somens/someimage",
					Tags:                  []imagev1.TagReference{},
				},
			},
			expectedRepository: &imagev1.RepositoryImportSpec{
				From:            corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage"},
				ImportPolicy:    imagev1.TagImportPolicy{Scheduled: true, ImportMode: imagev1.ImportModeLegacy},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
			},
		},
		"import with importMode PreserveOriginal": {
			name:       "testis",
			importMode: imagev1.ImportModePreserveOriginal,
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "latest", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"}},
					},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{{
				From: corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"},
				To:   &corev1.LocalObjectReference{Name: "latest"},
				ImportPolicy: imagev1.TagImportPolicy{
					ImportMode: imagev1.ImportModePreserveOriginal,
				},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
			}},
		},
		"import with importMode Legacy": {
			name:       "testis",
			importMode: imagev1.ImportModeLegacy,
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "latest", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"}},
					},
				},
			},
			expectedImages: []imagev1.ImageImportSpec{{
				From: corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"},
				To:   &corev1.LocalObjectReference{Name: "latest"},
				ImportPolicy: imagev1.TagImportPolicy{
					ImportMode: imagev1.ImportModeLegacy,
				},
				ReferencePolicy: imagev1.TagReferencePolicy{Type: imagev1.SourceTagReferencePolicy},
			}},
		},
		"importMode specified with incorrect mode": {
			name:       "testis",
			err:        fmt.Sprintf("valid ImportMode values are %s or %s", imagev1.ImportModeLegacy, imagev1.ImportModePreserveOriginal),
			importMode: imagev1.ImportModeType("Wrong"),
			stream: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "testis", Namespace: "other"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "latest", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "repo.com/somens/someimage:latest"}},
					},
				},
			},
		},
	}

	for name, test := range testCases {
		var fake *imagefake.Clientset
		if test.stream == nil {
			fake = imagefake.NewSimpleClientset()
		} else {
			fake = imagefake.NewSimpleClientset(test.stream)
		}
		o := ImportImageOptions{
			Target:          test.name,
			From:            test.from,
			All:             test.all,
			Scheduled:       test.scheduled,
			ReferencePolicy: test.referencePolicy,
			ImportMode:      string(test.importMode),
			Confirm:         test.confirm,
			isClient:        fake.ImageV1().ImageStreams("other"),
		}

		if test.insecure != nil {
			o.Insecure = *test.insecure
			o.InsecureFlagProvided = true
		}

		if err := o.parseImageReference(); err != nil {
			t.Errorf("unexpected error: %v", err)
			continue
		}

		if err := o.Validate(); err != nil {
			if !strings.Contains(err.Error(), test.err) {
				if len(test.err) == 0 {
					t.Errorf("unexpected error: %v", err)
				} else {
					t.Errorf("error expected: %v received: %v", test.err, err.Error())
				}
			}
			continue
		}

		_, isi, err := o.createImageImport()
		// check errors
		if len(test.err) > 0 {
			if err == nil || !strings.Contains(err.Error(), test.err) {
				t.Errorf("%s: unexpected error: expected %s, got %v", name, test.err, err)
			}
			if isi != nil {
				t.Errorf("%s: unexpected import spec: expected nil, got %#v", name, isi)
			}
			continue
		}
		if len(test.err) == 0 && err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
			continue
		}
		// check values
		if !listEqual(isi.Spec.Images, test.expectedImages) {
			t.Errorf("%s: unexpected import images, expected %#v, got %#v", name, test.expectedImages, isi.Spec.Images)
		}
		if !apiequality.Semantic.DeepEqual(isi.Spec.Repository, test.expectedRepository) {
			t.Errorf("%s: unexpected import repository, expected %#v, got %#v", name, test.expectedRepository, isi.Spec.Repository)
		}
	}
}

func TestWasError(t *testing.T) {
	testCases := map[string]struct {
		isi      *imagev1.ImageStreamImport
		expected bool
	}{
		"no error": {
			isi:      &imagev1.ImageStreamImport{},
			expected: false,
		},
		"error importing images": {
			isi: &imagev1.ImageStreamImport{
				Status: imagev1.ImageStreamImportStatus{
					Images: []imagev1.ImageImportStatus{
						{Status: metav1.Status{Status: metav1.StatusFailure}},
					},
				},
			},
			expected: true,
		},
		"error importing repository": {
			isi: &imagev1.ImageStreamImport{
				Status: imagev1.ImageStreamImportStatus{
					Repository: &imagev1.RepositoryImportStatus{
						Status: metav1.Status{Status: metav1.StatusFailure},
					},
				},
			},
			expected: true,
		},
	}

	for name, test := range testCases {
		if a, e := wasError(test.isi), test.expected; a != e {
			t.Errorf("%s: expected %v, got %v", name, e, a)
		}
	}
}

func listEqual(actual, expected []imagev1.ImageImportSpec) bool {
	if len(actual) != len(expected) {
		return false
	}

	for _, a := range actual {
		found := false
		for _, e := range expected {
			if apiequality.Semantic.DeepEqual(a, e) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func newBool(a bool) *bool {
	r := new(bool)
	*r = a
	return r
}
