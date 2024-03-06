package status

import (
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/google/go-cmp/cmp"
	configv1 "github.com/openshift/api/config/v1"
	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"
	"github.com/openshift/oc/pkg/cli/admin/upgrade/status/mco"
)

var allowUnexportedWorkerPools = cmp.AllowUnexported(nodeDisplayData{}, nodesOverviewDisplayData{}, poolDisplayData{})

type mcpBuilder struct {
	machineConfigPool mcfgv1.MachineConfigPool
}

func mcp(name string) *mcpBuilder {
	return &mcpBuilder{
		machineConfigPool: mcfgv1.MachineConfigPool{
			ObjectMeta: v1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					mcfgv1.KubeletConfigRoleLabelPrefix + name: "",
				},
			},
			Spec: mcfgv1.MachineConfigPoolSpec{
				NodeSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{
						fmt.Sprintf("node-role.kubernetes.io/%s", name): "",
					},
				},
			},
		},
	}
}

func (mcp *mcpBuilder) setMachineConfig(mcName string) *mcpBuilder {
	mcp.machineConfigPool.Spec.Configuration.Name = mcName
	return mcp
}

func (mcp *mcpBuilder) paused() *mcpBuilder {
	mcp.machineConfigPool.Spec.Paused = true
	return mcp
}

type mcBuilder struct {
	machineConfig mcfgv1.MachineConfig
}

func mc(name string) *mcBuilder {
	return &mcBuilder{
		machineConfig: mcfgv1.MachineConfig{
			ObjectMeta: v1.ObjectMeta{
				Name: name,
			},
		},
	}
}

func (mc *mcBuilder) version(version string) *mcBuilder {
	mc.machineConfig.Annotations = map[string]string{
		mco.ReleaseImageVersionAnnotationKey: version,
	}
	return mc
}

type nodeBuilder struct {
	node corev1.Node
}

func node(name string) *nodeBuilder {
	return &nodeBuilder{
		node: corev1.Node{
			ObjectMeta: v1.ObjectMeta{
				Name:        name,
				Annotations: map[string]string{},
				Labels:      map[string]string{},
			},
			Spec: corev1.NodeSpec{
				Unschedulable: false,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:               corev1.NodeReady,
						Status:             corev1.ConditionTrue,
						LastHeartbeatTime:  v1.NewTime(time.Now()),
						LastTransitionTime: v1.NewTime(time.Now()),
						Reason:             "KubeletReady",
						Message:            "kubelet is posting ready status",
					},
					{
						Type:               corev1.NodeMemoryPressure,
						Status:             corev1.ConditionFalse,
						LastHeartbeatTime:  v1.NewTime(time.Now()),
						LastTransitionTime: v1.NewTime(time.Now()),
						Reason:             "KubeletHasSufficientMemory",
						Message:            "kubelet has sufficient memory available",
					},
					{
						Type:               corev1.NodeDiskPressure,
						Status:             corev1.ConditionFalse,
						LastHeartbeatTime:  v1.NewTime(time.Now()),
						LastTransitionTime: v1.NewTime(time.Now()),
						Reason:             "KubeletHasNoDiskPressure",
						Message:            "kubelet has no disk pressure",
					},
					{
						Type:               corev1.NodePIDPressure,
						Status:             corev1.ConditionFalse,
						LastHeartbeatTime:  v1.NewTime(time.Now()),
						LastTransitionTime: v1.NewTime(time.Now()),
						Reason:             "KubeletHasSufficientPID",
						Message:            "kubelet has sufficient PID available",
					},
					{
						Type:               corev1.NodeNetworkUnavailable,
						Status:             corev1.ConditionFalse,
						LastHeartbeatTime:  v1.NewTime(time.Now()),
						LastTransitionTime: v1.NewTime(time.Now()),
						Reason:             "RouteCreated",
						Message:            "openshift-sdn cleared kubelet-set NoRouteCreated",
					},
				},
			},
		},
	}
}

func (n *nodeBuilder) annotated(annotations map[string]string) *nodeBuilder {
	n.node.Annotations = annotations
	return n
}

func (n *nodeBuilder) unavailable() *nodeBuilder {
	n.node.Spec.Unschedulable = true
	return n
}

