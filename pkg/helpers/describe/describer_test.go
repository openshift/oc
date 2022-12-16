package describe

import (
	"bytes"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"text/tabwriter"

	v1 "github.com/openshift/api/quota/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/openshift/api"
	appsv1 "github.com/openshift/api/apps/v1"
	authorizationv1 "github.com/openshift/api/authorization/v1"
	buildv1 "github.com/openshift/api/build/v1"
	consolev1 "github.com/openshift/api/console/v1"
	consolev1alpha1 "github.com/openshift/api/console/v1alpha1"
	dockerv10 "github.com/openshift/api/image/docker10"
	dockerpre012 "github.com/openshift/api/image/dockerpre012"
	imagev1 "github.com/openshift/api/image/v1"
	monitoringv1alpha1 "github.com/openshift/api/monitoring/v1alpha1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	projectv1 "github.com/openshift/api/project/v1"
	securityv1 "github.com/openshift/api/security/v1"
	templatev1 "github.com/openshift/api/template/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	kubernetesscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

type describeClient struct {
	T         *testing.T
	Namespace string
	Err       error
}

// DescriberCoverageExceptions is the list of API types that do NOT have corresponding describers
// If you add something to this list, explain why it doesn't need validation.  waaaa is not a valid
// reason.
var DescriberCoverageExceptions = []reflect.Type{
	reflect.TypeOf(&buildv1.BuildLog{}),                              // normal users don't ever look at these
	reflect.TypeOf(&buildv1.BuildLogOptions{}),                       // normal users don't ever look at these
	reflect.TypeOf(&buildv1.BinaryBuildRequestOptions{}),             // normal users don't ever look at these
	reflect.TypeOf(&buildv1.BuildRequest{}),                          // normal users don't ever look at these
	reflect.TypeOf(&appsv1.DeploymentConfigRollback{}),               // normal users don't ever look at these
	reflect.TypeOf(&appsv1.DeploymentLog{}),                          // normal users don't ever look at these
	reflect.TypeOf(&appsv1.DeploymentLogOptions{}),                   // normal users don't ever look at these
	reflect.TypeOf(&appsv1.DeploymentRequest{}),                      // normal users don't ever look at these
	reflect.TypeOf(&dockerpre012.DockerImage{}),                      // not a top level resource
	reflect.TypeOf(&dockerv10.DockerImage{}),                         // not a top level resource
	reflect.TypeOf(&imagev1.ImageStreamImport{}),                     // normal users don't ever look at these
	reflect.TypeOf(&oauthv1.OAuthAccessToken{}),                      // normal users don't ever look at these
	reflect.TypeOf(&oauthv1.OAuthAuthorizeToken{}),                   // normal users don't ever look at these
	reflect.TypeOf(&oauthv1.OAuthClientAuthorization{}),              // normal users don't ever look at these
	reflect.TypeOf(&oauthv1.UserOAuthAccessToken{}),                  // UserOAuthAccessToken is a virtual resource to mirror OAuthAccessTokens
	reflect.TypeOf(&projectv1.ProjectRequest{}),                      // normal users don't ever look at these
	reflect.TypeOf(&templatev1.TemplateInstance{}),                   // normal users don't ever look at these
	reflect.TypeOf(&templatev1.BrokerTemplateInstance{}),             // normal users don't ever look at these
	reflect.TypeOf(&authorizationv1.IsPersonalSubjectAccessReview{}), // not a top level resource
	// ATM image signature doesn't provide any human readable information
	reflect.TypeOf(&imagev1.ImageSignature{}),
	// we might want to add this in the future
	reflect.TypeOf(&imagev1.ImageStreamLayers{}),

	// these resources are descrbied with custom resource definitions
	reflect.TypeOf(&consolev1.ConsoleCLIDownload{}),
	reflect.TypeOf(&consolev1.ConsoleExternalLogLink{}),
	reflect.TypeOf(&consolev1.ConsoleLink{}),
	reflect.TypeOf(&consolev1.ConsoleNotification{}),
	reflect.TypeOf(&consolev1.ConsolePlugin{}),
	reflect.TypeOf(&consolev1.ConsoleQuickStart{}),
	reflect.TypeOf(&consolev1.ConsoleYAMLSample{}),
	reflect.TypeOf(&consolev1alpha1.ConsolePlugin{}),

	// these resources can't be "GET"ed, so you can't make a describer for them
	reflect.TypeOf(&authorizationv1.SubjectAccessReviewResponse{}),
	reflect.TypeOf(&authorizationv1.ResourceAccessReviewResponse{}),
	reflect.TypeOf(&authorizationv1.SubjectAccessReview{}),
	reflect.TypeOf(&authorizationv1.ResourceAccessReview{}),
	reflect.TypeOf(&authorizationv1.LocalSubjectAccessReview{}),
	reflect.TypeOf(&authorizationv1.LocalResourceAccessReview{}),
	reflect.TypeOf(&authorizationv1.SelfSubjectRulesReview{}),
	reflect.TypeOf(&authorizationv1.SubjectRulesReview{}),
	reflect.TypeOf(&securityv1.PodSecurityPolicySubjectReview{}),
	reflect.TypeOf(&securityv1.PodSecurityPolicySelfSubjectReview{}),
	reflect.TypeOf(&securityv1.PodSecurityPolicyReview{}),
	reflect.TypeOf(&securityv1.RangeAllocation{}),
	reflect.TypeOf(&oauthv1.OAuthRedirectReference{}),
	reflect.TypeOf(&monitoringv1alpha1.AlertRelabelConfig{}),
	reflect.TypeOf(&monitoringv1alpha1.AlertingRule{}),
}

// MissingDescriberCoverageExceptions is the list of types that were missing describer methods when I started
// You should never add to this list
// TODO describers should be added for these types
var MissingDescriberCoverageExceptions = []reflect.Type{
	reflect.TypeOf(&imagev1.ImageStreamMapping{}),
	reflect.TypeOf(&oauthv1.OAuthClient{}),
}

var MissingDescriberGroupCoverageExceptions = []schema.GroupVersion{
	{Group: "config.openshift.io", Version: "v1"},
	{Group: "osin.config.openshift.io", Version: "v1"},
	{Group: "servicecertsigner.config.openshift.io", Version: "v1alpha1"},
	{Group: "kubecontrolplane.config.openshift.io", Version: "v1"},
	{Group: "openshiftcontrolplane.config.openshift.io", Version: "v1"},

	{Group: "controlplane.operator.openshift.io", Version: "v1alpha1"},
	{Group: "imageregistry.operator.openshift.io", Version: "v1"},
	{Group: "operator.openshift.io", Version: "v1alpha1"},
	{Group: "operator.openshift.io", Version: "v1"},
	{Group: "network.operator.openshift.io", Version: "v1"},
	{Group: "samples.operator.openshift.io", Version: "v1"},

	{Group: "cloud.network.openshift.io", Version: "v1"},

	{Group: "apiserver.openshift.io", Version: "v1"},

	{Group: "helm.openshift.io", Version: "v1beta1"},
	{Group: "machine.openshift.io", Version: "v1beta1"},
	{Group: "machine.openshift.io", Version: "v1"},
	{Group: "sharedresource.openshift.io", Version: "v1alpha1"},
}

func TestDescriberCoverage(t *testing.T) {
	scheme := runtime.NewScheme()
	kubernetesscheme.AddToScheme(scheme)
	api.Install(scheme)

main:
	for gvk, apiType := range scheme.AllKnownTypes() {
		if !strings.HasPrefix(apiType.PkgPath(), "github.com/openshift/api") || strings.HasPrefix(apiType.PkgPath(), "github.com/openshift/origin/vendor/") {
			continue
		}
		// we don't describe lists
		if strings.HasSuffix(apiType.Name(), "List") {
			continue
		}

		ptrType := reflect.PtrTo(apiType)
		for _, exception := range DescriberCoverageExceptions {
			if ptrType == exception {
				continue main
			}
		}
		for _, exception := range MissingDescriberCoverageExceptions {
			if ptrType == exception {
				continue main
			}
		}

		_, ok := DescriberFor(gvk.GroupKind(), &rest.Config{}, fake.NewSimpleClientset(), "")
		if !ok {
			for _, exception := range MissingDescriberGroupCoverageExceptions {
				if exception == gvk.GroupVersion() {
					continue main
				}
			}

			t.Errorf("missing describer for %v (%s).  Check pkg/cmd/cli/describe/describer.go", apiType, gvk)
		}
	}
}

func TestDescribeBuildDuration(t *testing.T) {
	type testBuild struct {
		build  *buildv1.Build
		output string
	}

	// now a minute ago
	now := metav1.Now()
	minuteAgo := metav1.Unix(now.Rfc3339Copy().Time.Unix()-60, 0)
	twoMinutesAgo := metav1.Unix(now.Rfc3339Copy().Time.Unix()-120, 0)
	threeMinutesAgo := metav1.Unix(now.Rfc3339Copy().Time.Unix()-180, 0)

	tests := []testBuild{
		{ // 0 - build new
			&buildv1.Build{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: minuteAgo},
				Status: buildv1.BuildStatus{
					Phase: buildv1.BuildPhaseNew,
				},
			},
			"waiting for 1m",
		},
		{ // 1 - build pending
			&buildv1.Build{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: minuteAgo},
				Status: buildv1.BuildStatus{
					Phase: buildv1.BuildPhasePending,
				},
			},
			"waiting for 1m",
		},
		{ // 2 - build running
			&buildv1.Build{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: twoMinutesAgo},
				Status: buildv1.BuildStatus{
					StartTimestamp: &minuteAgo,
					Phase:          buildv1.BuildPhaseRunning,
				},
			},
			"running for 1m",
		},
		{ // 3 - build completed
			&buildv1.Build{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: threeMinutesAgo},
				Status: buildv1.BuildStatus{
					StartTimestamp:      &twoMinutesAgo,
					CompletionTimestamp: &minuteAgo,
					Phase:               buildv1.BuildPhaseComplete,
				},
			},
			"1m",
		},
		{ // 4 - build failed
			&buildv1.Build{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: threeMinutesAgo},
				Status: buildv1.BuildStatus{
					StartTimestamp:      &twoMinutesAgo,
					CompletionTimestamp: &minuteAgo,
					Phase:               buildv1.BuildPhaseFailed,
				},
			},
			"1m",
		},
		{ // 5 - build error
			&buildv1.Build{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: threeMinutesAgo},
				Status: buildv1.BuildStatus{
					StartTimestamp:      &twoMinutesAgo,
					CompletionTimestamp: &minuteAgo,
					Phase:               buildv1.BuildPhaseError,
				},
			},
			"1m",
		},
		{ // 6 - build cancelled before running, start time wasn't set yet
			&buildv1.Build{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: threeMinutesAgo},
				Status: buildv1.BuildStatus{
					CompletionTimestamp: &minuteAgo,
					Phase:               buildv1.BuildPhaseCancelled,
				},
			},
			"waited for 2m",
		},
		{ // 7 - build cancelled while running, start time is set already
			&buildv1.Build{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: threeMinutesAgo},
				Status: buildv1.BuildStatus{
					StartTimestamp:      &twoMinutesAgo,
					CompletionTimestamp: &minuteAgo,
					Phase:               buildv1.BuildPhaseCancelled,
				},
			},
			"1m",
		},
		{ // 8 - build failed before running, start time wasn't set yet
			&buildv1.Build{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: threeMinutesAgo},
				Status: buildv1.BuildStatus{
					CompletionTimestamp: &minuteAgo,
					Phase:               buildv1.BuildPhaseFailed,
				},
			},
			"waited for 2m",
		},
		{ // 9 - build error before running, start time wasn't set yet
			&buildv1.Build{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: threeMinutesAgo},
				Status: buildv1.BuildStatus{
					CompletionTimestamp: &minuteAgo,
					Phase:               buildv1.BuildPhaseError,
				},
			},
			"waited for 2m",
		},
	}

	for i, tc := range tests {
		if actual, expected := describeBuildDuration(tc.build), tc.output; !strings.Contains(actual, expected) {
			t.Errorf("(%d) expected duration output %s, got %s", i, expected, actual)
		}
	}
}

