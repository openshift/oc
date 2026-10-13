package create

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/openshift/api"
	"github.com/openshift/api/quota"
	"github.com/openshift/api/route"

	schemehelper "github.com/openshift/oc/pkg/helpers/scheme"
)

var (
	createCmdScheme = runtime.NewScheme()
	createCmdCodecs = serializer.NewCodecFactory(createCmdScheme)
)

func init() {
	utilruntime.Must(api.InstallKube(createCmdScheme))
	schemehelper.InstallSchemes(createCmdScheme)
	// All the other commands can use route object
	// as CRD and there is no benefit installing route
	// as native object which is normally managed
	// by openshift-apiserver(microshift has no openshift-apiserver).
	// However, the create command requires routes for PrintObj,
	// so we define a custom scheme and install routes here.
	// see https://github.com/openshift/oc/pull/1534 for background.
	utilruntime.Must(route.Install(createCmdScheme))
	utilruntime.Must(quota.Install(createCmdScheme))
}

func createCmdJSONEncoder() runtime.Encoder {
	return unstructured.NewJSONFallbackEncoder(createCmdCodecs.LegacyCodec(createCmdScheme.PrioritizedVersionsAllGroups()...))
}
