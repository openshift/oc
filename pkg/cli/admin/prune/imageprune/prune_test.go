package imageprune

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	kappsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/diff"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/scheme"

	"github.com/openshift/api"
	appsv1 "github.com/openshift/api/apps/v1"
	buildv1 "github.com/openshift/api/build/v1"
	imagev1 "github.com/openshift/api/image/v1"
	fakeimagev1client "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1/fake"
	imagetest "github.com/openshift/oc/pkg/helpers/image/test"
)

var logLevel = flag.Int("loglevel", 0, "")

func Images(images ...imagev1.Image) map[string]*imagev1.Image {
	m := map[string]*imagev1.Image{}
	for i := range images {
		image := &images[i]
		m[image.Name] = image
	}
	return m
}

func Streams(streams ...imagev1.ImageStream) map[string]*imagev1.ImageStream {
	m := map[string]*imagev1.ImageStream{}
	for i := range streams {
		stream := &streams[i]
		m[fmt.Sprintf("%s/%s", stream.Namespace, stream.Name)] = stream
	}
	return m
}

func TestImagePruning(t *testing.T) {
	var level klog.Level
	level.Set(fmt.Sprint(*logLevel))

	registryHost := "registry.io"

	tests := []struct {
		name                                 string
		pruneOverSizeLimit                   *bool
		allImages                            *bool
		pruneRegistry                        *bool
		ignoreInvalidRefs                    *bool
		keepTagRevisions                     *int
		namespace                            string
		images                               map[string]*imagev1.Image
		pods                                 corev1.PodList
		streams                              map[string]*imagev1.ImageStream
		rcs                                  corev1.ReplicationControllerList
		bcs                                  buildv1.BuildConfigList
		builds                               buildv1.BuildList
		dss                                  kappsv1.DaemonSetList
		deployments                          kappsv1.DeploymentList
		dcs                                  appsv1.DeploymentConfigList
		rss                                  kappsv1.ReplicaSetList
		sss                                  kappsv1.StatefulSetList
		jobs                                 batchv1.JobList
		cronjobs                             batchv1beta1.CronJobList
		limits                               map[string][]*corev1.LimitRange
		imageDeleterErr                      error
		imageStreamDeleterErr                error
		layerDeleterErr                      error
		manifestDeleterErr                   error
		blobDeleterErrorGetter               errorForSHA
		expectedImageDeletions               []string
		expectedImageDeletionFailures        []string
		expectedStreamUpdates                []string
		expectedStreamUpdateFailures         []string
		expectedLayerLinkDeletions           []string
		expectedLayerLinkDeletionFailures    []string
		expectedManifestLinkDeletions        []string
		expectedManifestLinkDeletionFailures []string
		expectedBlobDeletions                []string
		expectedBlobDeletionFailures         []string
		expectedErrorString                  string
	}{
		{
			name:             "1 pod - phase pending - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			pods:                   imagetest.PodList(imagetest.Pod("foo", "pod1", corev1.PodPending, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "3 pods - last phase pending - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			pods: imagetest.PodList(
				imagetest.Pod("foo", "pod1", corev1.PodSucceeded, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Pod("foo", "pod2", corev1.PodSucceeded, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Pod("foo", "pod3", corev1.PodPending, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
			),
			expectedImageDeletions: []string{},
		},

		{
			name:             "1 pod - phase running - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			pods:                   imagetest.PodList(imagetest.Pod("foo", "pod1", corev1.PodRunning, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "3 pods - last phase running - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			pods: imagetest.PodList(
				imagetest.Pod("foo", "pod1", corev1.PodSucceeded, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Pod("foo", "pod2", corev1.PodSucceeded, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Pod("foo", "pod3", corev1.PodRunning, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
			),
			expectedImageDeletions: []string{},
		},

		{
			name:             "pod phase succeeded - prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			pods:                   imagetest.PodList(imagetest.Pod("foo", "pod1", corev1.PodSucceeded, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			expectedStreamUpdates: []string{
				"foo/bar:latest",
				"foo/bar|latest|0|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
			expectedLayerLinkDeletions: []string{
				"foo/bar|" + imagetest.Layer1,
				"foo/bar|" + imagetest.Layer2,
				"foo/bar|" + imagetest.Layer3,
				"foo/bar|" + imagetest.Layer4,
				"foo/bar|" + imagetest.Layer5,
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
			expectedBlobDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000000",
				imagetest.Layer1,
				imagetest.Layer2,
				imagetest.Layer3,
				imagetest.Layer4,
				imagetest.Layer5,
			},
		},

		{
			name:             "pod phase succeeded - prune leave registry alone",
			keepTagRevisions: keepTagRevisions(0),
			pruneRegistry:    newBool(false),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			pods:                   imagetest.PodList(imagetest.Pod("foo", "pod1", corev1.PodSucceeded, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			expectedStreamUpdates: []string{
				"foo/bar:latest",
				"foo/bar|latest|0|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
			expectedBlobDeletions: []string{},
		},

		{
			name:             "pod phase succeeded, pod less than min pruning age - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			pods:                   imagetest.PodList(imagetest.AgedPod("foo", "pod1", corev1.PodSucceeded, 5, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "pod phase succeeded, image less than min pruning age - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.AgedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", 5)),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			pods:                   imagetest.PodList(imagetest.Pod("foo", "pod1", corev1.PodSucceeded, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
			expectedStreamUpdates: []string{
				"foo/bar:latest",
				"foo/bar|latest|0|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
			expectedLayerLinkDeletions: []string{
				"foo/bar|" + imagetest.Layer1,
				"foo/bar|" + imagetest.Layer2,
				"foo/bar|" + imagetest.Layer3,
				"foo/bar|" + imagetest.Layer4,
				"foo/bar|" + imagetest.Layer5,
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
		},

		{
			name:             "pod phase failed - prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			pods: imagetest.PodList(
				imagetest.Pod("foo", "pod1", corev1.PodFailed, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Pod("foo", "pod2", corev1.PodFailed, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Pod("foo", "pod3", corev1.PodFailed, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			expectedStreamUpdates: []string{
				"foo/bar:latest",
				"foo/bar|latest|0|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
			expectedLayerLinkDeletions: []string{
				"foo/bar|" + imagetest.Layer1,
				"foo/bar|" + imagetest.Layer2,
				"foo/bar|" + imagetest.Layer3,
				"foo/bar|" + imagetest.Layer4,
				"foo/bar|" + imagetest.Layer5,
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
			expectedBlobDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000000",
				imagetest.Layer1,
				imagetest.Layer2,
				imagetest.Layer3,
				imagetest.Layer4,
				imagetest.Layer5,
			},
		},

		{
			name:             "pod phase unknown - prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			pods: imagetest.PodList(
				imagetest.Pod("foo", "pod1", corev1.PodUnknown, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Pod("foo", "pod2", corev1.PodUnknown, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Pod("foo", "pod3", corev1.PodUnknown, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			expectedStreamUpdates: []string{
				"foo/bar:latest",
				"foo/bar|latest|0|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
			expectedLayerLinkDeletions: []string{
				"foo/bar|" + imagetest.Layer1,
				"foo/bar|" + imagetest.Layer2,
				"foo/bar|" + imagetest.Layer3,
				"foo/bar|" + imagetest.Layer4,
				"foo/bar|" + imagetest.Layer5,
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
			expectedBlobDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000000",
				imagetest.Layer1,
				imagetest.Layer2,
				imagetest.Layer3,
				imagetest.Layer4,
				imagetest.Layer5,
			},
		},

		{
			name:             "pod container image not parsable",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			pods: imagetest.PodList(
				imagetest.Pod("foo", "pod1", corev1.PodRunning, "a/b/c/d/e"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			expectedStreamUpdates: []string{
				"foo/bar:latest",
				"foo/bar|latest|0|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
			expectedLayerLinkDeletions: []string{
				"foo/bar|" + imagetest.Layer1,
				"foo/bar|" + imagetest.Layer2,
				"foo/bar|" + imagetest.Layer3,
				"foo/bar|" + imagetest.Layer4,
				"foo/bar|" + imagetest.Layer5,
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
			expectedBlobDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000000",
				imagetest.Layer1,
				imagetest.Layer2,
				imagetest.Layer3,
				imagetest.Layer4,
				imagetest.Layer5,
			},
		},

		{
			name:             "pod container image doesn't have an id",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			pods: imagetest.PodList(
				imagetest.Pod("foo", "pod1", corev1.PodRunning, registryHost+"/foo/bar:latest"),
			),
		},

		{
			name:             "pod refers to image not in graph",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			pods: imagetest.PodList(
				imagetest.Pod("foo", "pod1", corev1.PodRunning, registryHost+"/foo/bar@sha256:ABC0000000000000000000000000000000000000000000000000000000000002"),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			expectedStreamUpdates: []string{
				"foo/bar:latest",
				"foo/bar|latest|0|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
			expectedLayerLinkDeletions: []string{
				"foo/bar|" + imagetest.Layer1,
				"foo/bar|" + imagetest.Layer2,
				"foo/bar|" + imagetest.Layer3,
				"foo/bar|" + imagetest.Layer4,
				"foo/bar|" + imagetest.Layer5,
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
			expectedBlobDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000000",
				imagetest.Layer1,
				imagetest.Layer2,
				imagetest.Layer3,
				imagetest.Layer4,
				imagetest.Layer5,
			},
		},

		{
			name:             "referenced by rc - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			rcs:                    imagetest.RCList(imagetest.RC("foo", "rc1", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "referenced by dc - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			dcs:                    imagetest.DCList(imagetest.DC("foo", "rc1", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "referenced by daemonset - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images: Images(
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
					),
				}),
			),
			dss:                    imagetest.DSList(imagetest.DS("foo", "rc1", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
			expectedStreamUpdates: []string{
				"foo/bar|latest|1|sha256:0000000000000000000000000000000000000000000000000000000000000001",
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000001",
			},
			expectedBlobDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
		},

		{
			name:             "referenced by replicaset - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images: Images(
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
					),
				}),
			),
			rss:                    imagetest.RSList(imagetest.RS("foo", "rc1", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
			expectedStreamUpdates: []string{
				"foo/bar|latest|1|sha256:0000000000000000000000000000000000000000000000000000000000000001",
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000001",
			},
			expectedBlobDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
		},

		{
			name:             "referenced by upstream deployment - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images: Images(
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
					),
				}),
			),
			deployments:            imagetest.DeploymentList(imagetest.Deployment("foo", "rc1", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
			expectedStreamUpdates: []string{
				"foo/bar|latest|1|sha256:0000000000000000000000000000000000000000000000000000000000000001",
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000001",
			},
			expectedBlobDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
		},

		{
			name:             "referenced by bc - sti - ImageStreamImage - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			bcs:                    imagetest.BCList(imagetest.BC("foo", "bc1", "source", "ImageStreamImage", "foo", "bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "referenced by bc - docker - ImageStreamImage - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			bcs:                    imagetest.BCList(imagetest.BC("foo", "bc1", "docker", "ImageStreamImage", "foo", "bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "referenced by bc - custom - ImageStreamImage - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			bcs:                    imagetest.BCList(imagetest.BC("foo", "bc1", "custom", "ImageStreamImage", "foo", "bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "referenced by bc - sti - DockerImage - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			bcs:                    imagetest.BCList(imagetest.BC("foo", "bc1", "source", "DockerImage", "foo", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "referenced by bc - docker - DockerImage - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			bcs:                    imagetest.BCList(imagetest.BC("foo", "bc1", "docker", "DockerImage", "foo", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "referenced by bc - custom - DockerImage - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			bcs:                    imagetest.BCList(imagetest.BC("foo", "bc1", "custom", "DockerImage", "foo", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "referenced by build - sti - ImageStreamImage - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			builds:                 imagetest.BuildList(imagetest.Build("foo", "build1", "source", "ImageStreamImage", "foo", "bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "referenced by build - docker - ImageStreamImage - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			builds:                 imagetest.BuildList(imagetest.Build("foo", "build1", "docker", "ImageStreamImage", "foo", "bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "referenced by build - custom - ImageStreamImage - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			builds:                 imagetest.BuildList(imagetest.Build("foo", "build1", "custom", "ImageStreamImage", "foo", "bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "referenced by build - sti - DockerImage - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			builds:                 imagetest.BuildList(imagetest.Build("foo", "build1", "source", "DockerImage", "foo", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "referenced by build - docker - DockerImage - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			builds:                 imagetest.BuildList(imagetest.Build("foo", "build1", "docker", "DockerImage", "foo", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name:             "referenced by build - custom - DockerImage - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			builds:                 imagetest.BuildList(imagetest.Build("foo", "build1", "custom", "DockerImage", "foo", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
		},

		{
			name: "image stream - keep most recent n images",
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", false, "", ""),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
					),
				}),
			),
			expectedImageDeletions:        []string{"sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedStreamUpdates:         []string{"foo/bar|latest|3|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedManifestLinkDeletions: []string{"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedBlobDeletions:         []string{"sha256:0000000000000000000000000000000000000000000000000000000000000004"},
		},

		{
			name: "continue on blob deletion failure",
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", false, "", ""),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004", nil, "layer1", "layer2"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
					),
				}),
			),
			blobDeleterErrorGetter: func(dgst string) error {
				if dgst == "layer1" {
					return errors.New("err")
				}
				return nil
			},
			expectedImageDeletions:        []string{"sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedStreamUpdates:         []string{"foo/bar|latest|3|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedManifestLinkDeletions: []string{"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedLayerLinkDeletions:    []string{"foo/bar|layer1", "foo/bar|layer2"},
			expectedBlobDeletions: []string{
				"layer1",
				"layer2",
				"sha256:0000000000000000000000000000000000000000000000000000000000000004",
			},
			expectedBlobDeletionFailures: []string{
				"image sha256:0000000000000000000000000000000000000000000000000000000000000004: failed to delete blob layer1: err",
			},
		},

		{
			name: "keep image when all blob deletions fail",
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", false, "", ""),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004", nil, "layer1", "layer2"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
					),
				}),
			),
			blobDeleterErrorGetter:        func(dgst string) error { return errors.New("err") },
			expectedImageDeletions:        []string{},
			expectedStreamUpdates:         []string{"foo/bar|latest|3|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedManifestLinkDeletions: []string{"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedLayerLinkDeletions:    []string{"foo/bar|layer1", "foo/bar|layer2"},
			expectedBlobDeletions:         []string{"layer1", "layer2", "sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedBlobDeletionFailures: []string{
				"image sha256:0000000000000000000000000000000000000000000000000000000000000004: failed to delete blob layer1: err",
				"image sha256:0000000000000000000000000000000000000000000000000000000000000004: failed to delete blob layer2: err",
				"image sha256:0000000000000000000000000000000000000000000000000000000000000004: failed to delete manifest blob sha256:0000000000000000000000000000000000000000000000000000000000000004: err",
			},
		},

		{
			name: "continue on manifest link deletion failure",
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", false, "", ""),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
					),
				}),
			),
			manifestDeleterErr:            fmt.Errorf("err"),
			expectedImageDeletions:        []string{"sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedStreamUpdates:         []string{"foo/bar|latest|3|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedManifestLinkDeletions: []string{"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedManifestLinkDeletionFailures: []string{
				"imagestream foo/bar: failed to delete manifest link sha256:0000000000000000000000000000000000000000000000000000000000000004: err",
			},
			expectedBlobDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000004"},
		},

		{
			name: "stop on image stream update failure",
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", false, "", ""),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
					),
				}),
			),
			imageStreamDeleterErr: fmt.Errorf("err"),
			expectedStreamUpdateFailures: []string{
				"imagestream foo/bar: err",
			},
		},

		{
			name: "image stream - same manifest listed multiple times in tag history",
			images: Images(
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
					),
				}),
			),
			expectedStreamUpdates: []string{
				"foo/bar|latest|3|sha256:0000000000000000000000000000000000000000000000000000000000000002",
			},
		},

		{
			name: "image stream age less than min pruning age - don't prune",
			images: Images(
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
			),
			streams: Streams(
				imagetest.AgedStream(registryHost, "foo", "bar", 5, []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
					),
				}),
			),
			expectedImageDeletions: []string{},
			expectedStreamUpdates:  []string{},
		},

		{
			name: "image stream - unreference absent image",
			images: Images(
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
					),
				}),
			),
			expectedStreamUpdates: []string{"foo/bar|latest|0|sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		},

		{
			name: "image stream with dangling references - delete tags",
			images: Images(
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001", nil, "layer1"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
					imagetest.Tag("tag",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
					),
				}),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
			expectedStreamUpdates: []string{
				"foo/bar:latest",
				"foo/bar:tag",
				"foo/bar|latest|0|sha256:0000000000000000000000000000000000000000000000000000000000000000",
				"foo/bar|tag|0|sha256:0000000000000000000000000000000000000000000000000000000000000002",
			},
			expectedBlobDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001", "layer1"},
		},

		{
			name: "image stream - keep reference to a young absent image",
			images: Images(
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", nil),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.YoungTagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", metav1.Now()),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
					),
				}),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000002"},
			expectedBlobDeletions:  []string{"sha256:0000000000000000000000000000000000000000000000000000000000000002"},
		},

		{
			name:             "images referenced by istag - keep",
			keepTagRevisions: keepTagRevisions(0),
			images: Images(
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000005", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000005"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000006", registryHost+"/foo/baz@sha256:0000000000000000000000000000000000000000000000000000000000000006"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000005", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000005"),
					),
					imagetest.Tag("dummy", // removed because no object references the image (the nm/dcfoo has mismatched repository name)
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000005", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000005"),
					),
				}),
				imagetest.Stream(registryHost, "foo", "baz", []imagev1.NamedTagEventList{
					imagetest.Tag("late", // kept because replicaset references the tagged image
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
					),
					imagetest.Tag("keepme", // kept because a deployment references the tagged image
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000006", registryHost+"/foo/baz@sha256:0000000000000000000000000000000000000000000000000000000000000006"),
					),
				}),
			),
			dss: imagetest.DSList(imagetest.DS("nm", "dsfoo", fmt.Sprintf("%s/%s/%s:%s", registryHost, "foo", "bar", "latest"))),
			dcs: imagetest.DCList(imagetest.DC("nm", "dcfoo", fmt.Sprintf("%s/%s/%s:%s", registryHost, "foo", "repo", "dummy"))),
			rss: imagetest.RSList(imagetest.RS("nm", "rsfoo", fmt.Sprintf("%s/%s/%s:%s", registryHost, "foo", "baz", "late"))),
			// ignore different registry hostname
			deployments: imagetest.DeploymentList(imagetest.Deployment("nm", "depfoo", fmt.Sprintf("%s/%s/%s:%s", "external.registry:5000", "foo", "baz", "keepme"))),
			expectedImageDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000001",
				"sha256:0000000000000000000000000000000000000000000000000000000000000003",
				"sha256:0000000000000000000000000000000000000000000000000000000000000004",
				"sha256:0000000000000000000000000000000000000000000000000000000000000005",
			},
			expectedStreamUpdates: []string{
				"foo/bar:dummy",
				"foo/bar|latest|1|sha256:0000000000000000000000000000000000000000000000000000000000000001",
				"foo/bar|latest|2|sha256:0000000000000000000000000000000000000000000000000000000000000002",
				"foo/bar|latest|3|sha256:0000000000000000000000000000000000000000000000000000000000000003",
				"foo/bar|latest|4|sha256:0000000000000000000000000000000000000000000000000000000000000004",
				"foo/bar|latest|5|sha256:0000000000000000000000000000000000000000000000000000000000000005",
				"foo/bar|dummy|0|sha256:0000000000000000000000000000000000000000000000000000000000000005",
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000001",
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000002",
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000003",
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000004",
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000005",
			},
			expectedBlobDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000001",
				"sha256:0000000000000000000000000000000000000000000000000000000000000003",
				"sha256:0000000000000000000000000000000000000000000000000000000000000004",
				"sha256:0000000000000000000000000000000000000000000000000000000000000005",
			},
		},

		{
			name: "multiple resources pointing to image - don't prune",
			images: Images(
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
					),
				}),
			),
			rcs:                    imagetest.RCList(imagetest.RC("foo", "rc1", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002")),
			pods:                   imagetest.PodList(imagetest.Pod("foo", "pod1", corev1.PodRunning, registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002")),
			dcs:                    imagetest.DCList(imagetest.DC("foo", "rc1", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			bcs:                    imagetest.BCList(imagetest.BC("foo", "bc1", "source", "DockerImage", "foo", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			builds:                 imagetest.BuildList(imagetest.Build("foo", "build1", "custom", "ImageStreamImage", "foo", "bar@sha256:0000000000000000000000000000000000000000000000000000000000000000")),
			expectedImageDeletions: []string{},
			expectedStreamUpdates:  []string{},
		},

		{
			name: "image with nil annotations",
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", false, "", ""),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			expectedStreamUpdates:  []string{},
			expectedBlobDeletions:  []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		},

		{
			name:      "prune all-images=true image with nil annotations",
			allImages: newBool(true),
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", false, "", ""),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			expectedStreamUpdates:  []string{},
			expectedBlobDeletions:  []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		},

		{
			name:      "prune all-images=false image with nil annotations",
			allImages: newBool(false),
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", false, "", ""),
			),
			expectedImageDeletions: []string{},
			expectedStreamUpdates:  []string{},
		},

		{
			name: "image missing managed annotation",
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, "foo", "bar"),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			expectedStreamUpdates:  []string{},
			expectedBlobDeletions:  []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		},

		{
			name: "image with managed annotation != true",
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "false"),
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000001", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "0"),
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000002", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "1"),
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000003", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "True"),
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000004", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "yes"),
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000005", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "Yes"),
			),
			expectedImageDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000000",
				"sha256:0000000000000000000000000000000000000000000000000000000000000001",
				"sha256:0000000000000000000000000000000000000000000000000000000000000002",
				"sha256:0000000000000000000000000000000000000000000000000000000000000003",
				"sha256:0000000000000000000000000000000000000000000000000000000000000004",
				"sha256:0000000000000000000000000000000000000000000000000000000000000005",
			},
			expectedStreamUpdates: []string{},
			expectedBlobDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000000",
				"sha256:0000000000000000000000000000000000000000000000000000000000000001",
				"sha256:0000000000000000000000000000000000000000000000000000000000000002",
				"sha256:0000000000000000000000000000000000000000000000000000000000000003",
				"sha256:0000000000000000000000000000000000000000000000000000000000000004",
				"sha256:0000000000000000000000000000000000000000000000000000000000000005",
			},
		},

		{
			name:      "prune all-images=true with image missing managed annotation",
			allImages: newBool(true),
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, "foo", "bar"),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			expectedStreamUpdates:  []string{},
			expectedBlobDeletions:  []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		},

		{
			name:      "prune all-images=true with image with managed annotation != true",
			allImages: newBool(true),
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "false"),
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000001", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "0"),
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000002", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "1"),
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000003", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "True"),
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000004", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "yes"),
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000005", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "Yes"),
			),
			expectedImageDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000000",
				"sha256:0000000000000000000000000000000000000000000000000000000000000001",
				"sha256:0000000000000000000000000000000000000000000000000000000000000002",
				"sha256:0000000000000000000000000000000000000000000000000000000000000003",
				"sha256:0000000000000000000000000000000000000000000000000000000000000004",
				"sha256:0000000000000000000000000000000000000000000000000000000000000005",
			},
			expectedStreamUpdates: []string{},
			expectedBlobDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000000",
				"sha256:0000000000000000000000000000000000000000000000000000000000000001",
				"sha256:0000000000000000000000000000000000000000000000000000000000000002",
				"sha256:0000000000000000000000000000000000000000000000000000000000000003",
				"sha256:0000000000000000000000000000000000000000000000000000000000000004",
				"sha256:0000000000000000000000000000000000000000000000000000000000000005",
			},
		},

		{
			name:      "prune all-images=false with image missing managed annotation",
			allImages: newBool(false),
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, "foo", "bar"),
			),
			expectedImageDeletions: []string{},
			expectedStreamUpdates:  []string{},
		},

		{
			name:      "prune all-images=false with image with managed annotation != true",
			allImages: newBool(false),
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "false"),
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000001", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "0"),
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000002", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "1"),
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000003", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "True"),
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000004", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "yes"),
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000005", "someregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", true, imagev1.ManagedByOpenShiftAnnotation, "Yes"),
			),
			expectedImageDeletions: []string{},
			expectedStreamUpdates:  []string{},
		},

		{
			name: "image with layers",
			images: Images(
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001", &imagetest.Config1, "layer1", "layer2", "layer3", "layer4"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", &imagetest.Config2, "layer1", "layer2", "layer3", "layer4"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003", nil, "layer1", "layer2", "layer3", "layer4"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004", nil, "layer5", "layer6", "layer7", "layer8"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
					),
				}),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedStreamUpdates:  []string{"foo/bar|latest|3|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedLayerLinkDeletions: []string{
				"foo/bar|layer5",
				"foo/bar|layer6",
				"foo/bar|layer7",
				"foo/bar|layer8",
			},
			expectedManifestLinkDeletions: []string{"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedBlobDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000004",
				"layer5",
				"layer6",
				"layer7",
				"layer8",
			},
		},

		{
			name: "continue on layer link error",
			images: Images(
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001", &imagetest.Config1, "layer1", "layer2", "layer3", "layer4"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", &imagetest.Config2, "layer1", "layer2", "layer3", "layer4"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003", nil, "layer1", "layer2", "layer3", "layer4"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004", nil, "layer5", "layer6", "layer7", "layer8"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
					),
				}),
			),
			layerDeleterErr:               fmt.Errorf("err"),
			expectedImageDeletions:        []string{"sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedStreamUpdates:         []string{"foo/bar|latest|3|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedManifestLinkDeletions: []string{"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedBlobDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000004",
				"layer5",
				"layer6",
				"layer7",
				"layer8",
			},
			expectedLayerLinkDeletions: []string{
				"foo/bar|layer5",
				"foo/bar|layer6",
				"foo/bar|layer7",
				"foo/bar|layer8",
			},
			expectedLayerLinkDeletionFailures: []string{
				"imagestream foo/bar: failed to delete layer link layer5: err",
				"imagestream foo/bar: failed to delete layer link layer6: err",
				"imagestream foo/bar: failed to delete layer link layer7: err",
				"imagestream foo/bar: failed to delete layer link layer8: err",
			},
		},

		{
			name: "images with duplicate layers and configs",
			images: Images(
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001", &imagetest.Config1, "layer1", "layer2", "layer3", "layer4"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", &imagetest.Config1, "layer1", "layer2", "layer3", "layer4"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003", &imagetest.Config1, "layer1", "layer2", "layer3", "layer4"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004", &imagetest.Config2, "layer5", "layer6", "layer7", "layer8"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000005", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000005", &imagetest.Config2, "layer5", "layer6", "layer9", "layerX"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
					),
				}),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000004", "sha256:0000000000000000000000000000000000000000000000000000000000000005"},
			expectedStreamUpdates:  []string{"foo/bar|latest|3|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedLayerLinkDeletions: []string{
				"foo/bar|" + imagetest.Config2,
				"foo/bar|layer5",
				"foo/bar|layer6",
				"foo/bar|layer7",
				"foo/bar|layer8",
			},
			expectedManifestLinkDeletions: []string{"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedBlobDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000004",
				"sha256:0000000000000000000000000000000000000000000000000000000000000005",
				imagetest.Config2,
				"layer5",
				"layer6",
				"layer7",
				"layer8",
				"layer9",
				"layerX",
			},
		},

		{
			name: "continue on image deletion failure",
			images: Images(
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001", &imagetest.Config1, "layer1", "layer2", "layer3", "layer4"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", &imagetest.Config1, "layer1", "layer2", "layer3", "layer4"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003", &imagetest.Config1, "layer1", "layer2", "layer3", "layer4"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004", &imagetest.Config2, "layer5", "layer6", "layer7", "layer8"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000005", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000005", &imagetest.Config2, "layer5", "layer6", "layer9", "layerX"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
					),
				}),
			),
			imageDeleterErr:        fmt.Errorf("err"),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000004", "sha256:0000000000000000000000000000000000000000000000000000000000000005"},
			expectedStreamUpdates:  []string{"foo/bar|latest|3|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedLayerLinkDeletions: []string{
				"foo/bar|" + imagetest.Config2,
				"foo/bar|layer5",
				"foo/bar|layer6",
				"foo/bar|layer7",
				"foo/bar|layer8",
			},
			expectedManifestLinkDeletions: []string{"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedBlobDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000004",
				"sha256:0000000000000000000000000000000000000000000000000000000000000005",
				imagetest.Config2,
				"layer5",
				"layer6",
				"layer7",
				"layer8",
				"layer9",
				"layerX",
			},
			expectedImageDeletionFailures: []string{
				"image sha256:0000000000000000000000000000000000000000000000000000000000000004: failed to delete image sha256:0000000000000000000000000000000000000000000000000000000000000004: err",
				"image sha256:0000000000000000000000000000000000000000000000000000000000000005: failed to delete image sha256:0000000000000000000000000000000000000000000000000000000000000005: err",
			},
		},

		{
			name: "layers shared with young images are not pruned",
			images: Images(
				imagetest.AgedImage("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001", 43200),
				imagetest.AgedImage("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", 5),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
			expectedBlobDeletions:  []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
		},

		{
			name:               "image exceeding limits",
			pruneOverSizeLimit: newBool(true),
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", false, "", ""),
				imagetest.SizedImage("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", 100, nil),
				imagetest.SizedImage("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003", 200, nil),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
					),
				}),
			),
			limits: map[string][]*corev1.LimitRange{
				"foo": imagetest.LimitList(100, 200),
			},
			expectedImageDeletions:        []string{"sha256:0000000000000000000000000000000000000000000000000000000000000003"},
			expectedStreamUpdates:         []string{"foo/bar|latest|2|sha256:0000000000000000000000000000000000000000000000000000000000000003"},
			expectedManifestLinkDeletions: []string{"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000003"},
			expectedBlobDeletions:         []string{"sha256:0000000000000000000000000000000000000000000000000000000000000003"},
		},

		{
			name:               "multiple images in different namespaces exceeding different limits",
			pruneOverSizeLimit: newBool(true),
			images: Images(
				imagetest.SizedImage("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001", 100, nil),
				imagetest.SizedImage("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", 200, nil),
				imagetest.SizedImage("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/bar/foo@sha256:0000000000000000000000000000000000000000000000000000000000000003", 500, nil),
				imagetest.SizedImage("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/bar/foo@sha256:0000000000000000000000000000000000000000000000000000000000000004", 600, nil),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
					),
				}),
				imagetest.Stream(registryHost, "bar", "foo", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/bar/foo@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000004", registryHost+"/bar/foo@sha256:0000000000000000000000000000000000000000000000000000000000000004"),
					),
				}),
			),
			limits: map[string][]*corev1.LimitRange{
				"foo": imagetest.LimitList(150),
				"bar": imagetest.LimitList(550),
			},
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000002", "sha256:0000000000000000000000000000000000000000000000000000000000000004"},
			expectedStreamUpdates: []string{
				"foo/bar|latest|1|sha256:0000000000000000000000000000000000000000000000000000000000000002",
				"bar/foo|latest|1|sha256:0000000000000000000000000000000000000000000000000000000000000004",
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000002",
				"bar/foo|sha256:0000000000000000000000000000000000000000000000000000000000000004",
			},
			expectedBlobDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000002",
				"sha256:0000000000000000000000000000000000000000000000000000000000000004",
			},
		},

		{
			name:               "image within allowed limits",
			pruneOverSizeLimit: newBool(true),
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", false, "", ""),
				imagetest.SizedImage("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", 100, nil),
				imagetest.SizedImage("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003", 200, nil),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
					),
				}),
			),
			limits: map[string][]*corev1.LimitRange{
				"foo": imagetest.LimitList(300),
			},
			expectedImageDeletions: []string{},
			expectedStreamUpdates:  []string{},
		},

		{
			name:               "image exceeding limits with namespace specified",
			pruneOverSizeLimit: newBool(true),
			namespace:          "foo",
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", false, "", ""),
				imagetest.SizedImage("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", 100, nil),
				imagetest.SizedImage("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003", 200, nil),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
					),
				}),
			),
			limits: map[string][]*corev1.LimitRange{
				"foo": imagetest.LimitList(100, 200),
			},
			expectedManifestLinkDeletions: []string{"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000003"},
			expectedStreamUpdates:         []string{"foo/bar|latest|2|sha256:0000000000000000000000000000000000000000000000000000000000000003"},
		},

		{
			name:               "build with ignored bad image reference",
			pruneOverSizeLimit: newBool(true),
			ignoreInvalidRefs:  newBool(true),
			images: Images(
				imagetest.UnmanagedImage("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", false, "", ""),
				imagetest.SizedImage("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", 100, nil),
				imagetest.SizedImage("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003", 200, nil),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", "otherregistry/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
					),
				}),
			),
			builds: imagetest.BuildList(
				imagetest.Build("foo", "build1", "source", "DockerImage", "foo", registryHost+"/foo/bar@sha256:many-zeros-and-3"),
			),
			limits: map[string][]*corev1.LimitRange{
				"foo": imagetest.LimitList(100, 200),
			},
			expectedImageDeletions:        []string{"sha256:0000000000000000000000000000000000000000000000000000000000000003"},
			expectedManifestLinkDeletions: []string{"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000003"},
			expectedBlobDeletions:         []string{"sha256:0000000000000000000000000000000000000000000000000000000000000003"},
			expectedStreamUpdates:         []string{"foo/bar|latest|2|sha256:0000000000000000000000000000000000000000000000000000000000000003"},
		},

		{
			name:                "build with bad image reference",
			builds:              imagetest.BuildList(imagetest.Build("foo", "build1", "source", "DockerImage", "foo", registryHost+"/foo/bar@invalid-digest")),
			expectedErrorString: fmt.Sprintf(`build/build1 namespace=foo: invalid image reference "%s/foo/bar@invalid-digest": invalid reference format`, registryHost),
		},

		{
			name: "buildconfig with bad imagestreamtag",
			bcs:  imagetest.BCList(imagetest.BC("foo", "bc1", "source", "ImageStreamTag", "ns", "bad/tag@name")),
			expectedErrorString: `buildconfig/bc1 namespace=foo: invalid ImageStreamTag reference "bad/tag@name":` +
				` "bad/tag@name" is an image stream image, not an image stream tag`,
		},

		{
			name:        "more parsing errors",
			bcs:         imagetest.BCList(imagetest.BC("foo", "bc1", "source", "ImageStreamImage", "ns", "bad:isi")),
			deployments: imagetest.DeploymentList(imagetest.Deployment("nm", "dep1", "garbage")),
			rss:         imagetest.RSList(imagetest.RS("nm", "rs1", "I am certainly a valid reference")),
			expectedErrorString: `[` +
				`replicaset/rs1 namespace=nm: container app: invalid image reference "I am certainly a valid reference":` +
				` invalid reference format, ` +
				`buildconfig/bc1 namespace=foo: invalid ImageStreamImage reference "bad:isi":` +
				` expected exactly one @ in the isimage name "bad:isi"]`,
		},

		{
			name:             "schema 2 image blobs are pruned",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", &imagetest.Config1, "layer1", "layer2")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			expectedLayerLinkDeletions: []string{
				"foo/bar|layer1",
				"foo/bar|layer2",
				"foo/bar|" + imagetest.Config1,
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
			expectedBlobDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000000",
				"layer1",
				"layer2",
				imagetest.Config1,
			},
			expectedStreamUpdates: []string{
				"foo/bar:latest",
				"foo/bar|latest|0|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
		},

		{
			name:             "oci image blobs are pruned",
			keepTagRevisions: keepTagRevisions(0),
			images:           Images(imagetest.OCIImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", imagetest.Config1, "layer1", "layer2")),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			expectedLayerLinkDeletions: []string{
				"foo/bar|layer1",
				"foo/bar|layer2",
				"foo/bar|" + imagetest.Config1,
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
			expectedBlobDeletions: []string{
				"sha256:0000000000000000000000000000000000000000000000000000000000000000",
				"layer1",
				"layer2",
				imagetest.Config1,
			},
			expectedStreamUpdates: []string{
				"foo/bar:latest",
				"foo/bar|latest|0|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
		},

		{
			name:             "blobs are not deleted if there are oci images that still use them",
			keepTagRevisions: keepTagRevisions(1),
			images: Images(
				imagetest.OCIImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001", imagetest.Config1, "layer1", "layer2"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000", &imagetest.Config1, "layer1", "layer2"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
					),
				}),
			),
			expectedImageDeletions:        []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			expectedManifestLinkDeletions: []string{"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			expectedBlobDeletions:         []string{"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			expectedStreamUpdates: []string{
				"foo/bar|latest|1|sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
		},

		{
			name:             "referenced by statefulset - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images: Images(
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
					),
				}),
			),
			sss: imagetest.SSList(
				imagetest.StatefulSet("foo", "rc1", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.StatefulSet("foo", "rc2", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
			expectedStreamUpdates: []string{
				"foo/bar|latest|1|sha256:0000000000000000000000000000000000000000000000000000000000000001",
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000001",
			},
			expectedBlobDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
		},

		{
			name:             "referenced by job - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images: Images(
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
					),
				}),
			),
			jobs: imagetest.JobList(
				imagetest.Job("foo", "rc1", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
			expectedStreamUpdates: []string{
				"foo/bar|latest|1|sha256:0000000000000000000000000000000000000000000000000000000000000001",
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000001",
			},
			expectedBlobDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
		},

		{
			name:             "referenced by cronjob - don't prune",
			keepTagRevisions: keepTagRevisions(0),
			images: Images(
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
				imagetest.Image("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
			),
			streams: Streams(
				imagetest.Stream(registryHost, "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000000", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
					),
				}),
			),
			cronjobs: imagetest.CronJobList(
				imagetest.CronJob("foo", "rc1", registryHost+"/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
			),
			expectedImageDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
			expectedStreamUpdates: []string{
				"foo/bar|latest|1|sha256:0000000000000000000000000000000000000000000000000000000000000001",
			},
			expectedManifestLinkDeletions: []string{
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000001",
			},
			expectedBlobDeletions: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
		},
	}

	// we need to install OpenShift API types to kubectl's scheme for GetReference to work
	api.Install(scheme.Scheme)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := PrunerOptions{
				Namespace:   test.namespace,
				AllImages:   test.allImages,
				Images:      test.images,
				Streams:     test.streams,
				Pods:        &test.pods,
				RCs:         &test.rcs,
				BCs:         &test.bcs,
				Builds:      &test.builds,
				DSs:         &test.dss,
				Deployments: &test.deployments,
				DCs:         &test.dcs,
				RSs:         &test.rss,
				SSs:         &test.sss,
				Jobs:        &test.jobs,
				CronJobs:    &test.cronjobs,
				LimitRanges: test.limits,
			}
			if test.pruneOverSizeLimit != nil {
				options.PruneOverSizeLimit = test.pruneOverSizeLimit
			} else {
				youngerThan := time.Hour
				tagRevisions := 3
				if test.keepTagRevisions != nil {
					tagRevisions = *test.keepTagRevisions
				}
				options.KeepYoungerThan = &youngerThan
				options.KeepTagRevisions = &tagRevisions
			}
			if test.pruneRegistry != nil {
				options.PruneRegistry = test.pruneRegistry
			}
			if test.ignoreInvalidRefs != nil {
				options.IgnoreInvalidRefs = *test.ignoreInvalidRefs
			}
			p, err := NewPruner(options)
			if err != nil {
				if len(test.expectedErrorString) > 0 {
					if a, e := err.Error(), test.expectedErrorString; a != e {
						t.Fatalf("got unexpected error: %q != %q", a, e)
					}
				} else {
					t.Fatalf("got unexpected error: %v", err)
				}
				return
			} else if len(test.expectedErrorString) > 0 {
				t.Fatalf("got no error while expecting: %s", test.expectedErrorString)
				return
			}

			streamDeleter := &fakeImageStreamDeleter{err: test.imageStreamDeleterErr, invocations: sets.NewString()}
			layerLinkDeleter := &fakeLayerLinkDeleter{err: test.layerDeleterErr, invocations: sets.NewString()}
			manifestDeleter := &fakeManifestDeleter{err: test.manifestDeleterErr, invocations: sets.NewString()}
			blobDeleter := &fakeBlobDeleter{getError: test.blobDeleterErrorGetter, invocations: sets.NewString()}
			imageDeleter := newFakeImageDeleter(test.imageDeleterErr)

			stats, errs := p.Prune(streamDeleter, layerLinkDeleter, manifestDeleter, blobDeleter, imageDeleter)

			expectedFailures := sets.NewString()
			expectedFailures.Insert(test.expectedImageDeletionFailures...)
			expectedFailures.Insert(test.expectedStreamUpdateFailures...)
			expectedFailures.Insert(test.expectedLayerLinkDeletionFailures...)
			expectedFailures.Insert(test.expectedManifestLinkDeletionFailures...)
			expectedFailures.Insert(test.expectedBlobDeletionFailures...)
			renderedFailures := sets.NewString()
			if errs != nil {
				for _, f := range errs.Errors() {
					renderedFailures.Insert(f.Error())
				}
			}
			for f := range renderedFailures {
				if expectedFailures.Has(f) {
					expectedFailures.Delete(f)
					continue
				}
				t.Errorf("got unexpected failure: %v", f)
			}
			for f := range expectedFailures {
				t.Errorf("the following expected failure was not returned: %v", f)
			}

			expectedImageDeletions := sets.NewString(test.expectedImageDeletions...)
			if a, e := imageDeleter.invocations, expectedImageDeletions; !reflect.DeepEqual(a, e) {
				t.Errorf("unexpected image deletions (-actual, +expected): %s", diff.ObjectDiff(a, e))
			}
			if want := expectedImageDeletions.Len() - len(test.expectedImageDeletionFailures); stats.DeletedImages != want {
				t.Errorf("image deletions: got %d, want %d", stats.DeletedImages, want)
			}

			expectedStreamUpdates := sets.NewString(test.expectedStreamUpdates...)
			if a, e := streamDeleter.invocations, expectedStreamUpdates; !reflect.DeepEqual(a, e) {
				t.Errorf("unexpected stream updates (-actual, +expected): %s", diff.ObjectDiff(a, e))
			}
			expectedImageStreamUpdates := sets.NewString()
			expectedImageStreamItemsDeletions := 0
			for update := range expectedStreamUpdates {
				if i := strings.Index(update, "|"); i != -1 {
					expectedImageStreamUpdates.Insert(update[:i])
					expectedImageStreamItemsDeletions++
				} else if i := strings.Index(update, ":"); i != -1 {
					expectedImageStreamUpdates.Insert(update[:i])
				} else {
					t.Errorf("invalid update: %s", update)
				}
			}
			if got, want := stats.UpdatedImageStreams+stats.DeletedImageStreamTagItems, expectedImageStreamUpdates.Len()+expectedImageStreamItemsDeletions; got != want {
				t.Errorf("stream updates: got %d, want %d", got, want)
			}

			expectedLayerLinkDeletions := sets.NewString(test.expectedLayerLinkDeletions...)
			if a, e := layerLinkDeleter.invocations, expectedLayerLinkDeletions; !reflect.DeepEqual(a, e) {
				t.Errorf("unexpected layer link deletions (-actual, +expected): %s", diff.ObjectDiff(a, e))
			}
			if want := expectedLayerLinkDeletions.Len() - len(test.expectedLayerLinkDeletionFailures); stats.DeletedLayerLinks != want {
				t.Errorf("layer link deletions: got %d, want %d", stats.DeletedLayerLinks, want)
			}

			expectedManifestLinkDeletions := sets.NewString(test.expectedManifestLinkDeletions...)
			if a, e := manifestDeleter.invocations, expectedManifestLinkDeletions; !reflect.DeepEqual(a, e) {
				t.Errorf("unexpected manifest link deletions (-actual, +expected): %s", diff.ObjectDiff(a, e))
			}
			if want := expectedManifestLinkDeletions.Len() - len(test.expectedManifestLinkDeletionFailures); stats.DeletedManifestLinks != want {
				t.Errorf("manifest link deletions: got %d, want %d", stats.DeletedManifestLinks, want)
			}

			expectedBlobDeletions := sets.NewString(test.expectedBlobDeletions...)
			if a, e := blobDeleter.invocations, expectedBlobDeletions; !reflect.DeepEqual(a, e) {
				t.Errorf("unexpected blob deletions (-actual, +expected): %s", diff.ObjectDiff(a, e))
			}
			if want := expectedBlobDeletions.Len() - len(test.expectedBlobDeletionFailures); stats.DeletedBlobs != want {
				t.Errorf("blob deletions: got %d, want %d", stats.DeletedBlobs, want)
			}
		})
	}
}

