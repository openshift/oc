package mustgather

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/go-units"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/config/clusteroperator/v1helpers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

// PrintBasicClusterState prints a human-readable highlight of some basic information about the openshift cluster that
// is valuable to every caller of `oc must-gather`.  Even different products.
// This is NOT a place to add your pet conditions.  We have three places for components to report their errors,
//  1. clusterversion - shows errors applying payloads to transition versions
//  2. clusteroperators - shows whether every operand is functioning properly
//  3. alerts - show whether something might be at risk
// if you find yourself wanting to add an additional piece of information here, what you really want to do is add it
// to one of those three spots.  Doing so improves the self-diagnosis capabilities of our platform and lets *every*
// client benefit.
func (o *MustGatherOptions) PrintBasicClusterState(ctx context.Context) {
	fmt.Fprintf(o.RawOut, "When opening a support case, bugzilla, or issue please include the following summary data along with any other requested information.\n")

	clusterVersion, err := o.ConfigClient.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		fmt.Fprintf(o.RawOut, "error getting cluster version: %v\n", err)
	}
	fmt.Fprintf(o.RawOut, "ClusterID: %v\n", clusterVersion.Spec.ClusterID)
	fmt.Fprintf(o.RawOut, "ClusterVersion: %v\n", humanSummaryForClusterVersion(clusterVersion))

	clusterOperators, err := o.ConfigClient.ConfigV1().ClusterOperators().List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(o.RawOut, "error getting cluster operators: %v\n", err)
	}
	fmt.Fprintf(o.RawOut, "ClusterOperators:\n")
	fmt.Fprintf(o.RawOut, humanSummaryForInterestingClusterOperators(clusterOperators)+"\n")

	// TODO gather and display firing alerts
	fmt.Fprintf(o.RawOut, "\n\n")
}

// longExistingOperators is a list of operators that should be present on every variant of every platform and have
// existed for all supported version.  We use this to find when things are missing (like if they fail install).
var longExistingOperators = []string{
	"authentication",
	"cloud-credential",
	"cluster-autoscaler",
	"config-operator",
	"console",
	"dns",
	"etcd",
	"image-registry",
	"ingress",
	"insights",
	"kube-apiserver",
	"kube-controller-manager",
	"kube-scheduler",
	"kube-storage-version-migrator",
	"machine-api",
	"machine-approver",
	"machine-config",
	"marketplace",
	"monitoring",
	"network",
	"openshift-apiserver",
	"openshift-controller-manager",
	"operator-lifecycle-manager",
	"service-ca",
	"storage",
}

func humanSummaryForInterestingClusterOperators(clusterOperators *configv1.ClusterOperatorList) string {
	if clusterOperators == nil {
		return "\tclusteroperators not found"
	}
	if len(clusterOperators.Items) == 0 {
		return "\tclusteroperators are missing"
	}

	missingOperators := sets.NewString(longExistingOperators...)
	clusterOperatorStrings := []string{}
	for _, clusterOperator := range clusterOperators.Items {
		missingOperators.Delete(clusterOperator.Name)
		clusterOperatorSummary := humanSummaryForClusterOperator(clusterOperator)
		if len(clusterOperatorSummary) == 0 { // not noteworthy
			continue
		}
		clusterOperatorStrings = append(clusterOperatorStrings, humanSummaryForClusterOperator(clusterOperator))
	}

	for _, missingOperator := range missingOperators.List() {
		clusterOperatorStrings = append(clusterOperatorStrings, fmt.Sprintf("clusteroperator/%s is missing", missingOperator))
	}

	if len(clusterOperatorStrings) > 0 {
		return "\t" + strings.Join(clusterOperatorStrings, "\n\t")
	}
	return "\tAll healthy and stable"
}

