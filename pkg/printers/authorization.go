package printers

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kprinters "k8s.io/kubernetes/pkg/printers"

	authorizationv1 "github.com/openshift/api/authorization/v1"
	authorizationhelpers "github.com/openshift/oc/pkg/helpers/authorization"
)

const numOfRoleBindingRestrictionSubjectsShown = 3

func AddAuthorizationOpenShiftHandler(h kprinters.PrintHandler) {
	addRole(h)
	addRoleBinding(h)
	addRoleBindingRestriction(h)
	addSubjectRulesReview(h)
	addIsPersonalSubjectAccessReview(h)
}

func addRoleBindingRestriction(h kprinters.PrintHandler) {
	roleBindingRestrictionColumnsDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Subject Type", Type: "string", Description: "Describe the type of the role binding restriction"},
		{Name: "Subjects", Type: "string", Description: "List of subjects for this role binding restriction"},
	}
	if err := h.TableHandler(roleBindingRestrictionColumnsDefinitions, printRoleBindingRestriction); err != nil {
		panic(err)
	}
	if err := h.TableHandler(roleBindingRestrictionColumnsDefinitions, printRoleBindingRestrictionList); err != nil {
		panic(err)
	}
}

func printRoleBindingRestriction(roleBindingRestriction *authorizationv1.RoleBindingRestriction, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: roleBindingRestriction},
	}

	subjectList := []string{}

	switch {
	case roleBindingRestriction.Spec.UserRestriction != nil:
		for _, user := range roleBindingRestriction.Spec.UserRestriction.Users {
			subjectList = append(subjectList, user)
		}
		for _, group := range roleBindingRestriction.Spec.UserRestriction.Groups {
			subjectList = append(subjectList, fmt.Sprintf("group(%s)", group))
		}
		for _, selector := range roleBindingRestriction.Spec.UserRestriction.Selectors {
			subjectList = append(subjectList,
				metav1.FormatLabelSelector(&selector))
		}
	case roleBindingRestriction.Spec.GroupRestriction != nil:
		for _, group := range roleBindingRestriction.Spec.GroupRestriction.Groups {
			subjectList = append(subjectList, group)
		}
		for _, selector := range roleBindingRestriction.Spec.GroupRestriction.Selectors {
			subjectList = append(subjectList,
				metav1.FormatLabelSelector(&selector))
		}
	case roleBindingRestriction.Spec.ServiceAccountRestriction != nil:
		for _, sa := range roleBindingRestriction.Spec.ServiceAccountRestriction.ServiceAccounts {
			subjectList = append(subjectList, fmt.Sprintf("%s/%s",
				sa.Namespace, sa.Name))
		}
		for _, ns := range roleBindingRestriction.Spec.ServiceAccountRestriction.Namespaces {
			subjectList = append(subjectList, fmt.Sprintf("%s/*", ns))
		}
	}

	subjects := "<none>"

	if len(subjectList) > numOfRoleBindingRestrictionSubjectsShown {
		subjects = fmt.Sprintf("%s + %d more...",
			strings.Join(subjectList[:numOfRoleBindingRestrictionSubjectsShown], ", "),
			len(subjectList)-numOfRoleBindingRestrictionSubjectsShown)
	} else if len(subjectList) > 0 {
		subjects = strings.Join(subjectList, ", ")
	}

	row.Cells = append(row.Cells,
		formatResourceName(options.Kind, roleBindingRestriction.Name, options.WithKind),
		roleBindingRestrictionType(roleBindingRestriction),
		subjects,
	)

	return []metav1.TableRow{row}, nil
}

func printRoleBindingRestrictionList(roleBindingRestrictionList *authorizationv1.RoleBindingRestrictionList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(roleBindingRestrictionList.Items))
	for i := range roleBindingRestrictionList.Items {
		r, err := printRoleBindingRestriction(&roleBindingRestrictionList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

// roleBindingRestrictionType returns a string that indicates the type of the
// given RoleBindingRestriction.
func roleBindingRestrictionType(rbr *authorizationv1.RoleBindingRestriction) string {
	switch {
	case rbr.Spec.UserRestriction != nil:
		return "User"
	case rbr.Spec.GroupRestriction != nil:
		return "Group"
	case rbr.Spec.ServiceAccountRestriction != nil:
		return "ServiceAccount"
	default:
		return "<unknown>"
	}
}

func addIsPersonalSubjectAccessReview(h kprinters.PrintHandler) {
	isPersonalSubjectAccessReviewColumnsDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
	}
	if err := h.TableHandler(isPersonalSubjectAccessReviewColumnsDefinitions, printIsPersonalSubjectAccessReview); err != nil {
		panic(err)
	}
}

func printIsPersonalSubjectAccessReview(isPersonalSAR *authorizationv1.IsPersonalSubjectAccessReview, _ kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: isPersonalSAR},
	}
	row.Cells = append(row.Cells, "IsPersonalSubjectAccessReview")
	return []metav1.TableRow{row}, nil
}

