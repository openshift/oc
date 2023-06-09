package restartkubelet

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imagev1client "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	"github.com/openshift/library-go/pkg/image/imageutil"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
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
		# Restart all the nodes,  10% at a time
		oc adm clusteroperators restart-kubelet nodes --all

		# Restart all the nodes,  20 nodes at a time
		oc adm clusteroperators restart-kubelet nodes --all --parallelism=20

		# Restart all the nodes,  15% at a time
		oc adm clusteroperators restart-kubelet nodes --all --parallelism=15%

		# Restart all the masters at the same time
		oc adm clusteroperators restart-kubelet nodes -l node-role.kubernetes.io/master --parallelism=100%`)
)

type RestartKubeletOptions struct {
	RESTClientGetter     genericclioptions.RESTClientGetter
	PrintFlags           *genericclioptions.PrintFlags
	ResourceBuilderFlags *genericclioptions.ResourceBuilderFlags

	// TODO push this into genericclioptions
	DryRun bool

	Parallelism string

	genericclioptions.IOStreams
}

func NewRestartKubelet(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *RestartKubeletOptions {
	return &RestartKubeletOptions{
		RESTClientGetter: restClientGetter,
		PrintFlags:       genericclioptions.NewPrintFlags("regeneration set"),
		ResourceBuilderFlags: genericclioptions.NewResourceBuilderFlags().
			WithLabelSelector("").
			WithFieldSelector("").
			WithAll(false).
			WithLocal(false).
			WithLatest(),

		Parallelism: "10%",

		IOStreams: streams,
	}
}

func NewCmdRestartKubelet(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewRestartKubelet(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "restart-kubelet",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Restarts kubelet on the specified nodes"),
		Long:                  regenerateSignersLong,
		Example:               regenerateSignersExample,
		Run: func(cmd *cobra.Command, args []string) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			c := make(chan os.Signal, 2)
			signal.Notify(c, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-c
				cancel()
				<-c // second call exits
				os.Exit(1)
			}()

			r, err := o.ToRuntime(args)
			cmdutil.CheckErr(err)
			cmdutil.CheckErr(r.Run(ctx))
		},
	}

	o.AddFlags(cmd)

	return cmd
}

// AddFlags registers flags for a cli
func (o *RestartKubeletOptions) AddFlags(cmd *cobra.Command) {
	o.PrintFlags.AddFlags(cmd)
	o.ResourceBuilderFlags.AddFlags(cmd.Flags())

	cmd.Flags().BoolVar(&o.DryRun, "dry-run", o.DryRun, "Set to true to use server-side dry run.")
	cmd.Flags().StringVar(&o.Parallelism, "parallelism", o.Parallelism, "parallelism is a raw number or a percentage of the nodes we need to restart at the same time.")
}

func (o *RestartKubeletOptions) ToRuntime(args []string) (*RestartKubeletRuntime, error) {
	parallelPercentage := int64(0)
	parallelInt, intParseErr := strconv.ParseInt(o.Parallelism, 10, 32)
	if intParseErr != nil {
		if !strings.HasSuffix(o.Parallelism, "%") {
			return nil, fmt.Errorf("--parallelism must be either N or N%%: %w", intParseErr)
		}
		percentageString := o.Parallelism[0 : len(o.Parallelism)-1]
		var percentParseErr error
		parallelPercentage, percentParseErr = strconv.ParseInt(percentageString, 10, 32)
		if percentParseErr != nil {
			return nil, fmt.Errorf("--parallelism must be either N or N%%: %w", intParseErr)
		}
	}

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
	imageClient, err := imagev1client.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}
	imagePullSpec, err := resolveImageStreamTag(imageClient, "openshift", "must-gather", "latest")
	if err != nil {
		return nil, fmt.Errorf("unable to resolve image: %w", err)
	}

	ret := &RestartKubeletRuntime{
		ResourceFinder: builder,
		KubeClient:     kubeClient,

		DryRun:                   o.DryRun,
		ImagePullSpec:            imagePullSpec,
		NumberOfNodesInParallel:  int(parallelInt),
		PercentOfNodesInParallel: int(parallelPercentage),

		Printer:   printer,
		IOStreams: o.IOStreams,
	}

	return ret, nil
}

func resolveImageStreamTag(imageClient *imagev1client.ImageV1Client, namespace, name, tag string) (string, error) {
	imageStream, err := imageClient.ImageStreams(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	var image string
	if image, _, _, _, err = imageutil.ResolveRecentPullSpecForTag(imageStream, tag, false); err != nil {
		return "", fmt.Errorf("unable to resolve the imagestream tag %s/%s:%s: %v", namespace, name, tag, err)
	}
	return image, nil
}
