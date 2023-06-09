package trustpurge

import (
	"context"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	removeOldTrustLong = templates.LongDesc(`
		TODO:
	`)

	removeOldTrustExample = templates.Examples(`
		TODO:
	`)
)

type RemoveOldTrustOptions struct {
	RESTClientGetter     genericclioptions.RESTClientGetter
	PrintFlags           *genericclioptions.PrintFlags
	ResourceBuilderFlags *genericclioptions.ResourceBuilderFlags

	CreatedBefore string

	// TODO push this into genericclioptions
	DryRun bool

	genericclioptions.IOStreams
}

func NewRemoveOldTrustOptions(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *RemoveOldTrustOptions {
	return &RemoveOldTrustOptions{
		RESTClientGetter: restClientGetter,
		PrintFlags:       genericclioptions.NewPrintFlags("trust purged"),
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

func NewCmdRemoveOldTrust(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewRemoveOldTrustOptions(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "remove-old-trust",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Remove old CAs from ConfigMaps representing platform trust bundles in an OpenShift cluster"),
		Long:                  removeOldTrustLong,
		Example:               removeOldTrustExample,
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
func (o *RemoveOldTrustOptions) AddFlags(cmd *cobra.Command) {
	o.PrintFlags.AddFlags(cmd)
	o.ResourceBuilderFlags.AddFlags(cmd.Flags())

	cmd.Flags().BoolVar(&o.DryRun, "dry-run", o.DryRun, "Set to true to use server-side dry run.")
	cmd.Flags().StringVar(&o.CreatedBefore, "created-before", o.CreatedBefore, "Only remove CA certificates that were created before this date.  Format: 2023-06-05T14:44:06Z")
}

func (o *RemoveOldTrustOptions) ToRuntime(args []string) (*RemoveOldTrustRuntime, error) {
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

	ret := &RemoveOldTrustRuntime{
		ResourceFinder: builder,
		KubeClient:     kubeClient,

		dryRun: o.DryRun,

		Printer:   printer,
		IOStreams: o.IOStreams,
	}

	if len(o.CreatedBefore) > 0 {
		createdBefore, err := time.Parse(time.RFC3339, o.CreatedBefore)
		if err != nil {
			return nil, err
		}
		ret.createdBefore = createdBefore
	}

	return ret, nil
}
