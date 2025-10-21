package newapp

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/openshift/api"
	"github.com/openshift/api/route"

	schemehelper "github.com/openshift/oc/pkg/helpers/scheme"
)

var NewAppScheme = runtime.NewScheme()

var NewAppCodecs = serializer.NewCodecFactory(NewAppScheme)

func init() {
	utilruntime.Must(api.InstallKube(NewAppScheme))
	schemehelper.InstallSchemes(NewAppScheme)
	// All the other commands can use route object
	// as CRD and there is no benefit installing route
	// as native object which is normally managed
	// by openshift-apiserver(microshift has no openshift-apiserver).
	// But new-app command requires to
	// register route to let some application templates including
	// route continue working. That's why, we are manually
	// registering route in here.
	utilruntime.Must(route.Install(NewAppScheme))
}
