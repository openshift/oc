package appregistry

import (
	"archive/tar"
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-registry/pkg/apprclient"
)

const (
	etcdManifestLocation       = "../../manifests/etcd"
	prometheusManifestLocation = "../../manifests/prometheus"

	// Because we are using relative folder path to point to operator manifest,
	// when we create the tar ball we want to remove '../..' from the file path.
	tarFilePrefixTrim = "../../"

	deschedulerManifestLocation = "testdata/flattened/descheduler-bundle.yaml"
)

var (
	etcd = apprclient.RegistryMetadata{
		Namespace: "mynamespace",
		Name:      "etcd",
		Release:   "0.6.1",
		Digest:    "digest",
	}

	prometheus = apprclient.RegistryMetadata{
		Namespace: "mynamespace",
		Name:      "prometheus",
		Release:   "1.0.0",
		Digest:    "digest",
	}

	descheduler = apprclient.RegistryMetadata{
		Namespace: "mynamespace",
		Name:      "descheduler",
		Release:   "0.0.1",
		Digest:    "digest",
	}
)

func setupDownloadFolder(t *testing.T) (downloadpath string, remove func()) {
	const (
		nestedOutputDirectoryBase = "testdata/download"
	)

	// Create temporary working directory for manifests
	path, err := ioutil.TempDir(nestedOutputDirectoryBase, "manifests-")
	require.NoError(t, err)

	return path, func() {
		require.NoError(t, os.RemoveAll(path))
	}
}

func TestDecodeWithNestedBundleManifest(t *testing.T) {
	nestedDirectoryWant, remove := setupDownloadFolder(t)
	defer remove()

	manifests := []*apprclient.OperatorMetadata{
		{
			RegistryMetadata: etcd,
			Blob:             tarball(t, etcdManifestLocation, tarFilePrefixTrim),
		},
		{
			RegistryMetadata: prometheus,
			Blob:             tarball(t, prometheusManifestLocation, tarFilePrefixTrim),
		},
	}

	logger := logrus.WithField("test", "nested")

	decoder, err := NewManifestDecoder(logger)
	require.NoError(t, err)

	resultGot, errGot := decoder.Decode(manifests, nestedDirectoryWant)
	assert.NoError(t, errGot)
	assert.Equal(t, 0, resultGot.FlattenedCount)
	assert.Equal(t, 2, resultGot.NestedCount)
}

func TestDecodeWithFlattenedManifest(t *testing.T) {
	nestedDirectoryWant, remove := setupDownloadFolder(t)
	defer remove()

	manifests := []*apprclient.OperatorMetadata{
		{
			RegistryMetadata: descheduler,
			Blob:             tarball(t, deschedulerManifestLocation, tarFilePrefixTrim),
		},
	}

	logger := logrus.WithField("test", "flattened")

	decoder, err := NewManifestDecoder(logger)
	require.NoError(t, err)

	resultGot, errGot := decoder.Decode(manifests, nestedDirectoryWant)
	assert.NoError(t, errGot)
	assert.Equal(t, 1, resultGot.FlattenedCount)
	assert.Equal(t, 0, resultGot.NestedCount)
}

func TestDecodeWithBothFlattenedAndNestedManifest(t *testing.T) {
	nestedDirectoryWant, remove := setupDownloadFolder(t)
	defer remove()

	manifests := []*apprclient.OperatorMetadata{
		{
			RegistryMetadata: etcd,
			Blob:             tarball(t, etcdManifestLocation, tarFilePrefixTrim),
		},
		{
			RegistryMetadata: prometheus,
			Blob:             tarball(t, prometheusManifestLocation, tarFilePrefixTrim),
		},
		{
			RegistryMetadata: descheduler,
			Blob:             tarball(t, deschedulerManifestLocation, tarFilePrefixTrim),
		},
	}

	logger := logrus.WithField("test", "flattened+nested")

	decoder, err := NewManifestDecoder(logger)
	require.NoError(t, err)

	resultGot, errGot := decoder.Decode(manifests, nestedDirectoryWant)
	assert.NoError(t, errGot)
	assert.Equal(t, 1, resultGot.FlattenedCount)
	assert.Equal(t, 2, resultGot.NestedCount)
}

func tarball(t *testing.T, src string, trimPrefix string) (stream []byte) {
	var b bytes.Buffer

	_, err := os.Stat(src)
	require.NoError(t, err)

	writer := tar.NewWriter(&b)
	defer func() {
		writer.Close()
		stream = b.Bytes()
	}()

	load := func(file string) error {
		f, err := os.Open(file)
		if err != nil {
			return err
		}

		defer f.Close()

		if _, err := io.Copy(writer, f); err != nil {
			return err
		}

		return nil
	}

	namer := func(src, file string) string {
		name := strings.TrimPrefix(strings.Replace(file, trimPrefix, "", -1), string(filepath.Separator))
		return name
	}

	err = filepath.Walk(src, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(fi, fi.Name())
		if err != nil {
			return err
		}

		header.Name = namer(src, file)

		if err := writer.WriteHeader(header); err != nil {
			return err
		}

		if !fi.Mode().IsRegular() {
			return nil
		}

		if err = load(file); err != nil {
			return err
		}

		return nil
	})
	require.NoError(t, err)

	return
}