func (n *nodeBuilder) pending(currentConfig, desiredConfig string) *nodeBuilder {
	annotations := map[string]string{
		"machineconfiguration.openshift.io/currentConfig":    currentConfig,
		"machineconfiguration.openshift.io/desiredConfig":    desiredConfig,
		"machineconfiguration.openshift.io/desiredDrain":     "uncordon-rendered-1",
		"machineconfiguration.openshift.io/lastAppliedDrain": "uncordon-rendered-1",
		"machineconfiguration.openshift.io/reason":           "",
		"machineconfiguration.openshift.io/state":            "Done",
	}
	return n.annotated(annotations)
}

func (n *nodeBuilder) progressing_draining(currentConfig, desiredConfig string) *nodeBuilder {
	annotations := map[string]string{
		"machineconfiguration.openshift.io/currentConfig":    currentConfig,
		"machineconfiguration.openshift.io/desiredConfig":    desiredConfig,
		"machineconfiguration.openshift.io/desiredDrain":     "drain-rendered-1",
		"machineconfiguration.openshift.io/lastAppliedDrain": "uncordon-rendered-1",
		"machineconfiguration.openshift.io/reason":           "",
		"machineconfiguration.openshift.io/state":            "Working",
	}
	return n.annotated(annotations).unavailable()
}

func (n *nodeBuilder) progressing_draining_unset_mcd_state(currentConfig, desiredConfig string) *nodeBuilder {
	// MCD did not have time to update its state in a node
	annotations := map[string]string{
		"machineconfiguration.openshift.io/currentConfig":    currentConfig,
		"machineconfiguration.openshift.io/desiredConfig":    desiredConfig,
		"machineconfiguration.openshift.io/desiredDrain":     "uncordon-rendered-1",
		"machineconfiguration.openshift.io/lastAppliedDrain": "uncordon-rendered-1",
		"machineconfiguration.openshift.io/reason":           "",
		"machineconfiguration.openshift.io/state":            "Done",
	}
	return n.annotated(annotations).unavailable()
}

func (n *nodeBuilder) progressing_updating(currentConfig, desiredConfig string) *nodeBuilder {
	annotations := map[string]string{
		"machineconfiguration.openshift.io/currentConfig":    currentConfig,
		"machineconfiguration.openshift.io/desiredConfig":    desiredConfig,
		"machineconfiguration.openshift.io/desiredDrain":     "drain-rendered-1",
		"machineconfiguration.openshift.io/lastAppliedDrain": "drain-rendered-1",
		"machineconfiguration.openshift.io/reason":           "",
		"machineconfiguration.openshift.io/state":            "Working",
	}
	return n.annotated(annotations).unavailable()
}

func (n *nodeBuilder) progressing_rebooting(currentConfig, desiredConfig string) *nodeBuilder {
	annotations := map[string]string{
		"machineconfiguration.openshift.io/currentConfig":    currentConfig,
		"machineconfiguration.openshift.io/desiredConfig":    desiredConfig,
		"machineconfiguration.openshift.io/desiredDrain":     "drain-rendered-1",
		"machineconfiguration.openshift.io/lastAppliedDrain": "drain-rendered-1",
		"machineconfiguration.openshift.io/reason":           "",
		"machineconfiguration.openshift.io/state":            "Rebooting",
	}
	return n.annotated(annotations).unavailable()
}

func (n *nodeBuilder) updated(currentConfig, desiredConfig string) *nodeBuilder {
	annotations := map[string]string{
		"machineconfiguration.openshift.io/currentConfig":    currentConfig,
		"machineconfiguration.openshift.io/desiredConfig":    desiredConfig,
		"machineconfiguration.openshift.io/desiredDrain":     "drain-rendered-1",
		"machineconfiguration.openshift.io/lastAppliedDrain": "drain-rendered-1",
		"machineconfiguration.openshift.io/reason":           "",
		"machineconfiguration.openshift.io/state":            "Done",
	}
	return n.annotated(annotations)
}

func (n *nodeBuilder) degraded_draining(currentConfig, desiredConfig, reason string) *nodeBuilder {
	annotations := map[string]string{
		"machineconfiguration.openshift.io/currentConfig":    currentConfig,
		"machineconfiguration.openshift.io/desiredConfig":    desiredConfig,
		"machineconfiguration.openshift.io/desiredDrain":     "drain-rendered-1",
		"machineconfiguration.openshift.io/lastAppliedDrain": "uncordon-rendered-1",
		"machineconfiguration.openshift.io/reason":           reason,
		"machineconfiguration.openshift.io/state":            "Degraded",
	}
	return n.annotated(annotations).unavailable()
}

