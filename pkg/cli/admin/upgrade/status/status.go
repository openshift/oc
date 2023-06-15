// Package status contains a command for displaying a cluster's current status.
package status

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/blang/semver"
	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

const (
	// ClusterStatusFailing is set on the ClusterVersion status when a cluster
	// cannot reach the desired state. It is considered more serious than Degraded
	// and indicates the cluster is not healthy.
	ClusterStatusFailing = configv1.ClusterStatusConditionType("Failing")
)

func NewOptions(streams genericclioptions.IOStreams) *Options {
	return &Options{
		IOStreams: streams,
	}
}

func New(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewOptions(streams)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Display the cluster's current status",
		Long: templates.LongDesc(`
			Display the cluster's current status.

			Retrieve the current version info and display whether an upgrade is in progress or
			whether any errors might prevent an upgrade, as well as show the suggested
			updates available to the cluster. Information about compatible updates is periodically
			retrieved from the update server and cached on the cluster - these are updates that are
			known to be supported as upgrades from the current version.
		`),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run())
		},
	}
	flags := cmd.Flags()
	flags.BoolVar(&o.IncludeNotRecommended, "include-not-recommended", o.IncludeNotRecommended, "Display additional updates which are not recommended based on your cluster configuration.")
	return cmd
}

type Options struct {
	genericclioptions.IOStreams

	IncludeNotRecommended bool

	Client configv1client.Interface
}

func (o *Options) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return kcmdutil.UsageErrorf(cmd, "positional arguments given")
	}

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

func (o *Options) Run() error {
	ctx := context.TODO()
	clusterVersion, err := o.Client.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("no cluster version information available - you must be connected to an OpenShift version 4 server to fetch the current version")
		}
		return err
	}

	return o.Display(ctx, clusterVersion)
}