func mkV1Pod(status corev1.PodPhase, exitCode int) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "PodName"},
		Status: corev1.PodStatus{
			Phase: status,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{ExitCode: int32(exitCode)},
					},
				},
			},
		},
	}
}

func mkPod(status corev1.PodPhase, exitCode int) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "PodName"},
		Status: corev1.PodStatus{
			Phase: status,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{ExitCode: int32(exitCode)},
					},
				},
			},
		},
	}
}

func TestDescribePostCommitHook(t *testing.T) {
	tests := []struct {
		hook buildv1.BuildPostCommitSpec
		want string
	}{
		{
			hook: buildv1.BuildPostCommitSpec{},
			want: "",
		},
		{
			hook: buildv1.BuildPostCommitSpec{
				Script: "go test",
			},
			want: `"/bin/sh", "-ic", "go test"`,
		},
		{
			hook: buildv1.BuildPostCommitSpec{
				Command: []string{"go", "test"},
			},
			want: `"go", "test"`,
		},
		{
			hook: buildv1.BuildPostCommitSpec{
				Args: []string{"go", "test"},
			},
			want: `"<image-entrypoint>", "go", "test"`,
		},
		{
			hook: buildv1.BuildPostCommitSpec{
				Script: `go test "$@"`,
				Args:   []string{"-v", "-timeout", "2s"},
			},
			want: `"/bin/sh", "-ic", "go test \"$@\"", "/bin/sh", "-v", "-timeout", "2s"`,
		},
		{
			hook: buildv1.BuildPostCommitSpec{
				Command: []string{"go", "test"},
				Args:    []string{"-v", "-timeout", "2s"},
			},
			want: `"go", "test", "-v", "-timeout", "2s"`,
		},
		{
			// Invalid hook: Script and Command are not allowed
			// together. For printing, Script takes precedence.
			hook: buildv1.BuildPostCommitSpec{
				Script:  "go test -v",
				Command: []string{"go", "test"},
			},
			want: `"/bin/sh", "-ic", "go test -v"`,
		},
	}
	for _, tt := range tests {
		var b bytes.Buffer
		out := tabwriter.NewWriter(&b, 0, 8, 0, '\t', 0)
		describePostCommitHook(tt.hook, out)
		if err := out.Flush(); err != nil {
			t.Fatalf("%+v: flush error: %v", tt.hook, err)
		}
		var want string
		if tt.want != "" {
			want = fmt.Sprintf("Post Commit Hook:\t[%s]\n", tt.want)
		}
		if got := b.String(); got != want {
			t.Errorf("describePostCommitHook(%+v, out) = %q, want %q", tt.hook, got, want)
		}
	}
}

