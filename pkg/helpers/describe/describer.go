package describe

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"k8s.io/klog/v2"

	"github.com/docker/go-units"
	corev1 "k8s.io/api/core/v1"
	kerrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/describe"
	"k8s.io/kubectl/pkg/scheme"

	"github.com/openshift/api/annotations"
	oapps "github.com/openshift/api/apps"
	"github.com/openshift/api/authorization"
	authorizationv1 "github.com/openshift/api/authorization/v1"
	"github.com/openshift/api/build"
	buildv1 "github.com/openshift/api/build/v1"
	"github.com/openshift/api/config"
	configv1alpha1 "github.com/openshift/api/config/v1alpha1"
	"github.com/openshift/api/image"
	dockerv10 "github.com/openshift/api/image/docker10"
	imagev1 "github.com/openshift/api/image/v1"
	"github.com/openshift/api/network"
	networkv1 "github.com/openshift/api/network/v1"
	"github.com/openshift/api/oauth"
	"github.com/openshift/api/project"
	projectv1 "github.com/openshift/api/project/v1"
	"github.com/openshift/api/quota"
	quotav1 "github.com/openshift/api/quota/v1"
	"github.com/openshift/api/route"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/api/security"
	securityv1 "github.com/openshift/api/security/v1"
	"github.com/openshift/api/template"
	templatev1 "github.com/openshift/api/template/v1"
	"github.com/openshift/api/user"
	appstypedclient "github.com/openshift/client-go/apps/clientset/versioned/typed/apps/v1"
	oauthorizationclient "github.com/openshift/client-go/authorization/clientset/versioned/typed/authorization/v1"
	buildv1clienttyped "github.com/openshift/client-go/build/clientset/versioned/typed/build/v1"
	configclientv1alpha1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1alpha1"
	imageclient "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	onetworktypedclient "github.com/openshift/client-go/network/clientset/versioned/typed/network/v1"
	oauthclient "github.com/openshift/client-go/oauth/clientset/versioned/typed/oauth/v1"
	projectclient "github.com/openshift/client-go/project/clientset/versioned/typed/project/v1"
	quotaclient "github.com/openshift/client-go/quota/clientset/versioned/typed/quota/v1"
	routeclient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	securityclient "github.com/openshift/client-go/security/clientset/versioned/typed/security/v1"
	templateclient "github.com/openshift/client-go/template/clientset/versioned/typed/template/v1"
	userclient "github.com/openshift/client-go/user/clientset/versioned/typed/user/v1"
	"github.com/openshift/library-go/pkg/build/naming"
	"github.com/openshift/library-go/pkg/image/imageutil"
	authorizationhelpers "github.com/openshift/oc/pkg/helpers/authorization"
	buildhelpers "github.com/openshift/oc/pkg/helpers/build"
	"github.com/openshift/oc/pkg/helpers/legacy"
	quotahelpers "github.com/openshift/oc/pkg/helpers/quota"
	routedisplayhelpers "github.com/openshift/oc/pkg/helpers/route"
)

func describerMap(clientConfig *rest.Config, kclient kubernetes.Interface, host string) map[schema.GroupKind]describe.ResourceDescriber {
	// FIXME: This should use the client factory
	// we can't fail and we can't log at a normal level because this is sometimes called with `nils` for help :(
	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		klog.V(1).Info(err)
	}
	oauthorizationClient, err := oauthorizationclient.NewForConfig(clientConfig)
	if err != nil {
		klog.V(1).Info(err)
	}
	onetworkClient, err := onetworktypedclient.NewForConfig(clientConfig)
	if err != nil {
		klog.V(1).Info(err)
	}
	userClient, err := userclient.NewForConfig(clientConfig)
	if err != nil {
		klog.V(1).Info(err)
	}
	quotaClient, err := quotaclient.NewForConfig(clientConfig)
	if err != nil {
		klog.V(1).Info(err)
	}
	imageClient, err := imageclient.NewForConfig(clientConfig)
	if err != nil {
		klog.V(1).Info(err)
	}
	appsClient, err := appstypedclient.NewForConfig(clientConfig)
	if err != nil {
		klog.V(1).Info(err)
	}
	buildClient, err := buildv1clienttyped.NewForConfig(clientConfig)
	if err != nil {
		klog.V(1).Info(err)
	}
	templateClient, err := templateclient.NewForConfig(clientConfig)
	if err != nil {
		klog.V(1).Info(err)
	}
	routeClient, err := routeclient.NewForConfig(clientConfig)
	if err != nil {
		klog.V(1).Info(err)
	}
	projectClient, err := projectclient.NewForConfig(clientConfig)
	if err != nil {
		klog.V(1).Info(err)
	}
	oauthClient, err := oauthclient.NewForConfig(clientConfig)
	if err != nil {
		klog.V(1).Info(err)
	}
	securityClient, err := securityclient.NewForConfig(clientConfig)
	if err != nil {
		klog.V(1).Info(err)
	}
	configv1alpha1Client, err := configclientv1alpha1.NewForConfig(clientConfig)
	if err != nil {
		klog.V(1).Info(err)
	}

	m := map[schema.GroupKind]describe.ResourceDescriber{
		oapps.Kind("DeploymentConfig"):               &DeploymentConfigDescriber{appsClient, kubeClient, nil},
		build.Kind("Build"):                          &BuildDescriber{buildClient, kclient},
		build.Kind("BuildConfig"):                    &BuildConfigDescriber{buildClient, kclient, host},
		image.Kind("Image"):                          &ImageDescriber{imageClient},
		image.Kind("ImageStream"):                    &ImageStreamDescriber{imageClient},
		image.Kind("ImageStreamTag"):                 &ImageStreamTagDescriber{imageClient},
		image.Kind("ImageTag"):                       &ImageTagDescriber{imageClient},
		image.Kind("ImageStreamImage"):               &ImageStreamImageDescriber{imageClient},
		route.Kind("Route"):                          &RouteDescriber{routeClient, kclient},
		project.Kind("Project"):                      &ProjectDescriber{projectClient, kclient},
		template.Kind("Template"):                    &TemplateDescriber{templateClient, meta.NewAccessor(), scheme.Scheme, nil},
		template.Kind("TemplateInstance"):            &TemplateInstanceDescriber{kclient, templateClient, nil},
		authorization.Kind("RoleBinding"):            &RoleBindingDescriber{oauthorizationClient},
		authorization.Kind("Role"):                   &RoleDescriber{oauthorizationClient},
		authorization.Kind("ClusterRoleBinding"):     &ClusterRoleBindingDescriber{oauthorizationClient},
		authorization.Kind("ClusterRole"):            &ClusterRoleDescriber{oauthorizationClient},
		authorization.Kind("RoleBindingRestriction"): &RoleBindingRestrictionDescriber{oauthorizationClient},
		oauth.Kind("OAuthAccessToken"):               &OAuthAccessTokenDescriber{oauthClient},
		user.Kind("Identity"):                        &IdentityDescriber{userClient},
		user.Kind("User"):                            &UserDescriber{userClient},
		user.Kind("Group"):                           &GroupDescriber{userClient},
		user.Kind("UserIdentityMapping"):             &UserIdentityMappingDescriber{userClient},
		quota.Kind("ClusterResourceQuota"):           &ClusterQuotaDescriber{quotaClient},
		quota.Kind("AppliedClusterResourceQuota"):    &AppliedClusterQuotaDescriber{quotaClient},
		network.Kind("ClusterNetwork"):               &ClusterNetworkDescriber{onetworkClient},
		network.Kind("HostSubnet"):                   &HostSubnetDescriber{onetworkClient},
		network.Kind("NetNamespace"):                 &NetNamespaceDescriber{onetworkClient},
		network.Kind("EgressNetworkPolicy"):          &EgressNetworkPolicyDescriber{onetworkClient},
		security.Kind("SecurityContextConstraints"):  &SecurityContextConstraintsDescriber{securityClient},
		config.Kind("InsightsDataGather"):            &InsightsDataGatherDescriber{configv1alpha1Client},
	}

	// Register the legacy ("core") API group for all kinds as well.
	for gk, d := range m {
		m[legacy.Kind(gk.Kind)] = d
	}
	return m
}

// DescriberFor returns a describer for a given kind of resource
func DescriberFor(kind schema.GroupKind, clientConfig *rest.Config, kubeClient kubernetes.Interface, host string) (describe.ResourceDescriber, bool) {
	f, ok := describerMap(clientConfig, kubeClient, host)[kind]
	if ok {
		return f, true
	}
	return nil, false
}

// BuildDescriber generates information about a build
type BuildDescriber struct {
	buildClient buildv1clienttyped.BuildV1Interface
	kubeClient  kubernetes.Interface
}

// GetBuildPodName returns name of the build pod.
func getBuildPodName(build *buildv1.Build) string {
	return naming.GetPodName(build.Name, "build")
}