func (o *Options) Display(ctx context.Context, clusterVersion *configv1.ClusterVersion) error {
	if c := FindClusterOperatorStatusCondition(clusterVersion.Status.Conditions, ClusterStatusFailing); c != nil {
		if c.Status != configv1.ConditionFalse {
			fmt.Fprintf(o.Out, "%s=%s:\n\n  Reason: %s\n  Message: %s\n\n", c.Type, c.Status, c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
		}
	} else {
		fmt.Fprintf(o.ErrOut, "warning: No current %s info, see `oc describe clusterversion` for more details.\n", ClusterStatusFailing)
	}

	if c := FindClusterOperatorStatusCondition(clusterVersion.Status.Conditions, configv1.OperatorProgressing); c != nil && len(c.Message) > 0 {
		if c.Status == configv1.ConditionTrue {
			fmt.Fprintf(o.Out, "info: An upgrade is in progress. %s\n", c.Message)
		} else {
			fmt.Fprintln(o.Out, c.Message)
		}
	} else {
		fmt.Fprintf(o.ErrOut, "warning: No current %s info, see `oc describe clusterversion` for more details.\n", configv1.OperatorProgressing)
	}
	fmt.Fprintln(o.Out)

	if c := FindClusterOperatorStatusCondition(clusterVersion.Status.Conditions, configv1.OperatorUpgradeable); c != nil && c.Status == configv1.ConditionFalse {
		fmt.Fprintf(o.Out, "%s=%s\n\n  Reason: %s\n  Message: %s\n\n", c.Type, c.Status, c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
	}

	if c := FindClusterOperatorStatusCondition(clusterVersion.Status.Conditions, "ReleaseAccepted"); c != nil && c.Status != configv1.ConditionTrue {
		fmt.Fprintf(o.Out, "ReleaseAccepted=%s\n\n  Reason: %s\n  Message: %s\n\n", c.Status, c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
	}

	if clusterVersion.Spec.Channel != "" {
		if clusterVersion.Spec.Upstream == "" {
			fmt.Fprint(o.Out, "Upstream is unset, so the cluster will use an appropriate default.\n")
		} else {
			fmt.Fprintf(o.Out, "Upstream: %s\n", clusterVersion.Spec.Upstream)
		}
		if len(clusterVersion.Status.Desired.Channels) > 0 {
			fmt.Fprintf(o.Out, "Channel: %s (available channels: %s)\n", clusterVersion.Spec.Channel, strings.Join(clusterVersion.Status.Desired.Channels, ", "))
		} else {
			fmt.Fprintf(o.Out, "Channel: %s\n", clusterVersion.Spec.Channel)
		}
	}

	if len(clusterVersion.Status.AvailableUpdates) > 0 {
		fmt.Fprintf(o.Out, "\nRecommended updates:\n\n")
		// set the minimal cell width to 14 to have a larger space between the columns for shorter versions
		w := tabwriter.NewWriter(o.Out, 14, 2, 1, ' ', 0)
		fmt.Fprintf(w, "  VERSION\tIMAGE\n")
		// TODO: add metadata about version
		SortReleasesBySemanticVersions(clusterVersion.Status.AvailableUpdates)
		for _, update := range clusterVersion.Status.AvailableUpdates {
			fmt.Fprintf(w, "  %s\t%s\n", update.Version, update.Image)
		}
		w.Flush()
		if c := FindClusterOperatorStatusCondition(clusterVersion.Status.Conditions, configv1.RetrievedUpdates); c != nil && c.Status == configv1.ConditionFalse {
			fmt.Fprintf(o.ErrOut, "warning: Cannot refresh available updates:\n  Reason: %s\n  Message: %s\n\n", c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
		}
	} else {
		if c := FindClusterOperatorStatusCondition(clusterVersion.Status.Conditions, configv1.RetrievedUpdates); c != nil && c.Status == configv1.ConditionFalse {
			fmt.Fprintf(o.ErrOut, "warning: Cannot display available updates:\n  Reason: %s\n  Message: %s\n\n", c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
		} else {
			fmt.Fprintf(o.Out, "No updates available. You may still upgrade to a specific release image with --to-image or wait for new updates to be available.\n")
		}
	}

	if o.IncludeNotRecommended {
		if containsNotRecommendedUpdate(clusterVersion.Status.ConditionalUpdates) {
			sortConditionalUpdatesBySemanticVersions(clusterVersion.Status.ConditionalUpdates)
			fmt.Fprintf(o.Out, "\nSupported but not recommended updates:\n")
			for _, update := range clusterVersion.Status.ConditionalUpdates {
				if c := FindCondition(update.Conditions, "Recommended"); c != nil && c.Status != metav1.ConditionTrue {
					fmt.Fprintf(o.Out, "\n  Version: %s\n  Image: %s\n", update.Release.Version, update.Release.Image)
					fmt.Fprintf(o.Out, "  Recommended: %s\n  Reason: %s\n  Message: %s\n", c.Status, c.Reason, strings.ReplaceAll(strings.TrimSpace(c.Message), "\n", "\n  "))
				}
			}
		} else {
			fmt.Fprintf(o.Out, "\nNo updates which are not recommended based on your cluster configuration are available.\n")
		}
	} else if containsNotRecommendedUpdate(clusterVersion.Status.ConditionalUpdates) {
		fmt.Fprintf(o.Out, "\nAdditional updates which are not recommended based on your cluster configuration are available, to view those re-run the command with --include-not-recommended.\n")
	}

	// TODO: print previous versions

	return nil
}

// FindCondition finds a metav1 condition by type.
func FindCondition(conditions []metav1.Condition, name string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == name {
			return &conditions[i]
		}
	}
	return nil
}

// FindClusterOperatorStatusCondition finds a ClusterOperatorStatusCondition by name.
func FindClusterOperatorStatusCondition(conditions []configv1.ClusterOperatorStatusCondition, name configv1.ClusterStatusConditionType) *configv1.ClusterOperatorStatusCondition {
	for i := range conditions {
		if conditions[i].Type == name {
			return &conditions[i]
		}
	}
	return nil
}

func containsNotRecommendedUpdate(updates []configv1.ConditionalUpdate) bool {
	for _, update := range updates {
		if c := FindCondition(update.Conditions, "Recommended"); c != nil && c.Status != metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// SortReleasesBySemanticVersions sorts the input slice in decreasing order.
func SortReleasesBySemanticVersions(versions []configv1.Release) {
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
