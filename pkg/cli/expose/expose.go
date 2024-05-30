package expose

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/resource"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/kubectl/pkg/cmd/expose"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util"
	"k8s.io/kubectl/pkg/util/completion"
	"k8s.io/kubectl/pkg/util/templates"

	routev1 "github.com/openshift/api/route/v1"
	routev1client "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	"github.com/openshift/oc/pkg/cli/create/route"
)

var (
	exposeLong = templates.LongDesc(`
		Expose containers internally as services or externally via routes.

		There is also the ability to expose a deployment config, replication controller, service, or pod
		as a new service on a specified port. If no labels are specified, the new object will reuse the
		labels from the object it exposes.
	`)

	exposeExample = templates.Examples(`
		# Create a route based on service nginx. The new route will reuse nginx's labels
		oc expose service nginx

		# Create a route and specify your own label and route name
		oc expose service nginx -l name=myroute --name=fromdowntown

		# Create a route and specify a host name
		oc expose service nginx --hostname=www.example.com

		# Create a route with a wildcard
		oc expose service nginx --hostname=x.example.com --wildcard-policy=Subdomain
		# This would be equivalent to *.example.com. NOTE: only hosts are matched by the wildcard; subdomains would not be included

		# Expose a deployment configuration as a service and use the specified port
		oc expose dc ruby-hello-world --port=8080

		# Expose a service as a route in the specified path
		oc expose service nginx --path=/nginx
	`)
)

type ExposeFlags struct {
	Hostname       string
	Path           string
	WildcardPolicy string

	*expose.ExposeServiceFlags
}

type ExposeOptions struct {
	Hostname       string
	Path           string
	WildcardPolicy string

	Args        []string
	Cmd         *cobra.Command
	CoreClient  corev1client.CoreV1Interface
	RouteClient routev1client.RouteV1Interface
	Builder     *resource.Builder

	// Embed kubectl's ExposeServiceOptions directly.
	*expose.ExposeServiceOptions
}

func NewExposeFlags(streams genericiooptions.IOStreams) *ExposeFlags {
	flags := expose.NewExposeFlags(streams)
	flags.PrintFlags = genericclioptions.NewPrintFlags("exposed").WithTypeSetter(exposeCmdScheme)
	return &ExposeFlags{
		ExposeServiceFlags: flags,
	}
}

func (flags *ExposeFlags) ToOptions(cmd *cobra.Command, args []string) (*ExposeOptions, error) {
	exposeServiceOpts, err := flags.ExposeServiceFlags.ToOptions(cmd, args)
	if err != nil {
		return nil, err
	}

	return &ExposeOptions{
		Hostname:             flags.Hostname,
		Path:                 flags.Path,
		WildcardPolicy:       flags.WildcardPolicy,
		Args:                 args,
		Cmd:                  cmd,
		ExposeServiceOptions: exposeServiceOpts,
	}, nil
}

// NewCmdExpose is a wrapper for the Kubernetes cli expose command
func NewCmdExpose(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	flags := NewExposeFlags(streams)

	validArgs := []string{"pod", "service", "replicationcontroller", "deployment", "replicaset", "deploymentconfig"}
	cmd := &cobra.Command{
		Use:               "expose (-f FILENAME | TYPE NAME) [--port=port] [--protocol=TCP|UDP|SCTP] [--target-port=number-or-name] [--name=name] [--external-ip=external-ip-of-service] [--type=type]",
		Short:             "Expose a replicated application as a service or route",
		Long:              exposeLong,
		Example:           exposeExample,
		ValidArgsFunction: completion.SpecifiedResourceTypeAndNameCompletionFunc(f, validArgs),
		Run: func(cmd *cobra.Command, args []string) {
			opts, err := flags.ToOptions(cmd, args)
			kcmdutil.CheckErr(err)
			kcmdutil.CheckErr(opts.Complete(cmd, f, args))
			kcmdutil.CheckErr(opts.Validate())
			kcmdutil.CheckErr(opts.Run())
		},
	}

	flags.ExposeServiceFlags.AddFlags(cmd)
	cmd.Flags().Set("protocol", "")
	cmd.Flags().StringVar(&flags.Hostname, "hostname", flags.Hostname, "Set a hostname for the new route")
	cmd.Flags().StringVar(&flags.Path, "path", flags.Path, "Set a path for the new route")
	cmd.Flags().StringVar(&flags.WildcardPolicy, "wildcard-policy", flags.WildcardPolicy, "Sets the WildcardPolicy for the hostname, the default is \"None\". Valid values are \"None\" and \"Subdomain\"")

	return cmd
}

