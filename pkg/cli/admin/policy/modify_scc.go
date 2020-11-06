package policy

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	rbacv1 "k8s.io/api/rbac/v1"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	rbacv1client "k8s.io/client-go/kubernetes/typed/rbac/v1"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/api/security"
)

const (
	RBACNamesFmt = "system:openshift:scc:%s"
)

var (
	addSCCToUserExample = templates.Examples(`
		# Add the 'restricted' security context constraint to user1 and user2
		oc adm policy add-scc-to-user restricted user1 user2

		# Add the 'privileged' security context constraint to the service account serviceaccount1 in the current namespace
		oc adm policy add-scc-to-user privileged -z serviceaccount1
	`)

	addSCCToGroupExample = templates.Examples(`
		# Add the 'restricted' security context constraint to group1 and group2
		oc adm policy add-scc-to-group restricted group1 group2
	`)
)

type SCCModificationOptions struct {
	PrintFlags *genericclioptions.PrintFlags

	ToPrinter func(string) (printers.ResourcePrinter, error)

	SCCName    string
	RbacClient rbacv1client.RbacV1Interface
	SANames    []string

	DefaultSubjectNamespace string
	ExplicitNamespace       bool
	Subjects                []rbacv1.Subject

	IsGroup        bool
	DryRunStrategy kcmdutil.DryRunStrategy
	Output         string

	genericclioptions.IOStreams
}

func NewSCCModificationOptions(streams genericclioptions.IOStreams) *SCCModificationOptions {
	return &SCCModificationOptions{
		PrintFlags: genericclioptions.NewPrintFlags("added to").WithTypeSetter(scheme.Scheme),
		IOStreams:  streams,
	}
}

func NewCmdAddSCCToGroup(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewSCCModificationOptions(streams)
	cmd := &cobra.Command{
		Use:     "add-scc-to-group SCC GROUP [GROUP ...]",
		Short:   "Add security context constraint to groups",
		Long:    `Add security context constraint to groups`,
		Example: addSCCToGroupExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.CompleteGroups(f, cmd, args))
			kcmdutil.CheckErr(o.AddSCC())
		},
	}

	kcmdutil.AddDryRunFlag(cmd)
	o.PrintFlags.AddFlags(cmd)
	return cmd
}

func NewCmdAddSCCToUser(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewSCCModificationOptions(streams)
	o.SANames = []string{}
	cmd := &cobra.Command{
		Use:     "add-scc-to-user SCC (USER | -z SERVICEACCOUNT) [USER ...]",
		Short:   "Add security context constraint to users or a service account",
		Long:    `Add security context constraint to users or a service account`,
		Example: addSCCToUserExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.CompleteUsers(f, cmd, args))
			kcmdutil.CheckErr(o.AddSCC())
		},
	}

	cmd.Flags().StringSliceVarP(&o.SANames, "serviceaccount", "z", o.SANames, "service account in the current namespace to use as a user")

	kcmdutil.AddDryRunFlag(cmd)
	o.PrintFlags.AddFlags(cmd)
	return cmd
}

func NewCmdRemoveSCCFromGroup(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewSCCModificationOptions(streams)
	cmd := &cobra.Command{
		Use:   "remove-scc-from-group SCC GROUP [GROUP ...]",
		Short: "Remove group from scc",
		Long:  `Remove group from scc`,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.CompleteGroups(f, cmd, args))
			kcmdutil.CheckErr(o.RemoveSCC())
		},
	}

	kcmdutil.AddDryRunFlag(cmd)
	o.PrintFlags.AddFlags(cmd)
	return cmd
}

func NewCmdRemoveSCCFromUser(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewSCCModificationOptions(streams)
	o.SANames = []string{}
	cmd := &cobra.Command{
		Use:   "remove-scc-from-user SCC USER [USER ...]",
		Short: "Remove user from scc",
		Long:  `Remove user from scc`,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.CompleteUsers(f, cmd, args))
			kcmdutil.CheckErr(o.RemoveSCC())
		},
	}

	cmd.Flags().StringSliceVarP(&o.SANames, "serviceaccount", "z", o.SANames, "service account in the current namespace to use as a user")

	kcmdutil.AddDryRunFlag(cmd)
	o.PrintFlags.AddFlags(cmd)
	return cmd
}