func TestDescribeBuildSpec(t *testing.T) {
	tests := []struct {
		spec buildv1.BuildSpec
		want string
	}{
		{
			spec: buildv1.BuildSpec{
				CommonSpec: buildv1.CommonSpec{
					Source: buildv1.BuildSource{
						Git: &buildv1.GitBuildSource{
							URI: "http://github.com/my/repository",
						},
						ContextDir: "context",
					},
					Strategy: buildv1.BuildStrategy{
						DockerStrategy: &buildv1.DockerBuildStrategy{},
					},
					Output: buildv1.BuildOutput{
						To: &corev1.ObjectReference{
							Kind: "DockerImage",
							Name: "repository/data",
						},
					},
				},
			},
			want: "URL",
		},
		{
			spec: buildv1.BuildSpec{
				CommonSpec: buildv1.CommonSpec{
					Source: buildv1.BuildSource{},
					Strategy: buildv1.BuildStrategy{
						SourceStrategy: &buildv1.SourceBuildStrategy{
							From: corev1.ObjectReference{
								Kind: "DockerImage",
								Name: "myimage:tag",
							},
						},
					},
					Output: buildv1.BuildOutput{
						To: &corev1.ObjectReference{
							Kind: "DockerImage",
							Name: "repository/data",
						},
					},
				},
			},
			want: "Empty Source",
		},
		{
			spec: buildv1.BuildSpec{
				CommonSpec: buildv1.CommonSpec{
					Source: buildv1.BuildSource{},
					Strategy: buildv1.BuildStrategy{
						CustomStrategy: &buildv1.CustomBuildStrategy{
							From: corev1.ObjectReference{
								Kind: "DockerImage",
								Name: "myimage:tag",
							},
						},
					},
					Output: buildv1.BuildOutput{
						To: &corev1.ObjectReference{
							Kind: "DockerImage",
							Name: "repository/data",
						},
					},
				},
			},
			want: "Empty Source",
		},
		{
			spec: buildv1.BuildSpec{
				CommonSpec: buildv1.CommonSpec{
					Source: buildv1.BuildSource{},
					Strategy: buildv1.BuildStrategy{
						JenkinsPipelineStrategy: &buildv1.JenkinsPipelineBuildStrategy{
							Jenkinsfile: "openshiftBuild",
						},
					},
				},
			},
			want: "openshiftBuild",
		},
	}
	for _, tt := range tests {
		var b bytes.Buffer
		out := tabwriter.NewWriter(&b, 0, 8, 0, '\t', 0)
		describeCommonSpec(tt.spec.CommonSpec, out)
		if err := out.Flush(); err != nil {
			t.Fatalf("%+v: flush error: %v", tt.spec, err)
		}
		if got := b.String(); !strings.Contains(got, tt.want) {
			t.Errorf("describeBuildSpec(%+v, out) = %q, should contain %q", tt.spec, got, tt.want)
		}
	}
}

