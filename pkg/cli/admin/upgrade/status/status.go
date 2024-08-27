// Package status displays the status of current cluster version updates.
package status

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	// "sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"

	configv1 "github.com/openshift/api/config/v1"
	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
	routev1 "github.com/openshift/api/route/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	machineconfigv1client "github.com/openshift/client-go/machineconfiguration/clientset/versioned"
	routev1client "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	"github.com/openshift/oc/pkg/cli/admin/inspectalerts"
	"github.com/openshift/oc/pkg/cli/admin/upgrade/status/mco"
)

func newOptions(streams genericiooptions.IOStreams) *options {
	return &options{
		IOStreams: streams,
	}
}

const (
	detailedOutputNone   = "none"
	detailedOutputAll    = "all"
	detailedOutputNodes  = "nodes"
	detailedOutputHealth = "health"
)

var detailedOutputAllValues = []string{detailedOutputNone, detailedOutputAll, detailedOutputNodes, detailedOutputHealth}

func New(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := newOptions(streams)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Display the status of current cluster version updates.",
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run(cmd.Context()))
		},
	}

	flags := cmd.Flags()
	// TODO: We can remove these flags once the idea about `oc adm upgrade status` stabilizes and the command
	//       is promoted out of the OC_ENABLE_CMD_UPGRADE_STATUS feature gate
	flags.StringVar(&o.mockData.cvPath, "mock-clusterversion", "", "Path to a YAML ClusterVersion object to use for testing (will be removed later). Files in the same directory with the same name and suffixes -co.yaml, -mcp.yaml, -mc.yaml, and -node.yaml are required.")
	flags.StringVar(&o.detailedOutput, "details", "none", fmt.Sprintf("Show detailed output in selected section. One of: %s", strings.Join(detailedOutputAllValues, ", ")))

	return cmd
}

type options struct {
	genericiooptions.IOStreams

	mockData       mockData
	detailedOutput string

	ConfigClient        configv1client.Interface
	CoreClient          corev1client.CoreV1Interface
	MachineConfigClient machineconfigv1client.Interface
	RouteClient         routev1client.RouteV1Interface
	getAlerts           func(ctx context.Context) ([]byte, error)
}

func (o *options) enabledDetailed(what string) bool {
	return o.detailedOutput == detailedOutputAll || o.detailedOutput == what
}

func (o *options) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return kcmdutil.UsageErrorf(cmd, "positional arguments given")
	}

	if !sets.New[string](detailedOutputAllValues...).Has(o.detailedOutput) {
		return fmt.Errorf("invalid value for --details: %s (must be one of %s)", o.detailedOutput, strings.Join(detailedOutputAllValues, ", "))
	}

	cvSuffix := "-cv.yaml"
	if o.mockData.cvPath != "" {
		o.mockData.operatorsPath = strings.Replace(o.mockData.cvPath, cvSuffix, "-co.yaml", 1)
		o.mockData.machineConfigPoolsPath = strings.Replace(o.mockData.cvPath, cvSuffix, "-mcp.yaml", 1)
		o.mockData.machineConfigsPath = strings.Replace(o.mockData.cvPath, cvSuffix, "-mc.yaml", 1)
		o.mockData.nodesPath = strings.Replace(o.mockData.cvPath, cvSuffix, "-node.yaml", 1)
		o.mockData.alertsPath = strings.Replace(o.mockData.cvPath, cvSuffix, "-alerts.json", 1)
	}

	if o.mockData.cvPath == "" {
		cfg, err := f.ToRESTConfig()
		if err != nil {
			return err
		}
		configClient, err := configv1client.NewForConfig(cfg)
		if err != nil {
			return err
		}
		o.ConfigClient = configClient
		machineConfigClient, err := machineconfigv1client.NewForConfig(cfg)
		if err != nil {
			return err
		}
		o.MachineConfigClient = machineConfigClient
		coreClient, err := corev1client.NewForConfig(cfg)
		if err != nil {
			return err
		}
		o.CoreClient = coreClient

		routeClient, err := routev1client.NewForConfig(cfg)
		if err != nil {
			return err
		}
		o.RouteClient = routeClient

		routeGetter := func(ctx context.Context, namespace string, name string, opts metav1.GetOptions) (*routev1.Route, error) {
			return routeClient.Routes(namespace).Get(ctx, name, opts)
		}
		o.getAlerts = func(ctx context.Context) ([]byte, error) {
			return inspectalerts.GetAlerts(ctx, routeGetter, cfg.BearerToken)
		}
	} else {
		err := o.mockData.load()
		if err != nil {
			return err
		}
	}

	return nil
}