func TestImageDeleter(t *testing.T) {
	var level klog.Level
	level.Set(fmt.Sprint(*logLevel))

	tests := map[string]struct {
		imageDeletionError error
	}{
		"no error": {},
		"delete error": {
			imageDeletionError: fmt.Errorf("foo"),
		},
	}

	for name, test := range tests {
		imageClient := &fakeimagev1client.FakeImageV1{Fake: &clienttesting.Fake{}}
		imageClient.AddReactor("delete", "images", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, test.imageDeletionError
		})
		imageDeleter := NewImageDeleter(imageClient)
		err := imageDeleter.DeleteImage(&imagev1.Image{ObjectMeta: metav1.ObjectMeta{Name: "sha256:0000000000000000000000000000000000000000000000000000000000000002"}})
		if test.imageDeletionError != nil {
			if e, a := test.imageDeletionError, err; e != a {
				t.Errorf("%s: err: expected %v, got %v", name, e, a)
			}
			continue
		}

		if e, a := 1, len(imageClient.Actions()); e != a {
			t.Errorf("%s: expected %d actions, got %d: %#v", name, e, a, imageClient.Actions())
			continue
		}

		if !imageClient.Actions()[0].Matches("delete", "images") {
			t.Errorf("%s: expected action %s, got %v", name, "delete-images", imageClient.Actions()[0])
		}
	}
}