func Test_assessNodesStatus(t *testing.T) {
	oldOCPVersion := "3.10.0"
	newOCPVersion := "4.16.0"
	mcOld := mc("old").version(oldOCPVersion).machineConfig
	mcNew := mc("new").version(newOCPVersion).machineConfig
	machineConfigs := []mcfgv1.MachineConfig{
		mcOld,
		mcNew,
	}
	mcpMaster := mcp("master").setMachineConfig("new").machineConfigPool
	cvUpdating := configv1.ClusterVersion{
		Status: configv1.ClusterVersionStatus{
			Desired: configv1.Release{Version: newOCPVersion},
			History: []configv1.UpdateHistory{
				{
					State:   configv1.PartialUpdate,
					Version: newOCPVersion,
				},
				{
					State:   configv1.CompletedUpdate,
					Version: oldOCPVersion,
				},
			},
		},
	}
	cvUpdated := configv1.ClusterVersion{
		Status: configv1.ClusterVersionStatus{
			Desired: configv1.Release{Version: newOCPVersion},
			History: []configv1.UpdateHistory{
				{
					State:   configv1.CompletedUpdate,
					Version: newOCPVersion,
				},
				{
					State:   configv1.CompletedUpdate,
					Version: oldOCPVersion,
				},
			},
		},
	}
	type args struct {
		cv             *configv1.ClusterVersion
		pool           mcfgv1.MachineConfigPool
		nodes          []corev1.Node
		machineConfigs []mcfgv1.MachineConfig
	}
	testCases := []struct {
		name                    string
		args                    args
		expectedNodeDisplayData []nodeDisplayData
		expectedUpdateInsight   []updateInsight
	}{
		{
			name: "node is pending - all is well",
			args: args{
				cv:   &cvUpdating,
				pool: mcpMaster,
				nodes: []corev1.Node{
					node("a").pending(mcOld.Name, mcOld.Name).node,
				},
				machineConfigs: machineConfigs,
			},
			expectedNodeDisplayData: []nodeDisplayData{
				{
					Name:          "a",
					Assessment:    nodeAssessmentOutdated,
					Phase:         phaseStatePending,
					Version:       oldOCPVersion,
					Estimate:      "?",
					Message:       "",
					isUnavailable: false,
					isDegraded:    false,
					isUpdating:    false,
					isUpdated:     false,
				},
			},
			expectedUpdateInsight: nil,
		},
		{
			name: "node is draining - all is well",
			args: args{
				cv:   &cvUpdating,
				pool: mcpMaster,
				nodes: []corev1.Node{
					node("a").progressing_draining(mcOld.Name, mcNew.Name).node,
				},
				machineConfigs: machineConfigs,
			},
			expectedNodeDisplayData: []nodeDisplayData{
				{
					Name:          "a",
					Assessment:    nodeAssessmentProgressing,
					Phase:         phaseStateDraining,
					Version:       oldOCPVersion,
					Estimate:      "+30m",
					Message:       "",
					isUnavailable: true,
					isDegraded:    false,
					isUpdating:    true,
					isUpdated:     false,
				},
			},
			expectedUpdateInsight: nil,
		},
		{
			name: "node is draining - MCD did not have time to update its state",
			args: args{
				cv:   &cvUpdating,
				pool: mcpMaster,
				nodes: []corev1.Node{
					node("a").progressing_draining_unset_mcd_state(mcOld.Name, mcNew.Name).node,
				},
				machineConfigs: machineConfigs,
			},
			expectedNodeDisplayData: []nodeDisplayData{
				{
					Name:          "a",
					Assessment:    nodeAssessmentProgressing,
					Phase:         phaseStateDraining,
					Version:       oldOCPVersion,
					Estimate:      "+30m",
					Message:       "",
					isUnavailable: true,
					isDegraded:    false,
					isUpdating:    true,
					isUpdated:     false,
				},
			},
			expectedUpdateInsight: nil,
		},
		{
			name: "node is updating - all is well",
			args: args{
				cv:   &cvUpdating,
				pool: mcpMaster,
				nodes: []corev1.Node{
					node("a").progressing_updating(mcOld.Name, mcNew.Name).node,
				},
				machineConfigs: machineConfigs,
			},
			expectedNodeDisplayData: []nodeDisplayData{
				{
					Name:          "a",
					Assessment:    nodeAssessmentProgressing,
					Phase:         phaseStateUpdating,
					Version:       oldOCPVersion,
					Estimate:      "+20m",
					Message:       "",
					isUnavailable: true,
					isDegraded:    false,
					isUpdating:    true,
					isUpdated:     false,
				},
			},
			expectedUpdateInsight: nil,
		},
		{
			name: "node is rebooting - all is well",
			args: args{
				cv:   &cvUpdating,
				pool: mcpMaster,
				nodes: []corev1.Node{
					node("a").progressing_rebooting(mcOld.Name, mcNew.Name).node,
				},
				machineConfigs: machineConfigs,
			},
			expectedNodeDisplayData: []nodeDisplayData{
				{
					Name:          "a",
					Assessment:    nodeAssessmentProgressing,
					Phase:         phaseStateRebooting,
					Version:       oldOCPVersion,
					Estimate:      "+10m",
					Message:       "",
					isUnavailable: true,
					isDegraded:    false,
					isUpdating:    true,
					isUpdated:     false,
				},
			},
			expectedUpdateInsight: nil,
		},
		{
			name: "node is updated - all is well",
			args: args{
				cv:   &cvUpdated,
				pool: mcpMaster,
				nodes: []corev1.Node{
					node("a").updated(mcNew.Name, mcNew.Name).node,
				},
				machineConfigs: machineConfigs,
			},
			expectedNodeDisplayData: []nodeDisplayData{
				{
					Name:          "a",
					Assessment:    nodeAssessmentCompleted,
					Phase:         phaseStateUpdated,
					Version:       newOCPVersion,
					Estimate:      "-",
					Message:       "",
					isUnavailable: false,
					isDegraded:    false,
					isUpdating:    false,
					isUpdated:     true,
				},
			},
			expectedUpdateInsight: nil,
		},
		{
			name: "node is degraded - pdb prohibits draining",
			args: args{
				cv:   &cvUpdated,
				pool: mcpMaster,
				nodes: []corev1.Node{
					node("a").degraded_draining(mcOld.Name, mcNew.Name, "PDB prohibits draining").node,
				},
				machineConfigs: machineConfigs,
			},
			expectedNodeDisplayData: []nodeDisplayData{
				{
					Name:          "a",
					Assessment:    nodeAssessmentDegraded,
					Phase:         phaseStateDraining,
					Version:       oldOCPVersion,
					Estimate:      "?",
					Message:       "PDB prohibits draining",
					isUnavailable: true,
					isDegraded:    true,
					isUpdating:    true,
					isUpdated:     false,
				},
			},
			expectedUpdateInsight: []updateInsight{
				{
					impact: updateInsightImpact{
						level:      errorImpactLevel,
						impactType: updateStalledImpactType,
						summary:    "Node a is degraded | PDB prohibits draining",
					},
					scope: updateInsightScope{
						scopeType: scopeTypeControlPlane,
						resources: []scopeResource{{kind: scopeKindNode, name: "a"}},
					},
				},
			},
		},
		{
			name: "node is updated but unavailable",
			args: args{
				cv:   &cvUpdated,
				pool: mcpMaster,
				nodes: []corev1.Node{
					node("a").updated(mcNew.Name, mcNew.Name).unavailable().node,
				},
				machineConfigs: machineConfigs,
			},
			expectedNodeDisplayData: []nodeDisplayData{
				{
					Name:          "a",
					Assessment:    nodeAssessmentUnavailable,
					Phase:         phaseStateUpdated,
					Version:       newOCPVersion,
					Estimate:      "-",
					Message:       "Node is unavailable",
					isUnavailable: true,
					isDegraded:    false,
					isUpdating:    false,
					isUpdated:     true,
				},
			},
			expectedUpdateInsight: []updateInsight{
				{
					impact: updateInsightImpact{
						level:      warningImpactLevel,
						impactType: updateSpeedImpactType,
						summary:    "Node a is unavailable | Node is unavailable",
					},
					scope: updateInsightScope{
						scopeType: scopeTypeControlPlane,
						resources: []scopeResource{{kind: scopeKindNode, name: "a"}},
					},
				},
			}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nodeDisplayData, updateInsight := assessNodesStatus(tc.args.cv, tc.args.pool, tc.args.nodes, tc.args.machineConfigs)
			if diff := cmp.Diff(tc.expectedNodeDisplayData, nodeDisplayData, allowUnexportedWorkerPools); diff != "" {
				t.Errorf("nodeDisplayData differ from expected:\n%s", diff)
			}

			if diff := cmp.Diff(tc.expectedUpdateInsight, updateInsight, allowUnexportedInsightStructs); diff != "" {
				t.Errorf("updateInsight differ from expected:\n%s", diff)
			}
		})
	}
}

