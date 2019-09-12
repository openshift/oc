package originpolymorphichelpers

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubectl/pkg/polymorphichelpers"

	appsv1 "github.com/openshift/api/apps/v1"
)

func NewMapBasedSelectorForObjectFn(delegate polymorphichelpers.MapBasedSelectorForObjectFunc) polymorphichelpers.MapBasedSelectorForObjectFunc {
	return func(object runtime.Object) (string, error) {
		switch t := object.(type) {
		case *appsv1.DeploymentConfig:
			return polymorphichelpers.MakeLabels(t.Spec.Selector), nil

		default:
			return delegate(object)
		}
	}
}
