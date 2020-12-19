package status

import (
	"context"
	"errors"
	"fmt"

	"github.com/gonum/graph/encoding/dot"
	"github.com/spf13/cobra"

	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	appsv1client "github.com/openshift/client-go/apps/clientset/versioned/typed/apps/v1"
	buildv1client "github.com/openshift/client-go/build/clientset/versioned/typed/build/v1"
	imagev1client "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	projectv1client "github.com/openshift/client-go/project/clientset/versioned/typed/project/v1"
	routev1client "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	"github.com/openshift/oc/pkg/helpers/describe"
	dotutil "github.com/openshift/oc/pkg/helpers/dot"
	loginutil "github.com/openshift/oc/pkg/helpers/project"
)

var (
	statusLong = templates.LongDesc(`
		Show a high level overview of the current project

		This command will show services, deployment configs, build configurations, and active deployments.
		If you have any misconfigured components information about them will be shown. For more information
		about individual items, use the describe command (e.g. arvan paas describe buildconfig,
		arvan paas describe deploymentconfig, arvan paas describe service).

		You can specify an output format of "-o dot" to have this command output the generated status
		graph in DOT format that is suitable for use by the "dot" command.`)

	statusExample = templates.Examples(`
		# See an overview of the current project.
		arvan paas status

		# Export the overview of the current project in an svg file.
		arvan paas status -o dot | dot -T svg -o project.svg

		# See an overview of the current project including details for any identified issues.
		arvan paas --suggest`)
)

// StatusOptions contains all the necessary options for the Openshift cli status command.
type StatusOptions struct {
	namespace     string
	allNamespaces bool
	outputFormat  string
	describer     *describe.ProjectStatusDescriber
	suggest       bool

	logsCommandName             string
	securityPolicyCommandFormat string
	setProbeCommandName         string
	patchCommandName            string

	genericclioptions.IOStreams
}

func NewStatusOptions(streams genericclioptions.IOStreams) *StatusOptions {
	return &StatusOptions{
		IOStreams: streams,
	}
}

// NewCmdStatus implements the OpenShift cli status command.
// baseCLIName is the path from root cmd to the parent of this cmd.
func NewCmdStatus(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewStatusOptions(streams)
	cmd := &cobra.Command{
		Use:     "status [-o dot | --suggest ]",
		Short:   "Show an overview of the current project",
		Long:    statusLong,
		Example: statusExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.RunStatus())
		},
	}
	cmd.Flags().StringVarP(&o.outputFormat, "output", "o", o.outputFormat, "Output format. One of: dot.")
	cmd.Flags().BoolVar(&o.suggest, "suggest", o.suggest, "See details for resolving issues.")
	cmd.Flags().BoolVarP(&o.allNamespaces, "all-namespaces", "A", o.allNamespaces, "If true, display status for all namespaces (must have cluster admin)")

	return cmd
}

// Complete completes the options for the Openshift cli status command.
func (o *StatusOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return kcmdutil.UsageErrorf(cmd, "no arguments should be provided")
	}

	o.logsCommandName = fmt.Sprintf("%s logs", cmd.Parent().CommandPath())
	o.securityPolicyCommandFormat = "arvan paas adm policy add-scc-to-user anyuid -n %s -z %s"
	o.setProbeCommandName = fmt.Sprintf("%s set probe", cmd.Parent().CommandPath())

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	kclientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	projectClient, err := projectv1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	buildClient, err := buildv1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	imageClient, err := imagev1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	appsClient, err := appsv1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	routeClient, err := routev1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	rawConfig, err := f.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return err
	}
	restMapper, err := f.ToRESTMapper()
	if err != nil {
		return err
	}

	if o.allNamespaces {
		o.namespace = metav1.NamespaceAll
	} else {
		namespace, _, err := f.ToRawKubeConfigLoader().Namespace()
		if err != nil {
			return err
		}
		_, err = projectClient.Projects().Get(context.TODO(), namespace, metav1.GetOptions{})
		switch {
		case kapierrors.IsForbidden(err), kapierrors.IsNotFound(err):
			return fmt.Errorf("you do not have rights to view project %q specified in your config or the project doesn't exist", namespace)
		case err != nil:
			return err
		}
		o.namespace = namespace
	}

	currentNamespace := ""
	if currentContext, exists := rawConfig.Contexts[rawConfig.CurrentContext]; exists {
		currentNamespace = currentContext.Namespace
	}

	nsFlag := kcmdutil.GetFlagString(cmd, "namespace")
	canRequestProjects, _ := loginutil.CanRequestProjects(clientConfig, o.namespace)

	o.describer = &describe.ProjectStatusDescriber{
		KubeClient:    kclientset,
		RESTMapper:    restMapper,
		ProjectClient: projectClient,
		BuildClient:   buildClient,
		ImageClient:   imageClient,
		AppsClient:    appsClient,
		RouteClient:   routeClient,
		Suggest:       o.suggest,
		Server:        clientConfig.Host,

		RequestedNamespace: nsFlag,
		CurrentNamespace:   currentNamespace,

		CanRequestProjects: canRequestProjects,

		// TODO: Remove these and reference them inside the markers using constants.
		LogsCommandName:             o.logsCommandName,
		SecurityPolicyCommandFormat: o.securityPolicyCommandFormat,
		SetProbeCommandName:         o.setProbeCommandName,
	}

	return nil
}

// Validate validates the options for the Openshift cli status command.
func (o StatusOptions) Validate() error {
	if len(o.outputFormat) != 0 && o.outputFormat != "dot" {
		return fmt.Errorf("invalid output format provided: %s", o.outputFormat)
	}
	if len(o.outputFormat) > 0 && o.suggest {
		return errors.New("cannot provide suggestions when output format is dot")
	}
	return nil
}

// RunStatus contains all the necessary functionality for the OpenShift cli status command.
func (o StatusOptions) RunStatus() error {
	var (
		s   string
		err error
	)

	switch o.outputFormat {
	case "":
		s, err = o.describer.Describe(o.namespace, "")
		if err != nil {
			return err
		}
	case "dot":
		g, _, err := o.describer.MakeGraph(o.namespace)
		if err != nil {
			return err
		}
		data, err := dot.Marshal(g, dotutil.Quote(o.namespace), "", "  ", false)
		if err != nil {
			return err
		}
		s = string(data)
	default:
		return fmt.Errorf("invalid output format provided: %s", o.outputFormat)
	}

	fmt.Fprintf(o.Out, s)
	return nil
}