func (o *SCCModificationOptions) CompleteUsers(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return errors.New("you must specify a scc")
	}

	o.SCCName = args[0]
	o.Subjects = buildSubjects(args[1:], []string{})

	if (len(o.Subjects) == 0) && (len(o.SANames) == 0) {
		return errors.New("you must specify at least one user or service account")
	}

	var err error

	o.DryRunStrategy, err = kcmdutil.GetDryRunStrategy(cmd)
	if err != nil {
		return err
	}
	o.Output = kcmdutil.GetFlagString(cmd, "output")

	o.ToPrinter = func(operation string) (printers.ResourcePrinter, error) {
		o.PrintFlags.NamePrintFlags.Operation = getRolesSuccessMessage(o.DryRunStrategy, operation, o.getSubjectNames())
		return o.PrintFlags.ToPrinter()
	}

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.RbacClient, err = rbacv1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	o.DefaultSubjectNamespace, o.ExplicitNamespace, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	for _, sa := range o.SANames {
		o.Subjects = append(o.Subjects, rbacv1.Subject{Namespace: o.DefaultSubjectNamespace, Name: sa, Kind: "ServiceAccount"})
	}

	return nil
}

func (o *SCCModificationOptions) CompleteGroups(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) < 2 {
		return errors.New("you must specify at least two arguments: <scc> <group> [group]...")
	}

	o.Output = kcmdutil.GetFlagString(cmd, "output")

	var err error
	o.DryRunStrategy, err = kcmdutil.GetDryRunStrategy(cmd)
	if err != nil {
		return err
	}

	o.IsGroup = true
	o.SCCName = args[0]
	o.Subjects = buildSubjects([]string{}, args[1:])

	o.ToPrinter = func(operation string) (printers.ResourcePrinter, error) {
		o.PrintFlags.NamePrintFlags.Operation = getRolesSuccessMessage(o.DryRunStrategy, operation, o.getSubjectNames())
		return o.PrintFlags.ToPrinter()
	}

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.RbacClient, err = rbacv1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	o.DefaultSubjectNamespace, _, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	return nil
}

func (o *SCCModificationOptions) AddSCC() error {
	clusterRole := &rbacv1.ClusterRole{
		// this is ok because we know exactly how we want to be serialized
		TypeMeta:   metav1.TypeMeta{APIVersion: rbacv1.SchemeGroupVersion.String(), Kind: "ClusterRole"},
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf(RBACNamesFmt, o.SCCName)},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:     []string{"use"},
				APIGroups: []string{security.GroupName},
				Resources: []string{"securitycontextconstraints"},
			},
		},
	}

	if _, err := o.RbacClient.ClusterRoles().Get(context.TODO(), clusterRole.Name, metav1.GetOptions{}); err != nil && kapierrors.IsNotFound(err) {
		p, err := o.ToPrinter("added")
		if err != nil {
			return err
		}
		if o.DryRunStrategy == kcmdutil.DryRunClient {
			if err := p.PrintObj(clusterRole, o.Out); err != nil {
				return err
			}
		} else {
			if _, err := o.RbacClient.ClusterRoles().Create(context.TODO(), clusterRole, metav1.CreateOptions{}); err != nil {
				return err
			}
		}
	}
	addSubjects := RoleModificationOptions{
		RoleKind:        clusterRole.Kind,
		RoleName:        clusterRole.Name,
		RoleBindingName: fmt.Sprintf(RBACNamesFmt, o.SCCName),
		RbacClient:      o.RbacClient,
		Subjects:        o.Subjects,
		Targets:         o.getSubjectNames(),
		PrintFlags:      o.PrintFlags,
		ToPrinter:       o.ToPrinter,
		DryRunStrategy:  o.DryRunStrategy,
		IOStreams:       o.IOStreams,
	}
	if o.ExplicitNamespace {
		addSubjects.RoleBindingNamespace = o.DefaultSubjectNamespace
	}
	return addSubjects.AddRole()
}

func (o *SCCModificationOptions) RemoveSCC() error {
	removeSubjects := RoleModificationOptions{
		RoleKind:        "ClusterRole",
		RoleName:        fmt.Sprintf(RBACNamesFmt, o.SCCName),
		RoleBindingName: fmt.Sprintf(RBACNamesFmt, o.SCCName),
		RbacClient:      o.RbacClient,
		Subjects:        o.Subjects,
		Targets:         o.getSubjectNames(),
		PrintFlags:      o.PrintFlags,
		ToPrinter:       o.ToPrinter,
		DryRunStrategy:  o.DryRunStrategy,
		IOStreams:       o.IOStreams,
	}
	if o.ExplicitNamespace {
		removeSubjects.RoleBindingNamespace = o.DefaultSubjectNamespace
	}
	return removeSubjects.RemoveRole()
}

func (o *SCCModificationOptions) getSubjectNames() []string {
	targets := make([]string, 0, len(o.Subjects))
	for _, s := range o.Subjects {
		targets = append(targets, s.Name)
	}
	return targets
}
