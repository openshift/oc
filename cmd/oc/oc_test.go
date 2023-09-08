package main

import (
	"reflect"
	"testing"

	"github.com/openshift/oc/pkg/helpers/scheme"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/openshift/api/security"
)

func TestInstallNonCRDSecurity(t *testing.T) {
	withoutCRDs := runtime.NewScheme()
	utilruntime.Must(scheme.InstallNonCRDSecurity(withoutCRDs))
	nonCRDTypes := gvks(withoutCRDs.AllKnownTypes())

	complete := runtime.NewScheme()
	utilruntime.Must(security.Install(complete))
	expected := gvks(complete.AllKnownTypes())
	expected.Delete("security.openshift.io/v1, Kind=SecurityContextConstraints")
	expected.Delete("security.openshift.io/v1, Kind=SecurityContextConstraintsList")

	if !reflect.DeepEqual(expected, nonCRDTypes) {
		t.Errorf("unexpected security/v1 scheme without CRD types\nunexpected: %v\nmissing: %v", nonCRDTypes.Difference(expected).List(), expected.Difference(nonCRDTypes).List())
	}
}

func gvks(types map[schema.GroupVersionKind]reflect.Type) sets.String {
	ret := sets.NewString()
	for k := range types {
		ret.Insert(k.String())
	}
	return ret
}
