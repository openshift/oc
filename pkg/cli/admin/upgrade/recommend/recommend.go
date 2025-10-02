// Package recommend displays recommended update information.
package recommend

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/blang/semver"
	"github.com/spf13/cobra"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/rest"
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

			By default, this command displays recent potential target releases.  Use
			'--version VERSION' to display context for a particular target release.  Use
			'--show-outdated-releases' to display all known targets, including older
			releases.
		`),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run(cmd.Context()))
		},
	}
	flags := cmd.Flags()
	flags.BoolVar(&o.showOutdatedReleases, "show-outdated-releases", o.showOutdatedReleases, "Display additional older releases.  These releases may be exposed to known issues which have been fixed in more recent releases.  But all updates will contain fixes not present in your current release.")
	flags.StringVar(&o.rawVersion, "version", o.rawVersion, "Select a particular target release to display by version.")

	flags.BoolVar(&o.quiet, "quiet", o.quiet, "When --quiet is true and --version is set, only print unaccepted issue names.")
	flags.StringSliceVar(&o.accept, "accept", o.accept, "Comma-delimited names for issues that you find acceptable.  With --version, any unaccepted issues will result in a non-zero exit code.")

	flags.StringVar(&o.mockData.cvPath, "mock-clusterversion", "", "Path to a YAML ClusterVersion object to use for testing (will be removed later).")
	flags.MarkHidden("mock-clusterversion")

	return cmd
}

type options struct {
	genericiooptions.IOStreams

	mockData             mockData
	showOutdatedReleases bool

	// quiet configures the verbosity of output.  When 'quiet' is true and 'version' is set, only print unaccepted issue names.
	quiet bool

	// rawVersion is parsed into version by options.Complete.  Do not consume it directly outside of that early option handling.
	rawVersion string

	// version is the parsed form of rawVersion.  Consumers after options.Complete should prefer this property.
	version *semver.Version

	// accept is a slice of acceptable issue names.
	accept []string

	RESTConfig *rest.Config
	Client     configv1client.Interface
}

func (o *options) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	kcmdutil.RequireNoArguments(cmd, args)

	if o.mockData.cvPath == "" {
		var err error
		o.RESTConfig, err = f.ToRESTConfig()
		if err != nil {
			return err
		}
		o.RESTConfig.UserAgent = rest.DefaultKubernetesUserAgent() + "(upgrade-recommend)"

		o.Client, err = configv1client.NewForConfig(o.RESTConfig)
		if err != nil {
			return err
		}
	} else {
		cvSuffix := "-cv.yaml"
		o.mockData.alertsPath = strings.Replace(o.mockData.cvPath, cvSuffix, "-alerts.json", 1)
		err := o.mockData.load()
		if err != nil {
			return err
		}
	}

	if o.rawVersion == "" {
		if o.quiet {
			return errors.New("--quiet can only be set when --version is set")
		}
	} else {
		if o.showOutdatedReleases {
			return errors.New("when --version is set, --show-outdated-releases is unnecessary")
		}

		if version, err := semver.Parse(o.rawVersion); err != nil {
			return fmt.Errorf("cannot parse SemVer target %q: %v", o.rawVersion, err)
		} else {
			o.version = &version
		}
	}

	return nil
}

func (o *options) Run(ctx context.Context) error {
	issues := sets.New[string]()
	accept := sets.New[string](o.accept...)

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
			acceptContext := ""
			if accept.Has(string(clusterStatusFailing)) {
				acceptContext = "accepted "
			}
			if !o.quiet {
				fmt.Fprintf(o.Out, "%s%s=%s:\n\n  Reason: %s\n  Message: %s\n\n", acceptContext, c.Type, c.Status, c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
			}
			issues.Insert(string(clusterStatusFailing))
		}
	} else {
		acceptContext := ""
		if accept.Has(string(clusterStatusFailing)) {
			acceptContext = "accepted "
		}
		if !o.quiet {
			fmt.Fprintf(o.ErrOut, "%swarning: No current %s info, see `oc describe clusterversion` for more details.\n", acceptContext, clusterStatusFailing)
		}
		issues.Insert(string(clusterStatusFailing))
	}

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, configv1.OperatorProgressing); c != nil && c.Status == configv1.ConditionTrue && len(c.Message) > 0 {
		acceptContext := ""
		if accept.Has(string(configv1.OperatorProgressing)) {
			acceptContext = "accepted "
		}
		if !o.quiet {
			fmt.Fprintf(o.Out, "%sinfo: An update is in progress.  You may wish to let this update complete before requesting a new update.\n  %s\n\n", acceptContext, strings.ReplaceAll(c.Message, "\n", "\n  "))
		}
		issues.Insert(string(configv1.OperatorProgressing))
	}

	conditions, err := o.precheck(ctx)
	if err != nil {
		if !o.quiet {
			fmt.Fprintf(o.Out, "Failed to check for at least some preconditions: %v\n", err)
		}
		issues.Insert("FailedToCompletePrecheck")
	}
	var happyConditions []string
	var acceptedConditions []string
	var unhappyConditions []string
	for _, condition := range conditions {
		if condition.Status == metav1.ConditionTrue {
			happyConditions = append(happyConditions, fmt.Sprintf("%s (%s)", condition.Type, condition.Reason))
		} else {
			issues.Insert(condition.acceptanceName)
			if accept.Has(condition.acceptanceName) {
				acceptedConditions = append(acceptedConditions, condition.Type)
			} else {
				unhappyConditions = append(unhappyConditions, condition.Type)
			}
		}
	}

	if !o.quiet {
		if len(happyConditions) > 0 {
			sort.Strings(happyConditions)
			fmt.Fprintf(o.Out, "The following conditions found no cause for concern in updating this cluster to later releases: %s\n\n", strings.Join(happyConditions, ", "))
		}
		if len(acceptedConditions) > 0 {
			sort.Strings(acceptedConditions)
			fmt.Fprintf(o.Out, "The following conditions found cause for concern in updating this cluster to later releases, but were explicitly accepted via --accept: %s\n\n", strings.Join(acceptedConditions, ", "))
		}
		if len(unhappyConditions) > 0 {
			sort.Strings(unhappyConditions)
			fmt.Fprintf(o.Out, "The following conditions found cause for concern in updating this cluster to later releases: %s\n\n", strings.Join(unhappyConditions, ", "))

			for _, c := range conditions {
				if c.Status != metav1.ConditionTrue {
					fmt.Fprintf(o.Out, "%s=%s:\n\n  Reason: %s\n  Message: %s\n\n", c.Type, c.Status, c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
				}
			}
		}
	}

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, configv1.RetrievedUpdates); c != nil && c.Status != configv1.ConditionTrue {
		if !o.quiet {
			fmt.Fprintf(o.ErrOut, "warning: Cannot refresh available updates:\n  Reason: %s\n  Message: %s\n\n", c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
		}
		issues.Insert("CannotRetrieveUpdates")
	}

	if !o.quiet {
		if cv.Spec.Channel != "" {
			if cv.Spec.Upstream == "" {
				fmt.Fprint(o.Out, "Upstream update service is unset, so the cluster will use an appropriate default.\n")
			} else {
				fmt.Fprintf(o.Out, "Upstream update service: %s\n", cv.Spec.Upstream)
			}
			if len(cv.Status.Desired.Channels) > 0 {
				fmt.Fprintf(o.Out, "Channel: %s (available channels: %s)\n", cv.Spec.Channel, strings.Join(cv.Status.Desired.Channels, ", "))
			} else {
				fmt.Fprintf(o.Out, "Channel: %s\n", cv.Spec.Channel)
			}
		}
	}

	majorMinorBuckets := map[uint64]map[uint64][]configv1.ConditionalUpdate{}

	for i, update := range cv.Status.ConditionalUpdates {
		version, err := semver.Parse(update.Release.Version)
		if err != nil {
			if !o.quiet {
				fmt.Fprintf(o.ErrOut, "warning: Cannot parse SemVer available update %q: %v", update.Release.Version, err)
			}
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
			if !o.quiet {
				fmt.Fprintf(o.ErrOut, "warning: Cannot parse SemVer available update %q: %v", update.Version, err)
			}
			continue
		}

		if minorBuckets := majorMinorBuckets[version.Major]; minorBuckets == nil {
			majorMinorBuckets[version.Major] = make(map[uint64][]configv1.ConditionalUpdate, 0)
		}

		majorMinorBuckets[version.Major][version.Minor] = append(majorMinorBuckets[version.Major][version.Minor], configv1.ConditionalUpdate{
			Release: cv.Status.AvailableUpdates[i],
		})
	}

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, configv1.OperatorUpgradeable); c != nil && c.Status == configv1.ConditionFalse {
		if err := injectUpgradeableAsCondition(cv.Status.Desired.Version, c, majorMinorBuckets); err != nil && !o.quiet {
			if !o.quiet {
				fmt.Fprintf(o.ErrOut, "warning: Cannot inject %s=%s as a conditional update risk: %s\n\nReason: %s\n  Message: %s\n\n", c.Type, c.Status, err, c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
			}
		}
	}

	if o.version != nil {
		if len(majorMinorBuckets) == 0 {
			return fmt.Errorf("no updates available, so cannot display context for the requested release %s", o.version)
		}

		if major, ok := majorMinorBuckets[o.version.Major]; !ok {
			return fmt.Errorf("no updates to %d available, so cannot display context for the requested release %s", o.version.Major, o.version)
		} else if minor, ok := major[o.version.Minor]; !ok {
			return fmt.Errorf("no updates to %d.%d available, so cannot display context for the requested release %s", o.version.Major, o.version.Minor, o.version)
		} else {
			for _, update := range minor {
				if update.Release.Version == o.version.String() {
					if !o.quiet {
						fmt.Fprintln(o.Out)
					}
					if c := notRecommendedCondition(update); c == nil {
						if !o.quiet {
							fmt.Fprintf(o.Out, "Update to %s has no known issues relevant to this cluster.\nImage: %s\nRelease URL: %s\n", update.Release.Version, update.Release.Image, update.Release.URL)
						}
					} else {
						if !o.quiet {
							reason := c.Reason
							if accept.Has("ConditionalUpdateRisk") {
								reason = fmt.Sprintf("accepted %s via ConditionalUpdateRisk", c.Reason)
							}
							fmt.Fprintf(o.Out, "Update to %s %s=%s:\nImage: %s\nRelease URL: %s\nReason: %s\nMessage: %s\n", update.Release.Version, c.Type, c.Status, update.Release.Image, update.Release.URL, reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
						}
						issues.Insert("ConditionalUpdateRisk")
					}
					unaccepted := issues.Difference(accept)
					if unaccepted.Len() > 0 {
						return fmt.Errorf("issues that apply to this cluster but which were not included in --accept: %s", strings.Join(sets.List(unaccepted), ","))
					} else if issues.Len() > 0 && !o.quiet {
						fmt.Fprintf(o.Out, "Update to %s has no known issues relevant to this cluster other than the accepted %s.\n", update.Release.Version, strings.Join(sets.List(issues), ","))
					}
					return nil
				}
			}
			return fmt.Errorf("no update to %s available, so cannot display context for the requested release", o.version)
		}
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
			w := tabwriter.NewWriter(o.Out, 14, 2, 1, ' ', tabwriter.DiscardEmptyColumns)
			fmt.Fprintf(w, "  VERSION\tISSUES\n")
			// TODO: add metadata about version

			sortConditionalUpdatesBySemanticVersions(majorMinorBuckets[major][minor])
			for i, update := range majorMinorBuckets[major][minor] {
				c := notRecommendedCondition(update)
				if lastWasLong || (c != nil && !o.showOutdatedReleases) {
					fmt.Fprintln(o.Out)
					if c == nil && !headerQueued {
						fmt.Fprintf(w, "  VERSION\tISSUES\n")
						headerQueued = true
					}
					lastWasLong = false
				}
				if i == 2 && !o.showOutdatedReleases {
					fmt.Fprintf(o.Out, "And %d older %d.%d updates you can see with '--show-outdated-releases' or '--version VERSION'.\n", len(majorMinorBuckets[major][minor])-2, major, minor)
					lastWasLong = true
					break
				}
				if c == nil {
					fmt.Fprintf(w, "  %s\t%s\n", update.Release.Version, "no known issues relevant to this cluster")
					if !o.showOutdatedReleases {
						headerQueued = false
						w.Flush()
					}
				} else if o.showOutdatedReleases {
					fmt.Fprintf(w, "  %s\t%s\n", update.Release.Version, c.Reason)
				} else {
					fmt.Fprintf(o.Out, "  Version: %s\n  Image: %s\n", update.Release.Version, update.Release.Image)
					fmt.Fprintf(o.Out, "  Reason: %s\n  Message: %s\n", c.Reason, strings.ReplaceAll(strings.TrimSpace(c.Message), "\n", "\n  "))
					lastWasLong = true
				}
			}
			if o.showOutdatedReleases {
				w.Flush()
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

func injectUpgradeableAsCondition(version string, condition *configv1.ClusterOperatorStatusCondition, majorMinorBuckets map[uint64]map[uint64][]configv1.ConditionalUpdate) error {
	current, err := semver.Parse(version)
	if err != nil {
		return fmt.Errorf("cannot parse SemVer version %q: %v", version, err)
	}

	upgradeableURI := fmt.Sprintf("https://docs.openshift.com/container-platform/%d.%d/updating/preparing_for_updates/updating-cluster-prepare.html#cluster-upgradeable_updating-cluster-prepare", current.Major, current.Minor)
	if current.Minor <= 13 {
		upgradeableURI = fmt.Sprintf("https://docs.openshift.com/container-platform/%d.%d/updating/index.html#understanding_clusteroperator_conditiontypes_updating-clusters-overview", current.Major, current.Minor)
	}

	for major, minors := range majorMinorBuckets {
		if major < current.Major {
			continue
		}

		for minor, targets := range minors {
			if major == current.Major && minor <= current.Minor {
				continue
			}

			for i := 0; i < len(targets); i++ {
				majorMinorBuckets[major][minor][i] = ensureUpgradeableRisk(majorMinorBuckets[major][minor][i], condition, upgradeableURI)
			}
		}
	}

	return nil
}

func ensureUpgradeableRisk(target configv1.ConditionalUpdate, condition *configv1.ClusterOperatorStatusCondition, upgradeableURI string) configv1.ConditionalUpdate {
	if hasUpgradeableRisk(target, condition) {
		return target
	}

	target.Risks = append(target.Risks, configv1.ConditionalUpdateRisk{
		URL:           upgradeableURI,
		Name:          "UpgradeableFalse",
		Message:       condition.Message,
		MatchingRules: []configv1.ClusterCondition{{Type: "Always"}},
	})

	for i, c := range target.Conditions {
		if c.Type == "Recommended" {
			if c.Status == metav1.ConditionTrue {
				target.Conditions[i].Reason = condition.Reason
				target.Conditions[i].Message = condition.Message
			} else {
				target.Conditions[i].Reason = "MultipleReasons"
				target.Conditions[i].Message = fmt.Sprintf("%s\n\n%s", condition.Message, c.Message)
			}
			target.Conditions[i].Status = metav1.ConditionFalse
			return target
		}
	}

	target.Conditions = append(target.Conditions, metav1.Condition{
		Type:    "Recommended",
		Status:  metav1.ConditionFalse,
		Reason:  condition.Reason,
		Message: condition.Message,
	})
	return target
}

func hasUpgradeableRisk(target configv1.ConditionalUpdate, condition *configv1.ClusterOperatorStatusCondition) bool {
	for _, risk := range target.Risks {
		if strings.Contains(risk.Message, condition.Message) {
			return true
		}
	}
	return false
}