// Describe returns the description of a build
func (d *BuildDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.buildClient.Builds(namespace)
	buildObj, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	events, _ := d.kubeClient.CoreV1().Events(namespace).Search(scheme.Scheme, buildObj)
	if events == nil {
		events = &corev1.EventList{}
	}
	// get also pod events and merge it all into one list for describe
	if pod, err := d.kubeClient.CoreV1().Pods(namespace).Get(context.TODO(), getBuildPodName(buildObj), metav1.GetOptions{}); err == nil {
		if podEvents, _ := d.kubeClient.CoreV1().Events(namespace).Search(scheme.Scheme, pod); podEvents != nil {
			events.Items = append(events.Items, podEvents.Items...)
		}
	}
	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, buildObj.ObjectMeta)

		fmt.Fprintln(out, "")

		status := bold(buildObj.Status.Phase)
		if buildObj.Status.Message != "" {
			status += " (" + buildObj.Status.Message + ")"
		}
		formatString(out, "Status", status)

		if buildObj.Status.StartTimestamp != nil && !buildObj.Status.StartTimestamp.IsZero() {
			formatString(out, "Started", buildObj.Status.StartTimestamp.Time.Format(time.RFC1123))
		}

		// Create the time object with second-level precision so we don't get
		// output like "duration: 1.2724395728934s"
		formatString(out, "Duration", describeBuildDuration(buildObj))

		for _, stage := range buildObj.Status.Stages {
			duration := stage.StartTime.Time.Add(time.Duration(stage.DurationMilliseconds * int64(time.Millisecond))).Round(time.Second).Sub(stage.StartTime.Time.Round(time.Second))
			formatString(out, fmt.Sprintf("  %v", stage.Name), fmt.Sprintf("  %v", duration))
		}

		fmt.Fprintln(out, "")

		if buildObj.Status.Config != nil {
			formatString(out, "Build Config", buildObj.Status.Config.Name)
		}
		formatString(out, "Build Pod", getBuildPodName(buildObj))

		if buildObj.Status.Output.To != nil && len(buildObj.Status.Output.To.ImageDigest) > 0 {
			formatString(out, "Image Digest", buildObj.Status.Output.To.ImageDigest)
		}

		describeCommonSpec(buildObj.Spec.CommonSpec, out)
		describeBuildTriggerCauses(buildObj.Spec.TriggeredBy, out)
		if len(buildObj.Status.LogSnippet) != 0 {
			formatString(out, "Log Tail", buildObj.Status.LogSnippet)
		}
		if settings.ShowEvents {
			describe.DescribeEvents(events, describe.NewPrefixWriter(out))
		}

		return nil
	})
}

func describeBuildDuration(build *buildv1.Build) string {
	t := metav1.Now().Rfc3339Copy()
	if build.Status.StartTimestamp == nil &&
		build.Status.CompletionTimestamp != nil &&
		(build.Status.Phase == buildv1.BuildPhaseCancelled ||
			build.Status.Phase == buildv1.BuildPhaseFailed ||
			build.Status.Phase == buildv1.BuildPhaseError) {
		// time a build waited for its pod before ultimately being cancelled before that pod was created
		return fmt.Sprintf("waited for %s", build.Status.CompletionTimestamp.Rfc3339Copy().Time.Sub(build.CreationTimestamp.Rfc3339Copy().Time))
	} else if build.Status.StartTimestamp == nil && build.Status.Phase != buildv1.BuildPhaseCancelled {
		// time a new build has been waiting for its pod to be created so it can run
		return fmt.Sprintf("waiting for %v", t.Sub(build.CreationTimestamp.Rfc3339Copy().Time))
	} else if build.Status.StartTimestamp != nil && build.Status.CompletionTimestamp == nil {
		// time a still running build has been running in a pod
		duration := metav1.Now().Rfc3339Copy().Time.Sub(build.Status.StartTimestamp.Rfc3339Copy().Time)
		return fmt.Sprintf("running for %v", duration)
	} else if build.Status.CompletionTimestamp == nil &&
		build.Status.StartTimestamp == nil &&
		build.Status.Phase == buildv1.BuildPhaseCancelled {
		return "<none>"
	}

	duration := build.Status.CompletionTimestamp.Rfc3339Copy().Time.Sub(build.Status.StartTimestamp.Rfc3339Copy().Time)
	return fmt.Sprintf("%v", duration)
}

// BuildConfigDescriber generates information about a buildConfig
type BuildConfigDescriber struct {
	buildClient buildv1clienttyped.BuildV1Interface
	kubeClient  kubernetes.Interface
	host        string
}

func nameAndNamespace(ns, name string) string {
	if len(ns) != 0 {
		return fmt.Sprintf("%s/%s", ns, name)
	}
	return name
}

func describeCommonSpec(p buildv1.CommonSpec, out *tabwriter.Writer) {
	formatString(out, "\nStrategy", buildhelpers.StrategyType(p.Strategy))
	noneType := true
	if p.Source.Git != nil {
		noneType = false
		formatString(out, "URL", p.Source.Git.URI)
		if len(p.Source.Git.Ref) > 0 {
			formatString(out, "Ref", p.Source.Git.Ref)
		}
		if len(p.Source.ContextDir) > 0 {
			formatString(out, "ContextDir", p.Source.ContextDir)
		}
		if p.Source.SourceSecret != nil {
			formatString(out, "Source Secret", p.Source.SourceSecret.Name)
		}
		squashGitInfo(p.Revision, out)
	}
	if p.Source.Dockerfile != nil {
		if len(strings.TrimSpace(*p.Source.Dockerfile)) == 0 {
			formatString(out, "Dockerfile", "")
		} else {
			fmt.Fprintf(out, "Dockerfile:\n")
			for _, s := range strings.Split(*p.Source.Dockerfile, "\n") {
				fmt.Fprintf(out, "  %s\n", s)
			}
		}
	}
	switch {
	case p.Strategy.DockerStrategy != nil:
		describeDockerStrategy(p.Strategy.DockerStrategy, out)
	case p.Strategy.SourceStrategy != nil:
		describeSourceStrategy(p.Strategy.SourceStrategy, out)
	case p.Strategy.CustomStrategy != nil:
		describeCustomStrategy(p.Strategy.CustomStrategy, out)
	case p.Strategy.JenkinsPipelineStrategy != nil:
		describeJenkinsPipelineStrategy(p.Strategy.JenkinsPipelineStrategy, out)
	}

	if p.Output.To != nil {
		formatString(out, "Output to", fmt.Sprintf("%s %s", p.Output.To.Kind, nameAndNamespace(p.Output.To.Namespace, p.Output.To.Name)))
	}

	if p.Source.Binary != nil {
		noneType = false
		if len(p.Source.Binary.AsFile) > 0 {
			formatString(out, "Binary", fmt.Sprintf("provided as file %q on build", p.Source.Binary.AsFile))
		} else {
			formatString(out, "Binary", "provided on build")
		}
	}

	if len(p.Source.Secrets) > 0 {
		result := []string{}
		for _, s := range p.Source.Secrets {
			result = append(result, fmt.Sprintf("%s->%s", s.Secret.Name, filepath.Clean(s.DestinationDir)))
		}
		formatString(out, "Build Secrets", strings.Join(result, ","))
	}
	if len(p.Source.ConfigMaps) > 0 {
		result := []string{}
		for _, c := range p.Source.ConfigMaps {
			result = append(result, fmt.Sprintf("%s->%s", c.ConfigMap.Name, filepath.Clean(c.DestinationDir)))
		}
		formatString(out, "Build ConfigMaps", strings.Join(result, ","))
	}

	if len(p.Source.Images) == 1 && len(p.Source.Images[0].Paths) == 1 {
		noneType = false
		imageObj := p.Source.Images[0]
		path := imageObj.Paths[0]
		formatString(out, "Image Source", fmt.Sprintf("copies %s from %s to %s", path.SourcePath, nameAndNamespace(imageObj.From.Namespace, imageObj.From.Name), path.DestinationDir))
	} else {
		for _, image := range p.Source.Images {
			noneType = false
			formatString(out, "Image Source", fmt.Sprintf("%s", nameAndNamespace(image.From.Namespace, image.From.Name)))
			for _, path := range image.Paths {
				fmt.Fprintf(out, "\t- %s -> %s\n", path.SourcePath, path.DestinationDir)
			}
			for _, name := range image.As {
				fmt.Fprintf(out, "\t- as %s\n", name)
			}
		}
	}

	if noneType {
		formatString(out, "Empty Source", "no input source provided")
	}

	describePostCommitHook(p.PostCommit, out)

	if p.Output.PushSecret != nil {
		formatString(out, "Push Secret", p.Output.PushSecret.Name)
	}

	if p.CompletionDeadlineSeconds != nil {
		formatString(out, "Fail Build After", time.Duration(*p.CompletionDeadlineSeconds)*time.Second)
	}
}

func describePostCommitHook(hook buildv1.BuildPostCommitSpec, out *tabwriter.Writer) {
	command := hook.Command
	args := hook.Args
	script := hook.Script
	if len(command) == 0 && len(args) == 0 && len(script) == 0 {
		// Post commit hook is not set, nothing to do.
		return
	}
	if len(script) != 0 {
		command = []string{"/bin/sh", "-ic"}
		if len(args) > 0 {
			args = append([]string{script, command[0]}, args...)
		} else {
			args = []string{script}
		}
	}
	if len(command) == 0 {
		command = []string{"<image-entrypoint>"}
	}
	all := append(command, args...)
	for i, v := range all {
		all[i] = fmt.Sprintf("%q", v)
	}
	formatString(out, "Post Commit Hook", fmt.Sprintf("[%s]", strings.Join(all, ", ")))
}

func describeSourceStrategy(s *buildv1.SourceBuildStrategy, out *tabwriter.Writer) {
	if len(s.From.Name) != 0 {
		formatString(out, "From Image", fmt.Sprintf("%s %s", s.From.Kind, nameAndNamespace(s.From.Namespace, s.From.Name)))
	}
	if len(s.Scripts) != 0 {
		formatString(out, "Scripts", s.Scripts)
	}
	if s.PullSecret != nil {
		formatString(out, "Pull Secret Name", s.PullSecret.Name)
	}
	if s.Incremental != nil && *s.Incremental {
		formatString(out, "Incremental Build", "yes")
	}
	if s.ForcePull {
		formatString(out, "Force Pull", "yes")
	}
	describeBuildVolumes(out, s.Volumes)
}

func describeDockerStrategy(s *buildv1.DockerBuildStrategy, out *tabwriter.Writer) {
	if s.From != nil && len(s.From.Name) != 0 {
		formatString(out, "From Image", fmt.Sprintf("%s %s", s.From.Kind, nameAndNamespace(s.From.Namespace, s.From.Name)))
	}
	if len(s.DockerfilePath) != 0 {
		formatString(out, "Dockerfile Path", s.DockerfilePath)
	}
	if s.PullSecret != nil {
		formatString(out, "Pull Secret Name", s.PullSecret.Name)
	}
	if s.NoCache {
		formatString(out, "No Cache", "true")
	}
	if s.ForcePull {
		formatString(out, "Force Pull", "true")
	}
	describeBuildVolumes(out, s.Volumes)
}

