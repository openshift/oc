package status

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type coBuilder struct {
	operator configv1.ClusterOperator
}

func co(name string) *coBuilder {
	return &coBuilder{
		operator: configv1.ClusterOperator{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Annotations: map[string]string{"include.release.openshift.io/self-managed-high-availability": "true"},
			},
			Status: configv1.ClusterOperatorStatus{
				Versions: []configv1.OperandVersion{
					{
						Name:    "operator",
						Version: "old",
					},
				},
				Conditions: []configv1.ClusterOperatorStatusCondition{
					{
						Type:               configv1.OperatorAvailable,
						Status:             configv1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
						Reason:             "AsExpected",
						Message:            "All is well",
					},
					{
						Type:               configv1.OperatorProgressing,
						Status:             configv1.ConditionFalse,
						LastTransitionTime: metav1.Now(),
						Reason:             "AsExpected",
						Message:            "No changes necessary",
					},
					{
						Type:               configv1.OperatorDegraded,
						Status:             configv1.ConditionFalse,
						LastTransitionTime: metav1.Now(),
						Reason:             "AsExpected",
						Message:            "All is well",
					},
				},
			},
		},
	}
}

type conditionOption func(condition *configv1.ClusterOperatorStatusCondition)

func changed(t time.Time) conditionOption {
	return func(condition *configv1.ClusterOperatorStatusCondition) {
		condition.LastTransitionTime = metav1.NewTime(t)
	}
}

func (c *coBuilder) progressing(status configv1.ConditionStatus, optionFuncs ...conditionOption) *coBuilder {
	for i := range c.operator.Status.Conditions {
		if c.operator.Status.Conditions[i].Type == configv1.OperatorProgressing {
			c.operator.Status.Conditions[i].Status = status
			c.operator.Status.Conditions[i].Reason = "ProgressingTowardsDesired"
			c.operator.Status.Conditions[i].Message = "Operand is operated by operator"
			c.operator.Status.Conditions[i].LastTransitionTime = metav1.Date(2023, 12, 1, 23, 23, 0, 0, time.UTC)
			for _, f := range optionFuncs {
				f(&c.operator.Status.Conditions[i])
			}
			break
		}
	}
	return c
}

func (c *coBuilder) available(status configv1.ConditionStatus) *coBuilder {
	for i := range c.operator.Status.Conditions {
		if c.operator.Status.Conditions[i].Type == configv1.OperatorAvailable {
			c.operator.Status.Conditions[i].Status = status
			c.operator.Status.Conditions[i].Reason = "ProgressingTowardsDesired"
			c.operator.Status.Conditions[i].Message = "Operand is operated by operator"
			c.operator.Status.Conditions[i].LastTransitionTime = metav1.Now()
			break
		}
	}
	return c
}

func (c *coBuilder) degraded(status configv1.ConditionStatus) *coBuilder {
	for i := range c.operator.Status.Conditions {
		if c.operator.Status.Conditions[i].Type == configv1.OperatorDegraded {
			c.operator.Status.Conditions[i].Status = status
			c.operator.Status.Conditions[i].Reason = "ServiceDegraded"
			c.operator.Status.Conditions[i].Message = "Operand is misbehaving a little"
			c.operator.Status.Conditions[i].LastTransitionTime = metav1.Now()
			break
		}
	}
	return c
}

func (c *coBuilder) without(condition configv1.ClusterStatusConditionType) *coBuilder {
	var conditions []configv1.ClusterOperatorStatusCondition

	for i := range c.operator.Status.Conditions {
		if c.operator.Status.Conditions[i].Type != condition {
			conditions = append(conditions, c.operator.Status.Conditions[i])
		}
	}
	c.operator.Status.Conditions = conditions

	return c
}

func (c *coBuilder) version(version string) *coBuilder {
	c.operator.Status.Versions = []configv1.OperandVersion{
		{
			Name:    "operator",
			Version: version,
		},
	}
	return c
}

func (c *coBuilder) annotated(annotations map[string]string) *coBuilder {
	c.operator.Annotations = annotations
	return c
}

var cvFixture = configv1.ClusterVersion{
	Status: configv1.ClusterVersionStatus{
		Desired: configv1.Release{Version: "new"},
		Conditions: []configv1.ClusterOperatorStatusCondition{{
			Type:   clusterStatusFailing,
			Status: configv1.ConditionFalse,
			Reason: "AsExpected",
		}},
	},
}

var allowUnexportedInsightStructs = cmp.AllowUnexported(
	updateInsight{},
	updateInsightScope{},
	scopeResource{},
	scopeGroupKind{},
	updateInsightImpact{},
	updateInsightRemediation{},
	updateHealthData{},
)

