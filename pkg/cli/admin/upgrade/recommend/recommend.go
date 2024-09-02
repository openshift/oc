// Package recommend displays recommended update information.
package recommend

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/blang/semver"
	"github.com/spf13/cobra"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
)

const (
	// clusterStatusFailing is set on the ClusterVersion status when a cluster
	// cannot reach the desired state. It is considered more serious than Degraded
	// and indicates the cluster is not healthy.
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
		Use:   "recommend",
		Short: "Displays cluster update recommendations.",
		Long: templates.LongDesc(`
			Displays cluster update recommendations.

			This subcommand is read-only and does not affect the state of the cluster.
			To request an update, use the 'oc adm upgrade' subcommand.
		`),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run(cmd.Context()))
		},
	}
	flags := cmd.Flags()
	flags.BoolVar(&o.IncludeNotRecommended, "include-not-recommended", o.IncludeNotRecommended, "Display additional updates which are not recommended based on your cluster configuration.")

	return cmd
}

type options struct {
	genericiooptions.IOStreams

	IncludeNotRecommended bool

	Client configv1client.Interface
}

func (o *options) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	kcmdutil.RequireNoArguments(cmd, args)

	cfg, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	client, err := configv1client.NewForConfig(cfg)
	if err != nil {
		return err
	}
	o.Client = client
	return nil
}