func TestLayerDeleter(t *testing.T) {
	var level klog.Level
	level.Set(fmt.Sprint(*logLevel))

	var actions []string
	client := fake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
		actions = append(actions, req.Method+":"+req.URL.String())
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: ioutil.NopCloser(bytes.NewReader([]byte{}))}, nil
	})
	layerLinkDeleter := NewLayerLinkDeleter(client, &url.URL{Scheme: "http", Host: "registry1"})
	layerLinkDeleter.DeleteLayerLink("repo", "layer1")

	if e := []string{"DELETE:http://registry1/v2/repo/blobs/layer1"}; !reflect.DeepEqual(actions, e) {
		t.Errorf("unexpected actions: %s", diff.ObjectDiff(actions, e))
	}
}

func TestNotFoundLayerDeleter(t *testing.T) {
	var level klog.Level
	level.Set(fmt.Sprint(*logLevel))

	var actions []string
	client := fake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
		actions = append(actions, req.Method+":"+req.URL.String())
		return &http.Response{StatusCode: http.StatusNotFound, Body: ioutil.NopCloser(bytes.NewReader([]byte{}))}, nil
	})
	layerLinkDeleter := NewLayerLinkDeleter(client, &url.URL{Scheme: "https", Host: "registry1"})
	layerLinkDeleter.DeleteLayerLink("repo", "layer1")

	if e := []string{"DELETE:https://registry1/v2/repo/blobs/layer1"}; !reflect.DeepEqual(actions, e) {
		t.Errorf("unexpected actions: %s", diff.ObjectDiff(actions, e))
	}
}

