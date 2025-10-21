package pernodepod

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
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
)

type PerNodePodOptions struct {
	RESTClientGetter     genericclioptions.RESTClientGetter
	PrintFlags           *genericclioptions.PrintFlags
	ResourceBuilderFlags *genericclioptions.ResourceBuilderFlags

	// TODO push this into genericclioptions
	DryRun      bool
	Parallelism string

	NamespacePrefix string

	genericiooptions.IOStreams
}

func NewPerNodePodOptions(namespacePrefix, printerText string, restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *PerNodePodOptions {
	return &PerNodePodOptions{
		RESTClientGetter: restClientGetter,
		PrintFlags:       genericclioptions.NewPrintFlags(printerText),
		ResourceBuilderFlags: genericclioptions.NewResourceBuilderFlags().
			WithLabelSelector("").
			WithFieldSelector("").
			WithAll(false).
			WithLocal(false).
			WithLatest(),

		Parallelism:     "10%",
		NamespacePrefix: namespacePrefix,

		IOStreams: streams,
	}
}

func SignalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
		<-c // second call exits
		os.Exit(1)
	}()

	return ctx, cancel
}

// AddFlags registers flags for a cli
func (o *PerNodePodOptions) AddFlags(cmd *cobra.Command) {
	o.PrintFlags.AddFlags(cmd)
	o.ResourceBuilderFlags.AddFlags(cmd.Flags())

	cmd.Flags().BoolVar(&o.DryRun, "dry-run", o.DryRun, "Set to true to use server-side dry run.")
	cmd.Flags().StringVar(&o.Parallelism, "parallelism", o.Parallelism, "parallelism is a raw number or a percentage of the nodes to work with concurrently.")
}

func (o *PerNodePodOptions) ToRuntime(args []string) (*PerNodePodRuntime, error) {
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

	ret := &PerNodePodRuntime{
		ResourceFinder: builder,
		KubeClient:     kubeClient,

		DryRun:                   o.DryRun,
		NamespacePrefix:          o.NamespacePrefix,
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
