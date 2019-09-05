package printers

import (
	"strings"
	"time"

	units "github.com/docker/go-units"

	"k8s.io/apimachinery/pkg/runtime/schema"
	kprinters "k8s.io/kubernetes/pkg/printers"
	kprintersinternal "k8s.io/kubernetes/pkg/printers/internalversion"
)

func init() {
	// TODO this should be eliminated
	kprintersinternal.AddHandlers = func(p kprinters.PrintHandler) {
		// kubernetes handlers
		kprintersinternal.AddKubeHandlers(p)

		AddAppsOpenShiftHandlers(p)
		AddBuildOpenShiftHandlers(p)
		AddImageOpenShiftHandlers(p)
		AddProjectOpenShiftHandlers(p)
		AddRouteOpenShiftHandlers(p)
		AddTemplateOpenShiftHandlers(p)
		AddSecurityOpenShiftHandler(p)
		AddAuthorizationOpenShiftHandler(p)
		AddQuotaOpenShiftHandler(p)
		AddOAuthOpenShiftHandler(p)
		AddUserOpenShiftHandler(p)
	}
}

// formatResourceName receives a resource kind, name, and boolean specifying
// whether or not to update the current name to "kind/name"
func formatResourceName(kind schema.GroupKind, name string, withKind bool) string {
	if !withKind || kind.Empty() {
		return name
	}

	return strings.ToLower(kind.String()) + "/" + name
}

func formatRelativeTime(t time.Time) string {
	return units.HumanDuration(timeNowFn().Sub(t))
}

var timeNowFn = func() time.Time {
	return time.Now()
}