func TestDescribeImage(t *testing.T) {
	tests := []struct {
		image imagev1.Image
		want  []string
	}{
		{
			image: imagev1.Image{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				DockerImageMetadata: runtime.RawExtension{Object: &dockerv10.DockerImage{}},
			},
			want: []string{"Name:.+test"},
		},
		{
			image: imagev1.Image{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				DockerImageLayers: []imagev1.ImageLayer{
					{Name: "sha256:1234", LayerSize: 3409},
					{Name: "sha256:5678", LayerSize: 1024},
				},
				DockerImageMetadata: runtime.RawExtension{Object: &dockerv10.DockerImage{}},
			},
			want: []string{
				"Layers:.+3.409kB\\ssha256:1234",
				"1.024kB\\ssha256:5678",
				"Image Size:.+0B in 2 layers",
			},
		},
		{
			image: imagev1.Image{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				DockerImageLayers: []imagev1.ImageLayer{
					{Name: "sha256:1234", LayerSize: 3409},
					{Name: "sha256:5678", LayerSize: 1024},
				},
				DockerImageMetadata: runtime.RawExtension{Object: &dockerv10.DockerImage{Size: 4430}},
			},
			want: []string{
				"Layers:.+3.409kB\\ssha256:1234",
				"1.024kB\\ssha256:5678",
				"Image Size:.+4.43kB in 2 layers",
			},
		},
		{
			image: imagev1.Image{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				DockerImageManifests: []imagev1.ImageManifest{
					{Architecture: "amd64", OS: "linux", Digest: "sha256:1234"},
					{Architecture: "s390x", OS: "linux", Digest: "sha256:5678"},
				},
				DockerImageMetadata: runtime.RawExtension{Object: &dockerv10.DockerImage{}},
			},
			want: []string{
				"Manifests:\\slinux/amd64\\ssha256:1234",
				"linux/s390x\\ssha256:5678",
			},
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			out, err := DescribeImage(&tt.image, tt.image.Name)
			if err != nil {
				t.Fatal(err)
			}
			for _, match := range tt.want {
				if got := out; !regexp.MustCompile(match).MatchString(got) {
					t.Errorf("%s\nshould contain %q", got, match)
				}
			}
		})
	}
}

