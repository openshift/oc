// Package status displays the status of current cluster version updates.
package status

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"

	configv1 "github.com/openshift/api/config/v1"
	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	machineconfigv1client "github.com/openshift/client-go/machineconfiguration/clientset/versioned"
	machineconfigconst "github.com/openshift/machine-config-operator/pkg/daemon/constants"
)

const (
	// clusterStatusFailing is set on the ClusterVersion status when a cluster
	// cannot reach the desired state.
	clusterStatusFailing = configv1.ClusterStatusConditionType("Failing")
)

func newOptions(streams genericiooptions.IOStreams) *options {
	return &options{
		IOStreams: streams,
	}
}

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

	return cmd
}

type options struct {
	genericiooptions.IOStreams

	mockData mockData

	Client              configv1client.Interface
	CoreClient          corev1client.CoreV1Interface
	MachineConfigClient machineconfigv1client.Interface
}

func (o *options) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return kcmdutil.UsageErrorf(cmd, "positional arguments given")
	}

	cvSuffix := "-cv.yaml"
	if o.mockData.cvPath != "" {
		o.mockData.operatorsPath = strings.Replace(o.mockData.cvPath, cvSuffix, "-co.yaml", 1)
		o.mockData.machineConfigPoolsPath = strings.Replace(o.mockData.cvPath, cvSuffix, "-mcp.yaml", 1)
		o.mockData.machineConfigsPath = strings.Replace(o.mockData.cvPath, cvSuffix, "-mc.yaml", 1)
		o.mockData.nodesPath = strings.Replace(o.mockData.cvPath, cvSuffix, "-node.yaml", 1)
	}

	if o.mockData.cvPath == "" {
		cfg, err := f.ToRESTConfig()
		if err != nil {
			return err
		}
		client, err := configv1client.NewForConfig(cfg)
		if err != nil {
			return err
		}
		o.Client = client
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
		cv, err = o.Client.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("no cluster version information available - you must be connected to an OpenShift version 4 server to fetch the current version")
			}
			return err
		}
	} else {
		// mock "now" to be the latest time when something happened in the mocked data
		now = time.Time{}
		for _, condition := range cv.Status.Conditions {
			if condition.LastTransitionTime.After(now) {
				now = condition.LastTransitionTime.Time
			}
		}
	}

	var operators *configv1.ClusterOperatorList
	if operators = o.mockData.clusterOperators; operators == nil {
		var err error
		operators, err = o.Client.ConfigV1().ClusterOperators().List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
	} else {
		// mock "now" to be the latest time when something happened in the mocked data
		for _, co := range operators.Items {
			for _, condition := range co.Status.Conditions {
				if condition.LastTransitionTime.After(now) {
					now = condition.LastTransitionTime.Time
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
			for _, key := range []string{machineconfigconst.CurrentMachineConfigAnnotationKey, machineconfigconst.DesiredMachineConfigAnnotationKey} {
				mc, err := getMachineConfig(ctx, o.MachineConfigClient, machineConfigs.Items, node.Annotations[key])
				if err != nil {
					return err
				}
				if mc != nil {
					machineConfigs.Items = append(machineConfigs.Items, *mc)
				}
			}
		}
	}

	_, workerPools := separateMasterAndWorkerPools(pools)
	var updateInsights []updateInsight
	var workerPoolsStatusData []poolDisplayData
	for _, pool := range workerPools {
		nodes, err := selectNodesFromPool(pool, allNodes.Items)
		if err != nil {
			return err
		}
		nodesStatusData, insights := assessNodesStatus(cv, pool, nodes, machineConfigs.Items)
		updateInsights = append(updateInsights, insights...)
		poolStatus, insights := assessMachineConfigPool(pool, nodesStatusData)
		updateInsights = append(updateInsights, insights...)
		workerPoolsStatusData = append(workerPoolsStatusData, poolStatus)
	}

	var isWorkerPoolOutdated bool
	for _, pool := range workerPoolsStatusData {
		if pool.Completion != 100 {
			isWorkerPoolOutdated = true
			break
		}
	}

	if progressing.Status != configv1.ConditionTrue && !isWorkerPoolOutdated {
		var reason, message string
		if reason = progressing.Reason; reason == "" {
			reason = "<none>"
		}
		if message = progressing.Message; message == "" {
			message = "<none>"
		}
		fmt.Fprintf(o.Out, "The cluster version is not updating (%s=%s).\n\n  Reason: %s\n  Message: %s\n", progressing.Type, progressing.Status, reason, strings.ReplaceAll(message, "\n", "\n  "))
		return nil
	}

	updatingFor := now.Sub(progressing.LastTransitionTime.Time).Round(time.Second)
	fmt.Fprintf(o.Out, "An update is in progress for %s: %s\n", updatingFor, progressing.Message)

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, clusterStatusFailing); c != nil {
		if c.Status != configv1.ConditionFalse {
			fmt.Fprintf(o.Out, "\n%s=%s:\n\n  Reason: %s\n  Message: %s\n\n", c.Type, c.Status, c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
		}
	} else {
		fmt.Fprintf(o.ErrOut, "warning: No current %s info, see `oc describe clusterversion` for more details.\n", clusterStatusFailing)
	}

	controlPlaneStatusData, insights := assessControlPlaneStatus(cv, operators.Items, now)
	updateInsights = append(updateInsights, insights...)
	fmt.Fprintf(o.Out, "\n")
	_ = controlPlaneStatusData.Write(o.Out)

	fmt.Fprintf(o.Out, "\n= Worker Upgrade =\n")
	for _, pool := range workerPoolsStatusData {
		fmt.Fprintf(o.Out, "\n")
		_ = pool.WritePool(o.Out)
		fmt.Fprintf(o.Out, "\n")
		pool.WriteNodes(o.Out)
	}

	fmt.Fprintf(o.Out, "\n")
	upgradeHealth := assessUpdateInsights(updateInsights, updatingFor, now)
	_ = upgradeHealth.Write(o.Out)
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