func (o *options) Run(ctx context.Context) error {
	var cv *configv1.ClusterVersion
	now := time.Now()
	if cv = o.mockData.clusterVersion; cv == nil {
		var err error
		cv, err = o.ConfigClient.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("no cluster version information available - you must be connected to an OpenShift version 4 server to fetch the current version")
			}
			return err
		}
	} else {
		// mock "now" to be the latest time when something happened in the mocked data
		// add some nanoseconds to exercise rounding
		now = time.Time{}
		for _, condition := range cv.Status.Conditions {
			if condition.LastTransitionTime.After(now) {
				now = condition.LastTransitionTime.Time.Add(368975 * time.Nanosecond)
			}
		}
	}

	var operators *configv1.ClusterOperatorList
	if operators = o.mockData.clusterOperators; operators == nil {
		var err error
		operators, err = o.ConfigClient.ConfigV1().ClusterOperators().List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
	} else {
		// mock "now" to be the latest time when something happened in the mocked data
		for _, co := range operators.Items {
			for _, condition := range co.Status.Conditions {
				if condition.LastTransitionTime.After(now) {
					now = condition.LastTransitionTime.Time.Add(368975 * time.Nanosecond)
				}
			}
		}
	}
	if len(operators.Items) == 0 {
		return fmt.Errorf("no cluster operator information available - you must be connected to an OpenShift version 4 server")
	}

	progressing := findClusterOperatorStatusCondition(cv.Status.Conditions, configv1.OperatorProgressing)
	if progressing == nil {
		return fmt.Errorf("no current %s info, see `oc describe clusterversion` for more details.\n", configv1.OperatorProgressing)
	}

	var pools *machineconfigv1.MachineConfigPoolList
	if pools = o.mockData.machineConfigPools; pools == nil {
		var err error
		pools, err = o.MachineConfigClient.MachineconfigurationV1().MachineConfigPools().List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
	}
	var allNodes *corev1.NodeList
	if allNodes = o.mockData.nodes; allNodes == nil {
		var err error
		allNodes, err = o.CoreClient.Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
	}
	var machineConfigs *machineconfigv1.MachineConfigList
	if machineConfigs = o.mockData.machineConfigs; machineConfigs == nil {
		machineConfigs = &machineconfigv1.MachineConfigList{}
		for _, node := range allNodes.Items {
			for _, key := range []string{mco.CurrentMachineConfigAnnotationKey, mco.DesiredMachineConfigAnnotationKey} {
				machineConfigName, ok := node.Annotations[key]
				if !ok || machineConfigName == "" {
					continue
				}
				mc, err := getMachineConfig(ctx, o.MachineConfigClient, machineConfigs.Items, machineConfigName)
				if err != nil {
					return err
				}
				if mc != nil {
					machineConfigs.Items = append(machineConfigs.Items, *mc)
				}
			}
		}
	}

	var masterSelector labels.Selector
	var workerSelector labels.Selector
	customSelectors := map[string]labels.Selector{}
	for _, pool := range pools.Items {
		s, err := metav1.LabelSelectorAsSelector(pool.Spec.NodeSelector)
		if err != nil {
			return fmt.Errorf("failed to get label selector from the pool: %s", pool.Name)
		}
		switch pool.Name {
		case mco.MachineConfigPoolMaster:
			masterSelector = s
		case mco.MachineConfigPoolWorker:
			workerSelector = s
		default:
			customSelectors[pool.Name] = s
		}
	}

	nodesPerPool := map[string][]corev1.Node{}
	for _, node := range allNodes.Items {
		name := whichPool(masterSelector, workerSelector, customSelectors, node)
		nodesPerPool[name] = append(nodesPerPool[name], node)
	}

	var updateInsights []updateInsight
	var workerPoolsStatusData []poolDisplayData
	var controlPlanePoolStatusData poolDisplayData
	for _, pool := range pools.Items {
		nodesStatusData, insights := assessNodesStatus(cv, pool, nodesPerPool[pool.Name], machineConfigs.Items)
		updateInsights = append(updateInsights, insights...)
		poolStatus, insights := assessMachineConfigPool(pool, nodesStatusData)
		updateInsights = append(updateInsights, insights...)
		if poolStatus.Name == mco.MachineConfigPoolMaster {
			controlPlanePoolStatusData = poolStatus
		} else {
			workerPoolsStatusData = append(workerPoolsStatusData, poolStatus)
		}
	}

	var isWorkerPoolOutdated bool
	for _, pool := range workerPoolsStatusData {
		if pool.NodesOverview.Total > 0 && pool.Completion != 100 {
			isWorkerPoolOutdated = true
			break
		}
	}

	if progressing.Status != configv1.ConditionTrue && !isWorkerPoolOutdated {
		fmt.Fprintf(o.Out, "The cluster is not updating.\n")
		return nil
	}

	startedAt := progressing.LastTransitionTime.Time
	if len(cv.Status.History) > 0 {
		startedAt = cv.Status.History[0].StartedTime.Time
	}
	updatingFor := now.Sub(startedAt).Round(time.Second)

	// get the alerts for the cluster. if we're unable to fetch the alerts, we'll let the user know that alerts
	// are not being fetched, but rest of the command should work.
	var alertData AlertData
	var alertBytes []byte
	var err error
	if ap := o.mockData.alertsPath; ap != "" {
		alertBytes, err = os.ReadFile(o.mockData.alertsPath)
	} else {
		alertBytes, err = o.getAlerts(ctx)
	}
	if err != nil {
		fmt.Println("Unable to fetch alerts, ignoring alerts in 'Update Health': ", err)
	} else {
		// Unmarshal the JSON data into the struct
		if err := json.Unmarshal(alertBytes, &alertData); err != nil {
			fmt.Println("Ignoring alerts in 'Update Health'. Error unmarshaling alerts: %w", err)
		}
		updateInsights = append(updateInsights, parseAlertDataToInsights(alertData, startedAt)...)
	}

	controlPlaneStatusData, insights := assessControlPlaneStatus(cv, operators.Items, now)
	updateInsights = append(updateInsights, insights...)
	_ = controlPlaneStatusData.Write(o.Out)
	controlPlanePoolStatusData.WriteNodes(o.Out, o.enabledDetailed(detailedOutputNodes))

	var workerUpgrade bool
	for _, d := range workerPoolsStatusData {
		if len(d.Nodes) > 0 {
			workerUpgrade = true
			break
		}
	}

	if workerUpgrade {
		fmt.Fprintf(o.Out, "\n= Worker Upgrade =\n")
		writePools(o.Out, workerPoolsStatusData)
		for _, pool := range workerPoolsStatusData {
			pool.WriteNodes(o.Out, o.enabledDetailed(detailedOutputNodes))
		}
	}

	fmt.Fprintf(o.Out, "\n")
	upgradeHealth, allowDetailed := assessUpdateInsights(updateInsights, updatingFor, now)
	_ = upgradeHealth.Write(o.Out, allowDetailed && o.enabledDetailed(detailedOutputHealth))
	return nil
}

func findClusterOperatorStatusCondition(conditions []configv1.ClusterOperatorStatusCondition, name configv1.ClusterStatusConditionType) *configv1.ClusterOperatorStatusCondition {
	for i := range conditions {
		if conditions[i].Type == name {
			return &conditions[i]
		}
	}
	return nil
}
