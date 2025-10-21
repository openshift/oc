package auth

import (
	"io"
	"reflect"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
)

func TestClusterRoleReaper(t *testing.T) {
	tests := []struct {
		name                string
		role                *rbacv1.ClusterRole
		bindings            []*rbacv1.ClusterRoleBinding
		deletedBindingNames []string
	}{
		{
			name: "no bindings",
			role: &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "role",
				},
			},
		},
		{
			name: "bindings",
			role: &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "role",
				},
			},
			bindings: []*rbacv1.ClusterRoleBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-1",
					},
					RoleRef: rbacv1.RoleRef{Name: "role", Kind: "ClusterRole"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-2",
					},
					RoleRef: rbacv1.RoleRef{Name: "role2", Kind: "ClusterRole"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-3",
					},
					RoleRef: rbacv1.RoleRef{Name: "role", Kind: "ClusterRole"},
				},
			},
			deletedBindingNames: []string{"binding-1", "binding-3"},
		},
	}

	for _, test := range tests {
		startingObjects := []runtime.Object{}
		startingObjects = append(startingObjects, test.role)
		for _, binding := range test.bindings {
			startingObjects = append(startingObjects, binding)
		}
		tc := fake.NewSimpleClientset(startingObjects...)

		actualDeletedBindingNames := []string{}
		tc.PrependReactor("delete", "clusterrolebindings", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
			actualDeletedBindingNames = append(actualDeletedBindingNames, action.(clientgotesting.DeleteAction).GetName())
			return true, nil, nil
		})

		err := reapForClusterRole(tc.RbacV1(), tc.RbacV1(), "", test.role.Name, io.Discard)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", test.name, err)
		}

		expected := sets.NewString(test.deletedBindingNames...)
		actuals := sets.NewString(actualDeletedBindingNames...)
		if !reflect.DeepEqual(expected.List(), actuals.List()) {
			t.Errorf("%s: expected %v, got %v", test.name, expected.List(), actuals.List())
		}
	}
}

func TestClusterRoleReaperAgainstNamespacedBindings(t *testing.T) {
	tests := []struct {
		name                string
		role                *rbacv1.ClusterRole
		bindings            []*rbacv1.RoleBinding
		deletedBindingNames []string
	}{
		{
			name: "bindings",
			role: &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "role",
				},
			},
			bindings: []*rbacv1.RoleBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-1",
						Namespace: "ns-one",
					},
					RoleRef: rbacv1.RoleRef{Name: "role", Kind: "ClusterRole"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-2",
						Namespace: "ns-one",
					},
					RoleRef: rbacv1.RoleRef{Name: "role2", Kind: "ClusterRole"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-3",
						Namespace: "ns-one",
					},
					RoleRef: rbacv1.RoleRef{Name: "role", Kind: "ClusterRole"},
				},
			},
			deletedBindingNames: []string{"binding-1", "binding-3"},
		},
	}

	for _, test := range tests {
		startingObjects := []runtime.Object{}
		startingObjects = append(startingObjects, test.role)
		for _, binding := range test.bindings {
			startingObjects = append(startingObjects, binding)
		}
		tc := fake.NewSimpleClientset(startingObjects...)

		actualDeletedBindingNames := []string{}
		tc.PrependReactor("delete", "rolebindings", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
			actualDeletedBindingNames = append(actualDeletedBindingNames, action.(clientgotesting.DeleteAction).GetName())
			return true, nil, nil
		})

		err := reapForClusterRole(tc.RbacV1(), tc.RbacV1(), "", test.role.Name, io.Discard)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", test.name, err)
		}

		expected := sets.NewString(test.deletedBindingNames...)
		actuals := sets.NewString(actualDeletedBindingNames...)
		if !reflect.DeepEqual(expected.List(), actuals.List()) {
			t.Errorf("%s: expected %v, got %v", test.name, expected.List(), actuals.List())
		}
	}
}
