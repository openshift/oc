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
	flags.BoolVar(&o.showOutdatedReleases, "show-outdated-releases", o.showOutdatedReleases, "Display additional older releases.  These releases may be exposed to known issues which have been fixed in more recent releases.  But all updates will contain fixes not present in your current release.")

	// TODO: We can remove this flag once the idea about `oc adm upgrade recommend` stabilizes and the command
	//       is promoted out of the OC_ENABLE_CMD_UPGRADE_RECOMMEND feature gate
	flags.StringVar(&o.mockData.cvPath, "mock-clusterversion", "", "Path to a YAML ClusterVersion object to use for testing (will be removed later).")

	return cmd
}

type options struct {
	genericiooptions.IOStreams

	mockData             mockData
	showOutdatedReleases bool

	Client configv1client.Interface
}

func (o *options) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	kcmdutil.RequireNoArguments(cmd, args)

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
	if cv = o.mockData.clusterVersion; cv == nil {
		var err error
		cv, err = o.Client.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("No cluster version information available - you must be connected to an OpenShift version 4 server to fetch the current version")
			}
			return err
		}
	}

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, clusterStatusFailing); c != nil {
		if c.Status != configv1.ConditionFalse {
			fmt.Fprintf(o.Out, "%s=%s:\n\n  Reason: %s\n  Message: %s\n\n", c.Type, c.Status, c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
		}
	} else {
		fmt.Fprintf(o.ErrOut, "warning: No current %s info, see `oc describe clusterversion` for more details.\n", clusterStatusFailing)
	}

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, configv1.OperatorProgressing); c != nil && c.Status == configv1.ConditionTrue && len(c.Message) > 0 {
		fmt.Fprintf(o.Out, "info: An update is in progress.  You may wish to let this update complete before requesting a new update.\n  %s\n", strings.ReplaceAll(c.Message, "\n", "\n  "))
	}

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, configv1.RetrievedUpdates); c != nil && c.Status != configv1.ConditionTrue {
		fmt.Fprintf(o.ErrOut, "warning: Cannot refresh available updates:\n  Reason: %s\n  Message: %s\n\n", c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
	}

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, configv1.OperatorUpgradeable); c != nil && c.Status == configv1.ConditionFalse {
		fmt.Fprintf(o.Out, "%s=%s\n\n  Reason: %s\n  Message: %s\n\n", c.Type, c.Status, c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
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

	majorMinorBuckets := map[uint64]map[uint64][]configv1.ConditionalUpdate{}

	for i, update := range cv.Status.ConditionalUpdates {
		version, err := semver.Parse(update.Release.Version)
		if err != nil {
			fmt.Fprintf(o.ErrOut, "warning: Cannot parse SemVer available update %q: %v", update.Release.Version, err)
			continue
		}

		if minorBuckets := majorMinorBuckets[version.Major]; minorBuckets == nil {
			majorMinorBuckets[version.Major] = make(map[uint64][]configv1.ConditionalUpdate, 0)
		}

		majorMinorBuckets[version.Major][version.Minor] = append(majorMinorBuckets[version.Major][version.Minor], cv.Status.ConditionalUpdates[i])
	}

	for i, update := range cv.Status.AvailableUpdates {
		found := false
		for _, conditionalUpdate := range cv.Status.ConditionalUpdates {
			if conditionalUpdate.Release.Image == update.Image {
				found = true
				break
			}
		}
		if found {
			continue
		}

		version, err := semver.Parse(update.Version)
		if err != nil {
			fmt.Fprintf(o.ErrOut, "warning: Cannot parse SemVer available update %q: %v", update.Version, err)
			continue
		}

		if minorBuckets := majorMinorBuckets[version.Major]; minorBuckets == nil {
			majorMinorBuckets[version.Major] = make(map[uint64][]configv1.ConditionalUpdate, 0)
		}

		majorMinorBuckets[version.Major][version.Minor] = append(majorMinorBuckets[version.Major][version.Minor], configv1.ConditionalUpdate{
			Release: cv.Status.AvailableUpdates[i],
		})
	}

	if len(majorMinorBuckets) == 0 {
		fmt.Fprintf(o.Out, "No updates available. You may still upgrade to a specific release image with --to-image or wait for new updates to be available.\n")
		return nil
	}

	majors := make([]uint64, 0, len(majorMinorBuckets))
	for major := range majorMinorBuckets {
		majors = append(majors, major)
	}
	sort.Slice(majors, func(i, j int) bool {
		return majors[i] > majors[j] // sort descending, major updates bring lots of features (enough to justify breaking backwards compatibility)
	})
	for _, major := range majors {
		minors := make([]uint64, 0, len(majorMinorBuckets[major]))
		for minor := range majorMinorBuckets[major] {
			minors = append(minors, minor)
		}
		sort.Slice(minors, func(i, j int) bool {
			return minors[i] > minors[j] // sort descending, minor updates bring both feature and bugfixes
		})
		for _, minor := range minors {
			fmt.Fprintln(o.Out)
			fmt.Fprintf(o.Out, "Updates to %d.%d:\n", major, minor)
			lastWasLong := false
			headerQueued := true

			// set the minimal cell width to 14 to have a larger space between the columns for shorter versions
			w := tabwriter.NewWriter(o.Out, 14, 2, 1, ' ', 0)
			fmt.Fprintf(w, "  VERSION\tIMAGE\n")
			// TODO: add metadata about version

			sortConditionalUpdatesBySemanticVersions(majorMinorBuckets[major][minor])
			for i, update := range majorMinorBuckets[major][minor] {
				c := notRecommendedCondition(update)
				if lastWasLong || c != nil {
					fmt.Fprintln(o.Out)
					if c == nil && !headerQueued {
						fmt.Fprintf(w, "  VERSION\tIMAGE\n")
						headerQueued = true
					}
					lastWasLong = false
				}
				if i == 2 && !o.showOutdatedReleases {
					fmt.Fprintf(o.Out, "And %d older %d.%d updates you can see with --show-outdated-releases\n", len(majorMinorBuckets[major][minor])-2, major, minor)
					lastWasLong = true
					break
				}
				if c == nil {
					fmt.Fprintf(w, "  %s\t%s\n", update.Release.Version, update.Release.Image)
					w.Flush()
					headerQueued = false
				} else {
					fmt.Fprintf(o.Out, "  Version: %s\n  Image: %s\n", update.Release.Version, update.Release.Image)
					fmt.Fprintf(o.Out, "  Reason: %s\n  Message: %s\n", c.Reason, strings.ReplaceAll(strings.TrimSpace(c.Message), "\n", "\n  "))
					lastWasLong = true
				}
			}
		}
	}

	return nil
}

func notRecommendedCondition(update configv1.ConditionalUpdate) *metav1.Condition {
	if len(update.Risks) == 0 {
		return nil
	}
	if c := findCondition(update.Conditions, "Recommended"); c != nil {
		if c.Status == metav1.ConditionTrue {
			return nil
		}
		return c
	}

	risks := make([]string, len(update.Risks))
	for _, risk := range update.Risks {
		risks = append(risks, risk.Name)
	}
	sort.Strings(risks)
	return &metav1.Condition{
		Type:    "Recommended",
		Status:  "Unknown",
		Reason:  "NoConditions",
		Message: fmt.Sprintf("Conditional update to %s has risks (%s), but no conditions.", update.Release.Version, strings.Join(risks, ", ")),
	}
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
