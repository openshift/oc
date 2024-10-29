package nodepermissions

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/apiserver/pkg/authentication/user"
)

var (
	systemMastersClusterRole = rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "system:synthetic:system:masters",
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:           []string{"*"},
				APIGroups:       []string{"*"},
				Resources:       []string{"*"},
				NonResourceURLs: []string{"*"},
			},
		},
	}
)

type roleRef struct {
	namespace string
	name      string
}

type rbacCache struct {
	clusterRoles        []*rbacv1.ClusterRole
	clusterRoleBindings []*rbacv1.ClusterRoleBinding
	roles               []*rbacv1.Role
	roleBindings        []*rbacv1.RoleBinding

	clusterRolesByName   map[string]*rbacv1.ClusterRole
	rolesByNamespaceName map[roleRef]*rbacv1.Role
}

func newRBACCache(clusterRoleList *rbacv1.ClusterRoleList, clusterRoleBindingList *rbacv1.ClusterRoleBindingList,
	roleList *rbacv1.RoleList, roleBindingList *rbacv1.RoleBindingList) rbacCache {

	ret := rbacCache{
		clusterRolesByName:   map[string]*rbacv1.ClusterRole{},
		rolesByNamespaceName: map[roleRef]*rbacv1.Role{},
	}

	for i := range clusterRoleList.Items {
		curr := &clusterRoleList.Items[i]
		ret.clusterRoles = append(ret.clusterRoles, curr)
		ret.clusterRolesByName[curr.Name] = curr
	}
	for i := range clusterRoleBindingList.Items {
		curr := &clusterRoleBindingList.Items[i]
		ret.clusterRoleBindings = append(ret.clusterRoleBindings, curr)
	}
	for i := range roleList.Items {
		curr := &roleList.Items[i]
		ret.roles = append(ret.roles, curr)
		currRoleRef := roleRef{
			namespace: curr.Namespace,
			name:      curr.Name,
		}
		ret.rolesByNamespaceName[currRoleRef] = curr
	}
	for i := range roleBindingList.Items {
		curr := &roleBindingList.Items[i]
		ret.roleBindings = append(ret.roleBindings, curr)
	}

	return ret
}

func (r rbacCache) logicalRolesForUser(user user.Info) ([]*rbacv1.ClusterRole, []*rbacv1.Role) {
	for _, group := range user.GetGroups() {
		if group == "system:masters" {
			return []*rbacv1.ClusterRole{&systemMastersClusterRole}, nil
		}
	}

	clusterRoles := []*rbacv1.ClusterRole{}
	roles := []*rbacv1.Role{}

	for _, currClusterRoleBinding := range r.clusterRoleBindings {
		if !clusterRoleBindingMatches(user, currClusterRoleBinding) {
			continue
		}
		currClusterRole := r.clusterRolesByName[currClusterRoleBinding.RoleRef.Name]
		if currClusterRole != nil {
			clusterRoles = append(clusterRoles, currClusterRole)
		}
	}

	for _, currRoleBinding := range r.roleBindings {
		if !roleBindingMatches(user, currRoleBinding) {
			continue
		}
		switch {
		case currRoleBinding.RoleRef.Kind == "Role":
			currRoleRef := roleRef{
				namespace: currRoleBinding.Namespace,
				name:      currRoleBinding.RoleRef.Name,
			}
			currRole := r.rolesByNamespaceName[currRoleRef]
			if currRole != nil {
				roles = append(roles, currRole)
			}

		case currRoleBinding.RoleRef.Kind == "ClusterRole":
			currClusterRole := r.clusterRolesByName[currRoleBinding.RoleRef.Name]
			if currClusterRole != nil {
				// here we do a weird thing. We copy the clusterrole into a role, annotate it so we know it was original a ClusterRole
				// this will make the display local for namespace instead of all namespaces
				roles = append(roles, fakeRoleFromClusterRole(currClusterRole, currRoleBinding.Namespace))
			}
		}
	}

	return clusterRoles, roles
}

func fakeRoleFromClusterRole(in *rbacv1.ClusterRole, namespace string) *rbacv1.Role {
	inCopy := in.DeepCopy()
	ret := &rbacv1.Role{
		ObjectMeta: inCopy.ObjectMeta,
		Rules:      inCopy.Rules,
	}
	if ret.Labels == nil {
		ret.Labels = map[string]string{}
	}
	ret.Labels["operator.openshift.io/synthetic"] = "ActuallyClusterRole"
	ret.Name = "ActuallyClusterRole---" + inCopy.Name
	ret.Namespace = namespace
	return ret
}

func clusterRoleBindingMatches(needle user.Info, binding *rbacv1.ClusterRoleBinding) bool {
	return anySubjectMatches(needle, binding.Subjects)
}

func roleBindingMatches(needle user.Info, binding *rbacv1.RoleBinding) bool {
	return anySubjectMatches(needle, binding.Subjects)
}

func anySubjectMatches(needle user.Info, subjects []rbacv1.Subject) bool {
	for _, subject := range subjects {
		if subjectMatches(needle, subject) {
			return true
		}
	}

	return false
}

func subjectMatches(needle user.Info, subject rbacv1.Subject) bool {
	switch subject.Kind {
	case "User":
		if needle.GetName() == subject.Name {
			return true
		}
	case "Group":
		for _, needleGroup := range needle.GetGroups() {
			if subject.Name == needleGroup {
				return true
			}
		}
	case "ServiceAccount":
		if needleNamespace, needleName, err := serviceaccount.SplitUsername(needle.GetName()); err == nil {
			if needleNamespace == subject.Namespace && needleName == subject.Name {
				return true
			}
		}
	}

	return false
}

type nodeRoles struct {
	clusterRoles []*rbacv1.ClusterRole
	roles        []*rbacv1.Role

	clusterRolesByName   map[string]*rbacv1.ClusterRole
	rolesByNamespaceName map[roleRef]*rbacv1.Role
}

func newNodeRules() *nodeRoles {
	return &nodeRoles{
		clusterRolesByName:   map[string]*rbacv1.ClusterRole{},
		rolesByNamespaceName: map[roleRef]*rbacv1.Role{},
	}
}

// addRoles returns the rules that didn't previously exist in the nodeRoles. This is useful to know when we need to
// check for access to more secrets, pods, etc.
func (r *nodeRoles) addRoles(clusterRoles []*rbacv1.ClusterRole, roles []*rbacv1.Role) ([]*rbacv1.ClusterRole, []*rbacv1.Role) {
	novelClusterRoles := []*rbacv1.ClusterRole{}
	novelRoles := []*rbacv1.Role{}

	for i := range clusterRoles {
		curr := clusterRoles[i]
		_, existing := r.clusterRolesByName[curr.Name]
		if existing {
			continue
		}
		novelClusterRoles = append(novelClusterRoles, curr)
		r.clusterRoles = append(r.clusterRoles, curr)
		r.clusterRolesByName[curr.Name] = curr
	}

	for i := range roles {
		curr := roles[i]
		_, existing := r.clusterRolesByName[curr.Name]
		if existing {
			continue
		}
		novelRoles = append(novelRoles, curr)
		r.roles = append(r.roles, curr)

		currRoleRef := roleRef{
			namespace: curr.Namespace,
			name:      curr.Name,
		}
		r.rolesByNamespaceName[currRoleRef] = curr
	}

	return novelClusterRoles, novelRoles
}
