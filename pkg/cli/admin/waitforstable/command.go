package waitforstable

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
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

	genericiooptions.IOStreams
}

var (
	waitForStableLong = templates.LongDesc(`
		Wait for all OCP v4 clusteroperators to report Available=true, Progressing=false, Degraded=false.
	`)

	waitForStableExample = templates.Examples(`
		# Wait for all cluster operators to become stable
		oc adm wait-for-stable-cluster

		# Consider operators to be stable if they report as such for 5 minutes straight
		oc adm wait-for-stable-cluster --minimum-stable-period 5m
	`)
)

func NewWaitForStableOptions(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *WaitForStableOptions {
	return &WaitForStableOptions{
		RESTClientGetter:    restClientGetter,
		Timeout:             1 * time.Hour,
		MinimumStablePeriod: 5 * time.Minute,
		waitInterval:        10 * time.Second,

		IOStreams: streams,
	}
}

// NewCmdLogout implements a command to wait for OpenShift platform operators to become stable
func NewCmdWaitForStableClusterOperators(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewWaitForStableOptions(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:     "wait-for-stable-cluster",
		Short:   "Wait for the platform operators to become stable",
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
	cmd.Flags().DurationVar(&o.MinimumStablePeriod, "minimum-stable-period", o.MinimumStablePeriod, "minimum duration to consider a cluster stable. Defaults to 5 minutes.")

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
	previouslyUnstableOperators := sets.NewString()
	operatorInstabilityStartTime := map[string]time.Time{}
	waitErr := wait.PollUntilContextCancel(ctx, o.waitInterval, true, func(waitCtx context.Context) (bool, error) {
		defer fmt.Fprintln(o.Out)

		operators, err := o.configClient.ClusterOperators().List(waitCtx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(o.ErrOut, "failed to list clusteroperators: %v", err)
			stabilityStarted = nil
			return false, nil
		}
		now := time.Now()

		newUnstableOperators := sets.NewString()
		for _, operator := range operators.Items {
			if unstableReason := unstableOperatorReason(&operator); len(unstableReason) > 0 {
				newUnstableOperators.Insert(operator.Name)
				if _, ok := operatorInstabilityStartTime[operator.Name]; !ok {
					operatorInstabilityStartTime[operator.Name] = now
				}
				switch {
				case previouslyUnstableOperators.Has(operator.Name):
					fmt.Fprintf(o.Out, "clusteroperators/%v is still %v after %v\n", operator.Name, unstableReason, now.Sub(operatorInstabilityStartTime[operator.Name]).Round(time.Second))
				case len(previouslyUnstableOperators) == 0:
					fmt.Fprintf(o.Out, "clusteroperators/%v is %v at %v\n", operator.Name, unstableReason, now.Format(time.RFC3339))
				default:
					fmt.Fprintf(o.Out, "clusteroperators/%v became %v at %v\n", operator.Name, unstableReason, now.Format(time.RFC3339))
				}
			} else {
				if previouslyUnstableOperators.Has(operator.Name) {
					fmt.Fprintf(o.Out, "clusteroperators/%v stabilized at %v after %v\n", operator.Name, now.Format(time.RFC3339), now.Sub(operatorInstabilityStartTime[operator.Name]).Round(time.Second))
				}
				delete(operatorInstabilityStartTime, operator.Name)
			}
		}
		previouslyUnstableOperators = newUnstableOperators

		if len(newUnstableOperators) > 0 {
			stabilityStarted = nil
			return false, nil
		}

		if stabilityStarted == nil {
			stabilityStarted = &now
			fmt.Fprintf(o.Out, "All clusteroperators became stable at %v\n", stabilityStarted.Format(time.RFC3339))
		} else {
			fmt.Fprintf(o.Out, "All clusteroperators are still stable after %v\n", now.Sub(*stabilityStarted).Round(time.Second))
		}

		timeStable := now.Sub(*stabilityStarted)
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
	notableConditions := []string{}
	if !v1helpers.IsStatusConditionTrue(operator.Status.Conditions, configv1.OperatorAvailable) {
		notableConditions = append(notableConditions, "unavailable")
	}
	if !v1helpers.IsStatusConditionFalse(operator.Status.Conditions, configv1.OperatorProgressing) {
		notableConditions = append(notableConditions, "progressing")
	}
	if !v1helpers.IsStatusConditionFalse(operator.Status.Conditions, configv1.OperatorDegraded) {
		notableConditions = append(notableConditions, "degraded")
	}

	return strings.Join(notableConditions, " and ")
}
