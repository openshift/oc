// Package status displays the status of current cluster version updates.
package status

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openshift/oc/pkg/cli/admin/upgrade/status/mco"
	"github.com/spf13/cobra"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"

	updatev1alpha1 "github.com/openshift/api/update/v1alpha1"
	updatev1alpha1client "github.com/openshift/client-go/update/clientset/versioned"
)

func newOptions(streams genericiooptions.IOStreams) *options {
	return &options{
		IOStreams: streams,
	}
}

const (
	detailedOutputNone      = "none"
	detailedOutputAll       = "all"
	detailedOutputNodes     = "nodes"
	detailedOutputHealth    = "health"
	detailedOutputOperators = "operators"
)

var detailedOutputAllValues = []string{detailedOutputNone, detailedOutputAll, detailedOutputNodes, detailedOutputHealth, detailedOutputOperators}

func New(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := newOptions(streams)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Display the status of the current cluster version update or multi-arch migration",
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run(cmd.Context()))
		},
	}

	flags := cmd.Flags()
	// TODO: We can remove these flags once the idea about `oc adm upgrade status` stabilizes and the command
	//       is promoted out of the OC_ENABLE_CMD_UPGRADE_STATUS feature gate
	flags.StringVar(&o.mockData.updateStatusPath, "mock-updatestatus", "", "Path to a YAML UpdateStatus object to use for testing (will be removed later).")
	flags.StringVar(&o.detailedOutput, "details", "none", fmt.Sprintf("Show detailed output in selected section. One of: %s", strings.Join(detailedOutputAllValues, ", ")))

	return cmd
}

type insightProcessors []insightProcessor

func (i insightProcessors) process(us *updatev1alpha1.UpdateStatus) {
	for wi := range us.Status.WorkerPools {
		wp := &us.Status.WorkerPools[wi]
		for ii, informer := range wp.Informers {
			for in := range wp.Informers[ii].Insights {
				insight := &wp.Informers[ii].Insights[in]
				switch wp.Informers[ii].Insights[in].Type {
				case updatev1alpha1.MachineConfigPoolStatusInsightType:
					for ip := range i {
						i[ip].acceptMachineConfigPoolInsight(informer.Name, updatev1alpha1.WorkerPoolScope, insight.MachineConfigPoolStatusInsight)
					}
				case updatev1alpha1.NodeStatusInsightType:
					for ip := range i {
						i[ip].acceptNodeInsight(informer.Name, updatev1alpha1.WorkerPoolScope, insight.NodeStatusInsight)
					}
				case updatev1alpha1.HealthInsightType:
					for ip := range i {
						i[ip].acceptHealthInsight(informer.Name, updatev1alpha1.WorkerPoolScope, insight.HealthInsight)
					}
				}
			}
		}
	}
	for ii, informer := range us.Status.ControlPlane.Informers {
		for in := range us.Status.ControlPlane.Informers[ii].Insights {
			insight := &us.Status.ControlPlane.Informers[ii].Insights[in]
			switch us.Status.ControlPlane.Informers[ii].Insights[in].Type {
			case updatev1alpha1.ClusterVersionStatusInsightType:
				for ip := range i {
					i[ip].acceptClusterVersionStatusInsight(informer.Name, insight.ClusterVersionStatusInsight)
				}
			case updatev1alpha1.ClusterOperatorStatusInsightType:
				for ip := range i {
					i[ip].acceptClusterOperatorStatusInsight(informer.Name, insight.ClusterOperatorStatusInsight)
				}
			case updatev1alpha1.MachineConfigPoolStatusInsightType:
				for ip := range i {
					i[ip].acceptMachineConfigPoolInsight(informer.Name, updatev1alpha1.ControlPlaneScope, insight.MachineConfigPoolStatusInsight)
				}
			case updatev1alpha1.NodeStatusInsightType:
				for ip := range i {
					i[ip].acceptNodeInsight(informer.Name, updatev1alpha1.ControlPlaneScope, insight.NodeStatusInsight)
				}
			case updatev1alpha1.HealthInsightType:
				for ip := range i {
					i[ip].acceptHealthInsight(informer.Name, updatev1alpha1.ControlPlaneScope, insight.HealthInsight)
				}
			}
		}
	}
}

type insightProcessor interface {
	acceptClusterVersionStatusInsight(informer string, insight *updatev1alpha1.ClusterVersionStatusInsight)
	acceptClusterOperatorStatusInsight(informer string, insight *updatev1alpha1.ClusterOperatorStatusInsight)
	acceptMachineConfigPoolInsight(informer string, scope updatev1alpha1.ScopeType, insight *updatev1alpha1.MachineConfigPoolStatusInsight)
	acceptNodeInsight(informer string, scope updatev1alpha1.ScopeType, insight *updatev1alpha1.NodeStatusInsight)
	acceptHealthInsight(informer string, scope updatev1alpha1.ScopeType, insight *updatev1alpha1.HealthInsight)
}

type isUpdatingProcessor struct {
	controlPlane bool
	workerPools  bool
}

func (i *isUpdatingProcessor) acceptClusterVersionStatusInsight(_ string, insight *updatev1alpha1.ClusterVersionStatusInsight) {
	for _, condition := range insight.Conditions {
		if condition.Type == string(updatev1alpha1.ClusterVersionStatusInsightUpdating) {
			i.controlPlane = condition.Status == metav1.ConditionTrue
			break
		}
	}
}

func (i *isUpdatingProcessor) acceptClusterOperatorStatusInsight(_ string, _ *updatev1alpha1.ClusterOperatorStatusInsight) {
}

