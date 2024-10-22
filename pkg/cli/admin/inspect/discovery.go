package inspect

import (
	"context"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/discovery"
	"path/filepath"
	"sigs.k8s.io/yaml"
)

func inspectDiscovery(ctx context.Context, destDir string, discoveryClient discovery.CachedDiscoveryInterface) error {
	if err := writeDiscovery(ctx, filepath.Join(destDir, "aggregated-discovery-apis.yaml"), discoveryClient, "/apis"); err != nil {
		return err
	}
	if err := writeDiscovery(ctx, filepath.Join(destDir, "aggregated-discovery-api.yaml"), discoveryClient, "/api"); err != nil {
		return err
	}

	return nil
}

var v2GVK = schema.GroupVersionKind{Group: "apidiscovery.k8s.io", Version: "v2", Kind: "APIGroupDiscoveryList"}

func writeDiscovery(ctx context.Context, destFile string, discoveryClient discovery.CachedDiscoveryInterface, url string) error {
	var responseContentType string
	var statusCode int
	apiBytes, err := discoveryClient.RESTClient().Get().
		AbsPath(url).
		SetHeader("Accept", discovery.AcceptV2).
		Do(ctx).
		ContentType(&responseContentType).
		StatusCode(&statusCode).
		Raw()
	if err != nil {
		return fmt.Errorf("discovery %q failed: %w", url, err)
	}

	if statusCode < 200 && statusCode > 299 {
		return fmt.Errorf("discovery %q returned with status %v", url, statusCode)
	}
	if ok, _ := discovery.ContentTypeIsGVK(responseContentType, v2GVK); !ok {
		return fmt.Errorf("discovery %q returned with contentType %v", url, responseContentType)
	}

	apiMap := map[string]interface{}{}
	if err := json.Unmarshal(apiBytes, &apiMap); err != nil {
		return fmt.Errorf("discovery %q unmarshal failed: %w", url, err)
	}
	apiYAML, err := yaml.Marshal(apiMap)
	if err != nil {
		return fmt.Errorf("discovery %q marshal failed: %w", url, err)
	}
	if err := os.WriteFile(destFile, apiYAML, 0644); err != nil {
		return fmt.Errorf("discovery %q failed writing: %w", url, err)
	}

	return nil
}