func Test_assessNodesStatus_DisplayData_Sorting(t *testing.T) {
	oldOCPVersion := "3.10.0"
	newOCPVersion := "4.16.0"
	mcOld := mc("old").version(oldOCPVersion).machineConfig
	mcNew := mc("new").version(newOCPVersion).machineConfig
	machineConfigs := []mcfgv1.MachineConfig{
		mcOld,
		mcNew,
	}
	mcpMaster := mcp("master").setMachineConfig("new").machineConfigPool
	cvUpdating := configv1.ClusterVersion{
		Status: configv1.ClusterVersionStatus{
			Desired: configv1.Release{Version: newOCPVersion},
			History: []configv1.UpdateHistory{
				{
					State:   configv1.PartialUpdate,
					Version: newOCPVersion,
				},
				{
					State:   configv1.CompletedUpdate,
					Version: oldOCPVersion,
				},
			},
		},
	}
	type args struct {
		cv             *configv1.ClusterVersion
		pool           mcfgv1.MachineConfigPool
		nodes          []corev1.Node
		machineConfigs []mcfgv1.MachineConfig
	}
	testCases := []struct {
		name                    string
		args                    args
		expectedNodeDisplayData []nodeDisplayData
	}{
		{
			name: "priority is as follows Degraded > Unavailable > Progressing > Completed",
			args: args{
				cv:   &cvUpdating,
				pool: mcpMaster,
				nodes: []corev1.Node{
					node("a").updated(mcNew.Name, mcNew.Name).node,
					node("b").degraded_draining(mcOld.Name, mcNew.Name, "PDB prohibits draining").node,
					node("c").progressing_updating(mcOld.Name, mcNew.Name).node,
					node("d").pending(mcOld.Name, mcOld.Name).unavailable().node,
				},
				machineConfigs: machineConfigs,
			},
			expectedNodeDisplayData: []nodeDisplayData{
				{
					Name:          "b",
					Assessment:    nodeAssessmentDegraded,
					Phase:         phaseStateDraining,
					Version:       oldOCPVersion,
					Estimate:      "?",
					Message:       "PDB prohibits draining",
					isUnavailable: true,
					isDegraded:    true,
					isUpdating:    true,
					isUpdated:     false,
				},
				{
					Name:          "d",
					Assessment:    nodeAssessmentUnavailable,
					Phase:         phaseStatePending,
					Version:       oldOCPVersion,
					Estimate:      "?",
					Message:       "Node is unavailable",
					isUnavailable: true,
					isDegraded:    false,
					isUpdating:    false,
					isUpdated:     false,
				},
				{
					Name:          "c",
					Assessment:    nodeAssessmentProgressing,
					Phase:         phaseStateUpdating,
					Version:       oldOCPVersion,
					Estimate:      "+20m",
					Message:       "",
					isUnavailable: true,
					isDegraded:    false,
					isUpdating:    true,
					isUpdated:     false,
				},
				{
					Name:          "a",
					Assessment:    nodeAssessmentCompleted,
					Phase:         phaseStateUpdated,
					Version:       newOCPVersion,
					Estimate:      "-",
					Message:       "",
					isUnavailable: false,
					isDegraded:    false,
					isUpdating:    false,
					isUpdated:     true,
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nodeDisplayData, _ := assessNodesStatus(tc.args.cv, tc.args.pool, tc.args.nodes, tc.args.machineConfigs)
			if diff := cmp.Diff(tc.expectedNodeDisplayData, nodeDisplayData, allowUnexportedWorkerPools); diff != "" {
				t.Errorf("nodeDisplayData differ from expected:\n%s", diff)
			}
		})
	}
}

func Test_nodeInsights(t *testing.T) {
	oldOCPVersion := "3.10.0"
	newOCPVersion := "4.16.0"
	mcOld := mc("old").version(oldOCPVersion).machineConfig
	mcNew := mc("new").version(newOCPVersion).machineConfig
	mcpMaster := mcp("master").setMachineConfig("new").machineConfigPool
	mcpWorker := mcp("worker").setMachineConfig("new").machineConfigPool
	type args struct {
		pool          mcfgv1.MachineConfigPool
		node          corev1.Node
		reason        string
		isUnavailable bool
		isUpdating    bool
		isDegraded    bool
	}
	testCases := []struct {
		name                  string
		args                  args
		expectedUpdateInsight []updateInsight
	}{
		{
			name: "node is updated - all is well",
			args: args{
				pool: mcpMaster,
				node: node("a").updated(mcNew.Name, mcNew.Name).node,
			},
			expectedUpdateInsight: nil,
		},
		{
			name: "node is updated - unavailable - master pool",
			args: args{
				pool:          mcpMaster,
				node:          node("a").updated(mcNew.Name, mcNew.Name).unavailable().node,
				reason:        "Node is unavailable",
				isUnavailable: true,
			},
			expectedUpdateInsight: []updateInsight{
				{
					impact: updateInsightImpact{
						level:      warningImpactLevel,
						impactType: updateSpeedImpactType,
						summary:    "Node a is unavailable | Node is unavailable",
					},
					scope: updateInsightScope{
						scopeType: scopeTypeControlPlane,
						resources: []scopeResource{{kind: scopeKindNode, name: "a"}},
					},
				},
			},
		},
		{
			name: "node is updated - unavailable - worker pool",
			args: args{
				pool:          mcpWorker,
				node:          node("a").updated(mcNew.Name, mcNew.Name).unavailable().node,
				reason:        "Node is unavailable",
				isUnavailable: true,
			},
			expectedUpdateInsight: []updateInsight{
				{
					impact: updateInsightImpact{
						level:      warningImpactLevel,
						impactType: updateSpeedImpactType,
						summary:    "Node a is unavailable | Node is unavailable",
					},
					scope: updateInsightScope{
						scopeType: scopeTypeWorkerPool,
						resources: []scopeResource{{kind: scopeKindNode, name: "a"}},
					},
				},
			},
		},
		{
			name: "node is degraded - pdb prohibits draining",
			args: args{
				pool:          mcpWorker,
				node:          node("a").degraded_draining(mcOld.Name, mcNew.Name, "PDB prohibits draining").node,
				reason:        "PDB prohibits draining",
				isUnavailable: true,
				isUpdating:    true,
				isDegraded:    true,
			},
			expectedUpdateInsight: []updateInsight{
				{
					impact: updateInsightImpact{
						level:      errorImpactLevel,
						impactType: updateStalledImpactType,
						summary:    "Node a is degraded | PDB prohibits draining",
					},
					scope: updateInsightScope{
						scopeType: scopeTypeWorkerPool,
						resources: []scopeResource{{kind: scopeKindNode, name: "a"}},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			updateInsights := nodeInsights(tc.args.pool, tc.args.node, tc.args.reason, tc.args.isUnavailable, tc.args.isUpdating, tc.args.isDegraded)
			if diff := cmp.Diff(tc.expectedUpdateInsight, updateInsights, allowUnexportedInsightStructs); diff != "" {
				t.Errorf("updateInsight differ from expected:\n%s", diff)
			}
		})
	}
}

func Test_assessMachineConfigPool(t *testing.T) {
	oldOCPVersion := "3.10.0"
	newOCPVersion := "4.16.0"
	type args struct {
		pool  mcfgv1.MachineConfigPool
		nodes []nodeDisplayData
	}
	testCases := []struct {
		name                    string
		args                    args
		expectedPoolDisplayData poolDisplayData
		expectedUpdateInsight   []updateInsight
	}{
		{
			name: "progressing - all is well",
			args: args{
				pool: mcp("master").setMachineConfig("new").machineConfigPool,
				nodes: []nodeDisplayData{
					{
						Name:          "a",
						Assessment:    nodeAssessmentProgressing,
						Phase:         phaseStateUpdating,
						Version:       oldOCPVersion,
						Estimate:      "+20m",
						Message:       "",
						isUnavailable: true,
						isDegraded:    false,
						isUpdating:    true,
						isUpdated:     false,
					},
					{
						Name:       "b",
						Assessment: nodeAssessmentCompleted,
						Phase:      phaseStateUpdated,
						Version:    newOCPVersion,
						Estimate:   "-",
						isUpdated:  true,
					},
				},
			},
			expectedPoolDisplayData: poolDisplayData{
				Name:       "master",
				Assessment: assessmentStateProgressing,
				Completion: 50,
				NodesOverview: nodesOverviewDisplayData{
					Total:       2,
					Available:   1,
					Progressing: 1,
					Outdated:    1,
				},
				Nodes: []nodeDisplayData{
					{
						Name:          "a",
						Assessment:    nodeAssessmentProgressing,
						Phase:         phaseStateUpdating,
						Version:       oldOCPVersion,
						Estimate:      "+20m",
						Message:       "",
						isUnavailable: true,
						isDegraded:    false,
						isUpdating:    true,
						isUpdated:     false,
					},
					{
						Name:       "b",
						Assessment: nodeAssessmentCompleted,
						Phase:      phaseStateUpdated,
						Version:    newOCPVersion,
						Estimate:   "-",
						isUpdated:  true,
					},
				},
			},
			expectedUpdateInsight: nil,
		},
		{
			name: "degraded - pdb prohibits draining",
			args: args{
				pool: mcp("worker").setMachineConfig("new").machineConfigPool,
				nodes: []nodeDisplayData{
					{
						Name:          "a",
						Assessment:    nodeAssessmentDegraded,
						Phase:         phaseStateDraining,
						Version:       oldOCPVersion,
						Estimate:      "+30m",
						Message:       "PDB prohibits draining",
						isUnavailable: true,
						isDegraded:    true,
						isUpdating:    true,
						isUpdated:     false,
					},
				},
			},
			expectedPoolDisplayData: poolDisplayData{
				Name:       "worker",
				Assessment: assessmentStateDegraded,
				Completion: 0,
				NodesOverview: nodesOverviewDisplayData{
					Total:    1,
					Degraded: 1,
					Draining: 1,
					Outdated: 1,
				},
				Nodes: []nodeDisplayData{
					{
						Name:          "a",
						Assessment:    nodeAssessmentDegraded,
						Phase:         phaseStateDraining,
						Version:       oldOCPVersion,
						Estimate:      "+30m",
						Message:       "PDB prohibits draining",
						isUnavailable: true,
						isDegraded:    true,
						isUpdating:    true,
						isUpdated:     false,
					},
				},
			},
			expectedUpdateInsight: nil,
		},
		{
			name: "completed - all is well",
			args: args{
				pool: mcp("worker").setMachineConfig("new").machineConfigPool,
				nodes: []nodeDisplayData{
					{
						Name:       "a",
						Assessment: nodeAssessmentCompleted,
						Phase:      phaseStateUpdated,
						Version:    newOCPVersion,
						Estimate:   "-",
						isUpdated:  true,
					},
					{
						Name:       "b",
						Assessment: nodeAssessmentCompleted,
						Phase:      phaseStateUpdated,
						Version:    newOCPVersion,
						Estimate:   "-",
						isUpdated:  true,
					},
				},
			},
			expectedPoolDisplayData: poolDisplayData{
				Name:       "worker",
				Assessment: assessmentStateCompleted,
				Completion: 100,
				NodesOverview: nodesOverviewDisplayData{
					Total:     2,
					Available: 2,
				},
				Nodes: []nodeDisplayData{
					{
						Name:       "a",
						Assessment: nodeAssessmentCompleted,
						Phase:      phaseStateUpdated,
						Version:    newOCPVersion,
						Estimate:   "-",
						isUpdated:  true,
					},
					{
						Name:       "b",
						Assessment: nodeAssessmentCompleted,
						Phase:      phaseStateUpdated,
						Version:    newOCPVersion,
						Estimate:   "-",
						isUpdated:  true,
					},
				},
			},
			expectedUpdateInsight: nil,
		},
		{
			name: "paused worker pool with a pending node",
			args: args{
				pool: mcp("worker").setMachineConfig("new").paused().machineConfigPool,
				nodes: []nodeDisplayData{
					{
						Name:       "a",
						Assessment: nodeAssessmentExcluded,
						Phase:      phaseStatePaused,
						Version:    oldOCPVersion,
						Estimate:   "?",
						Message:    "",
					},
				},
			},
			expectedPoolDisplayData: poolDisplayData{
				Name:       "worker",
				Assessment: assessmentStateExcluded,
				Completion: 0,
				NodesOverview: nodesOverviewDisplayData{
					Total:     1,
					Available: 1,
					Excluded:  1,
					Outdated:  1,
				},
				Nodes: []nodeDisplayData{
					{
						Name:       "a",
						Assessment: nodeAssessmentExcluded,
						Phase:      phaseStatePaused,
						Version:    oldOCPVersion,
						Estimate:   "?",
						Message:    "",
					},
				},
			},
			expectedUpdateInsight: []updateInsight{
				{
					impact: updateInsightImpact{
						level:      warningImpactLevel,
						impactType: updateStalledImpactType,
						summary:    "Worker pool worker is paused | Outdated nodes in a paused pool will not be updated.",
					},
					scope: updateInsightScope{
						scopeType: scopeTypeWorkerPool,
						resources: []scopeResource{{kind: scopeKindMachineConfigPool, name: "worker"}},
					},
				},
			}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			poolDisplayData, updateInsight := assessMachineConfigPool(tc.args.pool, tc.args.nodes)
			if diff := cmp.Diff(tc.expectedPoolDisplayData, poolDisplayData, allowUnexportedWorkerPools); diff != "" {
				t.Errorf("poolDisplayData differ from expected:\n%s", diff)
			}

			if diff := cmp.Diff(tc.expectedUpdateInsight, updateInsight, allowUnexportedInsightStructs); diff != "" {
				t.Errorf("updateInsight differ from expected:\n%s", diff)
			}
		})
	}
}

func Test_machineConfigPoolInsights(t *testing.T) {
	oldOCPVersion := "3.10.0"
	type args struct {
		poolDisplay poolDisplayData
		pool        mcfgv1.MachineConfigPool
	}
	testCases := []struct {
		name                  string
		args                  args
		expectedUpdateInsight []updateInsight
	}{
		{
			name: "pending - all is well",
			args: args{
				pool: mcp("worker").setMachineConfig("new").machineConfigPool,
				poolDisplay: poolDisplayData{
					Name:       "worker",
					Assessment: assessmentStatePending,
					Completion: 0,
					NodesOverview: nodesOverviewDisplayData{
						Total:     1,
						Available: 1,
						Outdated:  1,
					},
					Nodes: []nodeDisplayData{
						{
							Name:       "a",
							Assessment: nodeAssessmentOutdated,
							Phase:      phaseStatePending,
							Version:    oldOCPVersion,
							Estimate:   "?",
							Message:    "",
						},
					},
				},
			},
			expectedUpdateInsight: nil,
		},
		{
			name: "paused pool",
			args: args{
				pool: mcp("worker").setMachineConfig("new").paused().machineConfigPool,
				poolDisplay: poolDisplayData{
					Name:       "worker",
					Assessment: assessmentStateExcluded,
					Completion: 0,
					NodesOverview: nodesOverviewDisplayData{
						Total:     1,
						Available: 1,
						Excluded:  1,
						Outdated:  1,
					},
					Nodes: []nodeDisplayData{
						{
							Name:       "a",
							Assessment: nodeAssessmentExcluded,
							Phase:      phaseStatePaused,
							Version:    oldOCPVersion,
							Estimate:   "?",
							Message:    "",
						},
					},
				},
			},
			expectedUpdateInsight: []updateInsight{
				{
					impact: updateInsightImpact{
						level:      warningImpactLevel,
						impactType: updateStalledImpactType,
						summary:    "Worker pool worker is paused | Outdated nodes in a paused pool will not be updated.",
					},
					scope: updateInsightScope{
						scopeType: scopeTypeWorkerPool,
						resources: []scopeResource{{kind: scopeKindMachineConfigPool, name: "worker"}},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			updateInsight := machineConfigPoolInsights(tc.args.poolDisplay, tc.args.pool)
			if diff := cmp.Diff(tc.expectedUpdateInsight, updateInsight, allowUnexportedInsightStructs); diff != "" {
				t.Errorf("updateInsight differ from expected:\n%s", diff)
			}
		})
	}
}