func describeCustomStrategy(s *buildv1.CustomBuildStrategy, out *tabwriter.Writer) {
	if len(s.From.Name) != 0 {
		formatString(out, "Image Reference", fmt.Sprintf("%s %s", s.From.Kind, nameAndNamespace(s.From.Namespace, s.From.Name)))
	}
	if s.ExposeDockerSocket {
		formatString(out, "Expose Docker Socket", "yes")
	}
	if s.ForcePull {
		formatString(out, "Force Pull", "yes")
	}
	if s.PullSecret != nil {
		formatString(out, "Pull Secret Name", s.PullSecret.Name)
	}
	for i, env := range s.Env {
		if i == 0 {
			formatString(out, "Environment", formatEnv(env))
		} else {
			formatString(out, "", formatEnv(env))
		}
	}
}

func describeJenkinsPipelineStrategy(s *buildv1.JenkinsPipelineBuildStrategy, out *tabwriter.Writer) {
	if len(s.JenkinsfilePath) != 0 {
		formatString(out, "Jenkinsfile path", s.JenkinsfilePath)
	}
	if len(s.Jenkinsfile) != 0 {
		fmt.Fprintf(out, "Jenkinsfile contents:\n")
		for _, s := range strings.Split(s.Jenkinsfile, "\n") {
			fmt.Fprintf(out, "  %s\n", s)
		}
	}
	if len(s.Jenkinsfile) == 0 && len(s.JenkinsfilePath) == 0 {
		formatString(out, "Jenkinsfile", "from source repository root")
	}
}

// DescribeTriggers generates information about the triggers associated with a
// buildconfig
func (d *BuildConfigDescriber) DescribeTriggers(bc *buildv1.BuildConfig, out *tabwriter.Writer) {
	describeBuildTriggers(bc.Spec.Triggers, bc.Name, bc.Namespace, out, d)
}

func describeBuildTriggers(triggers []buildv1.BuildTriggerPolicy, name, namespace string, w *tabwriter.Writer, d *BuildConfigDescriber) {
	if len(triggers) == 0 {
		formatString(w, "Triggered by", "<none>")
		return
	}

	labels := []string{}

	for _, t := range triggers {
		switch t.Type {
		case buildv1.GitHubWebHookBuildTriggerType, buildv1.GenericWebHookBuildTriggerType, buildv1.GitLabWebHookBuildTriggerType, buildv1.BitbucketWebHookBuildTriggerType:
			continue
		case buildv1.ConfigChangeBuildTriggerType:
			labels = append(labels, "Config")
		case buildv1.ImageChangeBuildTriggerType:
			if t.ImageChange != nil && t.ImageChange.From != nil && len(t.ImageChange.From.Name) > 0 {
				labels = append(labels, fmt.Sprintf("Image(%s %s)", t.ImageChange.From.Kind, t.ImageChange.From.Name))
			} else {
				labels = append(labels, string(t.Type))
			}
		case "":
			labels = append(labels, "<unknown>")
		default:
			labels = append(labels, string(t.Type))
		}
	}

	desc := strings.Join(labels, ", ")
	formatString(w, "Triggered by", desc)

	webHooks := webHooksDescribe(triggers, name, namespace, d.buildClient.RESTClient())
	seenHookTypes := make(map[string]bool)
	for webHookType, webHookDesc := range webHooks {
		fmt.Fprintf(w, "Webhook %s:\n", strings.Title(webHookType))
		for _, trigger := range webHookDesc {
			_, seen := seenHookTypes[webHookType]
			if webHookType != string(buildv1.GenericWebHookBuildTriggerType) && seen {
				continue
			}
			seenHookTypes[webHookType] = true
			fmt.Fprintf(w, "\tURL:\t%s\n", trigger.URL)
			if webHookType == string(buildv1.GenericWebHookBuildTriggerType) && trigger.AllowEnv != nil {
				fmt.Fprintf(w, fmt.Sprintf("\t%s:\t%v\n", "AllowEnv", *trigger.AllowEnv))
			}
		}
	}
}

// describeBuildVolumes returns the description of a slice of build volumes
func describeBuildVolumes(w *tabwriter.Writer, volumes []buildv1.BuildVolume) {
	if len(volumes) == 0 {
		formatString(w, "Volumes", "<none>")
		return
	}
	formatString(w, "Volumes", " ")
	fmt.Fprint(w, "\tName\tSource Type\tSource\tMounts\n")
	for _, v := range volumes {
		var sourceName string
		switch v.Source.Type {
		case buildv1.BuildVolumeSourceTypeSecret:
			sourceName = v.Source.Secret.SecretName
		case buildv1.BuildVolumeSourceTypeConfigMap:
			sourceName = v.Source.ConfigMap.Name
		case buildv1.BuildVolumeSourceTypeCSI:
			sourceName = v.Name
		default:
			sourceName = fmt.Sprintf("<InvalidSourceType: %q>", v.Source.Type)
		}

		var mounts []string
		for _, m := range v.Mounts {
			mounts = append(mounts, m.DestinationPath)
		}
		fmt.Fprintf(w, "\t%s\t%s\t%s\t%s\n", v.Name, v.Source.Type, sourceName, strings.Join(mounts, "\n\t\t\t\t"))
	}
}

// Describe returns the description of a buildConfig
func (d *BuildConfigDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.buildClient.BuildConfigs(namespace)
	buildConfig, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	buildList, err := d.buildClient.Builds(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	buildList.Items = buildhelpers.FilterBuilds(buildList.Items, buildhelpers.ByBuildConfigPredicate(name))

	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, buildConfig.ObjectMeta)
		if buildConfig.Status.LastVersion == 0 {
			formatString(out, "Latest Version", "Never built")
		} else {
			formatString(out, "Latest Version", strconv.FormatInt(buildConfig.Status.LastVersion, 10))
		}
		describeCommonSpec(buildConfig.Spec.CommonSpec, out)
		formatString(out, "\nBuild Run Policy", string(buildConfig.Spec.RunPolicy))
		d.DescribeTriggers(buildConfig, out)

		if buildConfig.Spec.SuccessfulBuildsHistoryLimit != nil || buildConfig.Spec.FailedBuildsHistoryLimit != nil {
			fmt.Fprintf(out, "Builds History Limit:\n")
			if buildConfig.Spec.SuccessfulBuildsHistoryLimit != nil {
				fmt.Fprintf(out, "\tSuccessful:\t%s\n", strconv.Itoa(int(*buildConfig.Spec.SuccessfulBuildsHistoryLimit)))
			}
			if buildConfig.Spec.FailedBuildsHistoryLimit != nil {
				fmt.Fprintf(out, "\tFailed:\t%s\n", strconv.Itoa(int(*buildConfig.Spec.FailedBuildsHistoryLimit)))
			}
		}

		if len(buildList.Items) > 0 {
			fmt.Fprintf(out, "\nBuild\tStatus\tDuration\tCreation Time\n")

			builds := buildList.Items
			sort.Sort(sort.Reverse(buildhelpers.BuildSliceByCreationTimestamp(builds)))

			for i, build := range builds {
				fmt.Fprintf(out, "%s \t%s \t%v \t%v\n",
					build.Name,
					strings.ToLower(string(build.Status.Phase)),
					describeBuildDuration(&build),
					build.CreationTimestamp.Rfc3339Copy().Time)
				// only print the 10 most recent builds.
				if i == 9 {
					break
				}
			}
		}

		if settings.ShowEvents {
			events, _ := d.kubeClient.CoreV1().Events(namespace).Search(scheme.Scheme, buildConfig)
			if events != nil {
				fmt.Fprint(out, "\n")
				describe.DescribeEvents(events, describe.NewPrefixWriter(out))
			}
		}
		return nil
	})
}

// OAuthAccessTokenDescriber generates information about an OAuth Acess Token (OAuth)
type OAuthAccessTokenDescriber struct {
	client oauthclient.OauthV1Interface
}

func (d *OAuthAccessTokenDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.client.OAuthAccessTokens()
	oAuthAccessToken, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	var timeCreated time.Time = oAuthAccessToken.ObjectMeta.CreationTimestamp.Time
	expires := "never"
	if oAuthAccessToken.ExpiresIn > 0 {
		var timeExpired time.Time = timeCreated.Add(time.Duration(oAuthAccessToken.ExpiresIn) * time.Second)
		expires = formatToHumanDuration(timeExpired.Sub(time.Now()))
	}

	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, oAuthAccessToken.ObjectMeta)
		formatString(out, "Scopes", oAuthAccessToken.Scopes)
		formatString(out, "Expires In", expires)
		formatString(out, "User Name", oAuthAccessToken.UserName)
		formatString(out, "User UID", oAuthAccessToken.UserUID)
		formatString(out, "Client Name", oAuthAccessToken.ClientName)

		return nil
	})
}

// ImageDescriber generates information about a Image
type ImageDescriber struct {
	c imageclient.ImageV1Interface
}

// Describe returns the description of an image
func (d *ImageDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.c.Images()
	image, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	return DescribeImage(image, "")
}

func describeImageSignature(s imagev1.ImageSignature, out *tabwriter.Writer) error {
	formatString(out, "\tName", s.Name)
	formatString(out, "\tType", s.Type)
	if s.IssuedBy == nil {
		// FIXME: Make this constant
		formatString(out, "\tStatus", "Unverified")
	} else {
		formatString(out, "\tStatus", "Verified")
		formatString(out, "\tIssued By", s.IssuedBy.CommonName)
		if len(s.Conditions) > 0 {
			for _, c := range s.Conditions {
				formatString(out, "\t", fmt.Sprintf("Signature is %s (%s on %s)", string(c.Type), c.Message, fmt.Sprintf("%s", c.LastProbeTime)))
			}
		}
	}
	return nil
}