func TestAssessControlPlaneStatus_Operators(t *testing.T) {
	testCases := []struct {
		name      string
		operators []configv1.ClusterOperator
		expected  operators
	}{
		{
			name: "all operators good",
			operators: []configv1.ClusterOperator{
				co("one").operator,
				co("two").operator,
			},
			expected: operators{Total: 2, Waiting: 2},
		},
		{
			name: "one out of two progressing",
			operators: []configv1.ClusterOperator{
				co("one").operator,
				co("two").progressing(configv1.ConditionTrue).operator,
			},
			expected: operators{Total: 2, Waiting: 1, Updating: []UpdatingClusterOperator{{Name: "two", Condition: &configv1.ClusterOperatorStatusCondition{
				Type:               "Progressing",
				Status:             "True",
				LastTransitionTime: metav1.Date(2023, 12, 1, 23, 23, 0, 0, time.UTC),
				Reason:             "ProgressingTowardsDesired",
				Message:            "Operand is operated by operator",
			},
			}}},
		},
		{
			name: "one out of two not available",
			operators: []configv1.ClusterOperator{
				co("one").operator,
				co("two").available(configv1.ConditionFalse).operator,
			},
			expected: operators{Total: 2, Unavailable: 1, Waiting: 2},
		},
		{
			name: "only count operators with *.release.openshift.io annotations",
			operators: []configv1.ClusterOperator{
				co("one").operator, // annotated by default
				co("two").annotated(map[string]string{"exclude.release.openshift.io/internal-openshift-hosted": "true"}).operator,
				co("three").annotated(map[string]string{"include.release.openshift.io/single-node-developer": "true"}).operator,
				co("four").annotated(map[string]string{"random-nonsense": "true"}).operator,
				co("five").annotated(map[string]string{}).degraded(configv1.ConditionTrue).operator,
				co("six").annotated(nil).available(configv1.ConditionUnknown).operator,
			},
			expected: operators{Total: 3, Waiting: 3},
		},
		{
			name: "available=unknown or missing implies available=false",
			operators: []configv1.ClusterOperator{
				co("one").available(configv1.ConditionUnknown).operator,
				co("two").without(configv1.OperatorAvailable).operator,
			},
			expected: operators{Total: 2, Unavailable: 2, Waiting: 2},
		},
		{
			name: "one out of two degraded",
			operators: []configv1.ClusterOperator{
				co("one").operator,
				co("two").degraded(configv1.ConditionTrue).operator,
			},
			expected: operators{Total: 2, Degraded: 1, Waiting: 2},
		},
		{
			name: "degraded=unknown or missing implies degraded=false",
			operators: []configv1.ClusterOperator{
				co("one").degraded(configv1.ConditionUnknown).operator,
				co("two").without(configv1.OperatorDegraded).operator,
			},
			expected: operators{Total: 2, Waiting: 2},
		},
		{
			name: "one out of two degraded, processing, not available",
			operators: []configv1.ClusterOperator{
				co("one").operator,
				co("two").
					degraded(configv1.ConditionTrue).
					available(configv1.ConditionFalse).
					progressing(configv1.ConditionTrue).operator,
			},
			expected: operators{Total: 2, Unavailable: 1, Waiting: 1, Updating: []UpdatingClusterOperator{{Name: "two", Condition: &configv1.ClusterOperatorStatusCondition{
				Type:               "Progressing",
				Status:             "True",
				LastTransitionTime: metav1.Date(2023, 12, 1, 23, 23, 0, 0, time.UTC),
				Reason:             "ProgressingTowardsDesired",
				Message:            "Operand is operated by operator",
			},
			}}},
		},
		{
			name: "one upgraded",
			operators: []configv1.ClusterOperator{
				co("one").
					version("new").
					progressing(configv1.ConditionTrue).operator,
			},
			expected: operators{Total: 1, Updated: 1},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, insights := assessControlPlaneStatus(&cvFixture, tc.operators, time.Now())
			if diff := cmp.Diff(tc.expected, actual.Operators, cmp.AllowUnexported(operators{})); diff != "" {
				t.Errorf("%s, actual output differs from expected:\n%s", tc.name, diff)
			}
			// expect empty insights, conditions in this test have LastTransitionTime set to Now()
			// so they never go over the threshold
			if diff := cmp.Diff([]updateInsight(nil), insights, allowUnexportedInsightStructs); diff != "" {
				t.Errorf("unexpected non-nil insights:\n%s", diff)
			}
		})
	}
}

