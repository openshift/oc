package catalog

import (
	"github.com/sirupsen/logrus"
	"k8s.io/klog"

	"github.com/operator-framework/operator-registry/pkg/apprclient"
	"github.com/operator-framework/operator-registry/pkg/appregistry"
)

// NewDownloader is a constructor for the Downloader interface
func NewDownloader(client apprclient.Client) Downloader {
	return &downloader{
		client: client,
	}
}

// Downloader is an interface that is implemented by structs that
// implement the DownloadManifests method.
type Downloader interface {
	// DownloadManifests downloads the manifests in a namespace into a local directory
	DownloadManifests(directory, namespace string) error
}

type downloader struct {
	client apprclient.Client
}

func (d *downloader) DownloadManifests(directory, namespace string) error {
	klog.V(4).Infof("Downloading manifests at namespace %s to %s", namespace, directory)

	log := logrus.New().WithField("ns", namespace)

	packages, err := d.client.ListPackages(namespace)
	if err != nil {
		return err
	}

	for _, pkg := range packages {
		klog.V(4).Infof("Downloading %s", pkg)
		manifest, err := d.client.RetrieveOne(namespace+"/"+pkg.Name, pkg.Release)
		if err != nil {
			return err
		}

		decoder, err := appregistry.NewManifestDecoder(log)
		if err != nil {
			return err
		}
		if _, err = decoder.Decode([]*apprclient.OperatorMetadata{manifest}, directory); err != nil {
			return err
		}
	}

	return nil
}