func DescribeImage(image *imagev1.Image, imageName string) (string, error) {
	return tabbedString(func(out *tabwriter.Writer) error {
		if len(imageName) > 0 {
			formatString(out, "Image Name", imageName)
		}
		formatString(out, "Docker Image", image.DockerImageReference)
		formatString(out, "Name", image.Name)
		if !image.CreationTimestamp.IsZero() {
			formatTime(out, "Created", image.CreationTimestamp.Time)
		}
		if len(image.Labels) > 0 {
			formatMapStringString(out, "Labels", image.Labels)
		}
		if len(image.Annotations) > 0 {
			formatAnnotations(out, image.ObjectMeta, "")
		}

		if err := imageutil.ImageWithMetadata(image); err != nil {
			return err
		}
		dockerImage, ok := image.DockerImageMetadata.Object.(*dockerv10.DockerImage)
		if !ok {
			klog.V(1).Infof("Unable to cast image metadata: %s, %#v", string(image.DockerImageMetadata.Raw), image.DockerImageMetadata.Object)
			return fmt.Errorf("unable to read image metadata")
		}
		switch l := len(image.DockerImageLayers); l {
		case 0:
			// legacy case, server does not know individual layers
			formatString(out, "Layer Size", units.HumanSize(float64(dockerImage.Size)))
		default:
			formatString(out, "Image Size", fmt.Sprintf("%s in %d layers", units.HumanSize(float64(dockerImage.Size)), len(image.DockerImageLayers)))
			var layers []string
			for _, layer := range image.DockerImageLayers {
				layers = append(layers, fmt.Sprintf("%s\t%s", units.HumanSize(float64(layer.LayerSize)), layer.Name))
			}
			formatString(out, "Layers", strings.Join(layers, "\n"))
		}
		if len(image.Signatures) > 0 {
			for _, s := range image.Signatures {
				formatString(out, "Image Signatures", " ")
				if err := describeImageSignature(s, out); err != nil {
					return err
				}
			}
		}
		formatString(out, "Image Created", fmt.Sprintf("%s ago", FormatRelativeTime(dockerImage.Created.Time)))
		formatString(out, "Author", dockerImage.Author)
		formatString(out, "Arch", dockerImage.Architecture)

		if len(image.DockerImageManifests) > 0 {
			var manifests []string
			for _, m := range image.DockerImageManifests {
				manifests = append(manifests, fmt.Sprintf("%s/%s\t%s", m.OS, m.Architecture, m.Digest))
			}
			formatString(out, "Manifests", strings.Join(manifests, "\n"))
		}

		dockerImageConfig := dockerImage.Config
		// This field should always be populated, if it is not we should print empty fields.
		if dockerImageConfig == nil {
			dockerImageConfig = &dockerv10.DockerConfig{}
		}

		// Config is the configuration of the container received from the client.
		// In most cases this field is always set for images.
		describeDockerImage(out, dockerImageConfig)
		return nil
	})
}

func describeDockerImage(out *tabwriter.Writer, image *dockerv10.DockerConfig) {
	if image == nil {
		return
	}
	hasCommand := false
	if len(image.Entrypoint) > 0 {
		hasCommand = true
		formatString(out, "Entrypoint", strings.Join(image.Entrypoint, " "))
	}
	if len(image.Cmd) > 0 {
		hasCommand = true
		formatString(out, "Command", strings.Join(image.Cmd, " "))
	}
	if !hasCommand {
		formatString(out, "Command", "")
	}
	formatString(out, "Working Dir", image.WorkingDir)
	formatString(out, "User", image.User)
	ports := sets.NewString()
	for k := range image.ExposedPorts {
		ports.Insert(k)
	}
	formatString(out, "Exposes Ports", strings.Join(ports.List(), ", "))
	formatMapStringString(out, "Docker Labels", image.Labels)
	for i, env := range image.Env {
		if i == 0 {
			formatString(out, "Environment", env)
		} else {
			fmt.Fprintf(out, "\t%s\n", env)
		}
	}
	volumes := sets.NewString()
	for k := range image.Volumes {
		volumes.Insert(k)
	}
	for i, volume := range volumes.List() {
		if i == 0 {
			formatString(out, "Volumes", volume)
		} else {
			fmt.Fprintf(out, "\t%s\n", volume)
		}
	}
}

// ImageStreamTagDescriber generates information about a ImageStreamTag (Image).
type ImageStreamTagDescriber struct {
	c imageclient.ImageV1Interface
}

// Describe returns the description of an imageStreamTag
func (d *ImageStreamTagDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.c.ImageStreamTags(namespace)
	repo, tag, err := imageutil.ParseImageStreamTagName(name)
	if err != nil {
		return "", err
	}
	if len(tag) == 0 {
		// TODO use repo's preferred default, when that's coded
		tag = imagev1.DefaultImageTag
	}
	imageStreamTag, err := c.Get(context.TODO(), repo+":"+tag, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	return DescribeImage(&imageStreamTag.Image, imageStreamTag.Image.Name)
}

// ImageTagDescriber generates information about a ImageTag (Image).
type ImageTagDescriber struct {
	c imageclient.ImageV1Interface
}

// Describe returns the description of an imageStreamTag
func (d *ImageTagDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.c.ImageTags(namespace)
	repo, tag, err := imageutil.ParseImageStreamTagName(name)
	if err != nil {
		return "", err
	}
	if len(tag) == 0 {
		// TODO use repo's preferred default, when that's coded
		tag = imagev1.DefaultImageTag
	}
	imageTag, err := c.Get(context.TODO(), repo+":"+tag, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	return DescribeImage(imageTag.Image, imageTag.Image.Name)
}

// ImageStreamImageDescriber generates information about a ImageStreamImage (Image).
type ImageStreamImageDescriber struct {
	c imageclient.ImageV1Interface
}

// Describe returns the description of an imageStreamImage
func (d *ImageStreamImageDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.c.ImageStreamImages(namespace)
	imageStreamImage, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	return DescribeImage(&imageStreamImage.Image, imageStreamImage.Image.Name)
}

// ImageStreamDescriber generates information about a ImageStream (Image).
type ImageStreamDescriber struct {
	ImageClient imageclient.ImageV1Interface
}

// Describe returns the description of an imageStream
func (d *ImageStreamDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.ImageClient.ImageStreams(namespace)
	imageStream, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return DescribeImageStream(imageStream)
}

func DescribeImageStream(imageStream *imagev1.ImageStream) (string, error) {
	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, imageStream.ObjectMeta)
		if len(imageStream.Status.PublicDockerImageRepository) > 0 {
			formatString(out, "Image Repository", imageStream.Status.PublicDockerImageRepository)
		} else {
			formatString(out, "Image Repository", imageStream.Status.DockerImageRepository)
		}
		formatString(out, "Image Lookup", fmt.Sprintf("local=%t", imageStream.Spec.LookupPolicy.Local))
		formatImageStreamTags(out, imageStream)
		return nil
	})
}

// RouteDescriber generates information about a Route
type RouteDescriber struct {
	routeClient routeclient.RouteV1Interface
	kubeClient  kubernetes.Interface
}

type routeEndpointInfo struct {
	*corev1.Endpoints
	Err        error
	TargetPort *intstr.IntOrString
}

// Describe returns the description of a route
func (d *RouteDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.routeClient.Routes(namespace)
	route, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	backends := append([]routev1.RouteTargetReference{route.Spec.To}, route.Spec.AlternateBackends...)
	totalWeight := int32(0)
	endpoints := make(map[string]routeEndpointInfo)
	port := &intstr.IntOrString{}
	if route.Spec.Port != nil {
		port = &route.Spec.Port.TargetPort
	}
	for _, backend := range backends {
		if backend.Weight != nil {
			totalWeight += *backend.Weight
		}
		ep, endpointsErr := d.kubeClient.CoreV1().Endpoints(namespace).Get(context.TODO(), backend.Name, metav1.GetOptions{})
		endpoints[backend.Name] = routeEndpointInfo{ep, endpointsErr, port}
	}

	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, route.ObjectMeta)
		if len(route.Spec.Host) > 0 {
			formatString(out, "Requested Host", route.Spec.Host)
			for _, ingress := range route.Status.Ingress {
				if route.Spec.Host != ingress.Host {
					continue
				}
				formatRouteIngress(out, true, ingress)
			}
		} else {
			formatString(out, "Requested Host", "<auto>")
		}

		for _, ingress := range route.Status.Ingress {
			if route.Spec.Host == ingress.Host {
				continue
			}
			formatRouteIngress(out, false, ingress)
		}
		formatString(out, "Path", route.Spec.Path)

		tlsTerm := ""
		insecurePolicy := ""
		if route.Spec.TLS != nil {
			tlsTerm = string(route.Spec.TLS.Termination)
			insecurePolicy = string(route.Spec.TLS.InsecureEdgeTerminationPolicy)
		}
		formatString(out, "TLS Termination", tlsTerm)
		formatString(out, "Insecure Policy", insecurePolicy)
		if route.Spec.Port != nil {
			formatString(out, "Endpoint Port", route.Spec.Port.TargetPort.String())
		} else {
			formatString(out, "Endpoint Port", "<all endpoint ports>")
		}

		for _, backend := range backends {
			fmt.Fprintln(out)
			formatString(out, "Service", backend.Name)
			weight := int32(0)
			if backend.Weight != nil {
				weight = *backend.Weight
			}
			if weight > 0 {
				fmt.Fprintf(out, "Weight:\t%d (%d%%)\n", weight, weight*100/totalWeight)
			} else {
				formatString(out, "Weight", "0")
			}

			info := endpoints[backend.Name]
			if info.Err != nil {
				formatString(out, "Endpoints", fmt.Sprintf("<error: %v>", info.Err))
				continue
			}
			endpoints := info.Endpoints
			if len(endpoints.Subsets) == 0 {
				formatString(out, "Endpoints", "<none>")
				continue
			}

			list := []string{}
			max := 3
			count := 0
			for i := range endpoints.Subsets {
				ss := &endpoints.Subsets[i]
				for p := range ss.Ports {
					// If the route specifies a target port, filter endpoints accordingly,
					// rather than display all endpoints for a route's target service(s).
					if info.TargetPort != nil {
						if info.TargetPort.String() != ss.Ports[p].Name && int32(info.TargetPort.IntValue()) != ss.Ports[p].Port {
							continue
						}
					}
					for a := range ss.Addresses {
						if len(list) < max {
							list = append(list, fmt.Sprintf("%s:%d", ss.Addresses[a].IP, ss.Ports[p].Port))
						}
						count++
					}
				}
			}
			ends := strings.Join(list, ", ")
			if count > max {
				ends += fmt.Sprintf(" + %d more...", count-max)
			}
			formatString(out, "Endpoints", ends)
		}
		return nil
	})
}