func TestAssessControlPlaneStatus_Estimate(t *testing.T) {
	now := time.Now()
	minutesAgo := [250]time.Time{}
	for i := range minutesAgo {
		minutesAgo[i] = now.Add(time.Duration(-i) * time.Minute)
	}

	testCases := []struct {
		name                            string
		started                         time.Time
		operators                       []configv1.ClusterOperator
		assumedToLastProgress           time.Duration
		assumedClusterOperatorCompleted float64
		expectedAssessment              assessmentState
	}{
		{
			name:    "last observed progress is most recent progressing change",
			started: minutesAgo[30],
			operators: []configv1.ClusterOperator{
				co("111").version("new").progressing(configv1.ConditionFalse, changed(minutesAgo[20])).operator,
				co("222").version("new").progressing(configv1.ConditionFalse, changed(minutesAgo[15])).operator,
				co("333").version("old").progressing(configv1.ConditionFalse, changed(minutesAgo[3])).operator,
				co("444").version("old").progressing(configv1.ConditionFalse, changed(minutesAgo[6])).operator,
			},
			assumedToLastProgress:           27 * time.Minute, // until 333 stopped progressing 3 minutes ago
			assumedClusterOperatorCompleted: 0.5,
			expectedAssessment:              assessmentStateProgressing,
		},
		{
			name:    "last observed progress is most recent progressing change",
			started: minutesAgo[30],
			operators: []configv1.ClusterOperator{
				co("111").version("new").progressing(configv1.ConditionFalse, changed(minutesAgo[20])).operator,
				co("222").version("new").progressing(configv1.ConditionFalse, changed(minutesAgo[15])).operator,
				co("333").version("old").progressing(configv1.ConditionTrue, changed(minutesAgo[10])).operator,
				co("444").version("old").progressing(configv1.ConditionFalse, changed(minutesAgo[6])).operator,
			},
			assumedToLastProgress:           24 * time.Minute, // until 444 stopped progressing 6 minutes ago
			assumedClusterOperatorCompleted: 0.5,
			expectedAssessment:              assessmentStateProgressing,
		},
		{
			name:    "backfill update duration as last observed progress when no progress is observed",
			started: minutesAgo[30],
			operators: []configv1.ClusterOperator{
				co("111").version("old").progressing(configv1.ConditionFalse, changed(minutesAgo[45])).operator,
				co("222").version("old").progressing(configv1.ConditionFalse, changed(minutesAgo[60])).operator,
				// New version but theirs Proceeding=False condition lastTransitionTime is before we started updating
				co("333").version("new").progressing(configv1.ConditionFalse, changed(minutesAgo[100])).operator,
				co("444").version("new").progressing(configv1.ConditionFalse, changed(minutesAgo[111])).operator,
			},
			assumedToLastProgress:           30 * time.Minute, // since started
			assumedClusterOperatorCompleted: 0.5,
			expectedAssessment:              assessmentStateProgressing,
		},
		{
			name:    "last observed progress too long ago, assessment goes to stalled",
			started: minutesAgo[240],
			operators: []configv1.ClusterOperator{
				co("111").version("new").progressing(configv1.ConditionFalse, changed(minutesAgo[235])).operator,
				co("222").version("new").progressing(configv1.ConditionFalse, changed(minutesAgo[220])).operator,
				co("333").version("new").progressing(configv1.ConditionFalse, changed(minutesAgo[215])).operator,
				co("444").version("old").progressing(configv1.ConditionFalse, changed(minutesAgo[220])).operator,
			},
			assumedToLastProgress:           25 * time.Minute, // until 333 stopped progressing 215 minutes ago
			assumedClusterOperatorCompleted: 0.75,
			expectedAssessment:              assessmentStateStalled,
		},
		{
			name:    "slightly over estimation, assessment goes to progressing slow",
			started: minutesAgo[60],
			operators: []configv1.ClusterOperator{
				co("111").version("new").progressing(configv1.ConditionFalse, changed(minutesAgo[38])).operator,
				co("222").version("new").progressing(configv1.ConditionFalse, changed(minutesAgo[45])).operator,
				co("333").version("new").progressing(configv1.ConditionFalse, changed(minutesAgo[50])).operator,
				co("444").version("old").progressing(configv1.ConditionFalse, changed(minutesAgo[48])).operator,
			},
			assumedToLastProgress:           22 * time.Minute,
			assumedClusterOperatorCompleted: 0.75,
			expectedAssessment:              assessmentStateProgressingSlow,
		},
		{
			name:    "machine-config progressing=true wins when it is the last CO updating",
			started: minutesAgo[60],
			operators: []configv1.ClusterOperator{
				co("111").version("new").progressing(configv1.ConditionFalse, changed(minutesAgo[10])).operator,
				co("222").version("new").progressing(configv1.ConditionFalse, changed(minutesAgo[15])).operator,
				co("333").version("new").progressing(configv1.ConditionFalse, changed(minutesAgo[20])).operator,
				co("machine-config").version("old").progressing(configv1.ConditionTrue, changed(minutesAgo[30])).operator,
			},
			assumedToLastProgress:           30 * time.Minute, // until machine-config stated progressing 30 minutes ago
			assumedClusterOperatorCompleted: 0.75,
			expectedAssessment:              assessmentStateProgressing,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cv := cvFixture.DeepCopy()
			cv.Status.History = []configv1.UpdateHistory{
				{
					State:       configv1.PartialUpdate,
					StartedTime: metav1.NewTime(tc.started),
					Version:     "new",
				},
				{
					State:       configv1.CompletedUpdate,
					StartedTime: metav1.NewTime(tc.started.Add(-24 * time.Hour)),
					Version:     "old",
				},
			}
			expectedEstCompletion := estimateCompletion(time.Hour, tc.assumedToLastProgress, now.Sub(tc.started), tc.assumedClusterOperatorCompleted)
			actual, _ := assessControlPlaneStatus(cv, tc.operators, now)
			if diff := cmp.Diff(expectedEstCompletion, actual.EstTimeToComplete); diff != "" {
				t.Errorf("estimate to finish differs:\n%s", diff)
			}
			if diff := cmp.Diff(tc.expectedAssessment, actual.Assessment); diff != "" {
				t.Errorf("assessment differs:\n%s", diff)
			}
		})

	}
}

