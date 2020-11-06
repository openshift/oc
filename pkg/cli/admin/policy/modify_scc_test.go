package policy

import (
	"fmt"
	"reflect"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	fakekubeclient "k8s.io/client-go/kubernetes/fake"
	fakerbacv1 "k8s.io/client-go/kubernetes/typed/rbac/v1/fake"
	clientgotesting "k8s.io/client-go/testing"
)

func TestModifySCC(t *testing.T) {
	sccName := "foo"
	tests := map[string]struct {
		startingCR  *rbacv1.ClusterRole
		startingCRB *rbacv1.ClusterRoleBinding
		subjects    []rbacv1.Subject
		expectedCR  *rbacv1.ClusterRole
		expectedCRB *rbacv1.ClusterRoleBinding
		remove      bool
	}{
		"add-user-to-empty": {
			expectedCR: &rbacv1.ClusterRole{
				Rules: []rbacv1.PolicyRule{
					{
						Verbs:     []string{"use"},
						APIGroups: []string{"security.openshift.io"},
						Resources: []string{"securitycontextconstraints"},
						ResourceNames: []string{sccName},
					},
			},
			startingCRB: &rbacv1.ClusterRoleBinding{},
			subjects:    []rbacv1.Subject{{Name: "one", Kind: "User"}, {Name: "two", Kind: "User"}},
			expectedCRB: &rbacv1.ClusterRoleBinding{
				Subjects: []rbacv1.Subject{{Kind: "User", Name: "one"}, {Kind: "User", Name: "two"}},
			},
			remove: false,
		},
		"add-user-to-existing": {
			startingCR:  &rbacv1.ClusterRole{},
			startingCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "User", Name: "one"}}},
			subjects:    []rbacv1.Subject{{Name: "two", Kind: "User"}},
			expectedCRB: &rbacv1.ClusterRoleBinding{
				Subjects: []rbacv1.Subject{{Kind: "User", Name: "one"}, {Kind: "User", Name: "two"}},
			},
			remove: false,
		},
		"add-user-to-existing-with-overlap": {
			startingCR:  &rbacv1.ClusterRole{},
			startingCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "User", Name: "one"}}},
			subjects:    []rbacv1.Subject{{Name: "one", Kind: "User"}, {Name: "two", Kind: "User"}},
			expectedCRB: &rbacv1.ClusterRoleBinding{
				Subjects: []rbacv1.Subject{{Kind: "User", Name: "one"}, {Kind: "User", Name: "two"}},
			},
			remove: false,
		},

		"add-sa-to-empty": {
			expectedCR: &rbacv1.ClusterRole{
				Rules: []rbacv1.PolicyRule{
					{
						Verbs:     []string{"use"},
						APIGroups: []string{"security.openshift.io"},
						Resources: []string{"securitycontextconstraints"},
						ResourceNames: []string{sccName},
					},
				},
			},
			startingCRB: &rbacv1.ClusterRoleBinding{},
			subjects:    []rbacv1.Subject{{Namespace: "a", Name: "one", Kind: "ServiceAccount"}, {Namespace: "b", Name: "two", Kind: "ServiceAccount"}},
			expectedCRB: &rbacv1.ClusterRoleBinding{
				Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: "a", Name: "one"}, {Kind: "ServiceAccount", Namespace: "b", Name: "two"}},
			},
			remove: false,
		},
		"add-sa-to-existing": {
			startingCR:  &rbacv1.ClusterRole{},
			startingCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "User", Name: "one"}}},
			subjects:    []rbacv1.Subject{{Namespace: "b", Name: "two", Kind: "ServiceAccount"}},
			expectedCRB: &rbacv1.ClusterRoleBinding{
				Subjects: []rbacv1.Subject{{Kind: "User", Name: "one"}, {Kind: "ServiceAccount", Namespace: "b", Name: "two"}},
			},
			remove: false,
		},
		"add-sa-to-existing-with-overlap": {
			startingCR:  &rbacv1.ClusterRole{},
			startingCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: "a", Name: "one"}}},
			subjects:    []rbacv1.Subject{{Namespace: "a", Name: "one", Kind: "ServiceAccount"}, {Namespace: "b", Name: "two", Kind: "ServiceAccount"}},
			expectedCRB: &rbacv1.ClusterRoleBinding{
				Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: "a", Name: "one"}, {Kind: "ServiceAccount", Namespace: "b", Name: "two"}},
			},
			remove: false,
		},

		"add-group-to-empty": {
			expectedCR: &rbacv1.ClusterRole{
				Rules: []rbacv1.PolicyRule{
					{
						Verbs:     []string{"use"},
						APIGroups: []string{"security.openshift.io"},
						Resources: []string{"securitycontextconstraints"},
						ResourceNames: []string{sccName},
					},
				},
			},
			startingCRB: &rbacv1.ClusterRoleBinding{},
			subjects:    []rbacv1.Subject{{Name: "one", Kind: "Group"}, {Name: "two", Kind: "Group"}},
			expectedCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "Group", Name: "one"}, {Kind: "Group", Name: "two"}}},
			remove:      false,
		},
		"add-group-to-existing": {
			startingCR:  &rbacv1.ClusterRole{},
			startingCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "Group", Name: "one"}}},
			subjects:    []rbacv1.Subject{{Name: "two", Kind: "Group"}},
			expectedCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "Group", Name: "one"}, {Kind: "Group", Name: "two"}}},
			remove:      false,
		},
		"add-group-to-existing-with-overlap": {
			startingCR:  &rbacv1.ClusterRole{},
			startingCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "Group", Name: "one"}}},
			subjects:    []rbacv1.Subject{{Name: "one", Kind: "Group"}, {Name: "two", Kind: "Group"}},
			expectedCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "Group", Name: "one"}, {Kind: "Group", Name: "two"}}},
			remove:      false,
		},

		"remove-user": {
			startingCR: &rbacv1.ClusterRole{},
			expectedCR: &rbacv1.ClusterRole{},
			startingCRB: &rbacv1.ClusterRoleBinding{
				Subjects: []rbacv1.Subject{{Kind: "User", Name: "one"}, {Kind: "User", Name: "two"}},
			},
			subjects:    []rbacv1.Subject{{Name: "one", Kind: "User"}, {Name: "two", Kind: "User"}},
			expectedCRB: &rbacv1.ClusterRoleBinding{},
			remove:      true,
		},
		"remove-user-from-existing-with-overlap": {
			startingCR: &rbacv1.ClusterRole{},
			startingCRB: &rbacv1.ClusterRoleBinding{
				Subjects: []rbacv1.Subject{{Kind: "User", Name: "one"}, {Kind: "User", Name: "two"}},
			},
			subjects:    []rbacv1.Subject{{Name: "two", Kind: "User"}},
			expectedCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "User", Name: "one"}}},
			remove:      true,
		},

		"remove-sa": {
			startingCR:  &rbacv1.ClusterRole{},
			startingCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: "a", Name: "one"}, {Kind: "ServiceAccount", Namespace: "b", Name: "two"}}},
			subjects:    []rbacv1.Subject{{Namespace: "a", Name: "one", Kind: "ServiceAccount"}, {Namespace: "b", Name: "two", Kind: "ServiceAccount"}},
			expectedCRB: &rbacv1.ClusterRoleBinding{},
			remove:      true,
		},
		"remove-sa-from-existing-with-overlap": {
			startingCR:  &rbacv1.ClusterRole{},
			startingCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: "a", Name: "one"}, {Kind: "ServiceAccount", Namespace: "b", Name: "two"}}},
			subjects:    []rbacv1.Subject{{Namespace: "b", Name: "two", Kind: "ServiceAccount"}},
			expectedCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: "a", Name: "one"}}},
			remove:      true,
		},

		"remove-group": {
			startingCR:  &rbacv1.ClusterRole{},
			startingCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "Group", Name: "one"}, {Kind: "Group", Name: "two"}}},
			subjects:    []rbacv1.Subject{{Name: "one", Kind: "Group"}, {Name: "two", Kind: "Group"}},
			expectedCRB: &rbacv1.ClusterRoleBinding{},
			remove:      true,
		},
		"remove-group-from-existing-with-overlap": {
			startingCR:  &rbacv1.ClusterRole{},
			startingCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "Group", Name: "one"}, {Kind: "Group", Name: "two"}}},
			subjects:    []rbacv1.Subject{{Name: "two", Kind: "Group"}},
			expectedCRB: &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{{Kind: "Group", Name: "one"}}},
			remove:      true,
		},
	}

	for tcName, tc := range tests {
		fakeRbacClient := fakerbacv1.FakeRbacV1{Fake: &(fakekubeclient.NewSimpleClientset().Fake)}

		roleRef := rbacv1.RoleRef{Kind: "ClusterRole", Name: fmt.Sprintf(RBACNamesFmt, sccName)}
		tc.expectedCRB.RoleRef = roleRef
		tc.startingCRB.RoleRef = roleRef

		fakeRbacClient.Fake.PrependReactor("get", "clusterroles", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
			if tc.startingCR == nil {
				return true, nil, kapierrors.NewNotFound(schema.GroupResource{}, "")
			}
			return true, tc.startingCR, nil
		})
		var actualCR *rbacv1.ClusterRole
		fakeRbacClient.Fake.PrependReactor("create", "clusterroles", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
			actualCR = action.(clientgotesting.CreateAction).GetObject().(*rbacv1.ClusterRole)
			return true, actualCR, nil
		})
		fakeRbacClient.Fake.PrependReactor("delete", "clusterroles", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
			actualCR = &rbacv1.ClusterRole{}
			return true, actualCR, nil
		})
		fakeRbacClient.Fake.PrependReactor("get", "clusterrolebindings", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
			return true, tc.startingCRB, nil
		})
		var actualCRB *rbacv1.ClusterRoleBinding
		fakeRbacClient.Fake.PrependReactor("update", "clusterrolebindings", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
			actualCRB = action.(clientgotesting.UpdateAction).GetObject().(*rbacv1.ClusterRoleBinding)
			return true, actualCRB, nil
		})
		fakeRbacClient.Fake.PrependReactor("delete", "clusterrolebindings", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
			actualCRB = &rbacv1.ClusterRoleBinding{Subjects: []rbacv1.Subject{}}
			return true, actualCRB, nil
		})

		o := &SCCModificationOptions{
			PrintFlags: genericclioptions.NewPrintFlags(""),
			ToPrinter:  func(string) (printers.ResourcePrinter, error) { return printers.NewDiscardingPrinter(), nil },

			SCCName:                 sccName,
			RbacClient:              &fakeRbacClient,
			DefaultSubjectNamespace: "",
			Subjects:                tc.subjects,

			IOStreams: genericclioptions.NewTestIOStreamsDiscard(),
		}

		var err error
		if tc.remove {
			err = o.RemoveSCC()
		} else {
			err = o.AddSCC()
		}
		if err != nil {
			t.Errorf("%s: unexpected err %v", tcName, err)
		}
		if !tc.remove && tc.startingCR == nil &&
			(actualCR == nil || !reflect.DeepEqual(actualCR.Rules, tc.expectedCR.Rules)) {
			t.Errorf("'%s': clusterrole should have been created", tcName)
		}
		shouldUpdate := !reflect.DeepEqual(tc.expectedCRB.Subjects, tc.startingCRB.Subjects)
		if shouldUpdate && actualCRB == nil {
			t.Errorf("'%s': clusterrolebinding should have been updated", tcName)
			continue
		}
		if e, a := tc.expectedCRB.Subjects, actualCRB.Subjects; !reflect.DeepEqual(e, a) {
			if len(e) == 0 && len(a) == 0 {
				continue
			}
			t.Errorf("%s: expected %v, actual %v", tcName, e, a)
		}
	}
}
