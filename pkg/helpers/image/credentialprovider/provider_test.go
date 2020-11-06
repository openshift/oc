package credentialprovider

import (
	"testing"
	"time"
)

func TestCachingProvider(t *testing.T) {
	provider := &testProvider{
		Count: 0,
	}

	cache := &CachingDockerConfigProvider{
		Provider: provider,
		Lifetime: 1 * time.Second,
	}

	image := "image"

	if provider.Count != 0 {
		t.Errorf("Unexpected number of Provide calls: %v", provider.Count)
	}
	cache.Provide(image)
	cache.Provide(image)
	cache.Provide(image)
	cache.Provide(image)
	if provider.Count != 1 {
		t.Errorf("Unexpected number of Provide calls: %v", provider.Count)
	}

	time.Sleep(cache.Lifetime)
	cache.Provide(image)
	cache.Provide(image)
	cache.Provide(image)
	cache.Provide(image)
	if provider.Count != 2 {
		t.Errorf("Unexpected number of Provide calls: %v", provider.Count)
	}

	time.Sleep(cache.Lifetime)
	cache.Provide(image)
	cache.Provide(image)
	cache.Provide(image)
	cache.Provide(image)
	if provider.Count != 3 {
		t.Errorf("Unexpected number of Provide calls: %v", provider.Count)
	}
}