func TestAssessControlPlaneStatus_Completion(t *testing.T) {
	testCases := []struct {
		name               string
		operators          []configv1.ClusterOperator
		expectedAssessment assessmentState
		expectedCompletion float64
	}{
		{
			name: "all operators old",
			operators: []configv1.ClusterOperator{
				co("one").version("old").operator,
				co("two").version("old").operator,
				co("three").version("old").operator,
			},
			expectedAssessment: assessmentStateProgressing,
			expectedCompletion: 0.0,
		},
		{
			name: "all operators new (done)",
			operators: []configv1.ClusterOperator{
				co("one").version("new").operator,
				co("two").version("new").operator,
				co("three").version("new").operator,
			},
			expectedAssessment: assessmentStateCompleted,
			expectedCompletion: 100,
		},
		{
			name: "two operators done, one to go",
			operators: []configv1.ClusterOperator{
				co("one").version("new").operator,
				co("two").version("old").operator,
				co("three").version("new").operator,
			},
			expectedAssessment: assessmentStateProgressing,
			expectedCompletion: 2.0 / 3.0 * 100.0,
		},
		{
			name: "non-platform operators do not count",
			operators: []configv1.ClusterOperator{
				co("one").version("new").operator,
				co("two").version("old").operator,
				co("three").version("new").operator,
				co("not-platform").annotated(nil).version("new").operator,
			},
			expectedAssessment: assessmentStateProgressing,
			expectedCompletion: 2.0 / 3.0 * 100.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, _ := assessControlPlaneStatus(&cvFixture, tc.operators, time.Now())
			if diff := cmp.Diff(tc.expectedCompletion, actual.Completion, cmpopts.EquateApprox(0, 0.1)); diff != "" {
				t.Errorf("expected completion %f, got %f", tc.expectedCompletion, actual.Completion)
			}

			if actual.Assessment != tc.expectedAssessment {
				t.Errorf("expected assessment %s, got %s", tc.expectedAssessment, actual.Assessment)
			}
		})
	}
}

func TestAssessControlPlaneStatus_Duration(t *testing.T) {
	// Inject ns skew to exercise expected rounding
	nsSkew := 12315 * time.Nanosecond

	now := time.Now()
	hourAgo := metav1.NewTime(now.Add(-time.Hour - nsSkew))
	halfHourAgo := metav1.NewTime(now.Add(-time.Minute*30 - nsSkew))
	underTenMinutesAgo := metav1.NewTime(now.Add(-333*time.Second - nsSkew))
	overTenMinutesAgo := metav1.NewTime(now.Add(-637*time.Second - nsSkew))

	testCases := []struct {
		name             string
		firstHistoryItem configv1.UpdateHistory
		expectedDuration time.Duration
	}{
		{
			name: "partial update -> still in progress",
			firstHistoryItem: configv1.UpdateHistory{
				State:       configv1.PartialUpdate,
				StartedTime: hourAgo,
				Version:     "new",
			},
			expectedDuration: time.Hour,
		},
		{
			name: "completed upgrade",
			firstHistoryItem: configv1.UpdateHistory{
				State:          configv1.CompletedUpdate,
				StartedTime:    hourAgo,
				CompletionTime: &halfHourAgo,
				Version:        "new",
			},
			expectedDuration: 30 * time.Minute,
		},
		{
			name: "partial update started 10s ago -> 10s duration",
			firstHistoryItem: configv1.UpdateHistory{
				State:       configv1.PartialUpdate,
				StartedTime: underTenMinutesAgo,
				Version:     "new",
			},
			expectedDuration: 333 * time.Second, // precision to seconds when under 10m
		},
		{
			name: "partial update started over 10m ago ago -> 11m duration",
			firstHistoryItem: configv1.UpdateHistory{
				State:       configv1.PartialUpdate,
				StartedTime: overTenMinutesAgo,
				Version:     "new",
			},
			expectedDuration: 11 * time.Minute, // rounded to minutes when over 10m
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			cv := cvFixture.DeepCopy()
			cv.Status.History = append(cv.Status.History, tc.firstHistoryItem)

			actual, _ := assessControlPlaneStatus(cv, nil, now)
			if diff := cmp.Diff(tc.expectedDuration, actual.Duration); diff != "" {
				t.Errorf("expected completion %s, got %s", tc.expectedDuration, actual.Duration)
			}
		})
	}
}

