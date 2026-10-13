package originpolymorphichelpers

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubectl/pkg/polymorphichelpers"

	appsv1 "github.com/openshift/api/apps/v1"
)

func NewProtocolsForObjectFn(delegate polymorphichelpers.MultiProtocolsWithForObjectFunc) polymorphichelpers.MultiProtocolsWithForObjectFunc {
	return func(object runtime.Object) (map[string][]string, error) {
		switch t := object.(type) {
		case *appsv1.DeploymentConfig:
			return getMultiProtocols(t.Spec.Template.Spec), nil

		default:
			return delegate(object)
		}
	}
}

func getMultiProtocols(spec corev1.PodSpec) map[string][]string {
	result := make(map[string][]string)
	var protocol corev1.Protocol
	for _, container := range spec.Containers {
		for _, port := range container.Ports {
			// Empty protocol must be defaulted (TCP)
			protocol = corev1.ProtocolTCP
			if len(port.Protocol) > 0 {
				protocol = port.Protocol
			}
			p := strconv.Itoa(int(port.ContainerPort))
			result[p] = append(result[p], string(protocol))
		}
	}
	return result
}