func formatRouteIngress(out *tabwriter.Writer, short bool, ingress routev1.RouteIngress) {
	hostName := ""
	if len(ingress.RouterCanonicalHostname) > 0 {
		hostName = fmt.Sprintf(" (host %s)", ingress.RouterCanonicalHostname)
	}
	switch status, condition := routedisplayhelpers.IngressConditionStatus(&ingress, routev1.RouteAdmitted); status {
	case corev1.ConditionTrue:
		fmt.Fprintf(out, "\t  ")
		if !short {
			fmt.Fprintf(out, "%s", ingress.Host)
		}
		fmt.Fprintf(out, " exposed on router %s%s", ingress.RouterName, hostName)
		if condition.LastTransitionTime != nil {
			fmt.Fprintf(out, " %s ago", strings.ToLower(FormatRelativeTime(condition.LastTransitionTime.Time)))
		}
		fmt.Fprintf(out, "\n")
	case corev1.ConditionFalse:
		fmt.Fprintf(out, "\trejected by router %s: %s%s", ingress.RouterName, hostName, condition.Reason)
		if condition.LastTransitionTime != nil {
			fmt.Fprintf(out, " (%s ago)", strings.ToLower(FormatRelativeTime(condition.LastTransitionTime.Time)))
		}
		fmt.Fprintf(out, "\n")
		if len(condition.Message) > 0 {
			fmt.Fprintf(out, "\t  %s\n", condition.Message)
		}
	}
}

// ProjectDescriber generates information about a Project
type ProjectDescriber struct {
	projectClient projectclient.ProjectV1Interface
	kubeClient    kubernetes.Interface
}

// Describe returns the description of a project
func (d *ProjectDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	projectsClient := d.projectClient.Projects()
	project, err := projectsClient.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	resourceQuotasClient := d.kubeClient.CoreV1().ResourceQuotas(name)
	resourceQuotaList, err := resourceQuotasClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	limitRangesClient := d.kubeClient.CoreV1().LimitRanges(name)
	limitRangeList, err := limitRangesClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	nodeSelector := ""
	if len(project.ObjectMeta.Annotations) > 0 {
		if ns, ok := project.ObjectMeta.Annotations[projectv1.ProjectNodeSelector]; ok {
			nodeSelector = ns
		}
	}

	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, project.ObjectMeta)
		formatString(out, "Display Name", project.Annotations[annotations.OpenShiftDisplayName])
		formatString(out, "Description", project.Annotations[annotations.OpenShiftDescription])
		formatString(out, "Status", project.Status.Phase)
		formatString(out, "Node Selector", nodeSelector)
		if len(resourceQuotaList.Items) == 0 {
			formatString(out, "Quota", "")
		} else {
			fmt.Fprintf(out, "Quota:\n")
			for i := range resourceQuotaList.Items {
				resourceQuota := &resourceQuotaList.Items[i]
				fmt.Fprintf(out, "\tName:\t%s\n", resourceQuota.Name)
				fmt.Fprintf(out, "\tResource\tUsed\tHard\n")
				fmt.Fprintf(out, "\t--------\t----\t----\n")

				resources := []corev1.ResourceName{}
				for resource := range resourceQuota.Status.Hard {
					resources = append(resources, resource)
				}
				sort.Sort(describe.SortableResourceNames(resources))

				msg := "\t%v\t%v\t%v\n"
				for i := range resources {
					resource := resources[i]
					hardQuantity := resourceQuota.Status.Hard[resource]
					usedQuantity := resourceQuota.Status.Used[resource]
					fmt.Fprintf(out, msg, resource, usedQuantity.String(), hardQuantity.String())
				}
			}
		}
		if len(limitRangeList.Items) == 0 {
			formatString(out, "Resource limits", "")
		} else {
			fmt.Fprintf(out, "Resource limits:\n")
			for i := range limitRangeList.Items {
				limitRange := &limitRangeList.Items[i]
				fmt.Fprintf(out, "\tName:\t%s\n", limitRange.Name)
				fmt.Fprintf(out, "\tType\tResource\tMin\tMax\tDefault Request\tDefault Limit\tMax Limit/Request Ratio\n")
				fmt.Fprintf(out, "\t----\t--------\t---\t---\t---------------\t-------------\t-----------------------\n")
				for i := range limitRange.Spec.Limits {
					item := limitRange.Spec.Limits[i]
					maxResources := item.Max
					minResources := item.Min
					defaultLimitResources := item.Default
					defaultRequestResources := item.DefaultRequest
					ratio := item.MaxLimitRequestRatio

					set := map[corev1.ResourceName]bool{}
					for k := range maxResources {
						set[k] = true
					}
					for k := range minResources {
						set[k] = true
					}
					for k := range defaultLimitResources {
						set[k] = true
					}
					for k := range defaultRequestResources {
						set[k] = true
					}
					for k := range ratio {
						set[k] = true
					}

					for k := range set {
						// if no value is set, we output -
						maxValue := "-"
						minValue := "-"
						defaultLimitValue := "-"
						defaultRequestValue := "-"
						ratioValue := "-"

						maxQuantity, maxQuantityFound := maxResources[k]
						if maxQuantityFound {
							maxValue = maxQuantity.String()
						}

						minQuantity, minQuantityFound := minResources[k]
						if minQuantityFound {
							minValue = minQuantity.String()
						}

						defaultLimitQuantity, defaultLimitQuantityFound := defaultLimitResources[k]
						if defaultLimitQuantityFound {
							defaultLimitValue = defaultLimitQuantity.String()
						}

						defaultRequestQuantity, defaultRequestQuantityFound := defaultRequestResources[k]
						if defaultRequestQuantityFound {
							defaultRequestValue = defaultRequestQuantity.String()
						}

						ratioQuantity, ratioQuantityFound := ratio[k]
						if ratioQuantityFound {
							ratioValue = ratioQuantity.String()
						}

						msg := "\t%v\t%v\t%v\t%v\t%v\t%v\t%v\n"
						fmt.Fprintf(out, msg, item.Type, k, minValue, maxValue, defaultRequestValue, defaultLimitValue, ratioValue)
					}
				}
			}
		}
		return nil
	})
}

// TemplateDescriber generates information about a template
type TemplateDescriber struct {
	templateClient templateclient.TemplateV1Interface
	meta.MetadataAccessor
	runtime.ObjectTyper
	describe.ObjectDescriber
}

// DescribeMessage prints the message that will be parameter substituted and displayed to the
// user when this template is processed.
func (d *TemplateDescriber) DescribeMessage(msg string, out *tabwriter.Writer) {
	if len(msg) == 0 {
		msg = "<none>"
	}
	formatString(out, "Message", msg)
}

// DescribeParameters prints out information about the parameters of a template
func (d *TemplateDescriber) DescribeParameters(params []templatev1.Parameter, out *tabwriter.Writer) {
	formatString(out, "Parameters", " ")
	indent := "    "
	for _, p := range params {
		formatString(out, indent+"Name", p.Name)
		if len(p.DisplayName) > 0 {
			formatString(out, indent+"Display Name", p.DisplayName)
		}
		if len(p.Description) > 0 {
			formatString(out, indent+"Description", p.Description)
		}
		formatString(out, indent+"Required", p.Required)
		if len(p.Generate) == 0 {
			formatString(out, indent+"Value", p.Value)
			out.Write([]byte("\n"))
			continue
		}
		if len(p.Value) > 0 {
			formatString(out, indent+"Value", p.Value)
			formatString(out, indent+"Generated (ignored)", p.Generate)
			formatString(out, indent+"From", p.From)
		} else {
			formatString(out, indent+"Generated", p.Generate)
			formatString(out, indent+"From", p.From)
		}
		out.Write([]byte("\n"))
	}
}

// describeObjects prints out information about the objects of a template
func (d *TemplateDescriber) describeObjects(objects []runtime.RawExtension, out *tabwriter.Writer) {
	formatString(out, "Objects", " ")
	indent := "    "
	for _, obj := range objects {
		converted, err := runtime.Decode(unstructured.UnstructuredJSONScheme, obj.Raw)
		if err != nil {
			klog.V(1).Infof("Unable to process %s, skipping: %v", string(obj.Raw), err)
			fmt.Fprintln(out, "<unknown>")
			continue
		}

		if d.ObjectDescriber != nil {
			output, err := d.DescribeObject(converted)
			if err != nil {
				fmt.Fprintf(out, "error: %v\n", err)
				continue
			}
			fmt.Fprint(out, output)
			fmt.Fprint(out, "\n")
			continue
		}

		name, _ := d.MetadataAccessor.Name(converted)
		groupKind := "<unknown>"
		if gvk, _, err := d.ObjectTyper.ObjectKinds(converted); err == nil {
			gk := gvk[0].GroupKind()
			groupKind = gk.String()
		} else {
			if unstructured, ok := converted.(*unstructured.Unstructured); ok {
				gvk := unstructured.GroupVersionKind()
				gk := gvk.GroupKind()
				groupKind = gk.String()
			}
		}
		fmt.Fprintf(out, fmt.Sprintf("%s%s\t%s\n", indent, groupKind, name))
	}
}

// Describe returns the description of a template
func (d *TemplateDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.templateClient.Templates(namespace)
	template, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return d.DescribeTemplate(template)
}

func (d *TemplateDescriber) DescribeTemplate(template *templatev1.Template) (string, error) {
	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, template.ObjectMeta)
		out.Write([]byte("\n"))
		out.Flush()
		d.DescribeParameters(template.Parameters, out)
		out.Write([]byte("\n"))
		formatString(out, "Object Labels", formatLabels(template.ObjectLabels))
		out.Write([]byte("\n"))
		d.DescribeMessage(template.Message, out)
		out.Write([]byte("\n"))
		out.Flush()
		d.describeObjects(template.Objects, out)
		return nil
	})
}

// TemplateInstanceDescriber generates information about a template instance
type TemplateInstanceDescriber struct {
	kubeClient     kubernetes.Interface
	templateClient templateclient.TemplateV1Interface
	describe.ObjectDescriber
}

