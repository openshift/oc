package set

import (
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/openshift/api"
	"github.com/openshift/api/route"

	schemehelper "github.com/openshift/oc/pkg/helpers/scheme"
)

var setCustomScheme = runtime.NewScheme()

func InstallSchemes() {
	utilruntime.Must(api.InstallKube(setCustomScheme))
	schemehelper.InstallSchemes(setCustomScheme)
	// All the other commands can use route object
	// as CRD and there is no benefit installing route
	// as native object which is normally managed
	// by openshift-apiserver(microshift has no openshift-apiserver).
	// But set route-backends command requires to
	// register route to let some application templates including
	// route continue working. That's why, we are manually
	// registering route in here.
	utilruntime.Must(route.Install(setCustomScheme))
}
