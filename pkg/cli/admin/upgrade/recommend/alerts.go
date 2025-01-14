package recommend

import (
	"context"
	"encoding/json"
	"fmt"

	routev1 "github.com/openshift/api/route/v1"
	routev1client "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/oc/pkg/cli/admin/inspectalerts"
	"github.com/openshift/oc/pkg/cli/admin/upgrade/status"
)

// alerts retrieves the clusters currently firing alerts, and returns
// Conditions that are True for happy signals, False for sad signals,
// and Unknown when we do not have enough information to make a
// happy-or-sad determination.
func (o *options) alerts(ctx context.Context) ([]metav1.Condition, error) {
	var alertsBytes []byte
	if o.mockData.alertsPath != "" {
		if len(o.mockData.alerts) == 0 {
			return []metav1.Condition{{
				Type:    "recommended/Alert",
				Status:  metav1.ConditionUnknown,
				Reason:  "NoTestData",
				Message: fmt.Sprintf("This --mock-clusterversion run did not have alert data available at %v", o.mockData.alertsPath),
			}}, nil
		}
		alertsBytes = o.mockData.alerts
	} else {
		client, err := routev1client.NewForConfig(o.RESTConfig)
		if err != nil {
			return nil, err
		}
		routeGetter := func(ctx context.Context, namespace string, name string, opts metav1.GetOptions) (*routev1.Route, error) {
			return client.Routes(namespace).Get(ctx, name, opts)
		}

		alertsBytes, err = inspectalerts.GetAlerts(ctx, routeGetter, o.RESTConfig.BearerToken)
		if err != nil {
			return nil, err
		}
	}

	var alertData status.AlertData
	err := json.Unmarshal(alertsBytes, &alertData)
	if err != nil {
		return nil, fmt.Errorf("parsing alerts: %w", err)
	}

	var conditions []metav1.Condition
	haveCritical := false
	havePodDisruptionBudget := false
	havePullWaiting := false
	haveNodes := false
	for _, alert := range alertData.Data.Alerts {
		var alertName string
		if alertName = alert.Labels.AlertName; alertName == "" {
			continue
		}
		fmt.Printf("alert: %v\n", alert)
	}

	if !haveCritical {
		conditions = append(conditions, metav1.Condition{
			Type:    "recommended/CriticalAlerts",
			Status:  metav1.ConditionTrue,
			Reason:  "AsExpected",
			Message: "No critical alerts firing.",
		})
	}

	if !havePodDisruptionBudget {
		conditions = append(conditions, metav1.Condition{
			Type:    "recommended/PodDisruptionBudgetAlerts",
			Status:  metav1.ConditionTrue,
			Reason:  "AsExpected",
			Message: "No PodDisruptionBudget alerts firing.",
		})
	}

	if !havePullWaiting {
		conditions = append(conditions, metav1.Condition{
			Type:    "recommended/PodImagePullAlerts",
			Status:  metav1.ConditionTrue,
			Reason:  "AsExpected",
			Message: "No Pod container image pull alerts firing.",
		})
	}

	if !haveNodes {
		conditions = append(conditions, metav1.Condition{
			Type:    "recommended/NodeAlerts",
			Status:  metav1.ConditionTrue,
			Reason:  "AsExpected",
			Message: "No Node alerts firing.",
		})
	}

	return conditions, nil
}