// Describe returns the description of a template instance
func (d *TemplateInstanceDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.templateClient.TemplateInstances(namespace)
	templateInstance, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return d.DescribeTemplateInstance(templateInstance, namespace, settings)
}

// DescribeTemplateInstance prints out information about the template instance
func (d *TemplateInstanceDescriber) DescribeTemplateInstance(templateInstance *templatev1.TemplateInstance, namespace string, settings describe.DescriberSettings) (string, error) {
	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, templateInstance.ObjectMeta)
		out.Write([]byte("\n"))
		out.Flush()
		d.DescribeConditions(templateInstance.Status.Conditions, out)
		out.Write([]byte("\n"))
		out.Flush()
		d.DescribeObjects(templateInstance.Status.Objects, out)
		out.Write([]byte("\n"))
		out.Flush()
		d.DescribeParameters(templateInstance.Spec.Template, namespace, templateInstance.Spec.Secret.Name, out)
		out.Write([]byte("\n"))
		out.Flush()
		return nil
	})
}

// DescribeConditions prints out information about the conditions of a template instance
func (d *TemplateInstanceDescriber) DescribeConditions(conditions []templatev1.TemplateInstanceCondition, out *tabwriter.Writer) {
	formatString(out, "Conditions", " ")
	indent := "    "
	for _, c := range conditions {
		formatString(out, indent+"Type", c.Type)
		formatString(out, indent+"Status", c.Status)
		formatString(out, indent+"LastTransitionTime", c.LastTransitionTime)
		formatString(out, indent+"Reason", c.Reason)
		formatString(out, indent+"Message", c.Message)
		out.Write([]byte("\n"))
	}
}

// DescribeObjects prints out information about the objects that a template instance creates
func (d *TemplateInstanceDescriber) DescribeObjects(objects []templatev1.TemplateInstanceObject, out *tabwriter.Writer) {
	formatString(out, "Objects", " ")
	indent := "    "
	for _, o := range objects {
		formatString(out, indent+o.Ref.Kind, fmt.Sprintf("%s/%s", o.Ref.Namespace, o.Ref.Name))
	}
}

// DescribeParameters prints out information about the secret that holds the template instance parameters
// kinternalprinter.SecretDescriber#Describe could have been used here, but the formatting
// is off when it prints the information and seems to not be easily fixable
func (d *TemplateInstanceDescriber) DescribeParameters(template templatev1.Template, namespace, name string, out *tabwriter.Writer) {
	secret, err := d.kubeClient.CoreV1().Secrets(namespace).Get(context.TODO(), name, metav1.GetOptions{})

	formatString(out, "Parameters", " ")

	if kerrs.IsForbidden(err) || kerrs.IsUnauthorized(err) {
		fmt.Fprintf(out, "Unable to access parameters, insufficient permissions.")
		return
	} else if kerrs.IsNotFound(err) {
		fmt.Fprintf(out, "Unable to access parameters, secret not found: %s", secret.Name)
		return
	} else if err != nil {
		fmt.Fprintf(out, "Unknown error occurred, please rerun with loglevel > 4 for more information")
		klog.V(4).Infof("%v", err)
		return
	}

	indent := "    "
	if len(template.Parameters) == 0 {
		fmt.Fprintf(out, indent+"No parameters found.")
	} else {
		for _, p := range template.Parameters {
			if val, ok := secret.Data[p.Name]; ok {
				formatString(out, indent+p.Name, fmt.Sprintf("%d bytes", len(val)))
			}
		}
	}
}

// IdentityDescriber generates information about a user
type IdentityDescriber struct {
	c userclient.UserV1Interface
}

// Describe returns the description of an identity
func (d *IdentityDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	userClient := d.c.Users()
	identityClient := d.c.Identities()

	identity, err := identityClient.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, identity.ObjectMeta)

		if len(identity.User.Name) == 0 {
			formatString(out, "User Name", identity.User.Name)
			formatString(out, "User UID", identity.User.UID)
		} else {
			resolvedUser, err := userClient.Get(context.TODO(), identity.User.Name, metav1.GetOptions{})

			nameValue := identity.User.Name
			uidValue := string(identity.User.UID)

			if kerrs.IsNotFound(err) {
				nameValue += fmt.Sprintf(" (Error: User does not exist)")
			} else if err != nil {
				nameValue += fmt.Sprintf(" (Error: User lookup failed)")
			} else {
				if !sets.NewString(resolvedUser.Identities...).Has(name) {
					nameValue += fmt.Sprintf(" (Error: User identities do not include %s)", name)
				}
				if resolvedUser.UID != identity.User.UID {
					uidValue += fmt.Sprintf(" (Error: Actual user UID is %s)", string(resolvedUser.UID))
				}
			}

			formatString(out, "User Name", nameValue)
			formatString(out, "User UID", uidValue)
		}
		return nil
	})

}

// UserIdentityMappingDescriber generates information about a user
type UserIdentityMappingDescriber struct {
	c userclient.UserV1Interface
}

// Describe returns the description of a userIdentity
func (d *UserIdentityMappingDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.c.UserIdentityMappings()

	mapping, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, mapping.ObjectMeta)
		formatString(out, "Identity", mapping.Identity.Name)
		formatString(out, "User Name", mapping.User.Name)
		formatString(out, "User UID", mapping.User.UID)
		return nil
	})
}

// UserDescriber generates information about a user
type UserDescriber struct {
	c userclient.UserV1Interface
}

// Describe returns the description of a user
func (d *UserDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	userClient := d.c.Users()
	identityClient := d.c.Identities()

	user, err := userClient.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, user.ObjectMeta)
		if len(user.FullName) > 0 {
			formatString(out, "Full Name", user.FullName)
		}

		if len(user.Identities) == 0 {
			formatString(out, "Identities", "<none>")
		} else {
			for i, identity := range user.Identities {
				resolvedIdentity, err := identityClient.Get(context.TODO(), identity, metav1.GetOptions{})

				value := identity
				if kerrs.IsNotFound(err) {
					value += fmt.Sprintf(" (Error: Identity does not exist)")
				} else if err != nil {
					value += fmt.Sprintf(" (Error: Identity lookup failed)")
				} else if resolvedIdentity.User.Name != name {
					value += fmt.Sprintf(" (Error: Identity maps to user name '%s')", resolvedIdentity.User.Name)
				} else if resolvedIdentity.User.UID != user.UID {
					value += fmt.Sprintf(" (Error: Identity maps to user UID '%s')", resolvedIdentity.User.UID)
				}

				if i == 0 {
					formatString(out, "Identities", value)
				} else {
					fmt.Fprintf(out, "           \t%s\n", value)
				}
			}
		}
		return nil
	})
}

// GroupDescriber generates information about a group
type GroupDescriber struct {
	c userclient.UserV1Interface
}

// Describe returns the description of a group
func (d *GroupDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	group, err := d.c.Groups().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, group.ObjectMeta)

		if len(group.Users) == 0 {
			formatString(out, "Users", "<none>")
		} else {
			for i, user := range group.Users {
				if i == 0 {
					formatString(out, "Users", user)
				} else {
					fmt.Fprintf(out, "           \t%s\n", user)
				}
			}
		}
		return nil
	})
}

const PolicyRuleHeadings = "Verbs\tNon-Resource URLs\tResource Names\tAPI Groups\tResources"

func DescribePolicyRule(out *tabwriter.Writer, rule authorizationv1.PolicyRule, indent string) {
	if len(rule.AttributeRestrictions.Raw) != 0 {
		// We are not supporting attribute restrictions going forward
		return
	}

	fmt.Fprintf(out, indent+"%v\t%v\t%v\t%v\t%v\n",
		rule.Verbs,
		rule.NonResourceURLsSlice,
		rule.ResourceNames,
		rule.APIGroups,
		rule.Resources,
	)
}

// RoleDescriber generates information about a Project
type RoleDescriber struct {
	c oauthorizationclient.AuthorizationV1Interface
}

// Describe returns the description of a role
func (d *RoleDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.c.Roles(namespace)
	role, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	return DescribeRole(role)
}

func DescribeRole(role *authorizationv1.Role) (string, error) {
	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, role.ObjectMeta)

		fmt.Fprint(out, PolicyRuleHeadings+"\n")
		for _, rule := range role.Rules {
			DescribePolicyRule(out, rule, "")

		}

		return nil
	})
}

// RoleBindingDescriber generates information about a Project
type RoleBindingDescriber struct {
	c oauthorizationclient.AuthorizationV1Interface
}

// Describe returns the description of a roleBinding
func (d *RoleBindingDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.c.RoleBindings(namespace)
	roleBinding, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	var role *authorizationv1.Role
	if len(roleBinding.RoleRef.Namespace) == 0 {
		var clusterRole *authorizationv1.ClusterRole
		clusterRole, err = d.c.ClusterRoles().Get(context.TODO(), roleBinding.RoleRef.Name, metav1.GetOptions{})
		role = authorizationhelpers.ToRole(clusterRole)
	} else {
		role, err = d.c.Roles(roleBinding.RoleRef.Namespace).Get(context.TODO(), roleBinding.RoleRef.Name, metav1.GetOptions{})
	}

	return DescribeRoleBinding(roleBinding, role, err)
}

// DescribeRoleBinding prints out information about a role binding and its associated role
func DescribeRoleBinding(roleBinding *authorizationv1.RoleBinding, role *authorizationv1.Role, err error) (string, error) {
	users, groups, sas, others := authorizationhelpers.SubjectsStrings(roleBinding.Namespace, roleBinding.Subjects)

	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, roleBinding.ObjectMeta)

		formatString(out, "Role", roleBinding.RoleRef.Namespace+"/"+roleBinding.RoleRef.Name)
		formatString(out, "Users", strings.Join(users, ", "))
		formatString(out, "Groups", strings.Join(groups, ", "))
		formatString(out, "ServiceAccounts", strings.Join(sas, ", "))
		formatString(out, "Subjects", strings.Join(others, ", "))

		switch {
		case err != nil:
			formatString(out, "Policy Rules", fmt.Sprintf("error: %v", err))

		case role != nil:
			fmt.Fprint(out, PolicyRuleHeadings+"\n")
			for _, rule := range role.Rules {
				DescribePolicyRule(out, rule, "")
			}

		default:
			formatString(out, "Policy Rules", "<none>")
		}

		return nil
	})
}

