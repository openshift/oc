package new

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/templates"

	userv1 "github.com/openshift/api/user/v1"
	userv1typedclient "github.com/openshift/client-go/user/clientset/versioned/typed/user/v1"
)

var (
	newLong = templates.LongDesc(`
		Create a new group.

		This command will create a new group with an optional list of users.
	`)

	newExample = templates.Examples(`
		# Add a group with no users
		oc adm groups new my-group

		# Add a group with two users
		oc adm groups new my-group user1 user2

		# Add a group with one user and shorter output
		oc adm groups new my-group user1 -o name
	`)
)

type NewGroupOptions struct {
	PrintFlags *genericclioptions.PrintFlags
	Printer    printers.ResourcePrinter

	GroupClient userv1typedclient.GroupsGetter

	Group string
	Users []string

	DryRunStrategy kcmdutil.DryRunStrategy

	genericclioptions.IOStreams
}

func NewNewGroupOptions(streams genericclioptions.IOStreams) *NewGroupOptions {
	return &NewGroupOptions{
		PrintFlags: genericclioptions.NewPrintFlags("created").WithTypeSetter(scheme.Scheme),
		IOStreams:  streams,
	}
}

func NewCmdNewGroup(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewNewGroupOptions(streams)
	cmd := &cobra.Command{
		Use:     "new GROUP [USER ...]",
		Short:   "Create a new group",
		Long:    newLong,
		Example: newExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}
	o.PrintFlags.AddFlags(cmd)
	kcmdutil.AddDryRunFlag(cmd)

	return cmd
}

func (o *NewGroupOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return errors.New("You must specify at least one argument: GROUP [USER ...]")
	}

	o.Group = args[0]
	if len(args) > 1 {
		o.Users = append(o.Users, args[1:]...)
	}

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.GroupClient, err = userv1typedclient.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	o.DryRunStrategy, err = kcmdutil.GetDryRunStrategy(cmd)
	if err != nil {
		return err
	}

	kcmdutil.PrintFlagsWithDryRunStrategy(o.PrintFlags, o.DryRunStrategy)
	o.Printer, err = o.PrintFlags.ToPrinter()
	if err != nil {
		return err
	}

	return nil
}

func (o *NewGroupOptions) Validate() error {
	if len(o.Group) == 0 {
		return fmt.Errorf("group is required")
	}

	return nil
}

func (o *NewGroupOptions) Run() error {
	group := &userv1.Group{
		// this is ok because we know exactly how we want to be serialized
		TypeMeta: metav1.TypeMeta{APIVersion: userv1.SchemeGroupVersion.String(), Kind: "Group"},
		ObjectMeta: metav1.ObjectMeta{
			Name: o.Group,
		},
	}

	usedNames := sets.String{}
	for _, user := range o.Users {
		if usedNames.Has(user) {
			continue
		}
		usedNames.Insert(user)

		group.Users = append(group.Users, user)
	}

	if o.DryRunStrategy != kcmdutil.DryRunClient {
		var err error
		group, err = o.GroupClient.Groups().Create(context.TODO(), group, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}

	return o.Printer.PrintObj(group, o.Out)
}
