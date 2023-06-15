package forceredeployment

import (
	"context"

	operatorclient "github.com/openshift/client-go/operator/clientset/versioned"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	forceRedeploymentExample = templates.Examples(`
		# Force the specified operator to redeploy its operator and operand.
		oc adm clusteroperator forceredeployment clusteroperators --all`)
)

type ForceRedeploymentOptions struct {
	RESTClientGetter     genericclioptions.RESTClientGetter
	PrintFlags           *genericclioptions.PrintFlags
	ResourceBuilderFlags *genericclioptions.ResourceBuilderFlags

	// TODO push this into genericclioptions
	DryRun bool

	genericclioptions.IOStreams
}

func NewForceRedeploymentOptions(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *ForceRedeploymentOptions {
	return &ForceRedeploymentOptions{
		RESTClientGetter: restClientGetter,
		PrintFlags:       genericclioptions.NewPrintFlags("redeployment started"),
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

func NewCmdForceRedeployment(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewForceRedeploymentOptions(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "force-redeployment",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Force the specified operator to redeploy its operator and operand."),
		Example:               forceRedeploymentExample,
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
func (o *ForceRedeploymentOptions) AddFlags(cmd *cobra.Command) {
	o.PrintFlags.AddFlags(cmd)
	o.ResourceBuilderFlags.AddFlags(cmd.Flags())

	cmd.Flags().BoolVar(&o.DryRun, "dry-run", o.DryRun, "Set to true to use server-side dry run.")
}

func (o *ForceRedeploymentOptions) ToRuntime(args []string) (*ForceRedeploymentRuntime, error) {
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
	operatorClient, err := operatorclient.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	ret := &ForceRedeploymentRuntime{
		ResourceFinder: builder,
		KubeClient:     kubeClient,
		OperatorClient: operatorClient,

		DryRun: o.DryRun,

		Printer:   printer,
		IOStreams: o.IOStreams,
	}
	ret.RedeployFns = map[string]operandRedeployFunc{
		"etcd":                    ret.redeployEtcd,
		"kube-apiserver":          ret.redeployKubeAPIServer,
		"kube-controller-manager": ret.redeployKubeControllerManager,
		"kube-scheduler":          ret.redeployKubeScheduler,
		"openshift-apiserver":     ret.allPodRedeploy,
		"authentication":          ret.allPodRedeploy,
		"operator-lifecycle-manager-packageserver": ret.allPodRedeploy,
		"network":        ret.allPodRedeploy,
		"monitoring":     ret.allPodRedeploy,
		"image-registry": ret.allPodRedeploy,
	}

	return ret, nil
}
