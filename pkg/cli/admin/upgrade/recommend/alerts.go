package recommend

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	routev1 "github.com/openshift/api/route/v1"
	routev1client "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"github.com/openshift/oc/pkg/cli/admin/inspectalerts"
	"github.com/openshift/oc/pkg/cli/admin/upgrade/status"
)

// alerts retrieves the clusters currently firing alerts, and returns
// Conditions that are True for happy signals, False for sad signals,
// and Unknown when we do not have enough information to make a
// happy-or-sad determination.
func (o *options) alerts(ctx context.Context) ([]acceptableCondition, error) {
	var alertsBytes []byte
	if o.mockData.alertsPath != "" {
		if len(o.mockData.alerts) == 0 {
			return []acceptableCondition{{
				Condition: metav1.Condition{
					Type:    "recommended/Alert",
					Status:  metav1.ConditionUnknown,
					Reason:  "NoTestData",
					Message: fmt.Sprintf("This --mock-clusterversion run did not have alert data available at %v", o.mockData.alertsPath),
				},
				acceptanceName: "AlertNoTestData",
			}}, nil
		}
		alertsBytes = o.mockData.alerts
	} else {
		if err := inspectalerts.ValidateRESTConfig(o.RESTConfig); err != nil {
			return nil, err
		}

		roundTripper, err := rest.TransportFor(o.RESTConfig)
		if err != nil {
			return nil, err
		}

		client, err := routev1client.NewForConfig(o.RESTConfig)
		if err != nil {
			return nil, err
		}
		routeGetter := func(ctx context.Context, namespace string, name string, opts metav1.GetOptions) (*routev1.Route, error) {
			return client.Routes(namespace).Get(ctx, name, opts)
		}

		alertsBytes, err = inspectalerts.GetAlerts(ctx, roundTripper, routeGetter)
		if err != nil {
			return nil, err
		}
	}

	var alertData status.AlertData
	err := json.Unmarshal(alertsBytes, &alertData)
	if err != nil {
		return nil, fmt.Errorf("parsing alerts: %w", err)
	}

	var conditions []acceptableCondition
	i := 0
	haveCritical := false
	havePodDisruptionBudget := false
	havePullWaiting := false
	haveNodes := false
	for _, alert := range alertData.Data.Alerts {
		var alertName string
		if alertName = alert.Labels.AlertName; alertName == "" {
			continue
		}
		if alert.State == "pending" {
			continue
		}

		var summary string
		if summary = alert.Annotations.Summary; summary == "" {
			summary = alertName
		}
		if !strings.HasSuffix(summary, ".") {
			summary += "."
		}

		var description string
		switch {
		case alert.Annotations.Message != "" && alert.Annotations.Description != "":
			description += " The alert description is: " + alert.Annotations.Description + " | " + alert.Annotations.Message
		case alert.Annotations.Description != "":
			description += " The alert description is: " + alert.Annotations.Description
		case alert.Annotations.Message != "":
			description += " The alert description is: " + alert.Annotations.Message
		default:
			description += " The alert has no description."
		}

		var runbook string
		if runbook = alert.Annotations.Runbook; runbook == "" {
			runbook = "<alert does not have a runbook_url annotation>"
		}

		details := fmt.Sprintf("%s%s %s", summary, description, runbook)

		if alert.Labels.Severity == "critical" {
			haveCritical = true
			if alertName == "PodDisruptionBudgetLimit" {
				havePodDisruptionBudget = true
				// ideally the upstream PodDisruptionBudget*Limit alerts templated in the namespace and PDB, but until they do, inject those ourselves
				details = fmt.Sprintf("Namespace=%s, PodDisruptionBudget=%s. %s", alert.Labels.Namespace, alert.Labels.PodDisruptionBudget, details)
			}
			conditions = append(conditions, acceptableCondition{
				Condition: metav1.Condition{
					Type:    fmt.Sprintf("recommended/CriticalAlerts/%s/%d", alertName, i),
					Status:  metav1.ConditionFalse,
					Reason:  fmt.Sprintf("Alert:%s", alert.State),
					Message: fmt.Sprintf("%s alert %s %s, suggesting significant cluster issues worth investigating. %s", alert.Labels.Severity, alert.Labels.AlertName, alert.State, details),
				},
				acceptanceName: alertName,
			})
			i += 1
			continue
		}

		if alertName == "PodDisruptionBudgetAtLimit" {
			havePodDisruptionBudget = true
			// ideally the upstream PodDisruptionBudget*Limit alerts templated in the namespace and PDB, but until they do, inject those ourselves
			details = fmt.Sprintf("Namespace=%s, PodDisruptionBudget=%s. %s", alert.Labels.Namespace, alert.Labels.PodDisruptionBudget, details)
			conditions = append(conditions, acceptableCondition{
				Condition: metav1.Condition{
					Type:    fmt.Sprintf("recommended/PodDisruptionBudgetAlerts/%s/%d", alertName, i),
					Status:  metav1.ConditionFalse,
					Reason:  fmt.Sprintf("Alert:%s", alert.State),
					Message: fmt.Sprintf("%s alert %s %s, which might slow node drains. %s", alert.Labels.Severity, alert.Labels.AlertName, alert.State, details),
				},
				acceptanceName: alertName,
			})
			i += 1
			continue
		}

		if alertName == "KubeContainerWaiting" {
			havePullWaiting = true
			conditions = append(conditions, acceptableCondition{
				Condition: metav1.Condition{
					Type:    fmt.Sprintf("recommended/PodImagePullAlerts/%s/%d", alertName, i),
					Status:  metav1.ConditionFalse,
					Reason:  fmt.Sprintf("Alert:%s", alert.State),
					Message: fmt.Sprintf("%s alert %s %s, which may slow workload redistribution during rolling node updates. %s", alert.Labels.Severity, alert.Labels.AlertName, alert.State, details),
				},
				acceptanceName: alertName,
			})
			i += 1
			continue
		}

		if alertName == "KubeNodeNotReady" || alertName == "KubeNodeReadinessFlapping" || alertName == "KubeNodeUnreachable" {
			haveNodes = true
			conditions = append(conditions, acceptableCondition{
				Condition: metav1.Condition{
					Type:    fmt.Sprintf("recommended/NodeAlerts/%s/%d", alertName, i),
					Status:  metav1.ConditionFalse,
					Reason:  fmt.Sprintf("Alert:%s", alert.State),
					Message: fmt.Sprintf("%s alert %s %s, which may slow workload redistribution during rolling node updates. %s", alert.Labels.Severity, alert.Labels.AlertName, alert.State, details),
				},
				acceptanceName: alertName,
			})
			i += 1
			continue
		}
	}

	if !haveCritical {
		conditions = append(conditions, acceptableCondition{Condition: metav1.Condition{
			Type:    "recommended/CriticalAlerts",
			Status:  metav1.ConditionTrue,
			Reason:  "AsExpected",
			Message: "No critical alerts firing.",
		}})
	}

	if !havePodDisruptionBudget {
		conditions = append(conditions, acceptableCondition{Condition: metav1.Condition{
			Type:    "recommended/PodDisruptionBudgetAlerts",
			Status:  metav1.ConditionTrue,
			Reason:  "AsExpected",
			Message: "No PodDisruptionBudget alerts firing.",
		}})
	}

	if !havePullWaiting {
		conditions = append(conditions, acceptableCondition{Condition: metav1.Condition{
			Type:    "recommended/PodImagePullAlerts",
			Status:  metav1.ConditionTrue,
			Reason:  "AsExpected",
			Message: "No Pod container image pull alerts firing.",
		}})
	}

	if !haveNodes {
		conditions = append(conditions, acceptableCondition{Condition: metav1.Condition{
			Type:    "recommended/NodeAlerts",
			Status:  metav1.ConditionTrue,
			Reason:  "AsExpected",
			Message: "No Node alerts firing.",
		}})
	}

	return conditions, nil
}
