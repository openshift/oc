package waitforstable

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"github.com/openshift/library-go/pkg/config/clusteroperator/v1helpers"
)

type WaitForStableOptions struct {
	RESTClientGetter genericclioptions.RESTClientGetter

	configClient configv1client.ClusterOperatorsGetter

	Timeout             time.Duration
	MinimumStablePeriod time.Duration
	waitInterval        time.Duration // to make unit testing easier

	genericclioptions.IOStreams
}

var (
	waitForStableLong = templates.LongDesc(`TODO:`)

	waitForStableExample = templates.Examples(`
		TODO: 
	`)
)

func NewWaitForStableOptions(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *WaitForStableOptions {
	return &WaitForStableOptions{
		RESTClientGetter:    restClientGetter,
		Timeout:             1 * time.Hour,
		MinimumStablePeriod: 5 * time.Minute,
		waitInterval:        10 * time.Second,

		IOStreams: streams,
	}
}

// NewCmdLogout implements a command to wait for OpenShift platform operators to become stable
func NewCmdWaitForStableClusterOperators(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewWaitForStableOptions(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:     "wait-for-stable-cluster",
		Short:   "wait for the platform operators to become stable",
		Long:    waitForStableLong,
		Example: waitForStableExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete())
			kcmdutil.CheckErr(o.Run(context.Background()))
		},
	}

	o.AddFlags(cmd)

	return cmd
}

func (o *WaitForStableOptions) AddFlags(cmd *cobra.Command) error {
	cmd.Flags().DurationVar(&o.Timeout, "timeout", o.Timeout, "duration before the command times out. Defaults to 1 hour.")
	cmd.Flags().DurationVar(&o.MinimumStablePeriod, "minumum-stable-period", o.MinimumStablePeriod, "minimum duration to consider a cluster stable. Defaults to 5 minutes.")

	return nil
}

func (o *WaitForStableOptions) Complete() error {
	if o.Timeout <= o.MinimumStablePeriod {
		return fmt.Errorf("--timeout must be greater than the --minimum-stable-period")
	}
	if o.waitInterval > o.MinimumStablePeriod {
		o.waitInterval = o.MinimumStablePeriod + 1*time.Second
	}

	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return err
	}

	configClient, err := configv1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	o.configClient = configClient

	return nil
}

func (o WaitForStableOptions) Run(ctx context.Context) error {
	if o.Timeout > 0 {
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
		defer cancel()
	}

	var stabilityStarted *time.Time
	waitErr := wait.PollImmediateUntilWithContext(ctx, o.waitInterval, func(waitCtx context.Context) (bool, error) {
		operators, err := o.configClient.ClusterOperators().List(waitCtx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(o.ErrOut, "failed to list clusteroperators: %v", err)
			stabilityStarted = nil
			return false, nil
		}

		for _, operator := range operators.Items {
			if unstableReason := unstableOperatorReason(&operator); len(unstableReason) > 0 {
				fmt.Fprintf(o.Out, "clusteroperators/%v is not yet stable: %v\n", operator.Name, unstableReason)
				stabilityStarted = nil
				return false, nil
			}
		}

		if stabilityStarted == nil {
			t := time.Now()
			stabilityStarted = &t
		}

		timeStable := time.Now().Sub(*stabilityStarted)
		if timeStable > o.MinimumStablePeriod {
			return true, nil
		}

		return false, nil
	})
	if waitErr != nil {
		return waitErr
	}

	fmt.Fprintf(o.Out, "All clusteroperators are stable\n")
	return nil
}

func unstableOperatorReason(operator *configv1.ClusterOperator) string {
	if !v1helpers.IsStatusConditionTrue(operator.Status.Conditions, configv1.OperatorAvailable) {
		return "available != true"
	}
	if !v1helpers.IsStatusConditionFalse(operator.Status.Conditions, configv1.OperatorProgressing) {
		return "progressing != false"
	}
	if !v1helpers.IsStatusConditionFalse(operator.Status.Conditions, configv1.OperatorDegraded) {
		return "degraded != false"
	}
	return ""
}
