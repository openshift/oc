package renderedmachineconfigs

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
	"unicode"

	mcfgclientset "github.com/openshift/client-go/machineconfiguration/clientset/versioned"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	pruneMCLong = templates.LongDesc(`
		Experimental: This command is under development and may change without notice.

		# Prune rendered MachineConfigs for an OCP v4 cluster.
		oc adm prune renderedmachineconfigs
	`)

	pruneMCExample = templates.Examples(`		
		# See what the prune command would delete if run with no options
		oc adm prune renderedmachineconfigs 

		# To actually perform the prune operation, the confirm flag must be appended
		oc adm prune renderedmachineconfigs --confirm

		# See what the prune command would delete if run on the worker MachineConfigPool
		oc adm prune renderedmachineconfigs --pool-name=worker

		# Prunes 10 oldest rendered MachineConfigs in the cluster
		oc adm prune renderedmachineconfigs --count=10 --confirm       
		
		# Prunes 10 oldest rendered MachineConfigs in the cluster for the worker MachineConfigPool
		oc adm prune renderedmachineconfigs --count=10 --pool-name=worker --confirm			

	`)

	pruneMCListLong = templates.LongDesc(`
		Experimental: This command is under development and may change without notice.

		# List rendered MachineConfigs for an OCP v4 cluster.
		oc adm prune renderedmachineconfigs list
	`)

	pruneMCListExample = templates.Examples(`
		# List all rendered MachineConfigs for the worker MachineConfigPool in the cluster
		oc adm prune renderedmachineconfigs list --pool-name=worker

		# List all rendered MachineConfigs in use by the cluster's MachineConfigPools
		oc adm prune renderedmachineconfigs list --in-use

	`)
)

type pruneMCOptions struct {
	RESTClientGetter genericclioptions.RESTClientGetter
	genericiooptions.IOStreams
	PoolFilter            string
	InUse                 bool
	Confirm               bool
	Count                 int
	CurrentRenderedConfig string
}

func NewCmdPruneMachineConfigs(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *cobra.Command {
	o := &pruneMCOptions{
		RESTClientGetter: restClientGetter,
		IOStreams:        streams,
	}

	cmd := &cobra.Command{
		Use:                   "renderedmachineconfigs",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Prunes rendered MachineConfigs in an OpenShift cluster"),
		Long:                  pruneMCLong,
		Example:               pruneMCExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Run(context.Background()))
		},
	}

	cmd.AddCommand(NewCmdPruneMCList(restClientGetter, streams))

	o.AddPruneFlags(cmd)

	return cmd
}

func (o *pruneMCOptions) AddPruneFlags(cmd *cobra.Command) {

	// Adds the pool-name filter flagpool-name filter flag
	cmd.Flags().StringVarP(&o.PoolFilter, "pool-name", "p", o.PoolFilter, "Specify the MachineConfigPool name to filter by (default: all pools)")

	// Adds the count flag to specify number of rendered configs to delete
	cmd.Flags().IntVar(&o.Count, "count", o.Count, "Number of rendered MachineConfigs to delete from the list (default: delete all but current rendered MachineConfigs)")

	// Adds the confirm flag
	cmd.Flags().BoolVar(&o.Confirm, "confirm", o.Confirm, "If true, specify that pruning should proceed. Defaults to false, displaying what would be deleted but not actually deleting anything.")

}

func NewCmdPruneMCList(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *cobra.Command {
	o := &pruneMCOptions{
		RESTClientGetter: restClientGetter,
		IOStreams:        streams,
	}

	cmd := &cobra.Command{
		Use:                   "list",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Lists rendered MachineConfigs in an OpenShift cluster"),
		Long:                  pruneMCListLong,
		Example:               pruneMCListExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.RunList(context.Background(), args))
		},
	}

	o.AddListFlags(cmd)

	return cmd
}

func (o *pruneMCOptions) AddListFlags(cmd *cobra.Command) {

	// Adds the pool-name filter flag
	cmd.Flags().StringVarP(&o.PoolFilter, "pool-name", "p", o.PoolFilter, "Specify the MachineConfigPool name to filter by (default: all pools)")

	// Add the in-use flag
	cmd.Flags().BoolVar(&o.InUse, "in-use", o.InUse,
		"List currently in use rendered MachineConfig for each MachineConfigPool if true. "+
			"Invoking just the argument (--in-use) will set the flag to true. "+
			"If manually set to false (--in-use=false), it will list all machine configs as the default list command does.")

}