func (o *ExposeOptions) Complete(cmd *cobra.Command, f kcmdutil.Factory, args []string) error {
	config, err := f.ToRESTConfig()
	if err != nil {
		return err
	}

	o.Cmd = cmd
	o.Args = args
	o.Builder = f.NewBuilder()

	o.CoreClient, err = corev1client.NewForConfig(config)
	if err != nil {
		return err
	}
	o.RouteClient, err = routev1client.NewForConfig(config)
	if err != nil {
		return err
	}

	return o.ExposeServiceOptions.Complete(f)
}

func (o *ExposeOptions) Validate() error {
	if len(o.WildcardPolicy) > 0 && (o.WildcardPolicy != string(routev1.WildcardPolicySubdomain) && o.WildcardPolicy != string(routev1.WildcardPolicyNone)) {
		return fmt.Errorf("only \"Subdomain\" or \"None\" are supported for wildcard-policy")
	}
	return nil
}

func (o *ExposeOptions) Run() error {
	r := o.Builder.
		WithScheme(exposeCmdScheme, exposeCmdScheme.PrioritizedVersionsAllGroups()...).
		ContinueOnError().
		NamespaceParam(o.Namespace).DefaultNamespace().
		FilenameParam(o.EnforceNamespace, &o.ExposeServiceOptions.FilenameOptions).
		ResourceTypeOrNameArgs(false, o.Args...).
		Flatten().
		Do()
	infos, err := r.Infos()
	if err != nil {
		return err
	}
	if len(infos) > 1 {
		return fmt.Errorf("multiple resources provided: %v", o.Args)
	}
	info := infos[0]
	mapping := info.ResourceMapping()

	switch mapping.Resource.GroupResource() {
	case corev1.Resource("services"):
		if len(o.ExposeServiceOptions.Type) != 0 {
			return fmt.Errorf("cannot use --type when exposing route")
		}
		// The upstream generator will incorrectly chose service.Port instead of service.TargetPort
		// for the route TargetPort when no port is present.  Passing forcePort=true
		// causes UnsecuredRoute to always set a Port so the upstream default is not used.
		route, err := route.UnsecuredRoute(o.CoreClient, o.Namespace, o.ExposeServiceOptions.Name, info.Name, o.Port, true, o.EnforceNamespace)
		if err != nil {
			return err
		}
		route.Spec.Host = o.Hostname
		route.Spec.Path = o.Path
		route.Spec.WildcardPolicy = routev1.WildcardPolicyType(o.WildcardPolicy)
		if err := util.CreateOrUpdateAnnotation(kcmdutil.GetFlagBool(o.Cmd, kcmdutil.ApplyAnnotationsFlag), route, exposeCmdJSONEncoder()); err != nil {
			return err
		}

		if o.DryRunStrategy != kcmdutil.DryRunClient {
			route, err = o.RouteClient.Routes(o.ExposeServiceOptions.Namespace).Create(context.TODO(), route, metav1.CreateOptions{})
			if err != nil {
				return err
			}
		}

		return o.ExposeServiceOptions.PrintObj(route, o.ExposeServiceOptions.Out)
	}

	// Set default protocol back for generating services
	if len(kcmdutil.GetFlagString(o.Cmd, "protocol")) == 0 {
		o.ExposeServiceOptions.Protocol = "TCP"
	}

	return o.ExposeServiceOptions.RunExpose(o.Cmd, o.Args)
}
