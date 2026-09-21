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

	if err := o.printTopologyStatus(ctx); err != nil {
		return err
	}

	return o.printTopologyTransitionStatus(ctx)
}

// printTopologyStatus will output the Control Plane and Infrastructure topologies from spec and status
func (o *statusOptions) printTopologyStatus(ctx context.Context) error {
	infra, err := o.configClient.ConfigV1().Infrastructures().Get(ctx, infrastructureResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get Infrastructure resource: %w", err)
	}

	notSet := "(not set)"
	cpSpecTopology := string(infra.Spec.ControlPlaneTopology)
	if cpSpecTopology == "" {
		cpSpecTopology = notSet
	}

	cpStatusTopology := string(infra.Status.ControlPlaneTopology)
	if cpStatusTopology == "" {
		cpStatusTopology = notSet
	}

	infraStatusTopology := string(infra.Status.InfrastructureTopology)
	if infraStatusTopology == "" {
		infraStatusTopology = notSet
	}

	var output strings.Builder
	statusOutput := `
Control Plane Topology:
  Spec (desired):   %s
  Status (current): %s

Infrastructure Topology:
  Status (current): %s
`
	fmt.Fprintf(&output, statusOutput, cpSpecTopology, cpStatusTopology, infraStatusTopology)

	if _, err := io.WriteString(o.Out, output.String()); err != nil {
		return err
	}

	return nil
}

func (o *statusOptions) printTopologyTransitionStatus(ctx context.Context) error {
	operatorConfig, err := o.operatorClient.OperatorV1().Configs().Get(ctx, clusterConfigOperatorResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get configs.operator.openshift.io/cluster: %w", err)
	}

	// Pull out the relevant conditions from CCO
	progressingCond := v1helpers.FindOperatorCondition(operatorConfig.Status.Conditions, topologyTransitionControllerProgressingCondition)
	upgradeableCond := v1helpers.FindOperatorCondition(operatorConfig.Status.Conditions, topologyTransitionControllerUpgradeableCondition)

	var output strings.Builder
	fmt.Fprintln(&output, "\nTransition Status")
	fmt.Fprintln(&output, formatTopologyConditionStatus("Progressing", progressingCond))
	fmt.Fprintln(&output, formatTopologyConditionStatus("Upgradeable", upgradeableCond))

	_, err = io.WriteString(o.Out, output.String())
	return err
}

// formatTopologyConditionStatus formats the provided topology transition condition status under a 'label' heading
func formatTopologyConditionStatus(label string, cond *operatorv1.OperatorCondition) string {
	if cond == nil {
		return fmt.Sprintf("  %s: Condition not available\n", label)
	}

	return fmt.Sprintf("  %s\n    Status: %s\n    Reason: %s\n    Condition: %s\n", label, cond.Status, cond.Reason, cond.Message)
}