// TODO(muller): Add updatev1alpha1.MachineConfigPoolStatusInsightUpdating to API
// TODO(muller): Add values for updatev1alpha1.MachineConfigPoolStatusInsightUpdating to API

const (
	MachineConfigPoolStatusInsightUpdating                = "Updating"
	MachineConfigPoolStatusInsightUpdatingReasonCompleted = "Completed"
	MachineConfigPoolStatusInsightUpdatingReasonPending   = "Pending"
)

func (i *isUpdatingProcessor) acceptMachineConfigPoolInsight(_ string, scope updatev1alpha1.ScopeType, insight *updatev1alpha1.MachineConfigPoolStatusInsight) {
	var outdated bool
	for _, condition := range insight.Conditions {
		if condition.Type == MachineConfigPoolStatusInsightUpdating {
			updated := condition.Status == metav1.ConditionFalse && condition.Reason == MachineConfigPoolStatusInsightUpdatingReasonCompleted
			outdated = !updated
			break
		}
	}

	if scope == updatev1alpha1.ControlPlaneScope {
		i.controlPlane = i.controlPlane || outdated
	} else {
		i.workerPools = i.workerPools || outdated
	}
}

func (i *isUpdatingProcessor) acceptNodeInsight(_ string, _ updatev1alpha1.ScopeType, _ *updatev1alpha1.NodeStatusInsight) {
}

func (i *isUpdatingProcessor) acceptHealthInsight(_ string, _ updatev1alpha1.ScopeType, _ *updatev1alpha1.HealthInsight) {
}

type latestTimeFinder struct {
	latest time.Time
}

func (l *latestTimeFinder) update(maybe time.Time) {
	if maybe.After(l.latest) {
		l.latest = maybe
	}
}

func (l *latestTimeFinder) updateFromConditions(conditions []metav1.Condition) {
	for _, condition := range conditions {
		l.update(condition.LastTransitionTime.Time)
	}
}

func (l *latestTimeFinder) acceptClusterVersionStatusInsight(_ string, insight *updatev1alpha1.ClusterVersionStatusInsight) {
	l.update(insight.StartedAt.Time)
	if insight.CompletedAt != nil {
		l.update(insight.CompletedAt.Time)
	}
	l.updateFromConditions(insight.Conditions)
}

func (l *latestTimeFinder) acceptClusterOperatorStatusInsight(_ string, insight *updatev1alpha1.ClusterOperatorStatusInsight) {
	l.updateFromConditions(insight.Conditions)
}

func (l *latestTimeFinder) acceptMachineConfigPoolInsight(_ string, _ updatev1alpha1.ScopeType, insight *updatev1alpha1.MachineConfigPoolStatusInsight) {
	l.updateFromConditions(insight.Conditions)
}

func (l *latestTimeFinder) acceptNodeInsight(_ string, _ updatev1alpha1.ScopeType, insight *updatev1alpha1.NodeStatusInsight) {
	l.updateFromConditions(insight.Conditions)

}

func (l *latestTimeFinder) acceptHealthInsight(_ string, _ updatev1alpha1.ScopeType, insight *updatev1alpha1.HealthInsight) {
	l.update(insight.StartedAt.Time)
}

type options struct {
	genericiooptions.IOStreams

	mockData       mockData
	detailedOutput string

	UpdateClient updatev1alpha1client.Interface
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

	if o.mockData.updateStatusPath == "" {
		cfg, err := f.ToRESTConfig()
		if err != nil {
			return err
		}

		updateClient, err := updatev1alpha1client.NewForConfig(cfg)
		if err != nil {
			return err
		}
		o.UpdateClient = updateClient
	} else {
		err := o.mockData.load()
		if err != nil {
			return err
		}
	}

	return nil
}

func (o *options) Run(ctx context.Context) error {
	var us *updatev1alpha1.UpdateStatus
	now := time.Now

	var processors insightProcessors
	if us = o.mockData.updateStatus; us == nil {
		var err error
		us, err = o.UpdateClient.UpdateV1alpha1().UpdateStatuses().Get(ctx, "status-api-prototype", metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("no update status information available - you must be connected to an OpenShift version 4 server to fetch the current version")
			}
			return err
		}
	} else {
		// mock "now" to be the latest time when something happened in the mocked data
		// add some nanoseconds to exercise rounding
		latest := latestTimeFinder{}
		now = func() time.Time { return latest.latest.Add(368975 * time.Nanosecond) }
		processors = append(processors, &latest)
		processors.process(us)
	}

	var isUpdating isUpdatingProcessor
	processors = append(processors, &isUpdating)

	updateHealth := updateHealthData{evaluatedAt: now()}
	processors = append(processors, &updateHealth)

	controlPlaneStatusData := controlPlaneStatusDisplayData{now: now}
	processors = append(processors, &controlPlaneStatusData)

	var workerPoolsStatusData workerPoolsDisplayData
	processors = append(processors, &workerPoolsStatusData)

	var controlPlanePoolStatusData poolDisplayData
	controlPlanePoolStatusData.Name = mco.MachineConfigPoolMaster
	processors = append(processors, &controlPlanePoolStatusData)

	processors.process(us)

	if !(isUpdating.controlPlane || isUpdating.workerPools) {
		fmt.Fprintf(o.Out, "The cluster is not updating.\n")
		return nil
	}

	_ = controlPlaneStatusData.Write(o.Out, o.enabledDetailed(detailedOutputOperators), now())
	controlPlanePoolStatusData.WriteNodes(o.Out, o.enabledDetailed(detailedOutputNodes))
	//
	// TODO: Encapsulate this in a higher-level processor?
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

	_ = updateHealth.Write(o.Out, o.enabledDetailed(detailedOutputHealth))
	return nil
}
