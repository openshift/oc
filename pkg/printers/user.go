package printers

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kprinters "k8s.io/kubernetes/pkg/printers"

	userv1 "github.com/openshift/api/user/v1"
)

func AddUserOpenShiftHandler(h kprinters.PrintHandler) {
	addUser(h)
	addIdentity(h)
	addUserIdentityMapping(h)
	addGroup(h)
}

func addUser(h kprinters.PrintHandler) {
	userColumnsDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "UID", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["uid"]},
		{Name: "Full Name", Type: "string", Description: userv1.User{}.SwaggerDoc()["fullName"]},
		{Name: "Identities", Type: "string", Description: userv1.User{}.SwaggerDoc()["identities"]},
	}
	if err := h.TableHandler(userColumnsDefinitions, printUser); err != nil {
		panic(err)
	}
	if err := h.TableHandler(userColumnsDefinitions, printUserList); err != nil {
		panic(err)
	}
}

func printUser(user *userv1.User, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: user},
	}

	row.Cells = append(row.Cells,
		formatResourceName(options.Kind, user.Name, options.WithKind),
		user.UID,
		user.FullName,
		strings.Join(user.Identities, ", "),
	)

	return []metav1.TableRow{row}, nil
}

func printUserList(userList *userv1.UserList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(userList.Items))
	for i := range userList.Items {
		r, err := printUser(&userList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func addIdentity(h kprinters.PrintHandler) {
	identityColumnsDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "IDP Name", Type: "string", Format: "name", Description: userv1.Identity{}.SwaggerDoc()["providerName"]},
		{Name: "IDP User Name", Type: "string", Format: "name", Description: userv1.Identity{}.SwaggerDoc()["providerUserName"]},
		{Name: "User Name", Type: "string", Format: "name", Description: userv1.Identity{}.SwaggerDoc()["user"]},
		{Name: "User UID", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["uid"]},
	}
	if err := h.TableHandler(identityColumnsDefinitions, printIdentity); err != nil {
		panic(err)
	}
	if err := h.TableHandler(identityColumnsDefinitions, printIdentityList); err != nil {
		panic(err)
	}
}

func printIdentity(identity *userv1.Identity, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: identity},
	}

	row.Cells = append(row.Cells,
		formatResourceName(options.Kind, identity.Name, options.WithKind),
		identity.ProviderName,
		identity.ProviderUserName,
		identity.User.Name,
		identity.User.UID,
	)

	return []metav1.TableRow{row}, nil
}

func printIdentityList(identityList *userv1.IdentityList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(identityList.Items))
	for i := range identityList.Items {
		r, err := printIdentity(&identityList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func addUserIdentityMapping(h kprinters.PrintHandler) {
	identityColumnsDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Identity", Type: "string", Description: userv1.UserIdentityMapping{}.SwaggerDoc()["identity"]},
		{Name: "User Name", Type: "string", Description: userv1.UserIdentityMapping{}.SwaggerDoc()["user"]},
		{Name: "User UID", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["uid"]},
	}
	if err := h.TableHandler(identityColumnsDefinitions, printUserIdentityMapping); err != nil {
		panic(err)
	}
}

func printUserIdentityMapping(mapping *userv1.UserIdentityMapping, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: mapping},
	}

	row.Cells = append(row.Cells,
		formatResourceName(options.Kind, mapping.Name, options.WithKind),
		mapping.Identity.Name,
		mapping.User.Name,
		mapping.User.UID,
	)

	return []metav1.TableRow{row}, nil
}

func addGroup(h kprinters.PrintHandler) {
	groupColumnsDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Users", Type: "string", Description: userv1.Group{}.SwaggerDoc()["users"]},
	}

	if err := h.TableHandler(groupColumnsDefinitions, printGroup); err != nil {
		panic(err)
	}
	if err := h.TableHandler(groupColumnsDefinitions, printGroupList); err != nil {
		panic(err)
	}
}

func printGroup(group *userv1.Group, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: group},
	}

	row.Cells = append(row.Cells,
		formatResourceName(options.Kind, group.Name, options.WithKind),
		strings.Join(group.Users, ", "),
	)

	return []metav1.TableRow{row}, nil
}

func printGroupList(groupList *userv1.GroupList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(groupList.Items))
	for i := range groupList.Items {
		r, err := printGroup(&groupList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}
