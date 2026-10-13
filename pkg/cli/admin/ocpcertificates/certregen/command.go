package certregen

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	regenerateSignersLong = templates.LongDesc(`
		Regenerate root certificates provided by an OCP v4 cluster.

		This command does not wait for changes to be acknowledged by the cluster.
		Some may take a very long time to roll out into a cluster, with different operators and operands involved for each.

		Experimental: This command is under active development and may change without notice.
	`)

	regenerateSignersExample = templates.Examples(`
		# Regenerate the signing certificate contained in a particular secret
		oc adm ocp-certificates regenerate-top-level -n openshift-kube-apiserver-operator secret/loadbalancer-serving-signer-key
	`)

	regenerateLeafLong = templates.LongDesc(`
		Regenerate leaf certificates provided by an OCP v4 cluster.

		This command does not wait for changes to be acknowledged by the cluster.
		Some may take a very long time to roll out into a cluster, with different operators and operands involved for each.

		Experimental: This command is under active development and may change without notice.
	`)

	regenerateLeafExample = templates.Examples(`
		# Regenerate a leaf certificate contained in a particular secret
		oc adm ocp-certificates regenerate-leaf -n openshift-config-managed secret/kube-controller-manager-client-cert-key
	`)
)

type RegenerateCertificatesOptions struct {
	RESTClientGetter     genericclioptions.RESTClientGetter
	PrintFlags           *genericclioptions.PrintFlags
	ResourceBuilderFlags *genericclioptions.ResourceBuilderFlags

	ValidBeforeString string

	// TODO push this into genericclioptions
	DryRun bool

	genericiooptions.IOStreams
}

func NewRegenerateCertsOptions(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *RegenerateCertificatesOptions {
	return &RegenerateCertificatesOptions{
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

func NewCmdRegenerateTopLevel(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewRegenerateCertsOptions(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "regenerate-top-level",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Regenerate the top level certificates in an OpenShift cluster"),
		Long:                  regenerateSignersLong,
		Example:               regenerateSignersExample,
		Run: func(cmd *cobra.Command, args []string) {
			r, err := o.ToRuntime(args)

			regenerator := RootsRegen{ValidBefore: r.ValidBefore}
			r.regenerateSecretFn = regenerator.forceRegenerationOnSecret

			cmdutil.CheckErr(err)
			cmdutil.CheckErr(r.Run(context.Background()))
		},
	}

	o.AddFlags(cmd)

	return cmd
}

func NewCmdRegenerateLeaves(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewRegenerateCertsOptions(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "regenerate-leaf",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Regenerate client and serving certificates of an OpenShift cluster"),
		Long:                  regenerateLeafLong,
		Example:               regenerateLeafExample,
		Run: func(cmd *cobra.Command, args []string) {
			r, err := o.ToRuntime(args)

			regenerator := LeavesRegen{ValidBefore: r.ValidBefore}
			r.regenerateSecretFn = regenerator.forceRegenerationOnSecret

			cmdutil.CheckErr(err)
			cmdutil.CheckErr(r.Run(context.Background()))
		},
	}

	o.AddFlags(cmd)

	return cmd
}

// AddFlags registers flags for a cli
func (o *RegenerateCertificatesOptions) AddFlags(cmd *cobra.Command) {
	o.PrintFlags.AddFlags(cmd)
	o.ResourceBuilderFlags.AddFlags(cmd.Flags())

	cmd.Flags().BoolVar(&o.DryRun, "dry-run", o.DryRun, "Set to true to use server-side dry run.")
	cmd.Flags().StringVar(&o.ValidBeforeString, "valid-before", o.ValidBeforeString, "Only regenerate top level certificates valid before this date.  Format: 2023-06-05T14:44:06Z")
}

func (o *RegenerateCertificatesOptions) ToRuntime(args []string) (*RegenerateCertsRuntime, error) {
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

	ret := &RegenerateCertsRuntime{
		ResourceFinder: builder,
		KubeClient:     kubeClient,

		DryRun: o.DryRun,

		Printer:   printer,
		IOStreams: o.IOStreams,
	}

	if len(o.ValidBeforeString) > 0 {
		validBefore, err := time.Parse(time.RFC3339, o.ValidBeforeString)
		if err != nil {
			return nil, err
		}
		ret.ValidBefore = &validBefore
	}

	return ret, nil
}