func addSubjectRulesReview(h kprinters.PrintHandler) {
	policyRuleColumnsDefinitions := []metav1.TableColumnDefinition{
		{Name: "Verbs", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Non-Resource URLs", Type: "string", Description: "Describe the type of the role binding restriction"},
		{Name: "Resource Names", Type: "string", Description: "List of subjects for this role binding restriction"},
		{Name: "API Groups", Type: "string", Description: "List of subjects for this role binding restriction"},
		{Name: "Resources", Type: "string", Description: "List of subjects for this role binding restriction"},
	}
	if err := h.TableHandler(policyRuleColumnsDefinitions, printSubjectRulesReview); err != nil {
		panic(err)
	}
	if err := h.TableHandler(policyRuleColumnsDefinitions, printSelfSubjectRulesReview); err != nil {
		panic(err)
	}
}

func printSubjectRulesReview(subjectAccessReview *authorizationv1.SubjectRulesReview, _ kprinters.PrintOptions) ([]metav1.TableRow, error) {
	return printPolicyRule(subjectAccessReview.Status.Rules)
}

func printSelfSubjectRulesReview(selfSubjectAccessReview *authorizationv1.SelfSubjectRulesReview, _ kprinters.PrintOptions) ([]metav1.TableRow, error) {
	return printPolicyRule(selfSubjectAccessReview.Status.Rules)
}

func printPolicyRule(policyRules []authorizationv1.PolicyRule) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(policyRules))
	for _, rule := range policyRules {
		row := metav1.TableRow{}
		row.Cells = append(row.Cells,
			rule.Verbs,
			rule.NonResourceURLsSlice,
			rule.ResourceNames,
			rule.APIGroups,
			rule.Resources,
		)
		rows = append(rows, row)
	}
	return rows, nil
}

func addRole(h kprinters.PrintHandler) {
	roleColumnsDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
	}
	if err := h.TableHandler(roleColumnsDefinitions, printRole); err != nil {
		panic(err)
	}
	if err := h.TableHandler(roleColumnsDefinitions, printRoleList); err != nil {
		panic(err)
	}
	if err := h.TableHandler(roleColumnsDefinitions, printClusterRole); err != nil {
		panic(err)
	}
	if err := h.TableHandler(roleColumnsDefinitions, printClusterRoleList); err != nil {
		panic(err)
	}
}

func printClusterRole(clusterRole *authorizationv1.ClusterRole, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: clusterRole},
	}
	name := formatResourceName(options.Kind, clusterRole.Name, options.WithKind)
	if options.WithNamespace {
		name = fmt.Sprintf("%s/%s", clusterRole.Namespace, name)
	}
	row.Cells = append(row.Cells, name)
	return []metav1.TableRow{row}, nil
}

func printClusterRoleList(clusterRoleList *authorizationv1.ClusterRoleList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(clusterRoleList.Items))
	for i := range clusterRoleList.Items {
		r, err := printClusterRole(&clusterRoleList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printRoleList(roleList *authorizationv1.RoleList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(roleList.Items))
	for i := range roleList.Items {
		r, err := printRole(&roleList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printRole(role *authorizationv1.Role, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: role},
	}
	name := formatResourceName(options.Kind, role.Name, options.WithKind)
	if options.WithNamespace {
		name = fmt.Sprintf("%s/%s", role.Namespace, name)
	}
	row.Cells = append(row.Cells, name)
	return []metav1.TableRow{row}, nil
}

func addRoleBinding(h kprinters.PrintHandler) {
	roleBindingColumnsDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Role", Type: "string", Format: "name", Description: "Role name"},
		{Name: "Users", Type: "string", Description: authorizationv1.RoleBinding{}.SwaggerDoc()["userNames"]},
		{Name: "Groups", Type: "string", Description: authorizationv1.RoleBinding{}.SwaggerDoc()["groupNames"]},
		{Name: "Service Accounts", Type: "string", Description: "Service Account names"},
		{Name: "Users", Type: "string", Description: "Users names"},
	}
	if err := h.TableHandler(roleBindingColumnsDefinitions, printRoleBinding); err != nil {
		panic(err)
	}
	if err := h.TableHandler(roleBindingColumnsDefinitions, printRoleBindingList); err != nil {
		panic(err)
	}
	if err := h.TableHandler(roleBindingColumnsDefinitions, printClusterRoleBinding); err != nil {
		panic(err)
	}
	if err := h.TableHandler(roleBindingColumnsDefinitions, printClusterRoleBindingList); err != nil {
		panic(err)
	}
}

func printRoleBinding(roleBinding *authorizationv1.RoleBinding, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: roleBinding},
	}
	name := formatResourceName(options.Kind, roleBinding.Name, options.WithKind)
	if options.WithNamespace {
		name = fmt.Sprintf("%s/%s", roleBinding.Namespace, name)
	}

	// TODO: Move this to helpers
	users, groups, sas, others := authorizationhelpers.SubjectsStrings(roleBinding.Namespace, roleBinding.Subjects)

	row.Cells = append(row.Cells,
		name,
		fmt.Sprintf("%s/%s", roleBinding.RoleRef.Namespace, roleBinding.RoleRef.Name),
		truncatedList(users, 5),
		truncatedList(groups, 5),
		strings.Join(sas, ", "),
		strings.Join(others, ", "),
	)
	return []metav1.TableRow{row}, nil
}

func printRoleBindingList(roleBindingList *authorizationv1.RoleBindingList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(roleBindingList.Items))
	for i := range roleBindingList.Items {
		r, err := printRoleBinding(&roleBindingList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printClusterRoleBinding(clusterRoleBinding *authorizationv1.ClusterRoleBinding, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	return printRoleBinding(authorizationhelpers.ToRoleBinding(clusterRoleBinding), options)
}

func printClusterRoleBindingList(clusterRoleBindingList *authorizationv1.ClusterRoleBindingList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	return printRoleBindingList(authorizationhelpers.ToRoleBindingList(clusterRoleBindingList), options)
}

func truncatedList(list []string, maxLength int) string {
	if len(list) > maxLength {
		return fmt.Sprintf("%s (%d more)", strings.Join(list[0:maxLength], ", "), len(list)-maxLength)
	}
	return strings.Join(list, ", ")
}
