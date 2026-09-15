package transition

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	operatorv1 "github.com/openshift/api/operator/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	operatorv1client "github.com/openshift/client-go/operator/clientset/versioned"
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
			kcmdutil.CheckErr(o.run(cmd.Context()))
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
func (o *statusOptions) run(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, apiRequestTimeout)
	defer cancel()

	// Get Infrastructure resource
	infra, err := o.configClient.ConfigV1().Infrastructures().Get(ctx, infrastructureResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get Infrastructure resource: %w", err)
	}

	// Display Control Plane Topology
	if _, err := fmt.Fprintf(o.Out, "Control Plane Topology:\n"); err != nil {
		return err
	}
	if infra.Spec.ControlPlaneTopology != "" {
		if _, err := fmt.Fprintf(o.Out, "  Spec (desired):   %s\n", infra.Spec.ControlPlaneTopology); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(o.Out, "  Spec (desired):   (not set)\n"); err != nil {
			return err
		}
	}
	if infra.Status.ControlPlaneTopology != "" {
		if _, err := fmt.Fprintf(o.Out, "  Status (current): %s\n\n", infra.Status.ControlPlaneTopology); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(o.Out, "  Status (current): (not set)\n\n"); err != nil {
			return err
		}
	}

	// Display Infrastructure Topology (status only - spec doesn't exist)
	if _, err := fmt.Fprintf(o.Out, "Infrastructure Topology:\n"); err != nil {
		return err
	}
	if infra.Status.InfrastructureTopology != "" {
		if _, err := fmt.Fprintf(o.Out, "  Status (current): %s\n\n", infra.Status.InfrastructureTopology); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(o.Out, "  Status (current): (not set)\n\n"); err != nil {
			return err
		}
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
	if _, err := fmt.Fprintf(o.Out, "Transition Status:\n"); err != nil {
		return err
	}

	// Check if transition is in progress
	if progressingCond != nil && progressingCond.Status == operatorv1.ConditionTrue {
		// Transition in progress
		if _, err := fmt.Fprintf(o.Out, "  %s\n\n  Progressing: %s\n  Reason: %s\n  Message: %s\n", progressingCond.Message, progressingCond.Status, progressingCond.Reason, progressingCond.Message); err != nil {
			return err
		}

		if upgradeableCond != nil {
			if _, err := fmt.Fprintf(o.Out, "\n  Upgradeable: %s\n  Reason: %s\n  Message: %s\n", upgradeableCond.Status, upgradeableCond.Reason, upgradeableCond.Message); err != nil {
				return err
			}
		}
	} else if progressingCond != nil && progressingCond.Status == operatorv1.ConditionFalse &&
		progressingCond.Reason == topologyTransitionPreflightCheckFailedReason {
		// Preflight failed
		if _, err := fmt.Fprintf(o.Out, "  Preflight checks failed\n\n  Progressing: %s\n  Reason: %s\n  Message: %s\n", progressingCond.Status, progressingCond.Reason, progressingCond.Message); err != nil {
			return err
		}

		if upgradeableCond != nil {
			if _, err := fmt.Fprintf(o.Out, "\n  Upgradeable: %s\n  Reason: %s\n  Message: %s\n", upgradeableCond.Status, upgradeableCond.Reason, upgradeableCond.Message); err != nil {
				return err
			}
		}
	} else if progressingCond != nil && progressingCond.Status == operatorv1.ConditionFalse &&
		progressingCond.Reason == topologyTransitionUnsupportedTransitionReason {
		// Unsupported transition
		if _, err := fmt.Fprintf(o.Out, "  Unsupported transition\n\n  Progressing: %s\n  Reason: %s\n  Message: %s\n", progressingCond.Status, progressingCond.Reason, progressingCond.Message); err != nil {
			return err
		}

		if upgradeableCond != nil {
			if _, err := fmt.Fprintf(o.Out, "\n  Upgradeable: %s\n  Reason: %s\n  Message: %s\n", upgradeableCond.Status, upgradeableCond.Reason, upgradeableCond.Message); err != nil {
				return err
			}
		}
	} else {
		// No transition in progress
		if _, err := fmt.Fprintf(o.Out, "  No transition in progress\n\n"); err != nil {
			return err
		}

		if upgradeableCond != nil {
			if _, err := fmt.Fprintf(o.Out, "  Upgradeable: %s\n  Reason: %s\n  Message: %s\n", upgradeableCond.Status, upgradeableCond.Reason, upgradeableCond.Message); err != nil {
				return err
			}
		}
	}

	return nil
}
