package appregistry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDuplicatePackage(t *testing.T) {
	parser := inputParser{sourceSpecifier: &registrySpecifier{}}
	expectedPackages := []string{"Jaeger"}

	actual, err := parser.Parse([]string{"https://quay.io/cnr|community-operator|"}, "Jaeger,Jaeger")
	assert.NoError(t, err)
	assert.Equal(t, expectedPackages, actual.Packages)
}