func TestRegistryPruning(t *testing.T) {
	var level klog.Level
	level.Set(fmt.Sprint(*logLevel))

	tests := []struct {
		name                       string
		images                     map[string]*imagev1.Image
		streams                    map[string]*imagev1.ImageStream
		expectedLayerLinkDeletions sets.String
		expectedBlobDeletions      sets.String
		expectedManifestDeletions  sets.String
		pruneRegistry              bool
		pingErr                    error
	}{
		{
			name:          "layers unique to id1 pruned",
			pruneRegistry: true,
			images: Images(
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000001", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001", &imagetest.Config1, "layer1", "layer2", "layer3", "layer4"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000002", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", &imagetest.Config2, "layer3", "layer4", "layer5", "layer6"),
			),
			streams: Streams(
				imagetest.Stream("registry1.io", "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
					),
				}),
				imagetest.Stream("registry1.io", "foo", "other", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", "registry1.io/foo/other@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
					),
				}),
			),
			expectedLayerLinkDeletions: sets.NewString(
				"foo/bar|"+imagetest.Config1,
				"foo/bar|layer1",
				"foo/bar|layer2",
			),
			expectedBlobDeletions: sets.NewString(
				"sha256:0000000000000000000000000000000000000000000000000000000000000001",
				imagetest.Config1,
				"layer1",
				"layer2",
			),
			expectedManifestDeletions: sets.NewString(
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000001",
			),
		},

		{
			name:          "no pruning when no images are pruned",
			pruneRegistry: true,
			images: Images(
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000001", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001", &imagetest.Config1, "layer1", "layer2", "layer3", "layer4"),
			),
			streams: Streams(
				imagetest.Stream("registry1.io", "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
					),
				}),
			),
			expectedLayerLinkDeletions: sets.NewString(),
			expectedBlobDeletions:      sets.NewString(),
			expectedManifestDeletions:  sets.NewString(),
		},

		{
			name:          "blobs pruned when streams have already been deleted",
			pruneRegistry: true,
			images: Images(
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000001", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001", &imagetest.Config1, "layer1", "layer2", "layer3", "layer4"),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000002", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", &imagetest.Config2, "layer3", "layer4", "layer5", "layer6"),
			),
			expectedLayerLinkDeletions: sets.NewString(),
			expectedBlobDeletions: sets.NewString(
				"sha256:0000000000000000000000000000000000000000000000000000000000000001",
				"sha256:0000000000000000000000000000000000000000000000000000000000000002",
				imagetest.Config1,
				imagetest.Config2,
				"layer1",
				"layer2",
				"layer3",
				"layer4",
				"layer5",
				"layer6",
			),
			expectedManifestDeletions: sets.NewString(),
		},

		{
			name:          "config used as a layer",
			pruneRegistry: true,
			images: Images(
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000001", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001", &imagetest.Config1, "layer1", "layer2", "layer3", imagetest.Config1),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000002", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", &imagetest.Config2, "layer3", "layer4", "layer5", imagetest.Config1),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000003", "registry1.io/foo/other@sha256:0000000000000000000000000000000000000000000000000000000000000003", nil, "layer3", "layer4", "layer6", imagetest.Config1),
			),
			streams: Streams(
				imagetest.Stream("registry1.io", "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
					),
				}),
				imagetest.Stream("registry1.io", "foo", "other", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", "registry1.io/foo/other@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
					),
				}),
			),
			expectedLayerLinkDeletions: sets.NewString(
				"foo/bar|layer1",
				"foo/bar|layer2",
			),
			expectedBlobDeletions: sets.NewString(
				"sha256:0000000000000000000000000000000000000000000000000000000000000001",
				"layer1",
				"layer2",
			),
			expectedManifestDeletions: sets.NewString(
				"foo/bar|sha256:0000000000000000000000000000000000000000000000000000000000000001",
			),
		},

		{
			name:          "config used as a layer, but leave registry alone",
			pruneRegistry: false,
			images: Images(
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000001", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001", &imagetest.Config1, "layer1", "layer2", "layer3", imagetest.Config1),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000002", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", &imagetest.Config2, "layer3", "layer4", "layer5", imagetest.Config1),
				imagetest.ImageWithLayers("sha256:0000000000000000000000000000000000000000000000000000000000000003", "registry1.io/foo/other@sha256:0000000000000000000000000000000000000000000000000000000000000003", nil, "layer3", "layer4", "layer6", imagetest.Config1),
			),
			streams: Streams(
				imagetest.Stream("registry1.io", "foo", "bar", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000001", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
					),
				}),
				imagetest.Stream("registry1.io", "foo", "other", []imagev1.NamedTagEventList{
					imagetest.Tag("latest",
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000003", "registry1.io/foo/other@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
						imagetest.TagEvent("sha256:0000000000000000000000000000000000000000000000000000000000000002", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
					),
				}),
			),
			expectedLayerLinkDeletions: sets.NewString(),
			expectedBlobDeletions:      sets.NewString(),
			expectedManifestDeletions:  sets.NewString(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			keepYoungerThan := 60 * time.Minute
			keepTagRevisions := 1
			options := PrunerOptions{
				KeepYoungerThan:  &keepYoungerThan,
				KeepTagRevisions: &keepTagRevisions,
				PruneRegistry:    &test.pruneRegistry,
				Images:           test.images,
				Streams:          test.streams,
				Pods:             &corev1.PodList{},
				RCs:              &corev1.ReplicationControllerList{},
				BCs:              &buildv1.BuildConfigList{},
				Builds:           &buildv1.BuildList{},
				DSs:              &kappsv1.DaemonSetList{},
				Deployments:      &kappsv1.DeploymentList{},
				DCs:              &appsv1.DeploymentConfigList{},
				RSs:              &kappsv1.ReplicaSetList{},
				SSs:              &kappsv1.StatefulSetList{},
				Jobs:             &batchv1.JobList{},
				CronJobs:         &batchv1beta1.CronJobList{},
			}
			p, err := NewPruner(options)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			streamDeleter := &fakeImageStreamDeleter{invocations: sets.NewString()}
			layerLinkDeleter := &fakeLayerLinkDeleter{invocations: sets.NewString()}
			manifestDeleter := &fakeManifestDeleter{invocations: sets.NewString()}
			blobDeleter := &fakeBlobDeleter{invocations: sets.NewString()}
			imageDeleter := newFakeImageDeleter(nil)

			p.Prune(streamDeleter, layerLinkDeleter, manifestDeleter, blobDeleter, imageDeleter)

			if a, e := layerLinkDeleter.invocations, test.expectedLayerLinkDeletions; !reflect.DeepEqual(a, e) {
				t.Errorf("unexpected layer link deletions: %s", diff.ObjectDiff(a, e))
			}
			if a, e := blobDeleter.invocations, test.expectedBlobDeletions; !reflect.DeepEqual(a, e) {
				t.Errorf("unexpected blob deletions: %s", diff.ObjectDiff(a, e))
			}
			if a, e := manifestDeleter.invocations, test.expectedManifestDeletions; !reflect.DeepEqual(a, e) {
				t.Errorf("unexpected manifest deletions: %s", diff.ObjectDiff(a, e))
			}
		})
	}
}

