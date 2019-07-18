package appregistry

import (
	"fmt"

	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/operator-framework/operator-registry/pkg/registry"
)

// NewLoader returns a new instance of AppregistryLoader.
//
// kubeconfig specifies the location of kube configuration file.
// dbName specifies the database name to be used for sqlite.
// downloadPath specifies the folder where the downloaded nested bundle(s) will
// be stored.
func NewLoader(kubeconfig string, dbName string, downloadPath string, logger *logrus.Entry) (*AppregistryLoader, error) {
	kubeClient, err := NewKubeClient(kubeconfig, logger)
	if err != nil {
		return nil, err
	}

	loader, err := NewDbLoader(dbName, logger)
	if err != nil {
		return nil, err
	}

	specifier := &registrySpecifier{}
	decoder, err := NewManifestDecoder(logger)
	if err != nil {
		return nil, err
	}

	return &AppregistryLoader{
		logger: logger,
		input: &inputParser{
			sourceSpecifier: specifier,
		},
		downloader: &downloader{
			logger:     logger,
			kubeClient: *kubeClient,
		},
		downloadPath: downloadPath,
		decoder: decoder,
		loader:  loader,
	}, nil
}

type AppregistryLoader struct {
	logger     *logrus.Entry
	input      *inputParser
	downloader *downloader
	downloadPath string
	decoder    *manifestDecoder
	loader     *dbLoader
}

func (a *AppregistryLoader) Load(csvSources []string, csvPackages string) (store registry.Query, err error) {
	a.logger.Infof("operator source(s) specified are - %s", csvSources)
	a.logger.Infof("package(s) specified are - %s", csvPackages)

	input, err := a.input.Parse(csvSources, csvPackages)
	if err != nil {
		a.logger.Errorf("the following error(s) occurred while parsing input - %v", err)

		if input == nil || !input.IsGoodToProceed() {
			a.logger.Info("can't proceed, bailing out")
			return
		}
	}

	a.logger.Info("input has been sanitized")
	a.logger.Infof("sources: %s", input.Sources)
	a.logger.Infof("packages: %s", input.Packages)

	rawManifests, err := a.downloader.Download(input)
	if err != nil {
		a.logger.Errorf("The following error occurred while downloading - %v", err)

		if len(rawManifests) == 0 {
			a.logger.Info("No package manifest downloaded")
			return
		}
	}

	a.logger.Infof("download complete - %d repositories have been downloaded", len(rawManifests))

	// The set of operator manifest(s) downloaded is a collection of both
	// flattened single file yaml and nested operator bundle(s).
	result, err := a.decoder.Decode(rawManifests, a.downloadPath)
	if err != nil {
		a.logger.Errorf("The following error occurred while decoding manifest - %v", err)

		if result.IsEmpty() {
			a.logger.Info("No operator manifest decoded")
			return
		}
	}

	a.logger.Infof("decoded %d flattened and %d nested operator manifest(s)", result.FlattenedCount, result.NestedCount)

	if err = a.loader.LoadBundleDirectoryToSQLite(a.downloadPath); err != nil {
		return
	}

	store, err = a.loader.GetStore()
	return
}

func NewKubeClient(kubeconfig string, logger *logrus.Entry) (clientset *kubernetes.Clientset, err error) {
	var config *rest.Config

	if kubeconfig != "" {
		logger.Infof("Loading kube client config from path %q", kubeconfig)
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		logger.Infof("Using in-cluster kube client config")
		config, err = rest.InClusterConfig()
	}

	if err != nil {
		err = fmt.Errorf("Cannot load config for REST client: %v", err)
		return
	}

	clientset, err = kubernetes.NewForConfig(config)
	return
}
