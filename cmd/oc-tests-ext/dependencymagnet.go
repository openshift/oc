// Blank imports register the Ginkgo test packages whose specs are picked up by
// oteginkgo.BuildExtensionTestSpecsFromOpenShiftGinkgoSuite() in main.go.
package main

import (
	_ "github.com/openshift/oc/test/e2e"
	_ "github.com/openshift/oc/test/e2e/admin/transition"
	_ "github.com/openshift/oc/test/e2e/admin/upgrade/accept"
)