func TestCoInsights(t *testing.T) {
	t.Parallel()
	anchorTime := time.Now()
	coKind := scopeGroupKind{group: configv1.GroupName, kind: "ClusterOperator"}
	testCases := []struct {
		name      string
		available configv1.ClusterOperatorStatusCondition
		degraded  configv1.ClusterOperatorStatusCondition
		expected  []updateInsight
	}{
		{
			name: "no insights on happy conditions",
			available: configv1.ClusterOperatorStatusCondition{
				Type:   configv1.OperatorAvailable,
				Status: configv1.ConditionTrue,
			},
			degraded: configv1.ClusterOperatorStatusCondition{
				Type:   configv1.OperatorDegraded,
				Status: configv1.ConditionFalse,
			},
		},
		{
			name: "no insights on below-threshold bad states",
			available: configv1.ClusterOperatorStatusCondition{
				Type:               configv1.OperatorAvailable,
				Status:             configv1.ConditionFalse,
				LastTransitionTime: metav1.NewTime(anchorTime.Add(-unavailableWarningThreshold).Add(time.Second)),
			},
			degraded: configv1.ClusterOperatorStatusCondition{
				Type:               configv1.OperatorDegraded,
				Status:             configv1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(anchorTime.Add(-degradedWarningThreshold).Add(time.Second)),
			},
		},
		{
			name: "warning insights on above-warn-threshold bad states",
			available: configv1.ClusterOperatorStatusCondition{
				Type:               configv1.OperatorAvailable,
				Status:             configv1.ConditionFalse,
				LastTransitionTime: metav1.NewTime(anchorTime.Add(-unavailableWarningThreshold).Add(-time.Second)),
				Reason:             "Broken",
				Message:            "Operator is broken",
			},
			degraded: configv1.ClusterOperatorStatusCondition{
				Type:               configv1.OperatorDegraded,
				Status:             configv1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(anchorTime.Add(-degradedWarningThreshold).Add(-time.Second)),
				Reason:             "Slow",
				Message:            "Networking is hard",
			},
			expected: []updateInsight{
				{
					startedAt: anchorTime.Add(-unavailableWarningThreshold).Add(-time.Second),
					scope:     updateInsightScope{scopeType: scopeTypeControlPlane, resources: []scopeResource{{kind: coKind, name: "testOperator"}}},
					impact: updateInsightImpact{
						level:       warningImpactLevel,
						impactType:  apiAvailabilityImpactType,
						summary:     "Cluster Operator testOperator is unavailable (Broken)",
						description: "Operator is broken",
					},
					remediation: updateInsightRemediation{
						reference: "https://github.com/openshift/runbooks/blob/master/alerts/cluster-monitoring-operator/ClusterOperatorDown.md",
					},
				},
				{
					startedAt: anchorTime.Add(-degradedWarningThreshold).Add(-time.Second),
					scope:     updateInsightScope{scopeType: scopeTypeControlPlane, resources: []scopeResource{{kind: coKind, name: "testOperator"}}},
					impact: updateInsightImpact{
						level:       warningImpactLevel,
						impactType:  apiAvailabilityImpactType,
						summary:     "Cluster Operator testOperator is degraded (Slow)",
						description: "Networking is hard",
					},
					remediation: updateInsightRemediation{
						reference: "https://github.com/openshift/runbooks/blob/master/alerts/cluster-monitoring-operator/ClusterOperatorDegraded.md",
					},
				},
			},
		},
		{
			name: "error insights on above-error-threshold bad states",
			available: configv1.ClusterOperatorStatusCondition{
				Type:               configv1.OperatorAvailable,
				Status:             configv1.ConditionFalse,
				LastTransitionTime: metav1.NewTime(anchorTime.Add(-unavailableErrorThreshold).Add(-time.Second)),
				Reason:             "Broken",
				Message:            "Operator is broken",
			},
			degraded: configv1.ClusterOperatorStatusCondition{
				Type:               configv1.OperatorDegraded,
				Status:             configv1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(anchorTime.Add(-degradedErrorThreshold).Add(-time.Second)),
				Reason:             "Slow",
				Message:            "Networking is hard",
			},
			expected: []updateInsight{
				{
					startedAt: anchorTime.Add(-unavailableErrorThreshold).Add(-time.Second),
					scope:     updateInsightScope{scopeType: scopeTypeControlPlane, resources: []scopeResource{{kind: coKind, name: "testOperator"}}},
					impact: updateInsightImpact{
						level:       errorImpactLevel,
						impactType:  apiAvailabilityImpactType,
						summary:     "Cluster Operator testOperator is unavailable (Broken)",
						description: "Operator is broken",
					},
					remediation: updateInsightRemediation{
						reference: "https://github.com/openshift/runbooks/blob/master/alerts/cluster-monitoring-operator/ClusterOperatorDown.md",
					},
				},
				{
					startedAt: anchorTime.Add(-degradedErrorThreshold).Add(-time.Second),
					scope:     updateInsightScope{scopeType: scopeTypeControlPlane, resources: []scopeResource{{kind: coKind, name: "testOperator"}}},
					impact: updateInsightImpact{
						level:       errorImpactLevel,
						impactType:  apiAvailabilityImpactType,
						summary:     "Cluster Operator testOperator is degraded (Slow)",
						description: "Networking is hard",
					},
					remediation: updateInsightRemediation{
						reference: "https://github.com/openshift/runbooks/blob/master/alerts/cluster-monitoring-operator/ClusterOperatorDegraded.md",
					},
				},
			},
		},
		{
			name: "insights do not flatten linebreaks in messages",
			available: configv1.ClusterOperatorStatusCondition{
				Type:               configv1.OperatorAvailable,
				Status:             configv1.ConditionFalse,
				LastTransitionTime: metav1.NewTime(anchorTime.Add(-unavailableErrorThreshold).Add(-time.Second)),
				Reason:             "Broken",
				Message:            "Operator is broken\nand message has linebreaks",
			},
			degraded: configv1.ClusterOperatorStatusCondition{
				Type:   configv1.OperatorDegraded,
				Status: configv1.ConditionFalse,
			},
			expected: []updateInsight{
				{
					startedAt: anchorTime.Add(-unavailableErrorThreshold).Add(-time.Second),
					scope:     updateInsightScope{scopeType: scopeTypeControlPlane, resources: []scopeResource{{kind: coKind, name: "testOperator"}}},
					impact: updateInsightImpact{
						level:       errorImpactLevel,
						impactType:  apiAvailabilityImpactType,
						summary:     `Cluster Operator testOperator is unavailable (Broken)`,
						description: "Operator is broken\nand message has linebreaks",
					},
					remediation: updateInsightRemediation{
						reference: "https://github.com/openshift/runbooks/blob/master/alerts/cluster-monitoring-operator/ClusterOperatorDown.md",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			actual := coInsights("testOperator", &tc.available, &tc.degraded, anchorTime)
			if diff := cmp.Diff(tc.expected, actual, allowUnexportedInsightStructs); diff != "" {
				t.Errorf("insights differ from expected:\n%s", diff)
			}
		})
	}
}

func Test_operators_StatusSummary(t *testing.T) {
	tests := []struct {
		name      string
		operators operators
		want      string
	}{
		{
			name: "all healthy",
			operators: operators{
				Total: 2,
			},
			want: "2 Healthy",
		},
		{
			name: "some unavailable",
			operators: operators{
				Total:       3,
				Unavailable: 1,
			},
			want: "2 Healthy, 1 Unavailable",
		},
		{
			name: "some degraded",
			operators: operators{
				Total:    3,
				Degraded: 1,
			},
			want: "2 Healthy, 1 Available but degraded",
		},
		{
			name: "some degraded and unavailable",
			operators: operators{
				Total:       3,
				Unavailable: 1,
				Degraded:    1,
			},
			want: "1 Healthy, 1 Unavailable, 1 Available but degraded",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, tt.operators.StatusSummary()); diff != "" {
				t.Errorf("insights differ from expected:\n%s", diff)
			}
		})
	}
}