func humanSummaryForClusterOperator(clusterOperator configv1.ClusterOperator) string {
	available := v1helpers.IsStatusConditionTrue(clusterOperator.Status.Conditions, configv1.OperatorAvailable)
	progressing := !v1helpers.IsStatusConditionFalse(clusterOperator.Status.Conditions, configv1.OperatorProgressing)
	degraded := !v1helpers.IsStatusConditionFalse(clusterOperator.Status.Conditions, configv1.OperatorDegraded)
	upgradeable := !v1helpers.IsStatusConditionFalse(clusterOperator.Status.Conditions, configv1.OperatorUpgradeable)

	availableMessage := "<missing>"
	progressingMessage := "<missing>"
	degradedMessage := "<missing>"
	upgradeableMessage := "<missing>"
	if condition := v1helpers.FindStatusCondition(clusterOperator.Status.Conditions, configv1.OperatorAvailable); condition != nil {
		availableMessage = condition.Message
	}
	if condition := v1helpers.FindStatusCondition(clusterOperator.Status.Conditions, configv1.OperatorProgressing); condition != nil {
		progressingMessage = condition.Message
	}
	if condition := v1helpers.FindStatusCondition(clusterOperator.Status.Conditions, configv1.OperatorDegraded); condition != nil {
		degradedMessage = condition.Message
	}
	if condition := v1helpers.FindStatusCondition(clusterOperator.Status.Conditions, configv1.OperatorUpgradeable); condition != nil {
		upgradeableMessage = condition.Message
	}

	switch {
	case !available:
		return fmt.Sprintf("clusteroperator/%s is not available (%v) because %v", clusterOperator.Name, availableMessage, degradedMessage)
	case degraded:
		return fmt.Sprintf("clusteroperator/%s is degraded because %v", clusterOperator.Name, degradedMessage)
	case !upgradeable:
		return fmt.Sprintf("clusteroperator/%s is not upgradeable because %v", clusterOperator.Name, upgradeableMessage)
	case progressing:
		return fmt.Sprintf("clusteroperator/%s is progressing: %v", clusterOperator.Name, progressingMessage)
	case available && !progressing && !degraded && upgradeable:
		return ""
	default:
		return fmt.Sprintf("clusteroperator/%s is in an edge case", clusterOperator.Name)
	}
}

func humanSummaryForClusterVersion(clusterVersion *configv1.ClusterVersion) string {
	if clusterVersion == nil {
		return "Cluster is in a version state we don't recognize"
	}

	isInstalling :=
		len(clusterVersion.Status.History) == 0 ||
			(len(clusterVersion.Status.History) == 1 && clusterVersion.Status.History[0].State != configv1.CompletedUpdate)
	isUpdating := len(clusterVersion.Status.History) > 1 && clusterVersion.Status.History[0].State != configv1.CompletedUpdate
	isStable := len(clusterVersion.Status.History) > 0 && clusterVersion.Status.History[0].State == configv1.CompletedUpdate

	lastChangeHumanDuration := "<unknown>"
	if len(clusterVersion.Status.History) > 0 {
		lastChangeHumanDuration = units.HumanDuration(time.Now().Sub(clusterVersion.Status.History[0].StartedTime.Time))
	}

	progressingConditionMessage := "<unknown>"
	for _, condition := range clusterVersion.Status.Conditions {
		if condition.Type == "Progressing" {
			progressingConditionMessage = condition.Message
			break
		}
	}
	switch {
	case isInstalling:
		return fmt.Sprintf("Installing %q for %v: %v",
			clusterVersion.Status.Desired.Version, lastChangeHumanDuration, progressingConditionMessage)
	case isUpdating:
		return fmt.Sprintf("Updating to %q from %q for %v: %v",
			clusterVersion.Status.Desired.Version, clusterVersion.Status.History[1].Version, lastChangeHumanDuration, progressingConditionMessage)
	case isStable:
		return fmt.Sprintf("Stable at %q", clusterVersion.Status.History[0].Version)
	default:
		return fmt.Sprintf("Unknown state")
	}
}
