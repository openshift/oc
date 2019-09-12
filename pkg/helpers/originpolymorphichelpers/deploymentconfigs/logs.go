package deploymentconfigs

import (
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/scheme"

	appsv1 "github.com/openshift/api/apps/v1"
)

type RolloutLogInterface interface {
	Logs(name string, options appsv1.DeploymentLogOptions) *rest.Request
}

func NewRolloutLogClient(c rest.Interface, ns string) RolloutLogInterface {
	return &rolloutLogs{client: c, ns: ns}
}

type rolloutLogs struct {
	client rest.Interface
	ns     string
}

func (c *rolloutLogs) Logs(name string, options appsv1.DeploymentLogOptions) *rest.Request {
	return c.client.
		Get().
		Namespace(c.ns).
		Resource("deploymentConfigs").
		Name(name).
		SubResource("log").
		VersionedParams(&options, scheme.ParameterCodec)
}