func newBool(a bool) *bool {
	r := new(bool)
	*r = a
	return r
}

func TestImageWithStrongAndWeakRefsIsNotPruned(t *testing.T) {
	var level klog.Level
	level.Set(fmt.Sprint(*logLevel))

	images := Images(
		imagetest.AgedImage("0000000000000000000000000000000000000000000000000000000000000001", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001", 1540),
		imagetest.AgedImage("0000000000000000000000000000000000000000000000000000000000000002", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002", 1540),
		imagetest.AgedImage("0000000000000000000000000000000000000000000000000000000000000003", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003", 1540),
	)
	streams := Streams(
		imagetest.Stream("registry1", "foo", "bar", []imagev1.NamedTagEventList{
			imagetest.Tag("latest",
				imagetest.TagEvent("0000000000000000000000000000000000000000000000000000000000000003", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000003"),
				imagetest.TagEvent("0000000000000000000000000000000000000000000000000000000000000002", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000002"),
				imagetest.TagEvent("0000000000000000000000000000000000000000000000000000000000000001", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
			),
			imagetest.Tag("strong",
				imagetest.TagEvent("0000000000000000000000000000000000000000000000000000000000000001", "registry1.io/foo/bar@sha256:0000000000000000000000000000000000000000000000000000000000000001"),
			),
		}),
	)
	pods := imagetest.PodList()
	rcs := imagetest.RCList()
	bcs := imagetest.BCList()
	builds := imagetest.BuildList()
	dss := imagetest.DSList()
	deployments := imagetest.DeploymentList()
	dcs := imagetest.DCList()
	rss := imagetest.RSList()
	sss := imagetest.SSList()
	jobs := imagetest.JobList()
	cjs := imagetest.CronJobList()

	options := PrunerOptions{
		Images:      images,
		Streams:     streams,
		Pods:        &pods,
		RCs:         &rcs,
		BCs:         &bcs,
		Builds:      &builds,
		DSs:         &dss,
		Deployments: &deployments,
		DCs:         &dcs,
		RSs:         &rss,
		SSs:         &sss,
		Jobs:        &jobs,
		CronJobs:    &cjs,
	}
	keepYoungerThan := 24 * time.Hour
	keepTagRevisions := 2
	options.KeepYoungerThan = &keepYoungerThan
	options.KeepTagRevisions = &keepTagRevisions
	p, err := NewPruner(options)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	streamDeleter := &fakeImageStreamDeleter{invocations: sets.NewString()}
	layerLinkDeleter := &fakeLayerLinkDeleter{invocations: sets.NewString()}
	manifestDeleter := &fakeManifestDeleter{invocations: sets.NewString()}
	blobDeleter := &fakeBlobDeleter{invocations: sets.NewString()}
	imageDeleter := newFakeImageDeleter(nil)

	stats, errs := p.Prune(streamDeleter, layerLinkDeleter, manifestDeleter, blobDeleter, imageDeleter)
	if errs != nil {
		t.Errorf("got unexpected errors: %#+v", errs)
	}
	if stats.String() != "deleted 1 image stream tag item(s), updated 1 image stream(s)" {
		t.Errorf("got unexpected deletions: %v", stats)
	}

	if imageDeleter.invocations.Len() > 0 {
		t.Fatalf("unexpected imageDeleter invocations: %v", imageDeleter.invocations)
	}
	if !streamDeleter.invocations.Equal(sets.NewString("foo/bar|latest|2|0000000000000000000000000000000000000000000000000000000000000001")) {
		t.Fatalf("unexpected streamDeleter invocations: %v", streamDeleter.invocations)
	}
	if layerLinkDeleter.invocations.Len() > 0 {
		t.Fatalf("unexpected layerLinkDeleter invocations: %v", layerLinkDeleter.invocations)
	}
	if blobDeleter.invocations.Len() > 0 {
		t.Fatalf("unexpected blobDeleter invocations: %v", blobDeleter.invocations)
	}
	if manifestDeleter.invocations.Len() > 0 {
		t.Fatalf("unexpected manifestDeleter invocations: %v", manifestDeleter.invocations)
	}
}

func keepTagRevisions(n int) *int {
	return &n
}

type fakeImageDeleter struct {
	mutex       sync.Mutex
	invocations sets.String
	err         error
}

var _ ImageDeleter = &fakeImageDeleter{}

func (p *fakeImageDeleter) DeleteImage(image *imagev1.Image) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.invocations.Insert(image.Name)
	return p.err
}

func newFakeImageDeleter(err error) *fakeImageDeleter {
	return &fakeImageDeleter{
		err:         err,
		invocations: sets.NewString(),
	}
}

type fakeImageStreamDeleter struct {
	mutex       sync.Mutex
	invocations sets.String
	err         error
	streams     map[string]map[string][]string
}

var _ ImageStreamDeleter = &fakeImageStreamDeleter{}

func (p *fakeImageStreamDeleter) GetImageStream(stream *imagev1.ImageStream) (*imagev1.ImageStream, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.streams == nil {
		p.streams = make(map[string]map[string][]string)
	}

	streamName := fmt.Sprintf("%s/%s", stream.Namespace, stream.Name)
	s := make(map[string][]string)
	for _, tag := range stream.Status.Tags {
		var items []string
		for _, tagEvent := range tag.Items {
			items = append(items, tagEvent.Image)
		}
		s[tag.Tag] = items
	}

	p.streams[streamName] = s

	return stream, p.err
}

func (p *fakeImageStreamDeleter) UpdateImageStream(stream *imagev1.ImageStream, revisionsDeleted int) (*imagev1.ImageStream, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	streamName := fmt.Sprintf("%s/%s", stream.Namespace, stream.Name)
	s := p.streams[streamName]

	n := make(map[string][]string)
	for _, tag := range stream.Status.Tags {
		var items []string
		for _, tagEvent := range tag.Items {
			items = append(items, tagEvent.Image)
		}
		n[tag.Tag] = items
	}

	for tag, items := range s {
		newItems, ok := n[tag]
		if !ok {
			p.invocations.Insert(fmt.Sprintf("%s:%s", streamName, tag))
		}

		newItemsIndex := 0
		for itemsIndex := 0; itemsIndex < len(items); itemsIndex++ {
			if newItemsIndex < len(newItems) && newItems[newItemsIndex] == items[itemsIndex] {
				newItemsIndex++
				continue
			}
			p.invocations.Insert(fmt.Sprintf("%s|%s|%d|%s", streamName, tag, itemsIndex, items[itemsIndex]))
		}
	}

	return stream, p.err
}

type errorForSHA func(dgst string) error

type fakeBlobDeleter struct {
	mutex       sync.Mutex
	invocations sets.String
	getError    errorForSHA
}

var _ BlobDeleter = &fakeBlobDeleter{}

func (p *fakeBlobDeleter) DeleteBlob(blob string) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.invocations.Insert(blob)
	if p.getError == nil {
		return nil
	}
	return p.getError(blob)
}

type fakeLayerLinkDeleter struct {
	mutex       sync.Mutex
	invocations sets.String
	err         error
}

var _ LayerLinkDeleter = &fakeLayerLinkDeleter{}

func (p *fakeLayerLinkDeleter) DeleteLayerLink(repo, layer string) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.invocations.Insert(fmt.Sprintf("%s|%s", repo, layer))
	return p.err
}

type fakeManifestDeleter struct {
	mutex       sync.Mutex
	invocations sets.String
	err         error
}

var _ ManifestDeleter = &fakeManifestDeleter{}

func (p *fakeManifestDeleter) DeleteManifest(repo, manifest string) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.invocations.Insert(fmt.Sprintf("%s|%s", repo, manifest))
	return p.err
}
