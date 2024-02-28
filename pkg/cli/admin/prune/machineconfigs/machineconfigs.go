package machineconfigs

import (
	"context"
	"fmt"

	mcfgclientset "github.com/openshift/client-go/machineconfiguration/clientset/versioned"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"

	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

var (
	pruneMCLong = templates.LongDesc(`
		Prune Machine Configs for an OCP v4 cluster.

		Experimental: This command is under active development and may change without notice.
	`)

	pruneMCExample = templates.Examples(`
	    # List all machine configs in the cluster
		oc adm prune machineconfigs list

	`)
)

type pruneMCOptions struct {
	RESTClientGetter genericclioptions.RESTClientGetter
	genericiooptions.IOStreams
}

func NewCmdPruneMachineConfigs(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *cobra.Command {
	o := &pruneMCOptions{
		RESTClientGetter: restClientGetter,
		IOStreams:        streams,
	}

	cmd := &cobra.Command{
		Use:                   "machineconfigs",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Prunes machineconfigs in an OpenShift cluster"),
		Long:                  pruneMCLong,
		Example:               pruneMCExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Run(context.Background()))
		},
	}

	cmd.AddCommand(NewCmdPruneMCList(restClientGetter, streams))

	o.AddFlags(cmd)

	return cmd
}
func NewCmdPruneMCList(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *cobra.Command {
	o := &pruneMCOptions{
		RESTClientGetter: restClientGetter,
		IOStreams:        streams,
	}

	cmd := &cobra.Command{
		Use:                   "list",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Lists machineconfigs in an OpenShift cluster"),
		Long:                  pruneMCLong,
		Example:               pruneMCExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.RunList(context.Background(), args))
		},
	}

	return cmd
}

// AddFlags registers flags for a cli
func (o *pruneMCOptions) AddFlags(cmd *cobra.Command) {
}

func (o *pruneMCOptions) Run(ctx context.Context) error {

	fmt.Fprintf(o.IOStreams.Out, "Hello from Prune!\n")
	return nil

}

func (o *pruneMCOptions) RunList(ctx context.Context, args []string) error {

	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return err
	}
	machineconfigClient, err := mcfgclientset.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	mcList, err := machineconfigClient.MachineconfigurationV1().MachineConfigs().List(context.TODO(), metav1.ListOptions{})

	if err != nil {
		return fmt.Errorf("getching configs failed: %w", err)
	}

	for _, mc := range mcList.Items {
		fmt.Fprintf(o.IOStreams.Out, "%s -- %s\n", mc.Name, mc.GetCreationTimestamp())

	}

	return nil

}
