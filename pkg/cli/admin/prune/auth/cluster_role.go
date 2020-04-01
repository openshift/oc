package auth

import (
	"context"
	"fmt"
	"io"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	rbacv1client "k8s.io/client-go/kubernetes/typed/rbac/v1"
)

func reapForClusterRole(clusterBindingClient rbacv1client.ClusterRoleBindingsGetter, bindingClient rbacv1client.RoleBindingsGetter, namespace, name string, out io.Writer) error {
	errors := []error{}

	clusterBindings, err := clusterBindingClient.ClusterRoleBindings().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, clusterBinding := range clusterBindings.Items {
		if clusterBinding.RoleRef.Name == name {
			if err := clusterBindingClient.ClusterRoleBindings().Delete(context.TODO(), clusterBinding.Name, metav1.DeleteOptions{}); err != nil && !kerrors.IsNotFound(err) {
				errors = append(errors, err)
			} else {
				fmt.Fprintf(out, "clusterrolebinding.rbac.authorization.k8s.io/"+clusterBinding.Name+" deleted\n")
			}
		}
	}

	namespacedBindings, err := bindingClient.RoleBindings(metav1.NamespaceNone).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, namespacedBinding := range namespacedBindings.Items {
		if namespacedBinding.RoleRef.Kind == "ClusterRole" && namespacedBinding.RoleRef.Name == name {
			if err := bindingClient.RoleBindings(namespacedBinding.Namespace).Delete(context.TODO(), namespacedBinding.Name, metav1.DeleteOptions{}); err != nil && !kerrors.IsNotFound(err) {
				errors = append(errors, err)
			} else {
				fmt.Fprintf(out, "rolebinding.rbac.authorization.k8s.io/"+namespacedBinding.Name+" deleted\n")
			}
		}
	}

	return utilerrors.NewAggregate(errors)
}
