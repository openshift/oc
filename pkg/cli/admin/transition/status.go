package transition

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	operatorv1 "github.com/openshift/api/operator/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	operatorv1client "github.com/openshift/client-go/operator/clientset/versioned"
	v1helpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	statusLong = templates.LongDesc(`
		Monitor topology transition progress.

		Displays the current control plane and infrastructure topology status
		(both spec and status), the topology transition lifecycle conditions,
		and CCO operator conditions for transition progress.
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
	topologyClient topologyTransitionClient

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

	o.topologyClient = newRESTTopologyClient(o.configClient.ConfigV1().RESTClient())

	return nil
}

// run executes the status subcommand
func (o *statusOptions) run(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, apiRequestTimeout)
	defer cancel()

	if err := o.printTopologyStatus(ctx); err != nil {
		return err
	}

	if err := o.printTopologyTransitionLifecycle(ctx); err != nil {
		return err
	}

	return o.printTopologyTransitionStatus(ctx)
}

// printTopologyStatus outputs the Control Plane and Infrastructure topologies from spec and status
func (o *statusOptions) printTopologyStatus(ctx context.Context) error {
	infra, err := o.configClient.ConfigV1().Infrastructures().Get(ctx, infrastructureResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get Infrastructure resource: %w", err)
	}

	// Read spec.infrastructureTopology from unstructured (not yet in vendored types)
	infraSpec, err := o.topologyClient.getInfrastructureTopologySpec(ctx)
	if err != nil {
		return fmt.Errorf("failed to read infrastructure topology spec: %w", err)
	}

	cpSpecTopology := formatTopologyValue(string(infra.Spec.ControlPlaneTopology))
	cpStatusTopology := formatTopologyValue(string(infra.Status.ControlPlaneTopology))
	infraSpecTopology := formatTopologyValue(string(infraSpec))
	infraStatusTopology := formatTopologyValue(string(infra.Status.InfrastructureTopology))

	var output strings.Builder
	fmt.Fprintf(&output, `
Control Plane Topology:
  Spec (desired):   %s
  Status (current): %s

Infrastructure Topology:
  Spec (desired):   %s
  Status (current): %s
`, cpSpecTopology, cpStatusTopology, infraSpecTopology, infraStatusTopology)

	if _, err := io.WriteString(o.Out, output.String()); err != nil {
		return err
	}

	return nil
}

// printTopologyTransitionLifecycle displays the topology transition lifecycle from
// status.topologyTransitionStatus.conditions on the Infrastructure resource.
func (o *statusOptions) printTopologyTransitionLifecycle(ctx context.Context) error {
	transitionStatus, err := o.topologyClient.getTopologyTransitionStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to read topology transition status: %w", err)
	}

	var output strings.Builder
	fmt.Fprintln(&output, "\nTopology Transition Lifecycle:")

	if transitionStatus == nil || len(transitionStatus.Conditions) == 0 {
		fmt.Fprintln(&output, "  No topology transition status available")
		_, err := io.WriteString(o.Out, output.String())
		return err
	}

	for _, cond := range transitionStatus.Conditions {
		fmt.Fprintf(&output, "  %s\n    Status:  %s\n    Reason:  %s\n    Message: %s\n",
			cond.Type, cond.Status, cond.Reason, cond.Message)
	}

	// Display per-transition evaluations if transitions are present
	if len(transitionStatus.Transitions) > 0 {
		fmt.Fprintln(&output, "\n  Transition Evaluations:")
		for _, t := range transitionStatus.Transitions {
			fmt.Fprintf(&output, "    %s\n", describeTransition(&t))
			for _, eval := range t.Evaluations {
				marker := "+"
				if eval.Status == metav1.ConditionFalse {
					marker = "-"
				}
				fmt.Fprintf(&output, "      %s %s: %s\n", marker, eval.Type, eval.Message)
			}
		}
	}

	_, err = io.WriteString(o.Out, output.String())
	return err
}

// printTopologyTransitionStatus displays CCO operator conditions for upgrade gating.
func (o *statusOptions) printTopologyTransitionStatus(ctx context.Context) error {
	operatorConfig, err := o.operatorClient.OperatorV1().Configs().Get(ctx, clusterConfigOperatorResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get configs.operator.openshift.io/cluster: %w", err)
	}

	progressingCond := v1helpers.FindOperatorCondition(operatorConfig.Status.Conditions, topologyTransitionControllerProgressingCondition)
	upgradeableCond := v1helpers.FindOperatorCondition(operatorConfig.Status.Conditions, topologyTransitionControllerUpgradeableCondition)

	var output strings.Builder
	fmt.Fprintln(&output, "\nCCO Transition Conditions:")
	fmt.Fprintln(&output, formatTopologyConditionStatus("Progressing", progressingCond))
	fmt.Fprintln(&output, formatTopologyConditionStatus("Upgradeable", upgradeableCond))

	_, err = io.WriteString(o.Out, output.String())
	return err
}

// formatTopologyConditionStatus formats a CCO operator condition for display.
func formatTopologyConditionStatus(label string, cond *operatorv1.OperatorCondition) string {
	if cond == nil {
		return fmt.Sprintf("  %s: Condition not available\n", label)
	}

	return fmt.Sprintf("  %s\n    Status: %s\n    Reason: %s\n    Condition: %s\n", label, cond.Status, cond.Reason, cond.Message)
}