func Test_versionsFromHistory(t *testing.T) {
	hourAgo := now.Add(-time.Hour)
	type args struct {
		history               []configv1.UpdateHistory
		cvScope               scopeResource
		controlPlaneCompleted bool
	}
	tests := []struct {
		name                   string
		args                   args
		expectedVersions       versions
		expectedUpdateInsights []updateInsight
	}{
		{
			name: "empty history",
			args: args{
				history:               []configv1.UpdateHistory{},
				cvScope:               scopeResource{},
				controlPlaneCompleted: false,
			},
			expectedVersions: versions{
				target:            "unknown",
				previous:          "unknown",
				isPreviousPartial: false,
			},
		},
		{
			name: "single history item - partial",
			args: args{
				history: []configv1.UpdateHistory{
					{
						State:   configv1.PartialUpdate,
						Version: "X.Y.Z",
					},
				},
			},
			expectedVersions: versions{
				target:            "X.Y.Z",
				previous:          "unknown",
				isPreviousPartial: false,
				isTargetInstall:   true,
			},
		},
		{
			name: "single history item - completed",
			args: args{
				history: []configv1.UpdateHistory{
					{
						State:   configv1.CompletedUpdate,
						Version: "X.Y.Z",
					},
				},
			},
			expectedVersions: versions{
				target:            "X.Y.Z",
				previous:          "unknown",
				isPreviousPartial: false,
				isTargetInstall:   true,
			},
		},
		{
			name: "update in progress, previous version completed",
			args: args{
				history: []configv1.UpdateHistory{
					{
						State:   configv1.PartialUpdate,
						Version: "X.Y.Z",
					},
					{
						State:   configv1.CompletedUpdate,
						Version: "X.Y.Z-1",
					},
					{
						State:   configv1.PartialUpdate,
						Version: "X.Y.Z-2",
					},
				},
			},
			expectedVersions: versions{
				target:            "X.Y.Z",
				previous:          "X.Y.Z-1",
				isPreviousPartial: false,
			},
		},
		{
			name: "update in progress, previous version partial, last completed version does not exist",
			args: args{
				history: []configv1.UpdateHistory{
					{
						State:       configv1.PartialUpdate,
						StartedTime: metav1.NewTime(hourAgo),
						Version:     "X.Y.Z",
					},
					{
						State:       configv1.PartialUpdate,
						StartedTime: metav1.NewTime(hourAgo.Add(-time.Hour)),
						Version:     "X.Y.Z-1",
					},
				},
			},
			expectedVersions: versions{
				target:            "X.Y.Z",
				previous:          "X.Y.Z-1",
				isPreviousPartial: true,
			},
			expectedUpdateInsights: []updateInsight{
				{
					startedAt: hourAgo,
					scope: updateInsightScope{
						scopeType: scopeTypeControlPlane,
						resources: []scopeResource{{}},
					},
					impact: updateInsightImpact{
						level:       warningImpactLevel,
						impactType:  noneImpactType,
						summary:     "Previous update to X.Y.Z-1 never completed, last complete update was unknown",
						description: "Current update to X.Y.Z was initiated while the previous update to version X.Y.Z-1 was still in progress",
					},
					remediation: updateInsightRemediation{
						reference: "https://docs.openshift.com/container-platform/latest/updating/troubleshooting_updates/gathering-data-cluster-update.html#gathering-clusterversion-history-cli_troubleshooting_updates",
					},
				},
			},
		},
		{
			name: "update in progress, previous version partial, last completed version exists",
			args: args{
				history: []configv1.UpdateHistory{
					{
						State:       configv1.PartialUpdate,
						StartedTime: metav1.NewTime(hourAgo),
						Version:     "X.Y.Z",
					},
					{
						State:       configv1.PartialUpdate,
						StartedTime: metav1.NewTime(hourAgo.Add(-time.Hour)),
						Version:     "X.Y.Z-1",
					},
					{
						State:       configv1.PartialUpdate,
						StartedTime: metav1.NewTime(hourAgo.Add(-2 * time.Hour)),
						Version:     "X.Y.Z-2",
					},
					{
						State:       configv1.CompletedUpdate,
						StartedTime: metav1.NewTime(hourAgo.Add(-3 * time.Hour)),
						Version:     "X.Y.Z-3",
					},
					{
						State:       configv1.PartialUpdate,
						StartedTime: metav1.NewTime(hourAgo.Add(-4 * time.Hour)),
						Version:     "X.Y.Z-4",
					},
				},
			},
			expectedVersions: versions{
				target:            "X.Y.Z",
				previous:          "X.Y.Z-1",
				isPreviousPartial: true,
			},
			expectedUpdateInsights: []updateInsight{
				{
					startedAt: hourAgo,
					scope: updateInsightScope{
						scopeType: scopeTypeControlPlane,
						resources: []scopeResource{{}},
					},
					impact: updateInsightImpact{
						level:       warningImpactLevel,
						impactType:  noneImpactType,
						summary:     "Previous update to X.Y.Z-1 never completed, last complete update was X.Y.Z-3",
						description: "Current update to X.Y.Z was initiated while the previous update to version X.Y.Z-1 was still in progress",
					},
					remediation: updateInsightRemediation{
						reference: "https://docs.openshift.com/container-platform/latest/updating/troubleshooting_updates/gathering-data-cluster-update.html#gathering-clusterversion-history-cli_troubleshooting_updates",
					},
				},
			},
		},
		{
			name: "update in progress, previous version partial, function copies over the scope resource",
			args: args{
				history: []configv1.UpdateHistory{
					{
						State:       configv1.PartialUpdate,
						StartedTime: metav1.NewTime(hourAgo),
						Version:     "X.Y.Z",
					},
					{
						State:       configv1.PartialUpdate,
						StartedTime: metav1.NewTime(hourAgo.Add(-2 * time.Hour)),
						Version:     "X.Y.Z-2",
					},
					{
						State:       configv1.CompletedUpdate,
						StartedTime: metav1.NewTime(hourAgo.Add(-3 * time.Hour)),
						Version:     "X.Y.Z-3",
					},
				},
				cvScope: scopeResource{
					kind: scopeGroupKind{
						group: "group",
						kind:  "ClusterVersion",
					},
					namespace: "",
					name:      "version",
				},
			},
			expectedVersions: versions{
				target:            "X.Y.Z",
				previous:          "X.Y.Z-2",
				isPreviousPartial: true,
			},
			expectedUpdateInsights: []updateInsight{
				{
					startedAt: hourAgo,
					scope: updateInsightScope{
						scopeType: scopeTypeControlPlane,
						resources: []scopeResource{
							{
								kind: scopeGroupKind{
									group: "group",
									kind:  "ClusterVersion",
								},
								namespace: "",
								name:      "version",
							},
						},
					},
					impact: updateInsightImpact{
						level:       warningImpactLevel,
						impactType:  noneImpactType,
						summary:     "Previous update to X.Y.Z-2 never completed, last complete update was X.Y.Z-3",
						description: "Current update to X.Y.Z was initiated while the previous update to version X.Y.Z-2 was still in progress",
					},
					remediation: updateInsightRemediation{
						reference: "https://docs.openshift.com/container-platform/latest/updating/troubleshooting_updates/gathering-data-cluster-update.html#gathering-clusterversion-history-cli_troubleshooting_updates",
					},
				},
			},
		},
		{
			name: "update in progress, previous version partial, control plane is updated - insight is not needed",
			args: args{
				history: []configv1.UpdateHistory{
					{
						State:       configv1.PartialUpdate,
						StartedTime: metav1.NewTime(hourAgo),
						Version:     "X.Y.Z",
					},
					{
						State:       configv1.PartialUpdate,
						StartedTime: metav1.NewTime(hourAgo.Add(-2 * time.Hour)),
						Version:     "X.Y.Z-2",
					},
					{
						State:       configv1.CompletedUpdate,
						StartedTime: metav1.NewTime(hourAgo.Add(-3 * time.Hour)),
						Version:     "X.Y.Z-3",
					},
				},
				controlPlaneCompleted: true,
			},
			expectedVersions: versions{
				target:            "X.Y.Z",
				previous:          "X.Y.Z-2",
				isPreviousPartial: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualVersions, actualInsights := versionsFromHistory(tt.args.history, tt.args.cvScope, tt.args.controlPlaneCompleted)

			if diff := cmp.Diff(tt.expectedVersions, actualVersions, cmp.AllowUnexported(versions{})); diff != "" {
				t.Errorf("versions differ from expected:\n%s", diff)
			}

			if diff := cmp.Diff(tt.expectedUpdateInsights, actualInsights, allowUnexportedInsightStructs); diff != "" {
				t.Errorf("updateInsight differ from expected:\n%s", diff)
			}
		})
	}
}

