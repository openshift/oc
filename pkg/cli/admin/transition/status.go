package transition

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	operatorv1client "github.com/openshift/client-go/operator/clientset/versioned"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	statusLong = templates.LongDesc(`
		Monitor topology transition progress.

		Displays the current control plane and infrastructure topology status,
		and shows the cluster-config-operator transition conditions to monitor
		transition progress.
	`)

	statusExample = templates.Examples(`
		# Monitor transition progress
		oc adm transition status
	`)
)

// statusOptions holds options for the status subcommand
type statusOptions struct {
	configClient   configv1client.Interface
	operatorClient operatorv1client.Interface

	genericclioptions.IOStreams
}

// newCmdStatus creates the status subcommand
func newCmdStatus(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := &statusOptions{
		IOStreams: streams,
	}

	cmd := &cobra.Command{
		Use:     "status",
		Short:   "Monitor topology transition progress",
		Long:    statusLong,
		Example: statusExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.complete(f, cmd, args))
			kcmdutil.CheckErr(o.run())
		},
	}

	return cmd
}

// complete sets up all required fields from the factory
func (o *statusOptions) complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	restConfig, err := f.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to get REST config: %w", err)
	}

	o.configClient, err = configv1client.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create config client: %w", err)
	}

	o.operatorClient, err = operatorv1client.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create operator client: %w", err)
	}

	return nil
}

// run executes the status subcommand
func (o *statusOptions) run() error {
	ctx := context.TODO()

	// Get Infrastructure resource
	infra, err := o.configClient.ConfigV1().Infrastructures().Get(ctx, infrastructureResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get Infrastructure resource: %w", err)
	}

	// Display Control Plane Topology
	fmt.Fprintf(o.Out, "Control Plane Topology:\n")
	if infra.Spec.ControlPlaneTopology != "" {
		fmt.Fprintf(o.Out, "  Spec (desired):   %s\n", infra.Spec.ControlPlaneTopology)
	} else {
		fmt.Fprintf(o.Out, "  Spec (desired):   (not set)\n")
	}
	if infra.Status.ControlPlaneTopology != "" {
		fmt.Fprintf(o.Out, "  Status (current): %s\n\n", infra.Status.ControlPlaneTopology)
	} else {
		fmt.Fprintf(o.Out, "  Status (current): (not set)\n\n")
	}

	// Display Infrastructure Topology (status only - spec doesn't exist)
	fmt.Fprintf(o.Out, "Infrastructure Topology:\n")
	if infra.Status.InfrastructureTopology != "" {
		fmt.Fprintf(o.Out, "  Status (current): %s\n\n", infra.Status.InfrastructureTopology)
	} else {
		fmt.Fprintf(o.Out, "  Status (current): (not set)\n\n")
	}

	// Get cluster-config-operator Config resource to read transition status
	operatorConfig, err := o.operatorClient.OperatorV1().Configs().Get(ctx, clusterConfigOperatorResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get configs.operator.openshift.io/cluster: %w", err)
	}

	// Find topology transition conditions
	var progressingCond, upgradeableCond *operatorv1.OperatorCondition
	for i := range operatorConfig.Status.Conditions {
		cond := &operatorConfig.Status.Conditions[i]
		if cond.Type == topologyTransitionControllerProgressingCondition {
			progressingCond = cond
		} else if cond.Type == topologyTransitionControllerUpgradeableCondition {
			upgradeableCond = cond
		}
	}

	// Display Transition Status
	fmt.Fprintf(o.Out, "Transition Status:\n")

	// Check if transition is in progress
	if progressingCond != nil && progressingCond.Status == operatorv1.ConditionTrue {
		// Transition in progress
		fmt.Fprintf(o.Out, "  %s\n\n", progressingCond.Message)

		fmt.Fprintf(o.Out, "  Progressing: %s\n", progressingCond.Status)
		fmt.Fprintf(o.Out, "  Reason: %s\n", progressingCond.Reason)
		fmt.Fprintf(o.Out, "  Message: %s\n", progressingCond.Message)

		if upgradeableCond != nil {
			fmt.Fprintf(o.Out, "\n  Upgradeable: %s\n", upgradeableCond.Status)
			fmt.Fprintf(o.Out, "  Reason: %s\n", upgradeableCond.Reason)
			fmt.Fprintf(o.Out, "  Message: %s\n", upgradeableCond.Message)
		}
	} else if progressingCond != nil && progressingCond.Status == operatorv1.ConditionFalse &&
		progressingCond.Reason == topologyTransitionPreflightCheckFailedReason {
		// Preflight failed
		fmt.Fprintf(o.Out, "  Preflight checks failed\n\n")

		fmt.Fprintf(o.Out, "  Progressing: %s\n", progressingCond.Status)
		fmt.Fprintf(o.Out, "  Reason: %s\n", progressingCond.Reason)
		fmt.Fprintf(o.Out, "  Message: %s\n", progressingCond.Message)

		if upgradeableCond != nil {
			fmt.Fprintf(o.Out, "\n  Upgradeable: %s\n", upgradeableCond.Status)
			fmt.Fprintf(o.Out, "  Reason: %s\n", upgradeableCond.Reason)
			fmt.Fprintf(o.Out, "  Message: %s\n", upgradeableCond.Message)
		}
	} else if progressingCond != nil && progressingCond.Status == operatorv1.ConditionFalse &&
		progressingCond.Reason == topologyTransitionUnsupportedTransitionReason {
		// Unsupported transition
		fmt.Fprintf(o.Out, "  Unsupported transition\n\n")

		fmt.Fprintf(o.Out, "  Progressing: %s\n", progressingCond.Status)
		fmt.Fprintf(o.Out, "  Reason: %s\n", progressingCond.Reason)
		fmt.Fprintf(o.Out, "  Message: %s\n", progressingCond.Message)

		if upgradeableCond != nil {
			fmt.Fprintf(o.Out, "\n  Upgradeable: %s\n", upgradeableCond.Status)
			fmt.Fprintf(o.Out, "  Reason: %s\n", upgradeableCond.Reason)
			fmt.Fprintf(o.Out, "  Message: %s\n", upgradeableCond.Message)
		}
	} else {
		// No transition in progress
		fmt.Fprintf(o.Out, "  No transition in progress\n\n")

		if upgradeableCond != nil {
			fmt.Fprintf(o.Out, "  Upgradeable: %s\n", upgradeableCond.Status)
			fmt.Fprintf(o.Out, "  Reason: %s\n", upgradeableCond.Reason)
			fmt.Fprintf(o.Out, "  Message: %s\n", upgradeableCond.Message)
		}
	}

	return nil
}
