package certregen

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"k8s.io/cli-runtime/pkg/genericclioptions"
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
		# Regenerate all known signing certificates on the cluster.
		oc adm certificates regenerate-signers -A --all

		# Regenerate the signing certificate contained in a particular secret.
		oc adm certificates regenerate-signers -n openshift-kube-apiserver-operator loadbalancer-serving-signer
	`)

	regenerateLeafLong = templates.LongDesc(`
		Regenerate leaf certificates provided by an OCP v4 cluster.

		This command does not wait for changes to be acknowledged by the cluster.
		Some may take a very long time to roll out into a cluster, with different operators and operands involved for each.

		Experimental: This command is under active development and may change without notice.
	`)

	regenerateLeafExample = templates.Examples(`
		# Regenerate all known leaf certificates on the cluster.
		oc adm certificates regenerate-leaf -A --all

		# Regenerate a leaf certificate contained in a particular secret.
		oc adm ocp-certificates regenerate-leaf -n openshift-config-managed kube-controller-manager-client-cert
	`)
)

type RegenerateCertificatesOptions struct {
	RESTClientGetter     genericclioptions.RESTClientGetter
	PrintFlags           *genericclioptions.PrintFlags
	ResourceBuilderFlags *genericclioptions.ResourceBuilderFlags

	ValidBeforeString string

	DryRun cmdutil.DryRunStrategy

	genericclioptions.IOStreams
}

func NewRegenerateCertsOptions(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *RegenerateCertificatesOptions {
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

func NewCmdRegenerateTopLevel(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewRegenerateCertsOptions(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "regenerate-top-level",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Regenerate the top level certificates in an OpenShift cluster"),
		Long:                  regenerateSignersLong,
		Example:               regenerateSignersExample,
		Run: func(cmd *cobra.Command, args []string) {
			r, err := o.ToRuntime(cmd, args)

			regenerator := RootsRegen{ValidBefore: r.ValidBefore}
			r.regenerateSecretFn = regenerator.forceRegenerationOnSecret

			cmdutil.CheckErr(err)
			cmdutil.CheckErr(r.Run(context.Background()))
		},
	}

	o.AddFlags(cmd)

	return cmd
}

func NewCmdRegenerateLeaves(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewRegenerateCertsOptions(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "regenerate-leaf",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Regenerate client and serving certificates of an OpenShift cluster"),
		Long:                  regenerateLeafLong,
		Example:               regenerateLeafExample,
		Run: func(cmd *cobra.Command, args []string) {
			r, err := o.ToRuntime(cmd, args)

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

	cmdutil.AddDryRunFlag(cmd)

	cmd.Flags().StringVar(&o.ValidBeforeString, "valid-before", o.ValidBeforeString, "Only regenerate top level certificates valid before this date.  Format: 2023-06-05T14:44:06Z")
}

func (o *RegenerateCertificatesOptions) ToRuntime(cmd *cobra.Command, args []string) (*RegenerateCertsRuntime, error) {
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

	dryRunStrategy, err := cmdutil.GetDryRunStrategy(cmd)
	if err != nil {
		return nil, err
	}

	if dryRunStrategy == cmdutil.DryRunClient {
		return nil, fmt.Errorf("--dry-run=client is not supported, please use --dry-run=server")
	}

	ret := &RegenerateCertsRuntime{
		ResourceFinder: builder,
		KubeClient:     kubeClient,

		DryRun: dryRunStrategy == cmdutil.DryRunServer,

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
