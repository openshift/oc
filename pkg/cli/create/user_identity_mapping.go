package create

import (
	"context"
	"fmt"

	ocmdhelpers "github.com/openshift/oc/pkg/helpers/cmd"
	"github.com/spf13/cobra"
	"k8s.io/client-go/discovery"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util"
	"k8s.io/kubectl/pkg/util/templates"

	userv1 "github.com/openshift/api/user/v1"
	userv1client "github.com/openshift/client-go/user/clientset/versioned/typed/user/v1"
)

var (
	userIdentityMappingLong = templates.LongDesc(`
		Typically, identities are automatically mapped to users during login. If automatic
		mapping is disabled (by using the "lookup" mapping method), or a mapping needs to
		be manually established between an identity and a user, this command can be used
		to create a user identity mapping object.
	`)

	userIdentityMappingExample = templates.Examples(`
		# Map the identity "acme_ldap:adamjones" to the user "ajones"
		oc create useridentitymapping acme_ldap:adamjones ajones
	`)
)

type CreateUserIdentityMappingOptions struct {
	CreateSubcommandOptions *CreateSubcommandOptions

	User     string
	Identity string

	UserIdentityMappingClient userv1client.UserIdentityMappingsGetter
	DiscoveryClient           discovery.DiscoveryInterface
}

func NewCreateUserIdentityMappingOptions(streams genericiooptions.IOStreams) *CreateUserIdentityMappingOptions {
	return &CreateUserIdentityMappingOptions{
		CreateSubcommandOptions: NewCreateSubcommandOptions(streams),
	}
}

// NewCmdCreateUserIdentityMapping is a macro command to create a new identity
func NewCmdCreateUserIdentityMapping(f genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewCreateUserIdentityMappingOptions(streams)
	cmd := &cobra.Command{
		Use:     "useridentitymapping <IDENTITY_NAME> <USER_NAME>",
		Short:   "Manually map an identity to a user",
		Long:    userIdentityMappingLong,
		Example: userIdentityMappingExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(cmd, f, args))
			ocmdhelpers.CheckOAuthDisabledErr(o.Run(), o.DiscoveryClient)
		},
	}

	o.CreateSubcommandOptions.AddFlags(cmd)
	cmdutil.AddDryRunFlag(cmd)

	return cmd
}

func (o *CreateUserIdentityMappingOptions) Complete(cmd *cobra.Command, f genericclioptions.RESTClientGetter, args []string) error {
	switch len(args) {
	case 0:
		return fmt.Errorf("identity is required")
	case 1:
		return fmt.Errorf("user name is required")
	case 2:
		o.Identity = args[0]
		o.User = args[1]
	default:
		return fmt.Errorf("exactly two arguments (identity and user name) are supported, not: %v", args)
	}

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.UserIdentityMappingClient, err = userv1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	o.DiscoveryClient, err = f.ToDiscoveryClient()
	if err != nil {
		return err
	}

	// we can't use Complete from CreateSubcommandOptions b/c it requires exactly one name
	// and create useridentitymapping requires exactly two
	o.CreateSubcommandOptions.Namespace, o.CreateSubcommandOptions.EnforceNamespace, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	o.CreateSubcommandOptions.DryRunStrategy, err = cmdutil.GetDryRunStrategy(cmd)
	if err != nil {
		return err
	}

	cmdutil.PrintFlagsWithDryRunStrategy(o.CreateSubcommandOptions.PrintFlags, o.CreateSubcommandOptions.DryRunStrategy)
	o.CreateSubcommandOptions.Printer, err = o.CreateSubcommandOptions.PrintFlags.ToPrinter()
	if err != nil {
		return err
	}

	return nil
}

func (o *CreateUserIdentityMappingOptions) Run() error {
	mapping := &userv1.UserIdentityMapping{
		// this is ok because we know exactly how we want to be serialized
		TypeMeta: metav1.TypeMeta{APIVersion: userv1.SchemeGroupVersion.String(), Kind: "UserIdentityMapping"},
		Identity: corev1.ObjectReference{Name: o.Identity},
		User:     corev1.ObjectReference{Name: o.User},
	}

	if err := util.CreateOrUpdateAnnotation(o.CreateSubcommandOptions.CreateAnnotation, mapping, createCmdJSONEncoder()); err != nil {
		return err
	}

	var err error
	if o.CreateSubcommandOptions.DryRunStrategy != cmdutil.DryRunClient {
		mapping, err = o.UserIdentityMappingClient.UserIdentityMappings().Create(context.TODO(), mapping, o.CreateSubcommandOptions.toCreateOptions())
		if err != nil {
			return err
		}
	}

	return o.CreateSubcommandOptions.Printer.PrintObj(mapping, o.CreateSubcommandOptions.Out)
}