func (o *options) Run(ctx context.Context) error {
	cv, err := o.Client.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("No cluster version information available - you must be connected to an OpenShift version 4 server to fetch the current version")
		}
		return err
	}

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, clusterStatusFailing); c != nil {
		if c.Status != configv1.ConditionFalse {
			fmt.Fprintf(o.Out, "%s=%s:\n\n  Reason: %s\n  Message: %s\n\n", c.Type, c.Status, c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
		}
	} else {
		fmt.Fprintf(o.ErrOut, "warning: No current %s info, see `oc describe clusterversion` for more details.\n", clusterStatusFailing)
	}

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, configv1.OperatorProgressing); c != nil && len(c.Message) > 0 {
		if c.Status == configv1.ConditionTrue {
			fmt.Fprintf(o.Out, "info: An upgrade is in progress. %s\n", c.Message)
		} else {
			fmt.Fprintln(o.Out, c.Message)
		}
	} else {
		fmt.Fprintf(o.ErrOut, "warning: No current %s info, see `oc describe clusterversion` for more details.\n", configv1.OperatorProgressing)
	}
	fmt.Fprintln(o.Out)

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, configv1.OperatorUpgradeable); c != nil && c.Status == configv1.ConditionFalse {
		fmt.Fprintf(o.Out, "%s=%s\n\n  Reason: %s\n  Message: %s\n\n", c.Type, c.Status, c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
	}

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, "ReleaseAccepted"); c != nil && c.Status != configv1.ConditionTrue {
		fmt.Fprintf(o.Out, "ReleaseAccepted=%s\n\n  Reason: %s\n  Message: %s\n\n", c.Status, c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
	}

	if cv.Spec.Channel != "" {
		if cv.Spec.Upstream == "" {
			fmt.Fprint(o.Out, "Upstream is unset, so the cluster will use an appropriate default.\n")
		} else {
			fmt.Fprintf(o.Out, "Upstream: %s\n", cv.Spec.Upstream)
		}
		if len(cv.Status.Desired.Channels) > 0 {
			fmt.Fprintf(o.Out, "Channel: %s (available channels: %s)\n", cv.Spec.Channel, strings.Join(cv.Status.Desired.Channels, ", "))
		} else {
			fmt.Fprintf(o.Out, "Channel: %s\n", cv.Spec.Channel)
		}
	}

	if len(cv.Status.AvailableUpdates) > 0 {
		fmt.Fprintf(o.Out, "\nRecommended updates:\n\n")
		// set the minimal cell width to 14 to have a larger space between the columns for shorter versions
		w := tabwriter.NewWriter(o.Out, 14, 2, 1, ' ', 0)
		fmt.Fprintf(w, "  VERSION\tIMAGE\n")
		// TODO: add metadata about version
		sortReleasesBySemanticVersions(cv.Status.AvailableUpdates)
		for _, update := range cv.Status.AvailableUpdates {
			fmt.Fprintf(w, "  %s\t%s\n", update.Version, update.Image)
		}
		w.Flush()
		if c := findClusterOperatorStatusCondition(cv.Status.Conditions, configv1.RetrievedUpdates); c != nil && c.Status == configv1.ConditionFalse {
			fmt.Fprintf(o.ErrOut, "warning: Cannot refresh available updates:\n  Reason: %s\n  Message: %s\n\n", c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
		}
	} else {
		if c := findClusterOperatorStatusCondition(cv.Status.Conditions, configv1.RetrievedUpdates); c != nil && c.Status == configv1.ConditionFalse {
			fmt.Fprintf(o.ErrOut, "warning: Cannot display available updates:\n  Reason: %s\n  Message: %s\n\n", c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
		} else {
			fmt.Fprintf(o.Out, "No updates available. You may still upgrade to a specific release image with --to-image or wait for new updates to be available.\n")
		}
	}

	if o.IncludeNotRecommended {
		if containsNotRecommendedUpdate(cv.Status.ConditionalUpdates) {
			sortConditionalUpdatesBySemanticVersions(cv.Status.ConditionalUpdates)
			fmt.Fprintf(o.Out, "\nUpdates with known issues:\n")
			for _, update := range cv.Status.ConditionalUpdates {
				if c := findCondition(update.Conditions, "Recommended"); c != nil && c.Status != metav1.ConditionTrue {
					fmt.Fprintf(o.Out, "\n  Version: %s\n  Image: %s\n", update.Release.Version, update.Release.Image)
					fmt.Fprintf(o.Out, "  Reason: %s\n  Message: %s\n", c.Reason, strings.ReplaceAll(strings.TrimSpace(c.Message), "\n", "\n  "))
				}
			}
		} else {
			fmt.Fprintf(o.Out, "\nNo updates which are not recommended based on your cluster configuration are available.\n")
		}
	} else if containsNotRecommendedUpdate(cv.Status.ConditionalUpdates) {
		qualifier := ""
		for _, upgrade := range cv.Status.ConditionalUpdates {
			if c := findCondition(upgrade.Conditions, "Recommended"); c != nil && c.Status != metav1.ConditionTrue && c.Status != metav1.ConditionFalse {
				qualifier = fmt.Sprintf(", or where the recommended status is %q,", c.Status)
				break
			}
		}
		fmt.Fprintf(o.Out, "\nAdditional updates which are not recommended%s for your cluster configuration are available, to view those re-run the command with --include-not-recommended.\n", qualifier)
	}

	// TODO: print previous versions

	return nil
}

func containsNotRecommendedUpdate(updates []configv1.ConditionalUpdate) bool {
	for _, update := range updates {
		if c := findCondition(update.Conditions, "Recommended"); c != nil && c.Status != metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// sortReleasesBySemanticVersions sorts the input slice in decreasing order.
func sortReleasesBySemanticVersions(versions []configv1.Release) {
	sort.Slice(versions, func(i, j int) bool {
		a, errA := semver.Parse(versions[i].Version)
		b, errB := semver.Parse(versions[j].Version)
		if errA == nil && errB != nil {
			return true
		}
		if errB == nil && errA != nil {
			return false
		}
		if errA != nil && errB != nil {
			return versions[i].Version > versions[j].Version
		}
		return a.GT(b)
	})
}

// sortConditionalUpdatesBySemanticVersions sorts the input slice in decreasing order.
func sortConditionalUpdatesBySemanticVersions(updates []configv1.ConditionalUpdate) {
	sort.Slice(updates, func(i, j int) bool {
		a, errA := semver.Parse(updates[i].Release.Version)
		b, errB := semver.Parse(updates[j].Release.Version)
		if errA == nil && errB != nil {
			return true
		}
		if errB == nil && errA != nil {
			return false
		}
		if errA != nil && errB != nil {
			return updates[i].Release.Version > updates[j].Release.Version
		}
		return a.GT(b)
	})
}

func findCondition(conditions []metav1.Condition, name string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == name {
			return &conditions[i]
		}
	}
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
