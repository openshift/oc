package newapp

import (
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/openshift/api"
	"github.com/openshift/api/apps"
	"github.com/openshift/api/authorization"
	"github.com/openshift/api/build"
	"github.com/openshift/api/image"
	"github.com/openshift/api/network"
	"github.com/openshift/api/oauth"
	"github.com/openshift/api/project"
	"github.com/openshift/api/quota"
	"github.com/openshift/api/route"
	"github.com/openshift/api/security"
	"github.com/openshift/api/template"
	"github.com/openshift/api/user"
)

// we need a scheme that contains ONLY groupped APIs for newapp
var (
	newAppBulkScheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(api.InstallKube(newAppBulkScheme))

	utilruntime.Must(apps.Install(newAppBulkScheme))
	utilruntime.Must(authorization.Install(newAppBulkScheme))
	utilruntime.Must(build.Install(newAppBulkScheme))
	utilruntime.Must(image.Install(newAppBulkScheme))
	utilruntime.Must(network.Install(newAppBulkScheme))
	utilruntime.Must(oauth.Install(newAppBulkScheme))
	utilruntime.Must(project.Install(newAppBulkScheme))
	utilruntime.Must(quota.Install(newAppBulkScheme))
	utilruntime.Must(route.Install(newAppBulkScheme))
	utilruntime.Must(security.Install(newAppBulkScheme))
	utilruntime.Must(template.Install(newAppBulkScheme))
	utilruntime.Must(user.Install(newAppBulkScheme))
}
