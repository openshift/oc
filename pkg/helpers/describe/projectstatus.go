package describe

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/openshift/api/annotations"
	appsv1 "github.com/openshift/api/apps/v1"
	buildv1 "github.com/openshift/api/build/v1"
	imagev1 "github.com/openshift/api/image/v1"
	projectv1 "github.com/openshift/api/project/v1"
	routev1 "github.com/openshift/api/route/v1"
	appsv1client "github.com/openshift/client-go/apps/clientset/versioned/typed/apps/v1"
	buildv1client "github.com/openshift/client-go/build/clientset/versioned/typed/build/v1"
	imagev1client "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	projectv1client "github.com/openshift/client-go/project/clientset/versioned/typed/project/v1"
	routev1client "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	"github.com/openshift/library-go/pkg/apps/appsutil"
	deployutil "github.com/openshift/oc/pkg/helpers/deployment"
	loginerrors "github.com/openshift/oc/pkg/helpers/errors"
	appsedges "github.com/openshift/oc/pkg/helpers/graph/appsgraph"
	appsanalysis "github.com/openshift/oc/pkg/helpers/graph/appsgraph/analysis"
	appsgraph "github.com/openshift/oc/pkg/helpers/graph/appsgraph/nodes"
	buildedges "github.com/openshift/oc/pkg/helpers/graph/buildgraph"
	buildanalysis "github.com/openshift/oc/pkg/helpers/graph/buildgraph/analysis"
	buildgraph "github.com/openshift/oc/pkg/helpers/graph/buildgraph/nodes"
	osgraph "github.com/openshift/oc/pkg/helpers/graph/genericgraph"
	"github.com/openshift/oc/pkg/helpers/graph/genericgraph/graphview"
	imageedges "github.com/openshift/oc/pkg/helpers/graph/imagegraph"
	imagegraph "github.com/openshift/oc/pkg/helpers/graph/imagegraph/nodes"
	kubeedges "github.com/openshift/oc/pkg/helpers/graph/kubegraph"
	kubeanalysis "github.com/openshift/oc/pkg/helpers/graph/kubegraph/analysis"
	kubegraph "github.com/openshift/oc/pkg/helpers/graph/kubegraph/nodes"
	routeedges "github.com/openshift/oc/pkg/helpers/graph/routegraph"
	routeanalysis "github.com/openshift/oc/pkg/helpers/graph/routegraph/analysis"
	routegraph "github.com/openshift/oc/pkg/helpers/graph/routegraph/nodes"
	"github.com/openshift/oc/pkg/helpers/parallel"
	routedisplayhelpers "github.com/openshift/oc/pkg/helpers/route"
	kappsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	kappsv1client "k8s.io/client-go/kubernetes/typed/apps/v1"
	autoscalingv1client "k8s.io/client-go/kubernetes/typed/autoscaling/v1"
	batchv1client "k8s.io/client-go/kubernetes/typed/batch/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

const ForbiddenListWarning = "Forbidden"

// ProjectStatusDescriber generates extended information about a Project
type ProjectStatusDescriber struct {
	KubeClient kubernetes.Interface
	RESTMapper meta.RESTMapper

	// OpenShift clients
	ProjectClient projectv1client.ProjectV1Interface
	BuildClient   buildv1client.BuildV1Interface
	ImageClient   imagev1client.ImageV1Interface
	AppsClient    appsv1client.AppsV1Interface
	RouteClient   routev1client.RouteV1Interface
	Server        string
	Suggest       bool

	RequestedNamespace string
	CurrentNamespace   string

	CanRequestProjects bool

	LogsCommandName             string
	SecurityPolicyCommandFormat string
	SetProbeCommandName         string
}

func (d *ProjectStatusDescriber) MakeGraph(namespace string) (osgraph.Graph, sets.String, error) {
	g := osgraph.New()

	loaders := []GraphLoader{
		&serviceLoader{namespace: namespace, lister: d.KubeClient.CoreV1()},
		&serviceAccountLoader{namespace: namespace, lister: d.KubeClient.CoreV1()},
		&secretLoader{namespace: namespace, lister: d.KubeClient.CoreV1()},
		&pvcLoader{namespace: namespace, lister: d.KubeClient.CoreV1()},
		&rcLoader{namespace: namespace, lister: d.KubeClient.CoreV1()},
		&podLoader{namespace: namespace, lister: d.KubeClient.CoreV1()},
		&jobLoader{namespace: namespace, lister: d.KubeClient.BatchV1()},
		&statefulSetLoader{namespace: namespace, lister: d.KubeClient.AppsV1()},
		&horizontalPodAutoscalerLoader{namespace: namespace, lister: d.KubeClient.AutoscalingV1()},
		&deploymentLoader{namespace: namespace, lister: d.KubeClient.AppsV1()},
		&replicasetLoader{namespace: namespace, lister: d.KubeClient.AppsV1()},
		&daemonsetLoader{namespace: namespace, lister: d.KubeClient.AppsV1()},
		// TODO check swagger for feature enablement and selectively add bcLoader and buildLoader
		// then remove errors.TolerateNotFoundError method.
		&bcLoader{namespace: namespace, lister: d.BuildClient},
		&buildLoader{namespace: namespace, lister: d.BuildClient},
		&isLoader{namespace: namespace, lister: d.ImageClient},
		&dcLoader{namespace: namespace, lister: d.AppsClient},
		&routeLoader{namespace: namespace, lister: d.RouteClient},
	}
	loadingFuncs := []func() error{}
	for _, loader := range loaders {
		loadingFuncs = append(loadingFuncs, loader.Load)
	}

	forbiddenResources := sets.String{}
	if errs := parallel.Run(loadingFuncs...); len(errs) > 0 {
		actualErrors := []error{}
		for _, err := range errs {
			if kapierrors.IsForbidden(err) {
				forbiddenErr := err.(*kapierrors.StatusError)
				if (forbiddenErr.Status().Details != nil) && (len(forbiddenErr.Status().Details.Kind) > 0) {
					forbiddenResources.Insert(forbiddenErr.Status().Details.Kind)
				}
				continue
			}
			if kapierrors.IsNotFound(err) {
				notfoundErr := err.(*kapierrors.StatusError)
				if (notfoundErr.Status().Details != nil) && (len(notfoundErr.Status().Details.Kind) > 0) {
					forbiddenResources.Insert(notfoundErr.Status().Details.Kind)
				}
				continue
			}
			actualErrors = append(actualErrors, err)
		}

		if len(actualErrors) > 0 {
			return g, forbiddenResources, utilerrors.NewAggregate(actualErrors)
		}
	}

	for _, loader := range loaders {
		loader.AddToGraph(g)
	}

	kubeedges.AddAllExposedPodTemplateSpecEdges(g)
	kubeedges.AddAllExposedPodEdges(g)
	kubeedges.AddAllManagedByControllerPodEdges(g)
	kubeedges.AddAllRequestedServiceAccountEdges(g)
	kubeedges.AddAllMountableSecretEdges(g)
	kubeedges.AddAllMountedSecretEdges(g)
	kubeedges.AddHPAScaleRefEdges(g, d.RESTMapper)
	buildedges.AddAllInputOutputEdges(g)
	buildedges.AddAllBuildEdges(g)
	appsedges.AddAllTriggerDeploymentConfigsEdges(g)
	kubeedges.AddAllTriggerDeploymentsEdges(g)
	kubeedges.AddAllTriggerStatefulSetsEdges(g)
	kubeedges.AddAllTriggerJobsEdges(g)
	appsedges.AddAllDeploymentConfigsDeploymentEdges(g)
	kubeedges.AddAllDeploymentEdges(g)
	appsedges.AddAllVolumeClaimEdges(g)
	imageedges.AddAllImageStreamRefEdges(g)
	imageedges.AddAllImageStreamImageRefEdges(g)
	routeedges.AddAllRouteEdges(g)

	return g, forbiddenResources, nil
}

// createSelector receives a map of strings and
// converts it into a labels.Selector
func createSelector(values map[string]string) labels.Selector {
	selector := labels.NewSelector()
	for k, v := range values {
		req, err := labels.NewRequirement(k, "=", []string{v})
		if err != nil {
			continue
		}

		selector = selector.Add(*req)
	}

	return selector
}