func TestDescribeClusterQuota(t *testing.T) {
	testStatus := &corev1.ResourceQuotaStatus{
		Hard: map[corev1.ResourceName]resource.Quantity{
			corev1.ResourceCPU:            resource.MustParse("1"),
			corev1.ResourceLimitsCPU:      resource.MustParse("2"),
			corev1.ResourceLimitsMemory:   resource.MustParse("2G"),
			corev1.ResourceMemory:         resource.MustParse("1G"),
			corev1.ResourceRequestsCPU:    resource.MustParse("1"),
			corev1.ResourceRequestsMemory: resource.MustParse("1G"),
			corev1.ResourcePods:           resource.MustParse("1000"),
		},
		Used: map[corev1.ResourceName]resource.Quantity{
			corev1.ResourceCPU:            resource.MustParse("300m"),
			corev1.ResourceLimitsCPU:      resource.MustParse("1"),
			corev1.ResourceLimitsMemory:   resource.MustParse("0G"),
			corev1.ResourceMemory:         resource.MustParse("100M"),
			corev1.ResourceRequestsCPU:    resource.MustParse("500m"),
			corev1.ResourceRequestsMemory: resource.MustParse("1000Ki"),
			corev1.ResourcePods:           resource.MustParse("20"),
		},
	}

	tests := []struct {
		quota v1.ClusterResourceQuota
		want  []string
	}{
		{
			quota: v1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: v1.ClusterResourceQuotaSpec{
					Selector: v1.ClusterResourceQuotaSelector{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "test",
							},
						},
					},
				},
				Status: v1.ClusterResourceQuotaStatus{
					Namespaces: []v1.ResourceQuotaStatusByNamespace{
						{
							Namespace: "test",
							Status:    *testStatus.DeepCopy(),
						},
					},
					Total: *testStatus.DeepCopy(),
				},
			},
			want: []string{
				"Name:\\s+test",
				"Namespace Selector:\\s+\\[\"test\"\\]",
				"Label Selector:\\s+app=test",
				"Resource\\s+Used\\s+Hard",
				"cpu\\s+300m\\s+1",
				"limits.cpu\\s+1\\s+2",
				"limits.memory\\s+0\\s+2G",
				"memory\\s+100M\\s+1G",
				"pods\\s+20\\s+1k",
				"requests.cpu\\s+500m\\s+1",
				"requests.memory\\s+1024k\\s+1G",
			},
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			out, err := DescribeClusterQuota(&tt.quota)
			if err != nil {
				t.Fatal(err)
			}
			for _, match := range tt.want {
				if got := out; !regexp.MustCompile(match).MatchString(got) {
					t.Errorf("%s\nshould contain %q", got, match)
				}
			}
		})
	}
}

