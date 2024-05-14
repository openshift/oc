// Package rollback initiates a rollback to a previous release.
package rollback

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/blang/semver"
	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

func newOptions(streams genericiooptions.IOStreams) *options {
	return &options{
		IOStreams: streams,
	}
}

func New(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := newOptions(streams)
	cmd := &cobra.Command{
		Use:    "rollback",
		Hidden: true,
		Short:  "Rollback the cluster to the previous release.",
		Long: templates.LongDesc(`
			Rollback the cluster to the previous release.

			Only patch version rollbacks within the same z stream, e.g. 4.y.newer to 4.y.older, are accepted by
			the cluster-version operator.  Minor version rollbacks from 4.newer to 4.older are not accepted.
			Updates to releases other than the most-recent previous release that the cluster was attempting are
			not accepted.  Rolling back re-exposes the cluster to all the bugs which had been fixed from
			4.y.older to 4.y.newer.  In most cases, you probably want to understand what is having trouble and
			roll forward with fixes.
		`),

		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run(cmd.Context()))
		},
	}

	return cmd
}

// clusterVersionInterface is the subset of configv1client.ClusterVersionInterface
// that we need, for easier mocking in unit tests.
type clusterVersionInterface interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*configv1.ClusterVersion, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *configv1.ClusterVersion, err error)
}

type options struct {
	genericiooptions.IOStreams

	Client clusterVersionInterface
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
	o.Client = client.ClusterVersions()

	return nil
}

func (o *options) Run(ctx context.Context) error {
	cv, err := o.Client.Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("no cluster version information available - you must be connected to an OpenShift version 4 server to fetch the current version")
		}
		return err
	}

	targetVersion, err := semver.Parse(cv.Status.Desired.Version)
	if err != nil {
		return fmt.Errorf("invalid ClusterVersion status.desired.version: %w", err)
	}

	var previousVersion *semver.Version
	var previousImage string
	for _, entry := range cv.Status.History {
		if entry.Version != targetVersion.String() || entry.Image != cv.Status.Desired.Image {
			version, err := semver.Parse(entry.Version)
			if err != nil {
				return fmt.Errorf("previous version %q invalid SemVer: %w", entry.Version, err)
			} else {
				previousVersion = &version
				previousImage = entry.Image
			}
			break
		}
	}

	if previousVersion == nil {
		return fmt.Errorf("no previous version found in ClusterVersion's status.history besides the current %s (%s).", targetVersion, cv.Status.Desired.Image)
	}

	if previousVersion.GE(targetVersion) {
		return fmt.Errorf("previous version %s (%s) is greater than or equal to current version %s (%s).  Use 'oc adm upgrade ...' to update, and not this rollback command.", previousVersion, previousImage, targetVersion, cv.Status.Desired.Image)
	}

	if previousVersion.Major != targetVersion.Major || previousVersion.Minor != targetVersion.Minor {
		return fmt.Errorf("%s is less than the current target %s and matches the cluster's previous version, but rollbacks that change major or minor versions are not recommended.", previousVersion, targetVersion)
	}

	update := &configv1.Update{
		Version: previousVersion.String(),
		Image:   previousImage,
	}

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, configv1.OperatorProgressing); c == nil {
		return fmt.Errorf("no current %s info, see `oc describe clusterversion` for more details.\n", configv1.OperatorProgressing)
	} else if c.Status != configv1.ConditionFalse {
		return fmt.Errorf("unable to rollback while an update is %s=%s: %s: %s.", c.Type, c.Status, c.Reason, c.Message)
	}

	if err := patchDesiredUpdate(ctx, update, o.Client, cv.Name); err != nil {
		return err
	}

	fmt.Fprintf(o.Out, "Requested rollback from %s to %s\n", targetVersion, previousVersion)

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

func patchDesiredUpdate(ctx context.Context, update *configv1.Update, client clusterVersionInterface,
	clusterVersionName string) error {

	updateJSON, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("marshal ClusterVersion patch: %v", err)
	}
	patch := []byte(fmt.Sprintf(`{"spec":{"desiredUpdate": %s}}`, updateJSON))
	if _, err := client.Patch(ctx, clusterVersionName, types.MergePatchType, patch,
		metav1.PatchOptions{}); err != nil {

		return fmt.Errorf("Unable to rollback: %v", err)
	}
	return nil
}
