package create

import (
	"context"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util"
	"k8s.io/kubectl/pkg/util/templates"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/oc/pkg/cli/create/route"
	fileutil "github.com/openshift/oc/pkg/helpers/file"
)

var (
	reencryptRouteLong = templates.LongDesc(`
		Create a route that uses reencrypt TLS termination

		Specify the service (either just its name or using type/name syntax) that the
		generated route should expose using the --service flag. You may also specify
		a destination CA certificate using the --dest-ca-cert flag. If --dest-ca-cert
		is omitted, the route will use the service CA, meaning the service must use
		a serving certificate from the serving cert signer.
	`)

	reencryptRouteExample = templates.Examples(`
		# Create a route named "my-route" that exposes the frontend service:
		arvan paas create route reencrypt my-route --service=frontend --dest-ca-cert cert.cert

		# Create a reencrypt route that exposes the frontend service, letting the
		# route name default to the service name and the destination CA certificate
		# default to the service CA:
		arvan paas create route reencrypt --service=frontend
	`)
)

type CreateReencryptRouteOptions struct {
	CreateRouteSubcommandOptions *CreateRouteSubcommandOptions

	Hostname       string
	Port           string
	InsecurePolicy string
	Service        string
	Path           string
	Cert           string
	Key            string
	CACert         string
	DestCACert     string
	WildcardPolicy string
}

// NewCmdCreateReencryptRoute is a macro command to create a reencrypt route.
func NewCmdCreateReencryptRoute(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := &CreateReencryptRouteOptions{
		CreateRouteSubcommandOptions: NewCreateRouteSubcommandOptions(streams),
	}
	cmd := &cobra.Command{
		Use:     "reencrypt [NAME] --service=SERVICE",
		Short:   "Create a route that uses reencrypt TLS termination",
		Long:    reencryptRouteLong,
		Example: reencryptRouteExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().StringVar(&o.Hostname, "hostname", o.Hostname, "Set a hostname for the new route")
	cmd.Flags().StringVar(&o.Port, "port", o.Port, "Name of the service port or number of the container port the route will route traffic to")
	cmd.Flags().StringVar(&o.InsecurePolicy, "insecure-policy", o.InsecurePolicy, "Set an insecure policy for the new route")
	cmd.Flags().StringVar(&o.Service, "service", o.Service, "Name of the service that the new route is exposing")
	cmd.MarkFlagRequired("service")
	cmd.Flags().StringVar(&o.Path, "path", o.Path, "Path that the router watches to route traffic to the service.")
	cmd.Flags().StringVar(&o.Cert, "cert", o.Cert, "Path to a certificate file.")
	cmd.MarkFlagFilename("cert")
	cmd.Flags().StringVar(&o.Key, "key", o.Key, "Path to a key file.")
	cmd.MarkFlagFilename("key")
	cmd.Flags().StringVar(&o.CACert, "ca-cert", o.CACert, "Path to a CA certificate file.")
	cmd.MarkFlagFilename("ca-cert")
	cmd.Flags().StringVar(&o.DestCACert, "dest-ca-cert", o.DestCACert, "Path to a CA certificate file, used for securing the connection from the router to the destination. Defaults to the Service CA.")
	cmd.MarkFlagFilename("dest-ca-cert")
	cmd.Flags().StringVar(&o.WildcardPolicy, "wildcard-policy", o.WildcardPolicy, "Sets the WilcardPolicy for the hostname, the default is \"None\". valid values are \"None\" and \"Subdomain\"")

	kcmdutil.AddValidateFlags(cmd)
	o.CreateRouteSubcommandOptions.AddFlags(cmd)
	kcmdutil.AddDryRunFlag(cmd)

	return cmd
}

func (o *CreateReencryptRouteOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	return o.CreateRouteSubcommandOptions.Complete(f, cmd, args)
}

func (o *CreateReencryptRouteOptions) Run() error {
	serviceName, err := resolveServiceName(o.CreateRouteSubcommandOptions.Mapper, o.Service)
	if err != nil {
		return err
	}
	route, err := route.UnsecuredRoute(o.CreateRouteSubcommandOptions.CoreClient, o.CreateRouteSubcommandOptions.Namespace, o.CreateRouteSubcommandOptions.Name, serviceName, o.Port, false)
	if err != nil {
		return err
	}

	if len(o.WildcardPolicy) > 0 {
		route.Spec.WildcardPolicy = routev1.WildcardPolicyType(o.WildcardPolicy)
	}

	route.Spec.Host = o.Hostname
	route.Spec.Path = o.Path

	route.Spec.TLS = new(routev1.TLSConfig)
	route.Spec.TLS.Termination = routev1.TLSTerminationReencrypt

	cert, err := fileutil.LoadData(o.Cert)
	if err != nil {
		return err
	}
	route.Spec.TLS.Certificate = string(cert)
	key, err := fileutil.LoadData(o.Key)
	if err != nil {
		return err
	}
	route.Spec.TLS.Key = string(key)
	caCert, err := fileutil.LoadData(o.CACert)
	if err != nil {
		return err
	}
	route.Spec.TLS.CACertificate = string(caCert)
	destCACert, err := fileutil.LoadData(o.DestCACert)
	if err != nil {
		return err
	}
	route.Spec.TLS.DestinationCACertificate = string(destCACert)

	if len(o.InsecurePolicy) > 0 {
		route.Spec.TLS.InsecureEdgeTerminationPolicy = routev1.InsecureEdgeTerminationPolicyType(o.InsecurePolicy)
	}

	if err := util.CreateOrUpdateAnnotation(o.CreateRouteSubcommandOptions.CreateAnnotation, route, scheme.DefaultJSONEncoder()); err != nil {
		return err
	}

	if o.CreateRouteSubcommandOptions.DryRunStrategy != kcmdutil.DryRunClient {
		route, err = o.CreateRouteSubcommandOptions.Client.Routes(o.CreateRouteSubcommandOptions.Namespace).Create(context.TODO(), route, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}

	return o.CreateRouteSubcommandOptions.Printer.PrintObj(route, o.CreateRouteSubcommandOptions.Out)
}
