package regeneratesigners

import (
	"context"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	regenerateSignersLong = templates.LongDesc(`
		Regenerate certificates provided by an OCP v4 cluster.
		
		This command does not wait for changes to be acknowledged by the cluster.
		Some may take a very long time to roll out into a cluster, with different operators and operands involved for each.

		Experimental: This command is under active development and may change without notice.
	`)

	regenerateSignersExample = templates.Examples(`
		# Regenerate all known signing certificates on the cluster.
		oc adm certificates regenerate-signers -A secrets --all

		# Regenerate the signing certificate contained in a particular secret.
		oc adm certificates regenerate-signers -n openshift-kube-apiserver-operator secrets/loadbalancer-serving-signer
	`)
)

type RegenerateSignersOptions struct {
	RESTClientGetter     genericclioptions.RESTClientGetter
	PrintFlags           *genericclioptions.PrintFlags
	ResourceBuilderFlags *genericclioptions.ResourceBuilderFlags

	// TODO push this into genericclioptions
	DryRun bool

	genericclioptions.IOStreams
}

func NewRegenerateSignersOptions(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *RegenerateSignersOptions {
	return &RegenerateSignersOptions{
		RESTClientGetter: restClientGetter,
		PrintFlags:       genericclioptions.NewPrintFlags("regeneration set"),
		ResourceBuilderFlags: genericclioptions.NewResourceBuilderFlags().
			WithLabelSelector("").
			WithFieldSelector("").
			WithAll(false).
			WithAllNamespaces(false).
			WithLocal(false).
			WithLatest(),

		IOStreams: streams,
	}
}

func NewCmdRegenerateSigners(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewRegenerateSignersOptions(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "regenerate-signers",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Update the annotations on a resource"),
		Long:                  regenerateSignersLong,
		Example:               regenerateSignersExample,
		Run: func(cmd *cobra.Command, args []string) {
			r, err := o.ToRuntime(args)
			cmdutil.CheckErr(err)
			cmdutil.CheckErr(r.Run(context.Background()))
		},
	}

	o.AddFlags(cmd)

	return cmd
}

// AddFlags registers flags for a cli
func (o *RegenerateSignersOptions) AddFlags(cmd *cobra.Command) {
	o.PrintFlags.AddFlags(cmd)
	o.ResourceBuilderFlags.AddFlags(cmd.Flags())

	cmd.Flags().BoolVar(&o.DryRun, "dry-run", o.DryRun, "Set to true to use server-side dry run.")
}

func (o *RegenerateSignersOptions) ToRuntime(args []string) (*RegenerateSignersRuntime, error) {
	printer, err := o.PrintFlags.ToPrinter()
	if err != nil {
		return nil, err
	}
	builder := o.ResourceBuilderFlags.ToBuilder(o.RESTClientGetter, args)
	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	ret := &RegenerateSignersRuntime{
		ResourceFinder: builder,
		KubeClient:     kubeClient,

		DryRun: o.DryRun,

		Printer:   printer,
		IOStreams: o.IOStreams,
	}

	return ret, nil
}