func TestEstimateCompletion(t *testing.T) {
	testCases := []struct {
		name string

		baseline               time.Duration
		toLastObservedProgress time.Duration
		updatingFor            time.Duration
		coComplete             float64

		expectedEstimateTimeToComplete string
	}{
		{
			name:                   "No CO complete after 30m: estimate 60m as a baseline and we spent 30m of it",
			baseline:               time.Hour,
			toLastObservedProgress: 30 * time.Minute,
			updatingFor:            30 * time.Minute,
			coComplete:             0,

			expectedEstimateTimeToComplete: "36m0s",
		},
		{
			name:                   "No CO complete after 31m, last observable progress is 1m ago: estimate 60m as a baseline and we spent 31m of it",
			baseline:               time.Hour,
			toLastObservedProgress: 30 * time.Minute,
			updatingFor:            31 * time.Minute,
			coComplete:             0,

			expectedEstimateTimeToComplete: "35m0s",
		},
		{
			name:                   "20% CO complete after 30m",
			baseline:               time.Hour,
			toLastObservedProgress: 30 * time.Minute,
			updatingFor:            30 * time.Minute,
			coComplete:             0.2,

			expectedEstimateTimeToComplete: "1h9m0s",
		},
		{
			name:                   "20% CO complete after 35m, last observable progress was 5m ago",
			baseline:               time.Hour,
			toLastObservedProgress: 30 * time.Minute,
			updatingFor:            35 * time.Minute,
			coComplete:             0.2,

			expectedEstimateTimeToComplete: "1h3m0s",
		},
		{
			name:                   "75% CO complete after 30m",
			baseline:               time.Hour,
			toLastObservedProgress: 30 * time.Minute,
			updatingFor:            30 * time.Minute,
			coComplete:             0.75,

			expectedEstimateTimeToComplete: "50m0s",
		},
		{
			name:                   "99% CO complete after 20m - short estimate, precision to seconds",
			baseline:               time.Hour,
			toLastObservedProgress: 20 * time.Minute,
			updatingFor:            20 * time.Minute,
			coComplete:             0.99,

			expectedEstimateTimeToComplete: "4m50s",
		},
		{
			name:                   "Avoid projecting too soon - when toLastObservedProgress is <5m estimated baseline",
			baseline:               80 * time.Minute,
			toLastObservedProgress: 4 * time.Minute,
			updatingFor:            10 * time.Minute,
			coComplete:             0.05,

			expectedEstimateTimeToComplete: "1h24m0s",
		},
		{
			name:                   "100% CO complete after 30m: estimate 0 remaining",
			baseline:               time.Hour,
			toLastObservedProgress: 30 * time.Minute,
			updatingFor:            30 * time.Minute,
			coComplete:             1,

			expectedEstimateTimeToComplete: "0s",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			expectedEstimate, err := time.ParseDuration(tc.expectedEstimateTimeToComplete)
			if err != nil {
				t.Fatalf("Failed to parse expected duration: %v", err)
			}
			estimate := estimateCompletion(tc.baseline, tc.toLastObservedProgress, tc.updatingFor, tc.coComplete)
			if diff := cmp.Diff(expectedEstimate, estimate); diff != "" {
				t.Errorf("estimate time to complete differs from expected:\n%s", diff)
			}
		})
	}
}

