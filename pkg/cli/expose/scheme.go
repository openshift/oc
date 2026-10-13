package expose

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/openshift/api"
	"github.com/openshift/api/route"

	schemehelper "github.com/openshift/oc/pkg/helpers/scheme"
)

var (
	exposeCmdScheme = runtime.NewScheme()
	exposeCmdCodecs = serializer.NewCodecFactory(exposeCmdScheme)
)

func init() {
	utilruntime.Must(api.InstallKube(exposeCmdScheme))
	schemehelper.InstallSchemes(exposeCmdScheme)
	// All the other commands can use route object
	// as CRD and there is no benefit installing route
	// as native object which is normally managed
	// by openshift-apiserver(microshift has no openshift-apiserver).
	// However, the expose command requires routes for PrintObj,
	// so we define a custom scheme and install routes here.
	// see https://github.com/openshift/oc/pull/1534 for background.
	utilruntime.Must(route.Install(exposeCmdScheme))
}

func exposeCmdJSONEncoder() runtime.Encoder {
	return unstructured.NewJSONFallbackEncoder(exposeCmdCodecs.LegacyCodec(exposeCmdScheme.PrioritizedVersionsAllGroups()...))
}