type ClusterRoleDescriber struct {
	c oauthorizationclient.AuthorizationV1Interface
}

// Describe returns the description of a role
func (d *ClusterRoleDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.c.ClusterRoles()
	role, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	return DescribeRole(authorizationhelpers.ToRole(role))
}

// ClusterRoleBindingDescriber generates information about a Project
type ClusterRoleBindingDescriber struct {
	c oauthorizationclient.AuthorizationV1Interface
}

// Describe returns the description of a roleBinding
func (d *ClusterRoleBindingDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.c.ClusterRoleBindings()
	roleBinding, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	role, err := d.c.ClusterRoles().Get(context.TODO(), roleBinding.RoleRef.Name, metav1.GetOptions{})
	return DescribeRoleBinding(authorizationhelpers.ToRoleBinding(roleBinding), authorizationhelpers.ToRole(role), err)
}

func describeBuildTriggerCauses(causes []buildv1.BuildTriggerCause, out *tabwriter.Writer) {
	if causes == nil {
		formatString(out, "\nBuild trigger cause", "<unknown>")
	}

	for _, cause := range causes {
		formatString(out, "\nBuild trigger cause", cause.Message)

		switch {
		case cause.GitHubWebHook != nil:
			squashGitInfo(cause.GitHubWebHook.Revision, out)
			formatString(out, "Secret", cause.GitHubWebHook.Secret)

		case cause.GitLabWebHook != nil:
			squashGitInfo(cause.GitLabWebHook.Revision, out)
			formatString(out, "Secret", cause.GitLabWebHook.Secret)

		case cause.BitbucketWebHook != nil:
			squashGitInfo(cause.BitbucketWebHook.Revision, out)
			formatString(out, "Secret", cause.BitbucketWebHook.Secret)

		case cause.GenericWebHook != nil:
			squashGitInfo(cause.GenericWebHook.Revision, out)
			formatString(out, "Secret", cause.GenericWebHook.Secret)

		case cause.ImageChangeBuild != nil:
			formatString(out, "Image ID", cause.ImageChangeBuild.ImageID)
			formatString(out, "Image Name/Kind", fmt.Sprintf("%s / %s", cause.ImageChangeBuild.FromRef.Name, cause.ImageChangeBuild.FromRef.Kind))
		}
	}
	fmt.Fprintf(out, "\n")
}

func squashGitInfo(sourceRevision *buildv1.SourceRevision, out *tabwriter.Writer) {
	if sourceRevision != nil && sourceRevision.Git != nil {
		rev := sourceRevision.Git
		var commit string
		if len(rev.Commit) > 7 {
			commit = rev.Commit[:7]
		} else {
			commit = rev.Commit
		}
		formatString(out, "Commit", fmt.Sprintf("%s (%s)", commit, rev.Message))
		hasAuthor := len(rev.Author.Name) != 0
		hasCommitter := len(rev.Committer.Name) != 0
		if hasAuthor && hasCommitter {
			if rev.Author.Name == rev.Committer.Name {
				formatString(out, "Author/Committer", rev.Author.Name)
			} else {
				formatString(out, "Author/Committer", fmt.Sprintf("%s / %s", rev.Author.Name, rev.Committer.Name))
			}
		} else if hasAuthor {
			formatString(out, "Author", rev.Author.Name)
		} else if hasCommitter {
			formatString(out, "Committer", rev.Committer.Name)
		}
	}
}

type ClusterQuotaDescriber struct {
	c quotaclient.QuotaV1Interface
}

func (d *ClusterQuotaDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	quota, err := d.c.ClusterResourceQuotas().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return DescribeClusterQuota(quota)
}

func DescribeClusterQuota(quota *quotav1.ClusterResourceQuota) (string, error) {
	labelSelector, err := metav1.LabelSelectorAsSelector(quota.Spec.Selector.LabelSelector)
	if err != nil {
		return "", err
	}

	nsSelector := make([]interface{}, 0, len(quota.Status.Namespaces))
	for _, nsQuota := range quota.Status.Namespaces {
		nsSelector = append(nsSelector, nsQuota.Namespace)
	}

	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, quota.ObjectMeta)
		fmt.Fprintf(out, "Namespace Selector: %q\n", nsSelector)
		fmt.Fprintf(out, "Label Selector: %s\n", labelSelector)
		fmt.Fprintf(out, "AnnotationSelector: %s\n", quota.Spec.Selector.AnnotationSelector)
		if len(quota.Spec.Quota.Scopes) > 0 {
			scopes := []string{}
			for _, scope := range quota.Spec.Quota.Scopes {
				scopes = append(scopes, string(scope))
			}
			sort.Strings(scopes)
			fmt.Fprintf(out, "Scopes:\t%s\n", strings.Join(scopes, ", "))
		}
		fmt.Fprintf(out, "Resource\tUsed\tHard\n")
		fmt.Fprintf(out, "--------\t----\t----\n")

		resources := []corev1.ResourceName{}
		for resource := range quota.Status.Total.Hard {
			resources = append(resources, resource)
		}
		sort.Sort(describe.SortableResourceNames(resources))

		msg := "%v\t%v\t%v\n"
		for i := range resources {
			resourceName := resources[i]
			hardQuantity := quota.Status.Total.Hard[resourceName]
			usedQuantity := quota.Status.Total.Used[resourceName]
			if hardQuantity.Format != usedQuantity.Format {
				usedQuantity = *resource.NewQuantity(usedQuantity.Value(), hardQuantity.Format)
			}
			fmt.Fprintf(out, msg, resourceName, usedQuantity.String(), hardQuantity.String())
		}
		return nil
	})
}

type AppliedClusterQuotaDescriber struct {
	c quotaclient.QuotaV1Interface
}

func (d *AppliedClusterQuotaDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	quota, err := d.c.AppliedClusterResourceQuotas(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return DescribeClusterQuota(quotahelpers.ConvertV1AppliedClusterResourceQuotaToV1ClusterResourceQuota(quota))
}

type ClusterNetworkDescriber struct {
	c onetworktypedclient.NetworkV1Interface
}

// Describe returns the description of a ClusterNetwork
func (d *ClusterNetworkDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	cn, err := d.c.ClusterNetworks().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, cn.ObjectMeta)
		formatString(out, "Service Network", cn.ServiceNetwork)
		formatString(out, "Plugin Name", cn.PluginName)
		fmt.Fprintf(out, "ClusterNetworks:\n")
		fmt.Fprintf(out, "CIDR\tHost Subnet Length\n")
		fmt.Fprintf(out, "----\t------------------\n")
		for _, clusterNetwork := range cn.ClusterNetworks {
			fmt.Fprintf(out, "%s\t%d\n", clusterNetwork.CIDR, clusterNetwork.HostSubnetLength)
		}
		return nil
	})
}

type HostSubnetDescriber struct {
	c onetworktypedclient.NetworkV1Interface
}

// Describe returns the description of a HostSubnet
func (d *HostSubnetDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	hs, err := d.c.HostSubnets().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, hs.ObjectMeta)
		formatString(out, "Node", hs.Host)
		formatString(out, "Node IP", hs.HostIP)
		formatString(out, "Pod Subnet", hs.Subnet)
		formatString(out, "Egress CIDRs", hostCIDRsJoin(hs.EgressCIDRs, ", "))
		formatString(out, "Egress IPs", hostIPsJoin(hs.EgressIPs, ", "))
		return nil
	})
}

func hostCIDRsJoin(cidrs []networkv1.HostSubnetEgressCIDR, sep string) string {
	var b strings.Builder
	for i, c := range cidrs {
		b.WriteString(string(c))
		if i < len(cidrs)-1 {
			b.WriteString(sep)
		}
	}
	return b.String()
}

func hostIPsJoin(ips []networkv1.HostSubnetEgressIP, sep string) string {
	var b strings.Builder
	for i, c := range ips {
		b.WriteString(string(c))
		if i < len(ips)-1 {
			b.WriteString(sep)
		}
	}
	return b.String()
}

type NetNamespaceDescriber struct {
	c onetworktypedclient.NetworkV1Interface
}

// Describe returns the description of a NetNamespace
func (d *NetNamespaceDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	netns, err := d.c.NetNamespaces().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, netns.ObjectMeta)
		formatString(out, "Name", netns.NetName)
		formatString(out, "ID", netns.NetID)
		formatString(out, "Egress IPs", netIPsJoin(netns.EgressIPs, ", "))
		return nil
	})
}

func netIPsJoin(ips []networkv1.NetNamespaceEgressIP, sep string) string {
	var b strings.Builder
	for i, c := range ips {
		b.WriteString(string(c))
		if i < len(ips)-1 {
			b.WriteString(sep)
		}
	}
	return b.String()
}

type EgressNetworkPolicyDescriber struct {
	c onetworktypedclient.NetworkV1Interface
}

// Describe returns the description of an EgressNetworkPolicy
func (d *EgressNetworkPolicyDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	c := d.c.EgressNetworkPolicies(namespace)
	policy, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, policy.ObjectMeta)
		for _, rule := range policy.Spec.Egress {
			if len(rule.To.CIDRSelector) > 0 {
				fmt.Fprintf(out, "Rule:\t%s to %s\n", rule.Type, rule.To.CIDRSelector)
			} else {
				fmt.Fprintf(out, "Rule:\t%s to %s\n", rule.Type, rule.To.DNSName)
			}
		}
		return nil
	})
}

type RoleBindingRestrictionDescriber struct {
	c oauthorizationclient.AuthorizationV1Interface
}

