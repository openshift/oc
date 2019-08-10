package appregistry

import (
	"archive/tar"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessWithFlattenedFormat(t *testing.T) {
	checker := formatChecker{}

	// Simulate a flattened format as used by Operator Courier.
	root := &tar.Header{
		Typeflag: tar.TypeDir,
		Name:     "/",
	}
	bundleYAML := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     "bundle.yaml",
	}

	workingDirectory, err := ioutil.TempDir(".", "manifests-")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.RemoveAll(workingDirectory))
	}()

	doneGot, errGot := checker.Process(root, "test", workingDirectory, nil)
	assert.NoError(t, errGot)
	assert.False(t, doneGot)

	doneGot, errGot = checker.Process(bundleYAML, "test", workingDirectory, nil)
	assert.NoError(t, errGot)
	assert.False(t, doneGot)

	assert.False(t, checker.IsNestedBundleFormat())
}

func TestProcessWithNestedBundleFormat(t *testing.T) {
	checker := formatChecker{}

	// Simulate a flattened format as used by Operator Courier
	headers := []*tar.Header{
		{Name: "manifests", Typeflag: tar.TypeDir},
		{Name: "manifests/etcd", Typeflag: tar.TypeDir},
		{Name: "manifests/etcd/0.6.1", Typeflag: tar.TypeDir},
		{Name: "manifests/etcd/0.6.1/etcdcluster.crd.yaml", Typeflag: tar.TypeReg},
		{Name: "manifests/etcd/0.6.1/etcdoperator.clusterserviceversion.yaml", Typeflag: tar.TypeReg},
		{Name: "manifests/etcd/etcd.package.yaml", Typeflag: tar.TypeReg},
	}

	workingDirectory, err := ioutil.TempDir(".", "manifests-")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.RemoveAll(workingDirectory))
	}()

	for _, header := range headers {
		doneGot, errGot := checker.Process(header, "test", workingDirectory, nil)
		assert.NoError(t, errGot)

		if checker.IsNestedBundleFormat() {
			assert.True(t, doneGot)
		}
	}

	assert.True(t, checker.IsNestedBundleFormat())
}