func Test_describeBuildVolumes(t *testing.T) {
	var InvalidSourceType buildv1.BuildVolumeSourceType = "InvalidType"
	tests := []struct {
		name    string
		volumes []buildv1.BuildVolume
		want    []string
	}{
		{
			name: "invalid source type",
			volumes: []buildv1.BuildVolume{
				{
					Name: "my-secret-volume",
					Source: buildv1.BuildVolumeSource{
						Type: InvalidSourceType,
						Secret: &corev1.SecretVolumeSource{
							SecretName: "my-secret",
						},
					},
					Mounts: []buildv1.BuildVolumeMount{
						{
							DestinationPath: "/my/secret/destination/path",
						},
						{
							DestinationPath: "/my/secret/destination/path/two",
						},
					},
				},
			},
			want: []string{
				"<InvalidSourceType: \"InvalidType\">",
			},
		},
		{
			name: "secret build volume",
			volumes: []buildv1.BuildVolume{
				{
					Name: "my-secret-volume",
					Source: buildv1.BuildVolumeSource{
						Type: buildv1.BuildVolumeSourceTypeSecret,
						Secret: &corev1.SecretVolumeSource{
							SecretName: "my-secret",
						},
					},
					Mounts: []buildv1.BuildVolumeMount{
						{
							DestinationPath: "/my/secret/destination/path",
						},
						{
							DestinationPath: "/my/secret/destination/path/two",
						},
					},
				},
			},
			want: []string{
				"my-secret-volume",
				"my-secret",
				"/my/secret/destination/path",
				"/my/secret/destination/path/two",
			},
		},
		{
			name: "config map build volume",
			volumes: []buildv1.BuildVolume{
				{
					Name: "my-configmap-volume",
					Source: buildv1.BuildVolumeSource{
						Type: buildv1.BuildVolumeSourceTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "my-configmap",
							},
						},
					},
					Mounts: []buildv1.BuildVolumeMount{
						{
							DestinationPath: "/my/configmap/destination/path",
						},
						{
							DestinationPath: "/my/configmap/destination/path/two",
						},
					},
				},
			},
			want: []string{
				"my-configmap-volume",
				"my-configmap",
				"/my/configmap/destination/path",
				"/my/configmap/destination/path/two",
			},
		},
		{
			name: "csi build volume",
			volumes: []buildv1.BuildVolume{
				{
					Name: "my-csi-volume",
					Source: buildv1.BuildVolumeSource{
						Type: buildv1.BuildVolumeSourceTypeCSI,
						CSI: &corev1.CSIVolumeSource{
							Driver:           "inline.storage.kubernetes.io",
							VolumeAttributes: map[string]string{"foo": "bar"},
						},
					},
					Mounts: []buildv1.BuildVolumeMount{
						{
							DestinationPath: "/my/csi/destination/path",
						},
						{
							DestinationPath: "/my/csi/destination/path/two",
						},
					},
				},
			},
			want: []string{
				"my-csi-volume",
				"my-csi-volume",
				"/my/csi/destination/path",
				"/my/csi/destination/path/two",
			},
		},
	}
	for _, tt := range tests {
		var b bytes.Buffer
		out := tabwriter.NewWriter(&b, 0, 8, 0, '\t', 0)
		t.Run(tt.name, func(t *testing.T) {
			describeBuildVolumes(out, tt.volumes)
			out.Flush()
			for _, match := range tt.want {
				if got := b.String(); !regexp.MustCompile(match).MatchString(got) {
					t.Errorf("%s\nshould contain %q", got, match)
				}
			}
		})
	}
}