// Describe returns the description of a project
func (d *ProjectStatusDescriber) Describe(namespace, name string) (string, error) {
	var f formatter = namespacedFormatter{}

	g, forbiddenResources, err := d.MakeGraph(namespace)
	if err != nil {
		return "", err
	}

	allNamespaces := namespace == metav1.NamespaceAll
	var project *projectv1.Project
	if !allNamespaces {
		p, err := d.ProjectClient.Projects().Get(context.TODO(), namespace, metav1.GetOptions{})
		if err != nil {
			// a forbidden error here (without a --namespace value) means that
			// the user has not created any projects, and is therefore using a
			// default namespace that they cannot list projects from.
			if kapierrors.IsForbidden(err) && len(d.RequestedNamespace) == 0 && len(d.CurrentNamespace) == 0 {
				return loginerrors.NoProjectsExistMessage(d.CanRequestProjects), nil
			}
			if !kapierrors.IsNotFound(err) {
				return "", err
			}
			p = &projectv1.Project{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		}
		project = p
		f = namespacedFormatter{currentNamespace: namespace}
	}

	coveredNodes := graphview.IntSet{}

	allServices, coveredByServices := graphview.AllServiceGroups(g, coveredNodes)
	coveredNodes.Insert(coveredByServices.List()...)

	// services grouped by selector
	servicesBySelector := map[string][]graphview.ServiceGroup{}
	services := []graphview.ServiceGroup{}

	// group services with identical selectors
	for _, svc := range allServices {
		selector := createSelector(svc.Service.Spec.Selector)
		if _, seen := servicesBySelector[selector.String()]; seen {
			servicesBySelector[selector.String()] = append(servicesBySelector[selector.String()], svc)
			continue
		}

		services = append(services, svc)
		servicesBySelector[selector.String()] = []graphview.ServiceGroup{}
	}

	standaloneDCs, coveredByDCs := graphview.AllDeploymentConfigPipelines(g, coveredNodes)
	coveredNodes.Insert(coveredByDCs.List()...)

	standaloneDeployments, coveredByDeployments := graphview.AllDeployments(g, coveredNodes)
	coveredNodes.Insert(coveredByDeployments.List()...)

	standaloneStatefulSets, coveredByStatefulSets := graphview.AllStatefulSets(g, coveredNodes)
	coveredNodes.Insert(coveredByStatefulSets.List()...)

	standaloneRCs, coveredByRCs := graphview.AllReplicationControllers(g, coveredNodes)
	coveredNodes.Insert(coveredByRCs.List()...)

	standaloneRSs, coveredByRSs := graphview.AllReplicaSets(g, coveredNodes)
	coveredNodes.Insert(coveredByRSs.List()...)

	standaloneImages, coveredByImages := graphview.AllImagePipelinesFromBuildConfig(g, coveredNodes)
	coveredNodes.Insert(coveredByImages.List()...)

	standaloneDaemonSets, coveredByDaemonSets := graphview.AllDaemonSets(g, coveredNodes)
	coveredNodes.Insert(coveredByDaemonSets.List()...)

	standaloneJobs, coveredByJobs := graphview.AllJobs(g, coveredNodes)
	coveredNodes.Insert(coveredByJobs.List()...)

	standalonePods, coveredByPods := graphview.AllPods(g, coveredNodes)
	coveredNodes.Insert(coveredByPods.List()...)

	return tabbedString(func(out *tabwriter.Writer) error {
		indent := "  "
		if allNamespaces {
			fmt.Fprintf(out, describeAllProjectsOnServer(f, d.Server))
		} else {
			fmt.Fprintf(out, describeProjectAndServer(f, project, d.Server))
		}

		for _, service := range services {
			if !service.Service.Found() {
				continue
			}
			local := namespacedFormatter{currentNamespace: service.Service.Namespace}

			var exposes []string
			for _, routeNode := range service.ExposingRoutes {
				exposes = append(exposes, describeRouteInServiceGroup(local, routeNode)...)
			}
			sort.Sort(exposedRoutes(exposes))

			fmt.Fprintln(out)

			// print services that should be grouped with this service based on matching selectors
			selector := createSelector(service.Service.Spec.Selector)
			groupedServices := servicesBySelector[selector.String()]
			for _, groupedSvc := range groupedServices {
				if !groupedSvc.Service.Found() {
					continue
				}

				grouppedLocal := namespacedFormatter{currentNamespace: service.Service.Namespace}

				var grouppedExposes []string
				for _, routeNode := range groupedSvc.ExposingRoutes {
					grouppedExposes = append(grouppedExposes, describeRouteInServiceGroup(grouppedLocal, routeNode)...)
				}
				sort.Sort(exposedRoutes(grouppedExposes))

				printLines(out, "", 0, describeServiceInServiceGroup(f, groupedSvc, grouppedExposes...)...)
			}

			printLines(out, "", 0, describeServiceInServiceGroup(f, service, exposes...)...)

			for _, dcPipeline := range service.DeploymentConfigPipelines {
				printLines(out, indent, 1, describeDeploymentConfigInServiceGroup(local, dcPipeline, func(rc *kubegraph.ReplicationControllerNode) int32 {
					return graphview.MaxRecentContainerRestartsForRC(g, rc)
				})...)
			}

			for _, node := range service.StatefulSets {
				printLines(out, indent, 1, describeStatefulSetInServiceGroup(local, node)...)
			}

			for _, node := range service.Deployments {
				printLines(out, indent, 1, describeDeploymentInServiceGroup(local, node, func(rs *kubegraph.ReplicaSetNode) int32 {
					return graphview.MaxRecentContainerRestartsForRS(g, rs)
				})...)
			}

			for _, node := range service.DaemonSets {
				printLines(out, indent, 1, describeDaemonSetInServiceGroup(local, node)...)
			}

		rsNode:
			for _, rsNode := range service.FulfillingRSs {
				for _, coveredD := range service.FulfillingDeployments {
					if kubeedges.BelongsToDeployment(coveredD.Deployment, rsNode.ReplicaSet) {
						continue rsNode
					}
				}
				printLines(out, indent, 1, describeRSInServiceGroup(local, rsNode)...)
			}

		rcNode:
			for _, rcNode := range service.FulfillingRCs {
				for _, coveredDC := range service.FulfillingDCs {
					if appsedges.BelongsToDeploymentConfig(coveredDC.DeploymentConfig, rcNode.ReplicationController) {
						continue rcNode
					}
				}
				printLines(out, indent, 1, describeRCInServiceGroup(local, rcNode)...)
			}

		pod:
			for _, node := range service.FulfillingPods {
				// skip pods that have been displayed in a roll-up of RCs and DCs (by implicit usage of RCs)
				for _, coveredRC := range service.FulfillingRCs {
					if g.Edge(node, coveredRC) != nil {
						continue pod
					}
				}
				for _, coveredRS := range service.FulfillingRSs {
					if g.Edge(node, coveredRS) != nil {
						continue pod
					}
				}
				// TODO: collapse into FulfillingControllers
				for _, covered := range service.FulfillingStatefulSets {
					if g.Edge(node, covered) != nil {
						continue pod
					}
				}
				for _, covered := range service.FulfillingDCs {
					if g.Edge(node, covered) != nil {
						continue pod
					}
				}
				for _, covered := range service.FulfillingDeployments {
					if g.Edge(node, covered) != nil {
						continue pod
					}
				}
				for _, covered := range service.FulfillingDSs {
					if g.Edge(node, covered) != nil {
						continue pod
					}
				}
				printLines(out, indent, 1, describePodInServiceGroup(local, node)...)
			}
		}

		for _, standaloneDC := range standaloneDCs {
			if !standaloneDC.DeploymentConfig.Found() {
				continue
			}

			fmt.Fprintln(out)
			printLines(out, indent, 0, describeDeploymentConfigInServiceGroup(f, standaloneDC, func(rc *kubegraph.ReplicationControllerNode) int32 {
				return graphview.MaxRecentContainerRestartsForRC(g, rc)
			})...)
		}
		for _, standaloneDeployment := range standaloneDeployments {
			if !standaloneDeployment.Deployment.Found() {
				continue
			}

			fmt.Fprintln(out)
			printLines(out, indent, 0, describeDeploymentInServiceGroup(f, standaloneDeployment, func(rs *kubegraph.ReplicaSetNode) int32 {
				return graphview.MaxRecentContainerRestartsForRS(g, rs)
			})...)
		}

		for _, standaloneStatefulSet := range standaloneStatefulSets {
			if !standaloneStatefulSet.StatefulSet.Found() {
				continue
			}

			fmt.Fprintln(out)
			printLines(out, indent, 0, describeStatefulSetInServiceGroup(f, standaloneStatefulSet)...)
		}

		for _, standaloneImage := range standaloneImages {
			fmt.Fprintln(out)
			lines := describeStandaloneBuildGroup(f, standaloneImage, namespace)
			lines = append(lines, describeAdditionalBuildDetail(standaloneImage.Build, standaloneImage.LastSuccessfulBuild, standaloneImage.LastUnsuccessfulBuild, standaloneImage.ActiveBuilds, standaloneImage.DestinationResolved, true)...)
			printLines(out, indent, 0, lines...)
		}

		for _, standaloneRC := range standaloneRCs {
			if !standaloneRC.RC.Found() {
				continue
			}

			fmt.Fprintln(out)
			printLines(out, indent, 0, describeRCInServiceGroup(f, standaloneRC.RC)...)
		}

		for _, standaloneRS := range standaloneRSs {
			if !standaloneRS.RS.Found() {
				continue
			}

			fmt.Fprintln(out)
			printLines(out, indent, 0, describeRSInServiceGroup(f, standaloneRS.RS)...)
		}

		for _, standaloneDaemonSet := range standaloneDaemonSets {
			if !standaloneDaemonSet.DaemonSet.Found() {
				continue
			}

			fmt.Fprintln(out)
			printLines(out, indent, 0, describeDaemonSetInServiceGroup(f, standaloneDaemonSet)...)
		}

		for _, standaloneJob := range standaloneJobs {
			if !standaloneJob.Job.Found() {
				continue
			}

			fmt.Fprintln(out)
			printLines(out, indent, 0, describeStandaloneJob(f, standaloneJob)...)
		}

		monopods, err := filterBoringPods(standalonePods)
		if err != nil {
			return err
		}
		for _, monopod := range monopods {
			fmt.Fprintln(out)
			printLines(out, indent, 0, describeMonopod(f, monopod.Pod)...)
		}

		allMarkers := osgraph.Markers{}
		allMarkers = append(allMarkers, createForbiddenMarkers(forbiddenResources)...)
		for _, scanner := range getMarkerScanners(d.LogsCommandName, d.SecurityPolicyCommandFormat, d.SetProbeCommandName, forbiddenResources) {
			allMarkers = append(allMarkers, scanner(g, f)...)
		}

		// TODO: Provide an option to chase these hidden markers.
		allMarkers = allMarkers.FilterByNamespace(namespace)

		fmt.Fprintln(out)

		sort.Stable(osgraph.ByKey(allMarkers))
		sort.Stable(osgraph.ByNodeID(allMarkers))

		errorMarkers := allMarkers.BySeverity(osgraph.ErrorSeverity)
		errorSuggestions := 0
		if len(errorMarkers) > 0 {
			fmt.Fprintln(out, "Errors:")
			errorSuggestions += printMarkerSuggestions(errorMarkers, d.Suggest, out, indent)
		}

		warningMarkers := allMarkers.BySeverity(osgraph.WarningSeverity)
		if len(warningMarkers) > 0 {
			if d.Suggest {
				// add linebreak between Errors list and Warnings list
				if len(errorMarkers) > 0 {
					fmt.Fprintln(out)
				}
				fmt.Fprintln(out, "Warnings:")
			}
			printMarkerSuggestions(warningMarkers, d.Suggest, out, indent)
		}

		infoMarkers := allMarkers.BySeverity(osgraph.InfoSeverity)
		if len(infoMarkers) > 0 {
			if d.Suggest {
				// add linebreak between Warnings list and Info List
				if len(warningMarkers) > 0 || len(errorMarkers) > 0 {
					fmt.Fprintln(out)
				}
				fmt.Fprintln(out, "Info:")
			}
			printMarkerSuggestions(infoMarkers, d.Suggest, out, indent)
		}

		// We print errors by default and warnings if --sugest is used. If we get none,
		// this would be an extra new line.
		if len(errorMarkers) != 0 || len(infoMarkers) != 0 || (d.Suggest && len(warningMarkers) != 0) {
			fmt.Fprintln(out)
		}

		errors, warnings, infos := "", "", ""
		if len(errorMarkers) == 1 {
			errors = "1 error"
		} else if len(errorMarkers) > 1 {
			errors = fmt.Sprintf("%d errors", len(errorMarkers))
		}
		if len(warningMarkers) == 1 {
			warnings = "1 warning"
		} else if len(warningMarkers) > 1 {
			warnings = fmt.Sprintf("%d warnings", len(warningMarkers))
		}
		if len(infoMarkers) == 1 {
			infos = "1 info"
		} else if len(infoMarkers) > 0 {
			infos = fmt.Sprintf("%d infos", len(infoMarkers))
		}

		markerStrings := []string{errors, warnings, infos}
		markerString := ""
		count := 0
		for _, m := range markerStrings {
			if len(m) > 0 {
				if count > 0 {
					markerString = fmt.Sprintf("%s, ", markerString)
				}
				markerString = fmt.Sprintf("%s%s", markerString, m)
				count++
			}
		}

		switch {
		case !d.Suggest && ((len(errorMarkers) > 0 && errorSuggestions > 0) || len(warningMarkers) > 0 || len(infoMarkers) > 0):
			fmt.Fprintf(out, "%s identified, use 'arvan paas status --suggest' to see details.\n", markerString)

		case (len(services) == 0) && (len(standaloneDCs) == 0) && (len(standaloneImages) == 0):
			fmt.Fprintln(out, "You have no services, deployment configs, or build configs.")
			fmt.Fprintf(out, "Run 'arvan paas new-app' to create an application.\n")

		default:
			fmt.Fprintf(out, "View details with 'arvan paas describe <resource>/<name>' or list resources with 'arvan paas get all'.\n")
		}

		return nil
	})
}

// printMarkerSuggestions prints a formatted list of marker suggestions
// and returns the amount of suggestions printed
func printMarkerSuggestions(markers []osgraph.Marker, suggest bool, out *tabwriter.Writer, indent string) int {
	suggestionAmount := 0
	for _, marker := range markers {
		if len(marker.Suggestion) > 0 {
			suggestionAmount++
		}
		if len(marker.Message) > 0 && (suggest || marker.Severity == osgraph.ErrorSeverity) {
			fmt.Fprintln(out, indent+"* "+marker.Message)
		}
		if len(marker.Suggestion) > 0 && suggest {
			switch s := marker.Suggestion.String(); {
			case strings.Contains(s, "\n"):
				fmt.Fprintln(out)
				for _, line := range strings.Split(s, "\n") {
					fmt.Fprintln(out, indent+"  "+line)
				}
			case len(s) > 0:
				fmt.Fprintln(out, indent+"  try: "+s)
			}
		}
	}
	return suggestionAmount
}

func createForbiddenMarkers(forbiddenResources sets.String) []osgraph.Marker {
	markers := []osgraph.Marker{}
	for forbiddenResource := range forbiddenResources {
		markers = append(markers, osgraph.Marker{
			Severity: osgraph.WarningSeverity,
			Key:      ForbiddenListWarning,
			Message:  fmt.Sprintf("Unable to list %s resources.  Not all status relationships can be established.", forbiddenResource),
		})
	}
	return markers
}

func getMarkerScanners(logsCommandName, securityPolicyCommandFormat, setProbeCommandName string, forbiddenResources sets.String) []osgraph.MarkerScanner {
	return []osgraph.MarkerScanner{
		func(g osgraph.Graph, f osgraph.Namer) []osgraph.Marker {
			return kubeanalysis.FindRestartingPods(g, f, logsCommandName, securityPolicyCommandFormat)
		},
		kubeanalysis.FindDuelingReplicationControllers,
		func(g osgraph.Graph, f osgraph.Namer) []osgraph.Marker {
			// do not attempt to add markers for missing secrets if dealing with forbidden errors
			if forbiddenResources.Has("secrets") {
				return []osgraph.Marker{}
			}
			return kubeanalysis.FindMissingSecrets(g, f)
		},
		kubeanalysis.FindHPASpecsMissingCPUTargets,
		// TODO(directxman12): re-enable FindHPASpecsMissingScaleRefs once the graph library
		// knows how to deal with arbitrary scale targets
		kubeanalysis.FindOverlappingHPAs,
		buildanalysis.FindUnpushableBuildConfigs,
		buildanalysis.FindCircularBuilds,
		buildanalysis.FindPendingTags,
		appsanalysis.FindDeploymentConfigTriggerErrors,
		appsanalysis.FindPersistentVolumeClaimWarnings,
		buildanalysis.FindMissingInputImageStreams,
		func(g osgraph.Graph, f osgraph.Namer) []osgraph.Marker {
			return appsanalysis.FindDeploymentConfigReadinessWarnings(g, f, setProbeCommandName)
		},
		func(g osgraph.Graph, f osgraph.Namer) []osgraph.Marker {
			return kubeanalysis.FindMissingLivenessProbes(g, f, setProbeCommandName)
		},
		routeanalysis.FindPortMappingIssues,
		routeanalysis.FindMissingTLSTerminationType,
		routeanalysis.FindPathBasedPassthroughRoutes,
		routeanalysis.FindRouteAdmissionFailures,
		routeanalysis.FindMissingRouter,
		// We disable this feature by default and we don't have a capability detection for this sort of thing.  Disable this check for now.
		// kubeanalysis.FindUnmountableSecrets,
	}
}

func printLines(out io.Writer, indent string, depth int, lines ...string) {
	for i, s := range lines {
		fmt.Fprintf(out, strings.Repeat(indent, depth))
		if i != 0 {
			fmt.Fprint(out, indent)
		}
		fmt.Fprintln(out, s)
	}
}

func indentLines(indent string, lines ...string) []string {
	ret := make([]string, 0, len(lines))
	for _, line := range lines {
		ret = append(ret, indent+line)
	}

	return ret
}

type formatter interface {
	ResourceName(obj interface{}) string
}

func namespaceNameWithType(resource, name, namespace, defaultNamespace string, noNamespace bool) string {
	if noNamespace || namespace == defaultNamespace || len(namespace) == 0 {
		return resource + "/" + name
	}
	return resource + "/" + name + "[" + namespace + "]"
}

var namespaced = namespacedFormatter{}

type namespacedFormatter struct {
	hideNamespace    bool
	currentNamespace string
}

func (f namespacedFormatter) ResourceName(obj interface{}) string {
	switch t := obj.(type) {

	case *kubegraph.PodNode:
		return namespaceNameWithType("pod", t.Name, t.Namespace, f.currentNamespace, f.hideNamespace)
	case *kubegraph.ServiceNode:
		return namespaceNameWithType("svc", t.Name, t.Namespace, f.currentNamespace, f.hideNamespace)
	case *kubegraph.SecretNode:
		return namespaceNameWithType("secret", t.Name, t.Namespace, f.currentNamespace, f.hideNamespace)
	case *kubegraph.ServiceAccountNode:
		return namespaceNameWithType("sa", t.Name, t.Namespace, f.currentNamespace, f.hideNamespace)
	case *kubegraph.ReplicationControllerNode:
		return namespaceNameWithType("rc", t.ReplicationController.Name, t.ReplicationController.Namespace, f.currentNamespace, f.hideNamespace)
	case *kubegraph.ReplicaSetNode:
		return namespaceNameWithType("rs", t.ReplicaSet.Name, t.ReplicaSet.Namespace, f.currentNamespace, f.hideNamespace)
	case *kubegraph.HorizontalPodAutoscalerNode:
		return namespaceNameWithType("hpa", t.HorizontalPodAutoscaler.Name, t.HorizontalPodAutoscaler.Namespace, f.currentNamespace, f.hideNamespace)
	case *kubegraph.StatefulSetNode:
		return namespaceNameWithType("statefulset", t.StatefulSet.Name, t.StatefulSet.Namespace, f.currentNamespace, f.hideNamespace)
	case *kubegraph.DeploymentNode:
		return namespaceNameWithType("deployment", t.Deployment.Name, t.Deployment.Namespace, f.currentNamespace, f.hideNamespace)
	case *kubegraph.PersistentVolumeClaimNode:
		return namespaceNameWithType("pvc", t.PersistentVolumeClaim.Name, t.PersistentVolumeClaim.Namespace, f.currentNamespace, f.hideNamespace)
	case *kubegraph.JobNode:
		return namespaceNameWithType("job", t.Job.Name, t.Job.Namespace, f.currentNamespace, f.hideNamespace)
	case *kubegraph.DaemonSetNode:
		return namespaceNameWithType("daemonset", t.DaemonSet.Name, t.DaemonSet.Namespace, f.currentNamespace, f.hideNamespace)

	case *imagegraph.ImageStreamNode:
		return namespaceNameWithType("is", t.ImageStream.Name, t.ImageStream.Namespace, f.currentNamespace, f.hideNamespace)
	case *imagegraph.ImageStreamTagNode:
		return namespaceNameWithType("istag", t.ImageStreamTag.Name, t.ImageStreamTag.Namespace, f.currentNamespace, f.hideNamespace)
	case *imagegraph.ImageStreamImageNode:
		return namespaceNameWithType("isi", t.ImageStreamImage.Name, t.ImageStreamImage.Namespace, f.currentNamespace, f.hideNamespace)
	case *imagegraph.ImageNode:
		return namespaceNameWithType("image", t.Image.Name, t.Image.Namespace, f.currentNamespace, f.hideNamespace)
	case *buildgraph.BuildConfigNode:
		return namespaceNameWithType("bc", t.BuildConfig.Name, t.BuildConfig.Namespace, f.currentNamespace, f.hideNamespace)
	case *buildgraph.BuildNode:
		return namespaceNameWithType("build", t.Build.Name, t.Build.Namespace, f.currentNamespace, f.hideNamespace)

	case *appsgraph.DeploymentConfigNode:
		return namespaceNameWithType("dc", t.DeploymentConfig.Name, t.DeploymentConfig.Namespace, f.currentNamespace, f.hideNamespace)

	case *routegraph.RouteNode:
		return namespaceNameWithType("route", t.Route.Name, t.Route.Namespace, f.currentNamespace, f.hideNamespace)

	default:
		return fmt.Sprintf("<unrecognized object: %#v>", obj)
	}
}

func describeProjectAndServer(f formatter, project *projectv1.Project, server string) string {
	projectName := project.Name
	displayName := project.Annotations[annotations.OpenShiftDisplayName]
	if len(displayName) == 0 {
		displayName = project.Annotations["displayName"]
	}
	if len(displayName) > 0 && displayName != project.Name {
		projectName = fmt.Sprintf("%s (%s)", displayName, project.Name)
	}
	if len(server) == 0 {
		return fmt.Sprintf("In project %s\n", projectName)
	}
	return fmt.Sprintf("In project %s on server %s\n", projectName, server)

}

func describeAllProjectsOnServer(f formatter, server string) string {
	if len(server) == 0 {
		return "Showing all projects\n"
	}
	return fmt.Sprintf("Showing all projects on server %s\n", server)
}

func describeDeploymentConfigInServiceGroup(f formatter, deploy graphview.DeploymentConfigPipeline, restartFn func(*kubegraph.ReplicationControllerNode) int32) []string {
	local := namespacedFormatter{currentNamespace: deploy.DeploymentConfig.DeploymentConfig.Namespace}

	includeLastPass := deploy.ActiveDeployment == nil
	if len(deploy.Images) == 1 {
		format := "%s deploys %s %s"
		if deploy.DeploymentConfig.DeploymentConfig.Spec.Test {
			format = "%s test deploys %s %s"
		}
		lines := []string{fmt.Sprintf(format, f.ResourceName(deploy.DeploymentConfig), describeImageInPipeline(local, deploy.Images[0], deploy.DeploymentConfig.DeploymentConfig.Namespace), describeDeploymentConfigTrigger(deploy.DeploymentConfig.DeploymentConfig))}
		if len(lines[0]) > 120 && strings.Contains(lines[0], " <- ") {
			segments := strings.SplitN(lines[0], " <- ", 2)
			lines[0] = segments[0] + " <-"
			lines = append(lines, segments[1])
		}
		lines = append(lines, indentLines("  ", describeAdditionalBuildDetail(deploy.Images[0].Build, deploy.Images[0].LastSuccessfulBuild, deploy.Images[0].LastUnsuccessfulBuild, deploy.Images[0].ActiveBuilds, deploy.Images[0].DestinationResolved, includeLastPass)...)...)
		lines = append(lines, describeDeploymentConfigDeployments(local, deploy.DeploymentConfig, deploy.ActiveDeployment, deploy.InactiveDeployments, restartFn, maxDisplayDeployments)...)
		return lines
	}

	format := "%s deploys %s"
	if deploy.DeploymentConfig.DeploymentConfig.Spec.Test {
		format = "%s test deploys %s"
	}
	lines := []string{fmt.Sprintf(format, f.ResourceName(deploy.DeploymentConfig), describeDeploymentConfigTrigger(deploy.DeploymentConfig.DeploymentConfig))}
	for _, image := range deploy.Images {
		lines = append(lines, describeImageInPipeline(local, image, deploy.DeploymentConfig.DeploymentConfig.Namespace))
		lines = append(lines, indentLines("  ", describeAdditionalBuildDetail(image.Build, image.LastSuccessfulBuild, image.LastUnsuccessfulBuild, image.ActiveBuilds, image.DestinationResolved, includeLastPass)...)...)
		lines = append(lines, describeDeploymentConfigDeployments(local, deploy.DeploymentConfig, deploy.ActiveDeployment, deploy.InactiveDeployments, restartFn, maxDisplayDeployments)...)
	}
	return lines
}

func describeDeploymentInServiceGroup(f formatter, deploy graphview.Deployment, restartFn func(node *kubegraph.ReplicaSetNode) int32) []string {
	local := namespacedFormatter{currentNamespace: deploy.Deployment.Deployment.Namespace}
	// TODO: Figure out what this is
	includeLastPass := false

	if len(deploy.Images) == 1 {
		format := "%s deploys %s %s"
		lines := []string{fmt.Sprintf(format, f.ResourceName(deploy.Deployment), describeImageInPipeline(local, deploy.Images[0], deploy.Deployment.Deployment.Namespace), "")}
		if len(lines[0]) > 120 && strings.Contains(lines[0], " <- ") {
			segments := strings.SplitN(lines[0], " <- ", 2)
			lines[0] = segments[0] + " <-"
			lines = append(lines, segments[1])
		}
		lines = append(lines, indentLines("  ", describeAdditionalBuildDetail(deploy.Images[0].Build, deploy.Images[0].LastSuccessfulBuild, deploy.Images[0].LastUnsuccessfulBuild, deploy.Images[0].ActiveBuilds, deploy.Images[0].DestinationResolved, includeLastPass)...)...)
		lines = append(lines, describeDeployments(local, deploy.Deployment, deploy.ActiveDeployment, deploy.InactiveDeployments, restartFn, maxDisplayDeployments)...)
		return lines
	}

	images := []string{}
	for _, container := range deploy.Deployment.Deployment.Spec.Template.Spec.Containers {
		images = append(images, container.Image)
	}
	imagesWithoutTriggers := ""
	if len(deploy.Images) == 0 {
		imagesWithoutTriggers = strings.Join(images, ",")
	}
	format := "%s deploys %s"
	lines := []string{fmt.Sprintf(format, f.ResourceName(deploy.Deployment), imagesWithoutTriggers)}
	for _, image := range deploy.Images {
		lines = append(lines, describeImageInPipeline(local, image, deploy.Deployment.Deployment.Namespace))
		lines = append(lines, indentLines("  ", describeAdditionalBuildDetail(image.Build, image.LastSuccessfulBuild, image.LastUnsuccessfulBuild, image.ActiveBuilds, image.DestinationResolved, includeLastPass)...)...)
	}
	lines = append(lines, describeDeployments(local, deploy.Deployment, deploy.ActiveDeployment, deploy.InactiveDeployments, restartFn, maxDisplayDeployments)...)

	return lines
}

func describeStatefulSetInServiceGroup(f formatter, node graphview.StatefulSet) []string {
	local := namespacedFormatter{currentNamespace: node.StatefulSet.StatefulSet.Namespace}
	includeLastPass := false
	format := "%s manages %s"
	images := []string{}
	for _, container := range node.StatefulSet.StatefulSet.Spec.Template.Spec.Containers {
		images = append(images, container.Image)
	}
	imagesWithoutTriggers := ""
	if len(node.Images) == 0 {
		imagesWithoutTriggers = strings.Join(images, ",")
	}
	if len(node.Images) == 1 {
		image := node.Images[0]
		lines := []string{fmt.Sprintf(format, f.ResourceName(node.StatefulSet), describeImageInPipeline(local, image, node.StatefulSet.StatefulSet.Namespace))}
		lines = append(lines, indentLines("  ", describeAdditionalBuildDetail(image.Build, image.LastSuccessfulBuild, image.LastUnsuccessfulBuild, image.ActiveBuilds, image.DestinationResolved, includeLastPass)...)...)
		lines = append(lines, describeStatefulSetStatus(node.StatefulSet.StatefulSet))
		return lines
	}
	lines := []string{fmt.Sprintf(format, f.ResourceName(node.StatefulSet), imagesWithoutTriggers)}
	for _, image := range node.Images {
		lines = append(lines, describeImageInPipeline(local, image, node.StatefulSet.StatefulSet.Namespace))
		lines = append(lines, indentLines("  ", describeAdditionalBuildDetail(image.Build, image.LastSuccessfulBuild, image.LastUnsuccessfulBuild, image.ActiveBuilds, image.DestinationResolved, includeLastPass)...)...)
	}
	lines = append(lines, describeStatefulSetStatus(node.StatefulSet.StatefulSet))
	return lines
}

func describeDaemonSetInServiceGroup(f formatter, node graphview.DaemonSet) []string {
	local := namespacedFormatter{currentNamespace: node.DaemonSet.DaemonSet.Namespace}
	includeLastPass := false

	if len(node.Images) == 1 {
		format := "%s manages %s %s"
		lines := []string{fmt.Sprintf(format, f.ResourceName(node.DaemonSet), describeImageInPipeline(local, node.Images[0], node.DaemonSet.DaemonSet.Namespace), "")}
		if len(lines[0]) > 120 && strings.Contains(lines[0], " <- ") {
			segments := strings.SplitN(lines[0], " <- ", 2)
			lines[0] = segments[0] + " <-"
			lines = append(lines, segments[1])
		}

		lines = append(lines, indentLines("  ", describeAdditionalBuildDetail(node.Images[0].Build, node.Images[0].LastSuccessfulBuild, node.Images[0].LastUnsuccessfulBuild, node.Images[0].ActiveBuilds, node.Images[0].DestinationResolved, includeLastPass)...)...)
		lines = append(lines, describeDaemonSetStatus(node.DaemonSet.DaemonSet))
		return lines
	}

	images := []string{}
	for _, container := range node.DaemonSet.DaemonSet.Spec.Template.Spec.Containers {
		images = append(images, container.Image)
	}
	imagesWithoutTriggers := ""
	if len(node.Images) == 0 {
		imagesWithoutTriggers = strings.Join(images, ",")
	}
	format := "%s manages %s"
	lines := []string{fmt.Sprintf(format, f.ResourceName(node.DaemonSet), imagesWithoutTriggers)}
	for _, image := range node.Images {
		lines = append(lines, describeImageInPipeline(local, image, node.DaemonSet.DaemonSet.Namespace))
		lines = append(lines, indentLines("  ", describeAdditionalBuildDetail(image.Build, image.LastSuccessfulBuild, image.LastUnsuccessfulBuild, image.ActiveBuilds, image.DestinationResolved, includeLastPass)...)...)
	}
	lines = append(lines, describeDaemonSetStatus(node.DaemonSet.DaemonSet))
	return lines
}

func describeRCInServiceGroup(f formatter, rcNode *kubegraph.ReplicationControllerNode) []string {
	if rcNode.ReplicationController.Spec.Template == nil {
		return []string{}
	}

	images := []string{}
	for _, container := range rcNode.ReplicationController.Spec.Template.Spec.Containers {
		images = append(images, container.Image)
	}

	lines := []string{fmt.Sprintf("%s runs %s", f.ResourceName(rcNode), strings.Join(images, ", "))}
	lines = append(lines, describeRCStatus(rcNode.ReplicationController))

	return lines
}

func describeRSInServiceGroup(f formatter, rsNode *kubegraph.ReplicaSetNode) []string {
	images := []string{}
	for _, container := range rsNode.ReplicaSet.Spec.Template.Spec.Containers {
		images = append(images, container.Image)
	}

	lines := []string{fmt.Sprintf("%s runs %s", f.ResourceName(rsNode), strings.Join(images, ", "))}
	lines = append(lines, describeRSStatus(rsNode.ReplicaSet))

	return lines
}

func describePodInServiceGroup(f formatter, podNode *kubegraph.PodNode) []string {
	images := []string{}
	for _, container := range podNode.Pod.Spec.Containers {
		images = append(images, container.Image)
	}

	lines := []string{fmt.Sprintf("%s runs %s", f.ResourceName(podNode), strings.Join(images, ", "))}
	return lines
}

func describeMonopod(f formatter, podNode *kubegraph.PodNode) []string {
	images := []string{}
	for _, container := range podNode.Pod.Spec.Containers {
		images = append(images, container.Image)
	}

	lines := []string{fmt.Sprintf("%s runs %s", f.ResourceName(podNode), strings.Join(images, ", "))}
	return lines
}

func describeStandaloneJob(f formatter, node graphview.Job) []string {
	local := namespacedFormatter{currentNamespace: node.Job.Job.Namespace}
	includeLastPass := false
	format := "%s manages %s"
	images := []string{}
	for _, container := range node.Job.Job.Spec.Template.Spec.Containers {
		images = append(images, container.Image)
	}
	imagesWithoutTriggers := ""
	if len(node.Images) == 0 {
		imagesWithoutTriggers = strings.Join(images, ",")
	}
	if len(node.Images) == 1 {
		image := node.Images[0]
		lines := []string{fmt.Sprintf(format, f.ResourceName(node.Job), describeImageInPipeline(local, image, node.Job.Job.Namespace))}
		lines = append(lines, indentLines("  ", describeAdditionalBuildDetail(image.Build, image.LastSuccessfulBuild, image.LastUnsuccessfulBuild, image.ActiveBuilds, image.DestinationResolved, includeLastPass)...)...)
		lines = append(lines, describeJobStatus(node.Job.Job))
		return lines
	}
	lines := []string{fmt.Sprintf(format, f.ResourceName(node.Job), imagesWithoutTriggers)}
	for _, image := range node.Images {
		lines = append(lines, describeImageInPipeline(local, image, node.Job.Job.Namespace))
		lines = append(lines, indentLines("  ", describeAdditionalBuildDetail(image.Build, image.LastSuccessfulBuild, image.LastUnsuccessfulBuild, image.ActiveBuilds, image.DestinationResolved, includeLastPass)...)...)
	}
	lines = append(lines, describeJobStatus(node.Job.Job))
	return lines
}

func describeJobStatus(job *batchv1.Job) string {
	timeAt := strings.ToLower(FormatRelativeTime(job.CreationTimestamp.Time))
	if job.Spec.Completions == nil {
		return ""
	}
	return fmt.Sprintf("created %s ago %d/%d completed %d running", timeAt, job.Status.Succeeded, *job.Spec.Completions, job.Status.Active)
}

// exposedRoutes orders strings by their leading prefix (https:// -> http:// other prefixes), then by
// the shortest distance up to the first space (indicating a break), then alphabetically:
//
//   https://test.com
//   https://www.test.com
//   http://t.com
//   other string
//
type exposedRoutes []string

func (e exposedRoutes) Len() int      { return len(e) }
func (e exposedRoutes) Swap(i, j int) { e[i], e[j] = e[j], e[i] }
func (e exposedRoutes) Less(i, j int) bool {
	a, b := e[i], e[j]
	prefixA, prefixB := strings.HasPrefix(a, "https://"), strings.HasPrefix(b, "https://")
	switch {
	case prefixA && !prefixB:
		return true
	case !prefixA && prefixB:
		return false
	case !prefixA && !prefixB:
		prefixA, prefixB = strings.HasPrefix(a, "http://"), strings.HasPrefix(b, "http://")
		switch {
		case prefixA && !prefixB:
			return true
		case !prefixA && prefixB:
			return false
		case !prefixA && !prefixB:
			return a < b
		default:
			a, b = a[7:], b[7:]
		}
	default:
		a, b = a[8:], b[8:]
	}
	lA, lB := strings.Index(a, " "), strings.Index(b, " ")
	if lA == -1 {
		lA = len(a)
	}
	if lB == -1 {
		lB = len(b)
	}
	switch {
	case lA < lB:
		return true
	case lA > lB:
		return false
	default:
		return a < b
	}
}

func extractRouteInfo(route *routev1.Route) (requested bool, other []string, errors []string) {
	reasons := sets.NewString()
	for _, ingress := range route.Status.Ingress {
		exact := route.Spec.Host == ingress.Host
		switch status, condition := routedisplayhelpers.IngressConditionStatus(&ingress, routev1.RouteAdmitted); status {
		case corev1.ConditionFalse:
			reasons.Insert(condition.Reason)
		default:
			if exact {
				requested = true
			} else {
				other = append(other, ingress.Host)
			}
		}
	}
	return requested, other, reasons.List()
}

func describeRouteExposed(host string, route *routev1.Route, errors bool) string {
	var trailer string
	if errors {
		trailer = " (!)"
	}
	var prefix string
	switch {
	case route.Spec.TLS == nil:
		prefix = fmt.Sprintf("http://%s", host)
	case route.Spec.TLS.Termination == routev1.TLSTerminationPassthrough:
		prefix = fmt.Sprintf("https://%s (passthrough)", host)
	case route.Spec.TLS.Termination == routev1.TLSTerminationReencrypt:
		prefix = fmt.Sprintf("https://%s (reencrypt)", host)
	case route.Spec.TLS.Termination != routev1.TLSTerminationEdge:
		// future proof against other types of TLS termination being added
		prefix = fmt.Sprintf("https://%s", host)
	case route.Spec.TLS.InsecureEdgeTerminationPolicy == routev1.InsecureEdgeTerminationPolicyRedirect:
		prefix = fmt.Sprintf("https://%s (redirects)", host)
	case route.Spec.TLS.InsecureEdgeTerminationPolicy == routev1.InsecureEdgeTerminationPolicyAllow:
		prefix = fmt.Sprintf("https://%s (and http)", host)
	default:
		prefix = fmt.Sprintf("https://%s", host)
	}

	if route.Spec.Port != nil && len(route.Spec.Port.TargetPort.String()) > 0 {
		return fmt.Sprintf("%s to pod port %s%s", prefix, route.Spec.Port.TargetPort.String(), trailer)
	}
	return fmt.Sprintf("%s%s", prefix, trailer)
}

func describeRouteInServiceGroup(f formatter, routeNode *routegraph.RouteNode) []string {
	// markers should cover printing information about admission failure
	requested, other, errors := extractRouteInfo(routeNode.Route)
	var lines []string
	if requested {
		lines = append(lines, describeRouteExposed(routeNode.Spec.Host, routeNode.Route, len(errors) > 0))
	}
	for _, s := range other {
		lines = append(lines, describeRouteExposed(s, routeNode.Route, len(errors) > 0))
	}
	if len(lines) == 0 {
		switch {
		case len(errors) >= 1:
			// router rejected the output
			lines = append(lines, fmt.Sprintf("%s not accepted: %s", f.ResourceName(routeNode), errors[0]))
		case len(routeNode.Spec.Host) == 0:
			// no errors or output, likely no router running and no default domain
			lines = append(lines, fmt.Sprintf("%s has no host set", f.ResourceName(routeNode)))
		case len(routeNode.Status.Ingress) == 0:
			// host set, but no ingress, an older legacy router
			lines = append(lines, describeRouteExposed(routeNode.Spec.Host, routeNode.Route, false))
		default:
			// multiple conditions but no host exposed, use the generic legacy output
			lines = append(lines, fmt.Sprintf("exposed as %s by %s", routeNode.Spec.Host, f.ResourceName(routeNode)))
		}
	}
	return lines
}

func describeDeploymentConfigTrigger(dc *appsv1.DeploymentConfig) string {
	if len(dc.Spec.Triggers) == 0 {
		return "(manual)"
	}

	return ""
}

func describeStandaloneBuildGroup(f formatter, pipeline graphview.ImagePipeline, namespace string) []string {
	switch {
	case pipeline.Build != nil:
		lines := []string{describeBuildInPipeline(f, pipeline, namespace)}
		if pipeline.Image != nil {
			lines = append(lines, fmt.Sprintf("-> %s", describeImageTagInPipeline(f, pipeline.Image, namespace)))
		}
		return lines
	case pipeline.Image != nil:
		return []string{describeImageTagInPipeline(f, pipeline.Image, namespace)}
	default:
		return []string{"<unknown>"}
	}
}

func describeImageInPipeline(f formatter, pipeline graphview.ImagePipeline, namespace string) string {
	switch {
	case pipeline.Image != nil && pipeline.Build != nil:
		return fmt.Sprintf("%s <- %s", describeImageTagInPipeline(f, pipeline.Image, namespace), describeBuildInPipeline(f, pipeline, namespace))
	case pipeline.Image != nil:
		return describeImageTagInPipeline(f, pipeline.Image, namespace)
	case pipeline.Build != nil:
		return describeBuildInPipeline(f, pipeline, namespace)
	default:
		return "<unknown>"
	}
}

func describeImageTagInPipeline(f formatter, image graphview.ImageTagLocation, namespace string) string {
	switch t := image.(type) {
	case *imagegraph.ImageStreamTagNode:
		if t.ImageStreamTag.Namespace != namespace {
			return image.ImageSpec()
		}
		return f.ResourceName(t)
	default:
		return image.ImageSpec()
	}
}

func describeBuildInPipeline(f formatter, pipeline graphview.ImagePipeline, namespace string) string {
	bldType := ""
	switch {
	case pipeline.Build.BuildConfig.Spec.Strategy.DockerStrategy != nil:
		bldType = "docker"
	case pipeline.Build.BuildConfig.Spec.Strategy.SourceStrategy != nil:
		bldType = "source"
	case pipeline.Build.BuildConfig.Spec.Strategy.CustomStrategy != nil:
		bldType = "custom"
	case pipeline.Build.BuildConfig.Spec.Strategy.JenkinsPipelineStrategy != nil:
		return fmt.Sprintf("bc/%s is a Jenkins Pipeline", pipeline.Build.BuildConfig.Name)
	default:
		return fmt.Sprintf("bc/%s unrecognized build", pipeline.Build.BuildConfig.Name)
	}

	source, ok := describeSourceInPipeline(&pipeline.Build.BuildConfig.Spec.Source)
	if !ok {
		return fmt.Sprintf("bc/%s unconfigured %s build", pipeline.Build.BuildConfig.Name, bldType)
	}

	retStr := fmt.Sprintf("bc/%s %s builds %s", pipeline.Build.BuildConfig.Name, bldType, source)
	if pipeline.BaseImage != nil {
		retStr = retStr + fmt.Sprintf(" on %s", describeImageTagInPipeline(f, pipeline.BaseImage, namespace))
	}
	if pipeline.BaseBuilds != nil && len(pipeline.BaseBuilds) > 0 {
		bcList := "bc/" + pipeline.BaseBuilds[0]
		for i, bc := range pipeline.BaseBuilds {
			if i == 0 {
				continue
			}
			bcList = bcList + ", bc/" + bc
		}
		retStr = retStr + fmt.Sprintf(" (from %s)", bcList)
	} else if pipeline.ScheduledImport {
		// technically, an image stream produced by a bc could also have a scheduled import,
		// but in the interest of saving space, we'll only note this possibility when there is no input BC
		// (giving the input BC precedence)
		retStr = retStr + " (import scheduled)"
	}
	return retStr
}

func describeAdditionalBuildDetail(build *buildgraph.BuildConfigNode, lastSuccessfulBuild *buildgraph.BuildNode, lastUnsuccessfulBuild *buildgraph.BuildNode, activeBuilds []*buildgraph.BuildNode, pushTargetResolved bool, includeSuccess bool) []string {
	if build == nil {
		return nil
	}
	out := []string{}

	passTime := metav1.Time{}
	if lastSuccessfulBuild != nil {
		passTime = buildTimestamp(lastSuccessfulBuild.Build)
	}
	failTime := metav1.Time{}
	if lastUnsuccessfulBuild != nil {
		failTime = buildTimestamp(lastUnsuccessfulBuild.Build)
	}

	lastTime := failTime
	if passTime.After(failTime.Time) {
		lastTime = passTime
	}

	var firstBuildToDisplay *buildgraph.BuildNode
	firstTime := metav1.Time{}
	var secondBuildToDisplay *buildgraph.BuildNode
	secondTime := metav1.Time{}

	// display the last successful build if specifically requested or we're going to display an active build for context
	if includeSuccess || len(activeBuilds) > 0 {
		if passTime.Before(&failTime) {
			firstBuildToDisplay = lastUnsuccessfulBuild
			firstTime = failTime
			secondBuildToDisplay = lastSuccessfulBuild
			secondTime = passTime
		} else {
			firstBuildToDisplay = lastSuccessfulBuild
			firstTime = passTime
			secondBuildToDisplay = lastUnsuccessfulBuild
			secondTime = failTime
		}
	} else {
		// only display last unsuccessful if it is later than last successful
		if passTime.Before(&failTime) {
			firstBuildToDisplay = lastUnsuccessfulBuild
			firstTime = failTime
		}
	}

	if firstBuildToDisplay != nil {
		out = append(out, describeBuildPhase(firstBuildToDisplay.Build, &firstTime, build.BuildConfig.Name, pushTargetResolved))
	}
	if secondBuildToDisplay != nil {
		out = append(out, describeBuildPhase(secondBuildToDisplay.Build, &secondTime, build.BuildConfig.Name, pushTargetResolved))
	}

	if len(activeBuilds) > 0 {
		activeOut := []string{}
		for i := range activeBuilds {
			activeOut = append(activeOut, describeBuildPhase(activeBuilds[i].Build, nil, build.BuildConfig.Name, pushTargetResolved))
		}

		buildTimestamp := buildTimestamp(activeBuilds[0].Build)
		if buildTimestamp.Before(&lastTime) {
			out = append(out, activeOut...)
		} else {
			out = append(activeOut, out...)
		}
	}
	if len(out) == 0 && lastSuccessfulBuild == nil {
		out = append(out, "not built yet")
	}
	return out
}

func describeBuildPhase(build *buildv1.Build, t *metav1.Time, parentName string, pushTargetResolved bool) string {
	imageStreamFailure := ""
	// if we're using an image stream and that image stream is the internal registry and that registry doesn't exist
	if (build.Spec.Output.To != nil) && !pushTargetResolved {
		imageStreamFailure = " (can't push to image)"
	}

	if t == nil {
		ts := buildTimestamp(build)
		t = &ts
	}
	var time string
	if t.IsZero() {
		time = "<unknown>"
	} else {
		time = strings.ToLower(FormatRelativeTime(t.Time))
	}
	buildIdentification := fmt.Sprintf("build/%s", build.Name)
	prefix := parentName + "-"
	if strings.HasPrefix(build.Name, prefix) {
		suffix := build.Name[len(prefix):]

		if buildNumber, err := strconv.Atoi(suffix); err == nil {
			buildIdentification = fmt.Sprintf("build #%d", buildNumber)
		}
	}

	revision := describeSourceRevision(build.Spec.Revision)
	if len(revision) != 0 {
		revision = fmt.Sprintf(" - %s", revision)
	}
	switch build.Status.Phase {
	case buildv1.BuildPhaseComplete:
		return fmt.Sprintf("%s succeeded %s ago%s%s", buildIdentification, time, revision, imageStreamFailure)
	case buildv1.BuildPhaseError:
		return fmt.Sprintf("%s stopped with an error %s ago%s%s", buildIdentification, time, revision, imageStreamFailure)
	case buildv1.BuildPhaseFailed:
		return fmt.Sprintf("%s failed %s ago%s%s", buildIdentification, time, revision, imageStreamFailure)
	default:
		status := strings.ToLower(string(build.Status.Phase))
		return fmt.Sprintf("%s %s for %s%s%s", buildIdentification, status, time, revision, imageStreamFailure)
	}
}

func describeSourceRevision(rev *buildv1.SourceRevision) string {
	if rev == nil {
		return ""
	}
	switch {
	case rev.Git != nil:
		author := describeSourceControlUser(rev.Git.Author)
		if len(author) == 0 {
			author = describeSourceControlUser(rev.Git.Committer)
		}
		if len(author) != 0 {
			author = fmt.Sprintf(" (%s)", author)
		}
		commit := rev.Git.Commit
		if len(commit) > 7 {
			commit = commit[:7]
		}
		return fmt.Sprintf("%s: %s%s", commit, strings.Split(rev.Git.Message, "\n")[0], author)
	default:
		return ""
	}
}

func describeSourceControlUser(user buildv1.SourceControlUser) string {
	if len(user.Name) == 0 {
		return user.Email
	}
	if len(user.Email) == 0 {
		return user.Name
	}
	return fmt.Sprintf("%s <%s>", user.Name, user.Email)
}

func buildTimestamp(build *buildv1.Build) metav1.Time {
	if build == nil {
		return metav1.Time{}
	}
	if !build.Status.CompletionTimestamp.IsZero() {
		return *build.Status.CompletionTimestamp
	}
	if !build.Status.StartTimestamp.IsZero() {
		return *build.Status.StartTimestamp
	}
	return build.CreationTimestamp
}

func describeSourceInPipeline(source *buildv1.BuildSource) (string, bool) {
	switch {
	case source.Git != nil:
		if len(source.Git.Ref) == 0 {
			return source.Git.URI, true
		}
		return fmt.Sprintf("%s#%s", source.Git.URI, source.Git.Ref), true
	case source.Dockerfile != nil:
		return "Dockerfile", true
	case source.Binary != nil:
		return "uploaded code", true
	case len(source.Images) > 0:
		return "contents in other images", true
	}
	return "", false
}

func describeDeployments(f formatter, dNode *kubegraph.DeploymentNode, activeDeployment *kubegraph.ReplicaSetNode, inactiveDeployments []*kubegraph.ReplicaSetNode, restartFn func(node *kubegraph.ReplicaSetNode) int32, count int) []string {
	if dNode == nil || activeDeployment == nil {
		return nil
	}
	out := []string{}
	deploymentsToPrint := append([]*kubegraph.ReplicaSetNode{}, inactiveDeployments...)
	revision, _ := deployutil.Revision(dNode.Deployment)

	deploymentsToPrint = append([]*kubegraph.ReplicaSetNode{activeDeployment}, inactiveDeployments...)
	for i, deployment := range deploymentsToPrint {
		restartCount := int32(0)
		if restartFn != nil {
			restartCount = restartFn(deployment)
		}
		out = append(out, describeDeploymentStatus(deployment.ReplicaSet, revision, i == 0, restartCount))
	}
	return out
}

func describeDeploymentStatus(rs *kappsv1.ReplicaSet, revision int64, first bool, restartCount int32) string {
	timeAt := strings.ToLower(FormatRelativeTime(rs.CreationTimestamp.Time))
	replicaSetRevision, _ := deployutil.Revision(rs)
	if replicaSetRevision == revision {
		return fmt.Sprintf("deployment #%d running for %s%s", replicaSetRevision, timeAt, describePodSummaryInline(rs.Status.ReadyReplicas, rs.Status.Replicas, *rs.Spec.Replicas, false, restartCount))
	} else {
		return fmt.Sprintf("deployment #%d deployed %s ago%s", replicaSetRevision, timeAt, describePodSummaryInline(rs.Status.ReadyReplicas, rs.Status.Replicas, *rs.Spec.Replicas, first, restartCount))
	}
}

func describeDeploymentConfigDeployments(f formatter, dcNode *appsgraph.DeploymentConfigNode, activeDeployment *kubegraph.ReplicationControllerNode, inactiveDeployments []*kubegraph.ReplicationControllerNode, restartFn func(*kubegraph.ReplicationControllerNode) int32, count int) []string {
	if dcNode == nil {
		return nil
	}
	out := []string{}
	deploymentsToPrint := append([]*kubegraph.ReplicationControllerNode{}, inactiveDeployments...)

	if activeDeployment == nil {
		on, auto := describeDeploymentConfigTriggers(dcNode.DeploymentConfig)
		if dcNode.DeploymentConfig.Status.LatestVersion == 0 {
			out = append(out, fmt.Sprintf("deployment #1 waiting %s", on))
		} else if auto {
			out = append(out, fmt.Sprintf("deployment #%d pending %s", dcNode.DeploymentConfig.Status.LatestVersion, on))
		}
		// TODO: detect new image available?
	} else {
		deploymentsToPrint = append([]*kubegraph.ReplicationControllerNode{activeDeployment}, inactiveDeployments...)
	}

	for i, deployment := range deploymentsToPrint {
		restartCount := int32(0)
		if restartFn != nil {
			restartCount = restartFn(deployment)
		}
		out = append(out, describeDeploymentConfigDeploymentStatus(deployment.ReplicationController, i == 0, dcNode.DeploymentConfig.Spec.Test, restartCount))
		switch {
		case count == -1:
			if appsutil.IsCompleteDeployment(deployment.ReplicationController) {
				return out
			}
		default:
			if i+1 >= count {
				return out
			}
		}
	}
	return out
}

func describeDeploymentConfigDeploymentStatus(rc *corev1.ReplicationController, first, test bool, restartCount int32) string {
	timeAt := strings.ToLower(FormatRelativeTime(rc.CreationTimestamp.Time))
	status := appsutil.DeploymentStatusFor(rc)
	version := appsutil.DeploymentVersionFor(rc)
	maybeCancelling := ""
	if appsutil.IsDeploymentCancelled(rc) && !appsutil.IsTerminatedDeployment(rc) {
		maybeCancelling = " (cancelling)"
	}

	switch status {
	case appsv1.DeploymentStatusFailed:
		reason := appsutil.DeploymentStatusReasonFor(rc)
		if len(reason) > 0 {
			reason = fmt.Sprintf(": %s", reason)
		}
		// TODO: encode fail time in the rc
		return fmt.Sprintf("deployment #%d failed %s ago%s%s", version, timeAt, reason, describePodSummaryInline(rc.Status.ReadyReplicas, rc.Status.Replicas, *rc.Spec.Replicas, false, restartCount))
	case appsv1.DeploymentStatusComplete:
		// TODO: pod status output
		if test {
			return fmt.Sprintf("test deployment #%d deployed %s ago", version, timeAt)
		}
		return fmt.Sprintf("deployment #%d deployed %s ago%s", version, timeAt, describePodSummaryInline(rc.Status.ReadyReplicas, rc.Status.Replicas, *rc.Spec.Replicas, first, restartCount))
	case appsv1.DeploymentStatusRunning:
		format := "deployment #%d running%s for %s%s"
		if test {
			format = "test deployment #%d running%s for %s%s"
		}
		return fmt.Sprintf(format, version, maybeCancelling, timeAt, describePodSummaryInline(rc.Status.ReadyReplicas, rc.Status.Replicas, *rc.Spec.Replicas, false, restartCount))
	default:
		return fmt.Sprintf("deployment #%d %s%s %s ago%s", version, strings.ToLower(string(status)), maybeCancelling, timeAt, describePodSummaryInline(rc.Status.ReadyReplicas, rc.Status.Replicas, *rc.Spec.Replicas, false, restartCount))
	}
}

func describeDeploymentConfigRolloutStatus(d *kappsv1.Deployment) string {
	timeAt := strings.ToLower(FormatRelativeTime(d.CreationTimestamp.Time))
	return fmt.Sprintf("created %s ago%s", timeAt, describePodSummaryInline(int32(d.Status.Replicas), int32(d.Status.Replicas), *d.Spec.Replicas, false, 0))
}

func describeStatefulSetStatus(p *kappsv1.StatefulSet) string {
	timeAt := strings.ToLower(FormatRelativeTime(p.CreationTimestamp.Time))
	// TODO: Replace first argument in describePodSummaryInline with ReadyReplicas once that's a thing for pet sets.
	return fmt.Sprintf("created %s ago%s", timeAt, describePodSummaryInline(int32(p.Status.Replicas), int32(p.Status.Replicas), *p.Spec.Replicas, false, 0))
}

func describeDaemonSetStatus(ds *kappsv1.DaemonSet) string {
	timeAt := strings.ToLower(FormatRelativeTime(ds.CreationTimestamp.Time))
	replicaSetRevision := ds.Generation
	return fmt.Sprintf("generation #%d running for %s%s", replicaSetRevision, timeAt, describePodSummaryInline(ds.Status.NumberReady, ds.Status.NumberAvailable, ds.Status.DesiredNumberScheduled, false, 0))
}

func describeRCStatus(rc *corev1.ReplicationController) string {
	timeAt := strings.ToLower(FormatRelativeTime(rc.CreationTimestamp.Time))
	return fmt.Sprintf("rc/%s created %s ago%s", rc.Name, timeAt, describePodSummaryInline(rc.Status.ReadyReplicas, rc.Status.Replicas, *rc.Spec.Replicas, false, 0))
}

func describeRSStatus(rs *kappsv1.ReplicaSet) string {
	timeAt := strings.ToLower(FormatRelativeTime(rs.CreationTimestamp.Time))
	return fmt.Sprintf("rs/%s created %s ago%s", rs.Name, timeAt, describePodSummaryInline(rs.Status.ReadyReplicas, rs.Status.Replicas, *rs.Spec.Replicas, false, 0))
}

func describePodSummaryInline(ready, actual, requested int32, includeEmpty bool, restartCount int32) string {
	s := describePodSummary(ready, requested, includeEmpty, restartCount)
	if len(s) == 0 {
		return s
	}
	change := ""
	switch {
	case requested < actual:
		change = fmt.Sprintf(" reducing to %d", requested)
	case requested > actual:
		change = fmt.Sprintf(" growing to %d", requested)
	}
	return fmt.Sprintf(" - %s%s", s, change)
}

func describePodSummary(ready, requested int32, includeEmpty bool, restartCount int32) string {
	var restartWarn string
	if restartCount > 0 {
		restartWarn = fmt.Sprintf(" (warning: %d restarts)", restartCount)
	}
	if ready == requested {
		switch {
		case ready == 0:
			if !includeEmpty {
				return ""
			}
			return "0 pods"
		case ready > 1:
			return fmt.Sprintf("%d pods", ready) + restartWarn
		default:
			return "1 pod" + restartWarn
		}
	}
	return fmt.Sprintf("%d/%d pods", ready, requested) + restartWarn
}

func describeDeploymentConfigTriggers(config *appsv1.DeploymentConfig) (string, bool) {
	hasConfig, hasImage := false, false
	for _, t := range config.Spec.Triggers {
		switch t.Type {
		case appsv1.DeploymentTriggerOnConfigChange:
			hasConfig = true
		case appsv1.DeploymentTriggerOnImageChange:
			hasImage = true
		}
	}
	switch {
	case hasConfig && hasImage:
		return "on image or update", true
	case hasConfig:
		return "on update", true
	case hasImage:
		return "on image", true
	default:
		return "for manual", false
	}
}

func describeServiceInServiceGroup(f formatter, svc graphview.ServiceGroup, exposed ...string) []string {
	spec := svc.Service.Spec
	ip := spec.ClusterIP
	externalName := spec.ExternalName
	port := describeServicePorts(spec)
	switch {
	case len(exposed) > 1:
		return append([]string{fmt.Sprintf("%s (%s)", exposed[0], f.ResourceName(svc.Service))}, exposed[1:]...)
	case len(exposed) == 1:
		return []string{fmt.Sprintf("%s (%s)", exposed[0], f.ResourceName(svc.Service))}
	case spec.Type == corev1.ServiceTypeNodePort:
		return []string{fmt.Sprintf("%s (all nodes)%s", f.ResourceName(svc.Service), port)}
	case ip == "None":
		return []string{fmt.Sprintf("%s (headless)%s", f.ResourceName(svc.Service), port)}
	case len(ip) == 0 && len(externalName) == 0:
		return []string{fmt.Sprintf("%s <initializing>%s", f.ResourceName(svc.Service), port)}
	case len(ip) == 0:
		return []string{fmt.Sprintf("%s - %s", f.ResourceName(svc.Service), externalName)}
	default:
		return []string{fmt.Sprintf("%s - %s%s", f.ResourceName(svc.Service), ip, port)}
	}
}

func portOrNodePort(spec corev1.ServiceSpec, port corev1.ServicePort) string {
	switch {
	case spec.Type != corev1.ServiceTypeNodePort:
		return strconv.Itoa(int(port.Port))
	case port.NodePort == 0:
		return "<initializing>"
	default:
		return strconv.Itoa(int(port.NodePort))
	}
}

func describeServicePorts(spec corev1.ServiceSpec) string {
	switch len(spec.Ports) {
	case 0:
		return " no ports"

	case 1:
		port := portOrNodePort(spec, spec.Ports[0])
		if spec.Ports[0].TargetPort.String() == "0" || spec.ClusterIP == corev1.ClusterIPNone || port == spec.Ports[0].TargetPort.String() {
			return fmt.Sprintf(":%s", port)
		}
		return fmt.Sprintf(":%s -> %s", port, spec.Ports[0].TargetPort.String())

	default:
		pairs := []string{}
		for _, port := range spec.Ports {
			externalPort := portOrNodePort(spec, port)
			if port.TargetPort.String() == "0" || spec.ClusterIP == corev1.ClusterIPNone {
				pairs = append(pairs, externalPort)
				continue
			}
			if port.Port == port.TargetPort.IntVal {
				pairs = append(pairs, port.TargetPort.String())
			} else {
				pairs = append(pairs, fmt.Sprintf("%s->%s", externalPort, port.TargetPort.String()))
			}
		}
		return " ports " + strings.Join(pairs, ", ")
	}
}

func filterBoringPods(pods []graphview.Pod) ([]graphview.Pod, error) {
	monopods := []graphview.Pod{}

	for _, pod := range pods {
		actualPod, ok := pod.Pod.Object().(*corev1.Pod)
		if !ok {
			continue
		}
		meta, err := meta.Accessor(actualPod)
		if err != nil {
			return nil, err
		}
		_, isDeployerPod := meta.GetLabels()[appsv1.DeployerPodForDeploymentLabel]
		_, isBuilderPod := meta.GetAnnotations()[buildv1.BuildAnnotation]
		isFinished := actualPod.Status.Phase == corev1.PodSucceeded || actualPod.Status.Phase == corev1.PodFailed
		if isDeployerPod || isBuilderPod || isFinished {
			continue
		}
		monopods = append(monopods, pod)
	}

	return monopods, nil
}

// GraphLoader is a stateful interface that provides methods for building the nodes of a graph
type GraphLoader interface {
	// Load is responsible for gathering and saving the objects this GraphLoader should AddToGraph
	Load() error
	// AddToGraph
	AddToGraph(g osgraph.Graph) error
}

type rcLoader struct {
	namespace string
	lister    corev1client.ReplicationControllersGetter
	items     []corev1.ReplicationController
}

func (l *rcLoader) Load() error {
	list, err := l.lister.ReplicationControllers(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *rcLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		kubegraph.EnsureReplicationControllerNode(g, &l.items[i])
	}

	return nil
}

type serviceLoader struct {
	namespace string
	lister    corev1client.ServicesGetter
	items     []corev1.Service
}

func (l *serviceLoader) Load() error {
	list, err := l.lister.Services(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *serviceLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		kubegraph.EnsureServiceNode(g, &l.items[i])
	}

	return nil
}

type podLoader struct {
	namespace string
	lister    corev1client.PodsGetter
	items     []corev1.Pod
}

func (l *podLoader) Load() error {
	list, err := l.lister.Pods(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *podLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		kubegraph.EnsurePodNode(g, &l.items[i])
	}

	return nil
}

type jobLoader struct {
	namespace string
	lister    batchv1client.JobsGetter
	items     []batchv1.Job
}

func (l *jobLoader) Load() error {
	list, err := l.lister.Jobs(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *jobLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		kubegraph.EnsureJobNode(g, &l.items[i])
	}

	return nil
}

type statefulSetLoader struct {
	namespace string
	lister    kappsv1client.StatefulSetsGetter
	items     []kappsv1.StatefulSet
}

func (l *statefulSetLoader) Load() error {
	list, err := l.lister.StatefulSets(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *statefulSetLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		kubegraph.EnsureStatefulSetNode(g, &l.items[i])
	}

	return nil
}

type horizontalPodAutoscalerLoader struct {
	namespace string
	lister    autoscalingv1client.HorizontalPodAutoscalersGetter
	items     []autoscalingv1.HorizontalPodAutoscaler
}

func (l *horizontalPodAutoscalerLoader) Load() error {
	list, err := l.lister.HorizontalPodAutoscalers(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *horizontalPodAutoscalerLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		kubegraph.EnsureHorizontalPodAutoscalerNode(g, &l.items[i])
	}

	return nil
}

type deploymentLoader struct {
	namespace string
	lister    kappsv1client.DeploymentsGetter
	items     []kappsv1.Deployment
}

func (l *deploymentLoader) Load() error {
	list, err := l.lister.Deployments(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *deploymentLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		kubegraph.EnsureDeploymentNode(g, &l.items[i])
	}

	return nil
}

type daemonsetLoader struct {
	namespace string
	lister    kappsv1client.DaemonSetsGetter
	items     []kappsv1.DaemonSet
}

func (l *daemonsetLoader) Load() error {
	list, err := l.lister.DaemonSets(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *daemonsetLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		kubegraph.EnsureDaemonSetNode(g, &l.items[i])
	}

	return nil
}

type replicasetLoader struct {
	namespace string
	lister    kappsv1client.ReplicaSetsGetter
	items     []kappsv1.ReplicaSet
}

func (l *replicasetLoader) Load() error {
	list, err := l.lister.ReplicaSets(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *replicasetLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		kubegraph.EnsureReplicaSetNode(g, &l.items[i])
	}

	return nil
}

type serviceAccountLoader struct {
	namespace string
	lister    corev1client.ServiceAccountsGetter
	items     []corev1.ServiceAccount
}

func (l *serviceAccountLoader) Load() error {
	list, err := l.lister.ServiceAccounts(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *serviceAccountLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		kubegraph.EnsureServiceAccountNode(g, &l.items[i])
	}

	return nil
}

type secretLoader struct {
	namespace string
	lister    corev1client.SecretsGetter
	items     []corev1.Secret
}

func (l *secretLoader) Load() error {
	list, err := l.lister.Secrets(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *secretLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		kubegraph.EnsureSecretNode(g, &l.items[i])
	}

	return nil
}

type pvcLoader struct {
	namespace string
	lister    corev1client.PersistentVolumeClaimsGetter
	items     []corev1.PersistentVolumeClaim
}

func (l *pvcLoader) Load() error {
	list, err := l.lister.PersistentVolumeClaims(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *pvcLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		kubegraph.EnsurePersistentVolumeClaimNode(g, &l.items[i])
	}

	return nil
}

type isLoader struct {
	namespace string
	lister    imagev1client.ImageStreamsGetter
	items     []imagev1.ImageStream
}

func (l *isLoader) Load() error {
	list, err := l.lister.ImageStreams(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *isLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		imagegraph.EnsureImageStreamNode(g, &l.items[i])
		imagegraph.EnsureAllImageStreamTagNodes(g, &l.items[i])
	}

	return nil
}

type dcLoader struct {
	namespace string
	lister    appsv1client.DeploymentConfigsGetter
	items     []appsv1.DeploymentConfig
}

func (l *dcLoader) Load() error {
	list, err := l.lister.DeploymentConfigs(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *dcLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		appsgraph.EnsureDeploymentConfigNode(g, &l.items[i])
	}

	return nil
}

type bcLoader struct {
	namespace string
	lister    buildv1client.BuildConfigsGetter
	items     []buildv1.BuildConfig
}

func (l *bcLoader) Load() error {
	list, err := l.lister.BuildConfigs(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *bcLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		buildgraph.EnsureBuildConfigNode(g, &l.items[i])
	}

	return nil
}

type buildLoader struct {
	namespace string
	lister    buildv1client.BuildsGetter
	items     []buildv1.Build
}

func (l *buildLoader) Load() error {
	list, err := l.lister.Builds(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	l.items = list.Items
	return nil
}

func (l *buildLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		buildgraph.EnsureBuildNode(g, &l.items[i])
	}

	return nil
}

type routeLoader struct {
	namespace string
	lister    routev1client.RoutesGetter
	items     []routev1.Route
}

func (l *routeLoader) Load() error {
	list, err := l.lister.Routes(l.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	l.items = list.Items
	return nil
}

func (l *routeLoader) AddToGraph(g osgraph.Graph) error {
	for i := range l.items {
		routegraph.EnsureRouteNode(g, &l.items[i])
	}

	return nil
}
