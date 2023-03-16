package gendocs

import (
	"k8s.io/apimachinery/pkg/util/sets"
)

// a set of commands excluded from microshift
var microshiftCommands = sets.NewString(
	"oc autoscale",
	"oc cancel-build",
	"oc create build",
	"oc create clusterresourcequota",
	"oc create deploymentconfig",
	"oc create identity",
	"oc create imagestream",
	"oc create imagestreamtag",
	"oc create user",
	"oc create useridentitymapping",
	"oc idle",
	"oc import-image",
	"oc login",
	"oc logout",
	"oc new-app",
	"oc new-build",
	"oc new-project",
	"oc process",
	"oc project",
	"oc projects",
	"oc registry info",
	"oc registry login",
	"oc replace",
	"oc set build-hook",
	"oc set build-secret",
	"oc set deployment-hook",
	"oc set triggers",
	"oc start-build",
	"oc status",
	"oc whoami",
)