func (o *pruneMCOptions) Run(ctx context.Context) error {
	machineConfigs, err := o.listRenderedConfigs(ctx)
	if err != nil {
		return err
	}

	// Get the set of in-use configs
	inuseConfigs, err := o.getInUseConfigs(ctx)
	if err != nil {
		return err
	}

	// Sort the machine configs based on the flags provided
	if o.PoolFilter == "" {
		if o.Count > 0 {
			o.sortMachineConfigsByTime(machineConfigs)
		} else {
			o.sortMachineConfigs(machineConfigs)
		}
	} else {
		o.sortMachineConfigsByTime(machineConfigs)
	}

	// Delete the oldest rendered configs based on the count
	countToDelete := o.Count
	if countToDelete < 0 {
		return errors.New("count cannot be negative")
	}
	if countToDelete == 0 || countToDelete > len(machineConfigs) {
		countToDelete = len(machineConfigs)
	}

	contextMessage := "deleting"
	if !o.Confirm {
		contextMessage = "dry-run deleting"
		fmt.Fprintln(o.IOStreams.ErrOut, "Dry run enabled - no modifications will be made. Add --confirm to remove rendered machine configs.")
	}

	for i := 0; i < countToDelete; i++ {
		if !inuseConfigs.Has(machineConfigs[i].Name) {
			if err := o.deleteRenderedConfig(ctx, machineConfigs[i].Name, o.Confirm); err != nil {
				fmt.Fprintf(o.IOStreams.ErrOut, "Error %v\n", err)
			}
		} else {
			fmt.Fprintf(o.IOStreams.ErrOut, "Skip %s rendered MachineConfig %s as it's currently in use\n", contextMessage, machineConfigs[i].Name)
		}
	}

	return nil
}

func (o *pruneMCOptions) RunList(ctx context.Context, args []string) error {
	// Check the --in-use flag
	if o.InUse {
		return o.listInUseRenderedConfigs(ctx)
	}
	// List machine config pools for in-use rendered configs
	machineConfigs, err := o.listRenderedConfigs(ctx)
	if err != nil {
		return err
	}

	// Call the sortAndPrintMachineConfigs function to sort and print the MachineConfig structs
	o.sortMachineConfigs(machineConfigs)
	o.printMachineConfigs(machineConfigs)

	return nil
}

// extractPoolName retrieves the pool name from the owner references of the machine config
func extractPoolName(meta *metav1.ObjectMeta) string {
	ownerReferences := meta.GetOwnerReferences()
	for _, ownerRef := range ownerReferences {
		return ownerRef.Name
	}
	return "unknown" // fallback value if no owner reference is found
}

// MachineConfig is a struct representing a machine config
type MachineConfig struct {
	Name      string
	CreatedAt time.Time
	Pool      string
	IsCurrent bool
}

func (o *pruneMCOptions) listInUseRenderedConfigs(ctx context.Context) error {
	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return err
	}

	machineConfigClient, err := mcfgclientset.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	poolList, err := machineConfigClient.MachineconfigurationV1().MachineConfigPools().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("getting MachineConfigPools failed: %w", err)
	}

	found := false

	for _, pool := range poolList.Items {
		// Get the rendered config name from the status section
		renderedConfigName := pool.Status.Configuration.Name
		specRenderedConfigName := pool.Spec.Configuration.Name

		// Check if the pool matches the specified pool name (if provided)
		if o.PoolFilter == "" || o.PoolFilter == pool.Name {
			found = true
			fmt.Fprintf(o.IOStreams.Out, "%s\nstatus: %s\nspec: %s\n", pool.Name, renderedConfigName, specRenderedConfigName)
		}
	}

	if !found && o.PoolFilter != "" {
		return fmt.Errorf("MachineConfigPool with name '%s' not found", o.PoolFilter)
	}

	return nil
}

