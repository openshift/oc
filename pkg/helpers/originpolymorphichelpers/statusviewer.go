package originpolymorphichelpers

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/kubectl/pkg/polymorphichelpers"

	appsv1 "github.com/openshift/api/apps/v1"
	deploymentcmd "github.com/openshift/oc/pkg/helpers/originpolymorphichelpers/deploymentconfigs"
)

func NewStatusViewerFn(delegate polymorphichelpers.StatusViewerFunc) polymorphichelpers.StatusViewerFunc {
	return func(mapping *meta.RESTMapping) (polymorphichelpers.StatusViewer, error) {
		if appsv1.SchemeGroupVersion.WithKind("DeploymentConfig").GroupKind() == mapping.GroupVersionKind.GroupKind() {
			return deploymentcmd.NewDeploymentConfigStatusViewer(), nil
		}

		return delegate(mapping)
	}
}
