package apps

import (
	"strings"

	"k8s.io/client-go/rest"
)

type ignoreDeploymentConfigWarningHandler struct {
	handler rest.WarningHandler
}

func NewIgnoreDeploymentConfigWarningHandler(handler rest.WarningHandler) rest.WarningHandler {
	return &ignoreDeploymentConfigWarningHandler{handler: handler}
}

func (h *ignoreDeploymentConfigWarningHandler) HandleWarningHeader(code int, agent string, text string) {
	if h.handler == nil || strings.Contains(text, `apps.openshift.io/v1 DeploymentConfig is deprecated`) {
		return
	}
	h.handler.HandleWarningHeader(code, agent, text)
}