// Describe returns the description of a RoleBindingRestriction.
func (d *RoleBindingRestrictionDescriber) Describe(namespace, name string, settings describe.DescriberSettings) (string, error) {
	rbr, err := d.c.RoleBindingRestrictions(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return tabbedString(func(out *tabwriter.Writer) error {
		formatMeta(out, rbr.ObjectMeta)

		subjectType := roleBindingRestrictionType(rbr)
		if subjectType == "" {
			subjectType = "<none>"
		}
		formatString(out, "Subject type", subjectType)

		var labelSelectors []metav1.LabelSelector

		switch {
		case rbr.Spec.UserRestriction != nil:
			formatString(out, "Users",
				strings.Join(rbr.Spec.UserRestriction.Users, ", "))
			formatString(out, "Users in groups",
				strings.Join(rbr.Spec.UserRestriction.Groups, ", "))
			labelSelectors = rbr.Spec.UserRestriction.Selectors
		case rbr.Spec.GroupRestriction != nil:
			formatString(out, "Groups",
				strings.Join(rbr.Spec.GroupRestriction.Groups, ", "))
			labelSelectors = rbr.Spec.GroupRestriction.Selectors
		case rbr.Spec.ServiceAccountRestriction != nil:
			serviceaccounts := []string{}
			for _, sa := range rbr.Spec.ServiceAccountRestriction.ServiceAccounts {
				serviceaccounts = append(serviceaccounts, sa.Name)
			}
			formatString(out, "ServiceAccounts", strings.Join(serviceaccounts, ", "))
			formatString(out, "Namespaces",
				strings.Join(rbr.Spec.ServiceAccountRestriction.Namespaces, ", "))
		}

		if rbr.Spec.UserRestriction != nil || rbr.Spec.GroupRestriction != nil {
			if len(labelSelectors) == 0 {
				formatString(out, "Label selectors", "")
			} else {
				fmt.Fprintf(out, "Label selectors:\n")
				for _, labelSelector := range labelSelectors {
					selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
					if err != nil {
						return err
					}
					fmt.Fprintf(out, "\t%s\n", selector)
				}
			}
		}

		return nil
	})
}

// SecurityContextConstraintsDescriber generates information about an SCC
type SecurityContextConstraintsDescriber struct {
	c securityclient.SecurityContextConstraintsGetter
}

func (d *SecurityContextConstraintsDescriber) Describe(namespace, name string, s describe.DescriberSettings) (string, error) {
	scc, err := d.c.SecurityContextConstraints().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return describeSecurityContextConstraints(scc)
}

func describeSecurityContextConstraints(scc *securityv1.SecurityContextConstraints) (string, error) {
	return tabbedString(func(out *tabwriter.Writer) error {
		fmt.Fprintf(out, "Name:\t%s\n", scc.Name)

		priority := ""
		if scc.Priority != nil {
			priority = fmt.Sprintf("%d", *scc.Priority)
		}
		fmt.Fprintf(out, "Priority:\t%s\n", stringOrNone(priority))

		fmt.Fprintf(out, "Access:\t\n")
		fmt.Fprintf(out, "  Users:\t%s\n", stringOrNone(strings.Join(scc.Users, ",")))
		fmt.Fprintf(out, "  Groups:\t%s\n", stringOrNone(strings.Join(scc.Groups, ",")))

		fmt.Fprintf(out, "Settings:\t\n")
		fmt.Fprintf(out, "  Allow Privileged:\t%t\n", scc.AllowPrivilegedContainer)

		allowPrivilegeEscalation := ""
		if scc.AllowPrivilegeEscalation != nil {
			allowPrivilegeEscalation = strconv.FormatBool(*scc.AllowPrivilegeEscalation)
		}
		fmt.Fprintf(out, "  Allow Privilege Escalation:\t%s\n", stringOrNone(allowPrivilegeEscalation))

		fmt.Fprintf(out, "  Default Add Capabilities:\t%s\n", capsToString(scc.DefaultAddCapabilities))
		fmt.Fprintf(out, "  Required Drop Capabilities:\t%s\n", capsToString(scc.RequiredDropCapabilities))
		fmt.Fprintf(out, "  Allowed Capabilities:\t%s\n", capsToString(scc.AllowedCapabilities))
		fmt.Fprintf(out, "  Allowed Seccomp Profiles:\t%s\n", stringOrNone(strings.Join(scc.SeccompProfiles, ",")))
		fmt.Fprintf(out, "  Allowed Volume Types:\t%s\n", fsTypeToString(scc.Volumes))
		fmt.Fprintf(out, "  Allowed Flexvolumes:\t%s\n", flexVolumesToString(scc.AllowedFlexVolumes))
		fmt.Fprintf(out, "  Allowed Unsafe Sysctls:\t%s\n", sysctlsToString(scc.AllowedUnsafeSysctls))
		fmt.Fprintf(out, "  Forbidden Sysctls:\t%s\n", sysctlsToString(scc.ForbiddenSysctls))
		fmt.Fprintf(out, "  Allow Host Network:\t%t\n", scc.AllowHostNetwork)
		fmt.Fprintf(out, "  Allow Host Ports:\t%t\n", scc.AllowHostPorts)
		fmt.Fprintf(out, "  Allow Host PID:\t%t\n", scc.AllowHostPID)
		fmt.Fprintf(out, "  Allow Host IPC:\t%t\n", scc.AllowHostIPC)
		fmt.Fprintf(out, "  Read Only Root Filesystem:\t%t\n", scc.ReadOnlyRootFilesystem)

		fmt.Fprintf(out, "  Run As User Strategy: %s\t\n", string(scc.RunAsUser.Type))
		uid := ""
		if scc.RunAsUser.UID != nil {
			uid = strconv.FormatInt(*scc.RunAsUser.UID, 10)
		}
		fmt.Fprintf(out, "    UID:\t%s\n", stringOrNone(uid))

		uidRangeMin := ""
		if scc.RunAsUser.UIDRangeMin != nil {
			uidRangeMin = strconv.FormatInt(*scc.RunAsUser.UIDRangeMin, 10)
		}
		fmt.Fprintf(out, "    UID Range Min:\t%s\n", stringOrNone(uidRangeMin))

		uidRangeMax := ""
		if scc.RunAsUser.UIDRangeMax != nil {
			uidRangeMax = strconv.FormatInt(*scc.RunAsUser.UIDRangeMax, 10)
		}
		fmt.Fprintf(out, "    UID Range Max:\t%s\n", stringOrNone(uidRangeMax))

		fmt.Fprintf(out, "  SELinux Context Strategy: %s\t\n", string(scc.SELinuxContext.Type))
		var user, role, seLinuxType, level string
		if scc.SELinuxContext.SELinuxOptions != nil {
			user = scc.SELinuxContext.SELinuxOptions.User
			role = scc.SELinuxContext.SELinuxOptions.Role
			seLinuxType = scc.SELinuxContext.SELinuxOptions.Type
			level = scc.SELinuxContext.SELinuxOptions.Level
		}
		fmt.Fprintf(out, "    User:\t%s\n", stringOrNone(user))
		fmt.Fprintf(out, "    Role:\t%s\n", stringOrNone(role))
		fmt.Fprintf(out, "    Type:\t%s\n", stringOrNone(seLinuxType))
		fmt.Fprintf(out, "    Level:\t%s\n", stringOrNone(level))

		fmt.Fprintf(out, "  FSGroup Strategy: %s\t\n", string(scc.FSGroup.Type))
		fmt.Fprintf(out, "    Ranges:\t%s\n", idRangeToString(scc.FSGroup.Ranges))

		fmt.Fprintf(out, "  Supplemental Groups Strategy: %s\t\n", string(scc.SupplementalGroups.Type))
		fmt.Fprintf(out, "    Ranges:\t%s\n", idRangeToString(scc.SupplementalGroups.Ranges))

		return nil
	})
}

type InsightsDataGatherDescriber struct {
	c configclientv1alpha1.InsightsDataGathersGetter
}

func (d *InsightsDataGatherDescriber) Describe(namespace, name string, s describe.DescriberSettings) (string, error) {
	idg, err := d.c.InsightsDataGathers().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return describeInsightsDataGathers(idg)
}

func describeInsightsDataGathers(idg *configv1alpha1.InsightsDataGather) (string, error) {
	return tabbedString(func(out *tabwriter.Writer) error {
		fmt.Fprintf(out, "Name:\t%s\n", idg.Name)
		fmt.Fprintf(out, "GatherConfig:\t\n")
		fmt.Fprintf(out, "  DataPolicy:\t%s\n", stringOrNone(string(idg.Spec.GatherConfig.DataPolicy)))
		fmt.Fprintf(out, "  DisabledGatherers:\t%s\n", stringOrNone(strings.Join(idg.Spec.GatherConfig.DisabledGatherers, ",")))

		return nil
	})
}

func stringOrNone(s string) string {
	return stringOrDefaultValue(s, "<none>")
}

func stringOrDefaultValue(s, defaultValue string) string {
	if len(s) > 0 {
		return s
	}
	return defaultValue
}

func fsTypeToString(volumes []securityv1.FSType) string {
	strVolumes := []string{}
	for _, v := range volumes {
		strVolumes = append(strVolumes, string(v))
	}
	return stringOrNone(strings.Join(strVolumes, ","))
}

func flexVolumesToString(flexVolumes []securityv1.AllowedFlexVolume) string {
	volumes := []string{}
	for _, flexVolume := range flexVolumes {
		volumes = append(volumes, "driver="+flexVolume.Driver)
	}
	return stringOrDefaultValue(strings.Join(volumes, ","), "<all>")
}

func sysctlsToString(sysctls []string) string {
	return stringOrNone(strings.Join(sysctls, ","))
}

func idRangeToString(ranges []securityv1.IDRange) string {
	formattedString := ""
	if ranges != nil {
		strRanges := []string{}
		for _, r := range ranges {
			strRanges = append(strRanges, fmt.Sprintf("%d-%d", r.Min, r.Max))
		}
		formattedString = strings.Join(strRanges, ",")
	}
	return stringOrNone(formattedString)
}

func capsToString(caps []corev1.Capability) string {
	formattedString := ""
	if caps != nil {
		strCaps := []string{}
		for _, c := range caps {
			strCaps = append(strCaps, string(c))
		}
		formattedString = strings.Join(strCaps, ",")
	}
	return stringOrNone(formattedString)
}