func (o *pruneMCOptions) listRenderedConfigs(ctx context.Context) ([]MachineConfig, error) {
	// Get the current in-use rendered config name for each pool
	currentRenderedConfigs, err := o.getInUseConfigs(ctx)
	if err != nil {
		return nil, err
	}

	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	machineConfigClient, err := mcfgclientset.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	if o.PoolFilter != "" {
		_, err := machineConfigClient.MachineconfigurationV1().MachineConfigPools().Get(ctx, o.PoolFilter, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("MachineConfigPool with name '%s' not found", o.PoolFilter)
		}
	}

	mcList, err := machineConfigClient.MachineconfigurationV1().MachineConfigs().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting rendered MachineConfig failed: %w", err)
	}

	// Convert the machine configs to a slice of structs for sorting and filtering
	var machineConfigs []MachineConfig
	for _, mc := range mcList.Items {
		// Check if the mc is generated by the MCO controller, as all rendered configs have to be.
		_, generatedbyController := mc.Annotations["machineconfiguration.openshift.io/generated-by-controller-version"]
		// Check if the name starts with a number, skip if it does.
		if !unicode.IsDigit(rune(mc.Name[0])) && generatedbyController {
			// Retrieve the pool name from the owner references
			poolName := extractPoolName(&mc.ObjectMeta)

			// Apply the pool filter if specified
			if o.PoolFilter == "" || o.PoolFilter == poolName {
				// Determine if the current rendered config for the pool exists in the set
				isCurrent := currentRenderedConfigs.Has(mc.Name)

				// Append the machine config to the list
				machineConfigs = append(machineConfigs, MachineConfig{
					Name:      mc.Name,
					CreatedAt: mc.GetCreationTimestamp().Time.UTC(),
					Pool:      poolName,
					IsCurrent: isCurrent,
				})
			}
		}
	}

	return machineConfigs, nil
}

func (o *pruneMCOptions) sortMachineConfigs(machineConfigs []MachineConfig) {
	// Sort the list based on Pool and CreatedAt fields
	sort.Slice(machineConfigs, func(i, j int) bool {

		if machineConfigs[i].Pool < machineConfigs[j].Pool {
			return true
		} else if machineConfigs[i].Pool > machineConfigs[j].Pool {
			return false
		}

		return machineConfigs[i].CreatedAt.Before(machineConfigs[j].CreatedAt)
	})
}

func (o *pruneMCOptions) sortMachineConfigsByTime(machineConfigs []MachineConfig) {
	// Sort the list based on just CreatedAt fields
	sort.Slice(machineConfigs, func(i, j int) bool {

		return machineConfigs[i].CreatedAt.Before(machineConfigs[j].CreatedAt)
	})
}

func (o *pruneMCOptions) printMachineConfigs(machineConfigs []MachineConfig) {
	// Print the sorted list of MachineConfig structs
	currentPool := ""
	for _, mc := range machineConfigs {
		if currentPool != mc.Pool {

			padding := (80 - len(mc.Pool)) / 2
			fmt.Fprintf(o.IOStreams.Out, "\n%*s\n", padding+len(mc.Pool), mc.Pool)
			currentPool = mc.Pool
		}

		fmt.Fprintf(o.IOStreams.Out, "\n%s -- %s (Currently in use: %t)\n", mc.Name, mc.CreatedAt, mc.IsCurrent)
	}
}

func (o *pruneMCOptions) deleteRenderedConfig(ctx context.Context, name string, confirm bool) error {
	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return err
	}

	machineConfigClient, err := mcfgclientset.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	deleteOptions := &metav1.DeleteOptions{}
	contextMessage := "deleting"
	if !confirm {
		// Dry run logic
		deleteOptions.DryRun = []string{metav1.DryRunAll}
		contextMessage = "dry-run deleting"
	}

	err = machineConfigClient.MachineconfigurationV1().MachineConfigs().Delete(ctx, name, *deleteOptions)
	if err != nil {
		return fmt.Errorf("%s rendered MachineConfig %s failed: %w", contextMessage, name, err)
	}

	// Output deletion message
	fmt.Fprintf(o.IOStreams.Out, "%s rendered MachineConfig %s\n", contextMessage, name)

	return nil
}

func (o *pruneMCOptions) getInUseConfigs(ctx context.Context) (sets.Set[string], error) {
	// Create a set to store in-use configs
	inuseConfigs := sets.New[string]()

	// Retrieve in-use configs from nodes and add them to the set
	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	machineConfigClient, err := mcfgclientset.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	poolList, err := machineConfigClient.MachineconfigurationV1().MachineConfigPools().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting MachineConfigPools failed: %w", err)
	}

	for _, pool := range poolList.Items {
		// Check if the pool matches the specified pool name (if provided)
		if o.PoolFilter == "" || o.PoolFilter == pool.Name {
			// Get the rendered config name from the status section
			inuseConfigs.Insert(pool.Status.Configuration.Name)
			inuseConfigs.Insert(pool.Spec.Configuration.Name)
		}
	}

	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}
	nodeList, err := kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, node := range nodeList.Items {
		current, ok := node.Annotations["machineconfiguration.openshift.io/currentConfig"]
		if ok {
			inuseConfigs.Insert(current)
		}
		desired, ok := node.Annotations["machineconfiguration.openshift.io/desiredConfig"]
		if ok {
			inuseConfigs.Insert(desired)
		}
	}

	return inuseConfigs, nil
}
