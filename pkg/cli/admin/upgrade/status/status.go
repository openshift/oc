// Package status displays the status of current cluster version updates.
package status

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
)

const (
	// clusterStatusFailing is set on the ClusterVersion status when a cluster
	// cannot reach the desired state.
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
		Use:   "status",
		Short: "Display the status of current cluster version updates.",
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run(cmd.Context()))
		},
	}

	return cmd
}

type options struct {
	genericiooptions.IOStreams

	Client configv1client.Interface
}

func (o *options) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
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

func (o *options) Run(ctx context.Context) error {
	cv, err := o.Client.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("no cluster version information available - you must be connected to an OpenShift version 4 server to fetch the current version")
		}
		return err
	}

	progressing := findClusterOperatorStatusCondition(cv.Status.Conditions, configv1.OperatorProgressing)
	if progressing == nil {
		return fmt.Errorf("no current %s info, see `oc describe clusterversion` for more details.\n", configv1.OperatorProgressing)
	}

	if progressing.Status != configv1.ConditionTrue {
		fmt.Fprintf(o.Out, "The cluster version is not updating (%s=%s).\n\n  Reason: %s\n  Message: %s\n", progressing.Type, progressing.Status, progressing.Reason, strings.ReplaceAll(progressing.Message, "\n", "\n  "))
		return nil
	}

	fmt.Fprintf(o.Out, "An update is in progress: %s\n", progressing.Message)

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, clusterStatusFailing); c != nil {
		if c.Status != configv1.ConditionFalse {
			fmt.Fprintf(o.Out, "\n%s=%s:\n\n  Reason: %s\n  Message: %s\n\n", c.Type, c.Status, c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
		}
	} else {
		fmt.Fprintf(o.ErrOut, "warning: No current %s info, see `oc describe clusterversion` for more details.\n", clusterStatusFailing)
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