func TestBaselineDuration(t *testing.T) {
	baseline := time.Hour

	now := time.Now()
	minutesAgo := [250]metav1.Time{}
	for i := range minutesAgo {
		minutesAgo[i] = metav1.NewTime(now.Add(time.Duration(-i) * time.Minute))
	}

	testCases := []struct {
		name     string
		history  []configv1.UpdateHistory
		expected time.Duration
	}{
		{
			name:     "empty history -> baseline",
			history:  []configv1.UpdateHistory{},
			expected: baseline,
		},
		{
			name: "one item -> baseline",
			history: []configv1.UpdateHistory{{
				State:          configv1.CompletedUpdate,
				StartedTime:    minutesAgo[60],
				CompletionTime: &minutesAgo[30],
			}},
			expected: baseline,
		},
		{
			name: "two items -> baseline",
			history: []configv1.UpdateHistory{
				{State: configv1.PartialUpdate, StartedTime: minutesAgo[60], CompletionTime: &minutesAgo[30]},
				{State: configv1.CompletedUpdate, StartedTime: minutesAgo[60], CompletionTime: &minutesAgo[30]},
			},
			expected: baseline,
		},
		{
			name: "all except install and current are partials -> baseline",
			history: []configv1.UpdateHistory{
				{State: configv1.CompletedUpdate, StartedTime: minutesAgo[60], CompletionTime: &minutesAgo[30]},
				{State: configv1.PartialUpdate, StartedTime: minutesAgo[60], CompletionTime: &minutesAgo[30]},
				{State: configv1.PartialUpdate, StartedTime: minutesAgo[60], CompletionTime: &minutesAgo[30]},
				{State: configv1.PartialUpdate, StartedTime: minutesAgo[60], CompletionTime: &minutesAgo[30]},
				{State: configv1.CompletedUpdate, StartedTime: minutesAgo[60], CompletionTime: &minutesAgo[30]},
			},
			expected: baseline,
		},
		{
			name: "first complete that is not current or install",
			history: []configv1.UpdateHistory{
				{State: configv1.CompletedUpdate, StartedTime: minutesAgo[60], CompletionTime: &minutesAgo[30]},
				{State: configv1.PartialUpdate, StartedTime: minutesAgo[60], CompletionTime: &minutesAgo[30]},
				{State: configv1.CompletedUpdate, StartedTime: minutesAgo[10], CompletionTime: &minutesAgo[5]},
				{State: configv1.CompletedUpdate, StartedTime: minutesAgo[20], CompletionTime: &minutesAgo[10]},
				{State: configv1.CompletedUpdate, StartedTime: minutesAgo[60], CompletionTime: &minutesAgo[30]},
			},
			expected: 5 * time.Minute,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := baselineDuration(tc.history)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("baseline duration differs from expected:\n%s", diff)
			}
		})
	}
}
