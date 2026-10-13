// This file was previously used to import test packages via blank imports.
// Test registration is now done explicitly via e2e.RegisterTests() in main.go.
package main

import (
	// Import test packages to register Ginkgo tests
	_ "github.com/openshift/oc/test/e2e"
)
