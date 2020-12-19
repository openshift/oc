package expose

import (
	"fmt"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/kubectl/pkg/cmd/expose"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/cli/create/route"
)

var (
	exposeLong = templates.LongDesc(`
		Expose containers internally as services or externally via routes

		There is also the ability to expose a deployment configuration, replication controller, service, or pod
		as a new service on a specified port. If no labels are specified, the new object will re-use the
		labels from the object it exposes.
	`)

	exposeExample = templates.Examples(`
		# Create a route based on service nginx. The new route will re-use nginx's labels
		arvan paas expose service nginx

		# Create a route and specify your own label and route name
		arvan paas expose service nginx -l name=myroute --name=fromdowntown

		# Create a route and specify a hostname
		arvan paas expose service nginx --hostname=www.example.com

		# Create a route with wildcard
		arvan paas expose service nginx --hostname=x.example.com --wildcard-policy=Subdomain
		This would be equivalent to *.example.com. NOTE: only hosts are matched by the wildcard, subdomains would not be included.

		# Expose a deployment configuration as a service and use the specified port
		arvan paas expose dc ruby-hello-world --port=8080

		# Expose a service as a route in the specified path
		arvan paas expose service nginx --path=/nginx

		# Expose a service using different generators
		arvan paas expose service nginx --name=exposed-svc --port=12201 --protocol="TCP" --generator="service/v2"
		arvan paas expose service nginx --name=my-route --port=12201 --generator="route/v1"

		Exposing a service using the "route/v1" generator (default) will create a new exposed route with the "--name" provided
		(or the name of the service otherwise). You may not specify a "--protocol" or "--target-port" option when using this generator.
	`)
)

type ExposeOptions struct {
	Hostname       string
	Path           string
	WildcardPolicy string

	Namespace        string
	EnforceNamespace bool
	CoreClient       corev1client.CoreV1Interface
	Builder          func() *resource.Builder
	Args             []string
	Generator        string
	Filenames        []string
	Port             string
	Protocol         string
}

func NewExposeOptions() *ExposeOptions {
	return &ExposeOptions{}
}

// NewCmdExpose is a wrapper for the Kubernetes cli expose command
func NewCmdExpose(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewExposeOptions()

	cmd := expose.NewCmdExposeService(f, streams)
	cmd.Short = "Expose a replicated application as a service or route"
	cmd.Long = exposeLong
	cmd.Example = exposeExample
	// Default generator to an empty string so we can get more flexibility
	// when setting defaults based on input resources
	cmd.Flags().Set("generator", "")
	cmd.Flag("generator").Usage = "The name of the API generator to use. Defaults to \"route/v1\". Available generators include \"service/v1\", \"service/v2\", and \"route/v1\". \"service/v1\" will automatically name the port \"default\", while \"service/v2\" will leave it unnamed."
	cmd.Flag("generator").DefValue = ""
	// Default protocol to an empty string so we can get more flexibility
	// when validating the use of it (invalid for routes)
	cmd.Flags().Set("protocol", "")
	cmd.Flag("protocol").DefValue = ""
	cmd.Flag("protocol").Changed = false
	cmd.Flag("port").Usage = "The port that the resource should serve on."
	defRun := cmd.Run
	cmd.Run = func(cmd *cobra.Command, args []string) {
		kcmdutil.CheckErr(o.Complete(cmd, f, args))
		kcmdutil.CheckErr(o.Validate(cmd))
		defRun(cmd, args)
	}

	cmd.Flags().StringVar(&o.Hostname, "hostname", o.Hostname, "Set a hostname for the new route")
	cmd.Flags().StringVar(&o.Path, "path", o.Path, "Set a path for the new route")
	cmd.Flags().StringVar(&o.WildcardPolicy, "wildcard-policy", o.WildcardPolicy, "Sets the WildcardPolicy for the hostname, the default is \"None\". Valid values are \"None\" and \"Subdomain\"")

	return cmd
}

func (o *ExposeOptions) Complete(cmd *cobra.Command, f kcmdutil.Factory, args []string) error {
	var err error
	o.Namespace, o.EnforceNamespace, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	config, err := f.ToRESTConfig()
	if err != nil {
		return err
	}

	o.CoreClient, err = corev1client.NewForConfig(config)
	if err != nil {
		return err
	}

	o.Builder = f.NewBuilder
	o.Args = args
	o.Generator = kcmdutil.GetFlagString(cmd, "generator")
	o.Filenames = kcmdutil.GetFlagStringSlice(cmd, "filename")
	o.Port = kcmdutil.GetFlagString(cmd, "port")
	o.Protocol = kcmdutil.GetFlagString(cmd, "protocol")

	return nil
}

// Validate adds one layer of validation prior to calling the upstream
// expose command.
func (o *ExposeOptions) Validate(cmd *cobra.Command) error {
	r := o.Builder().
		WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
		ContinueOnError().
		NamespaceParam(o.Namespace).DefaultNamespace().
		FilenameParam(o.EnforceNamespace, &resource.FilenameOptions{Recursive: false, Filenames: o.Filenames}).
		ResourceTypeOrNameArgs(false, o.Args...).
		Flatten().
		Do()
	infos, err := r.Infos()
	if err != nil {
		return err
	}

	if len(o.WildcardPolicy) > 0 && (o.WildcardPolicy != "Subdomain" && o.WildcardPolicy != "None") {
		return fmt.Errorf("only \"Subdomain\" or \"None\" are supported for wildcard-policy")
	}

	if len(infos) > 1 {
		return fmt.Errorf("multiple resources provided: %v", o.Args)
	}
	info := infos[0]
	mapping := info.ResourceMapping()

	switch mapping.Resource.GroupResource() {
	case corev1.Resource("services"):
		switch o.Generator {
		case "service/v1", "service/v2":
			// Set default protocol back for generating services
			if len(o.Protocol) == 0 {
				cmd.Flags().Set("protocol", "TCP")
			}
		case "":
			// Default exposing services as a route
			cmd.Flags().Set("generator", "route/v1")
			fallthrough
		case "route/v1":
			// The upstream generator will incorrectly chose service.Port instead of service.TargetPort
			// for the route TargetPort when no port is present.  Passing forcePort=true
			// causes UnsecuredRoute to always set a Port so the upstream default is not used.
			route, err := route.UnsecuredRoute(o.CoreClient, o.Namespace, info.Name, info.Name, o.Port, true)
			if err != nil {
				return err
			}
			if route.Spec.Port != nil {
				cmd.Flags().Set("port", route.Spec.Port.TargetPort.String())
			}
		}

	default:
		switch o.Generator {
		case "route/v1":
			return fmt.Errorf("cannot expose a %s as a route", mapping.GroupVersionKind.Kind)
		case "":
			// Default exposing everything except services as a service
			cmd.Flags().Set("generator", "service/v2")
			fallthrough
		case "service/v1", "service/v2":
			// Set default protocol back for generating services
			if len(kcmdutil.GetFlagString(cmd, "protocol")) == 0 {
				cmd.Flags().Set("protocol", "TCP")
			}
		}
	}

	return nil
}
