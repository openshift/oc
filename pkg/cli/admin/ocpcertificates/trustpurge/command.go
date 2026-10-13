package trustpurge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	removeOldTrustLong = templates.LongDesc(`
		Prune CA certificate bundles supplied by the platform and stored in ConfigMaps
		throughout the cluster.

		This command does not wait for changes to be acknowledged by the cluster.
		Some may take a very long time to roll out into a cluster, with different operators and operands involved for each.

		Experimental: This command is under active development and may change without notice.
	`)

	removeOldTrustExample = templates.Examples(`
		# Remove a trust bundled contained in a particular config map
		oc adm ocp-certificates remove-old-trust -n openshift-config-managed configmaps/kube-apiserver-aggregator-client-ca --created-before 2023-06-05T14:44:06Z

		#  Remove only CA certificates created before a certain date from all trust bundles
		oc adm ocp-certificates remove-old-trust configmaps -A --all --created-before 2023-06-05T14:44:06Z
	`)
)

type RemoveOldTrustOptions struct {
	RESTClientGetter     genericclioptions.RESTClientGetter
	PrintFlags           *genericclioptions.PrintFlags
	ResourceBuilderFlags *genericclioptions.ResourceBuilderFlags

	CreatedBefore  string
	ExcludeBundles []string

	// TODO push this into genericclioptions
	DryRun bool

	genericiooptions.IOStreams
}

func NewRemoveOldTrustOptions(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *RemoveOldTrustOptions {
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

func NewCmdRemoveOldTrust(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *cobra.Command {
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
	cmd.Flags().StringSliceVar(&o.ExcludeBundles, "exclude-bundles", o.ExcludeBundles, "CA bundles to exclude from trust pruning. Can be specified multiple times. Format: namespace/name")

	cmd.MarkFlagRequired("created-before")
}

func (o *RemoveOldTrustOptions) ToRuntime(args []string) (*RemoveOldTrustRuntime, error) {
	printer, err := o.PrintFlags.ToPrinter()
	if err != nil {
		return nil, err
	}

	exclude := map[string]sets.Set[string]{}
	for _, b := range o.ExcludeBundles {
		excludedPair := strings.Split(b, "/")
		if len(excludedPair) != 2 {
			return nil, fmt.Errorf("wrong format of excluded bundle: %q. Expected format of 'namespace/name'", b)
		}
		if exclude[excludedPair[0]] == nil {
			exclude[excludedPair[0]] = sets.New[string]()
		}
		exclude[excludedPair[0]].Insert(excludedPair[1])
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

		dryRun:            o.DryRun,
		excludeBundles:    exclude,
		cachedSecretCerts: map[string][]*cachedSecretCert{},

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
