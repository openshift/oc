package inspect

import (
	"context"
	"fmt"
	"os"
	"path"
	"regexp"

	"k8s.io/cli-runtime/pkg/resource"

	configv1 "github.com/openshift/api/config/v1"
)

var proxyRegex = regexp.MustCompile(`(https?://)([^:]+):([^@]+)@`)

var _ listAccessor = &proxyList{}

type proxyList struct {
	*configv1.ProxyList
}

func (c *proxyList) addItem(obj interface{}) error {
	structuredItem, ok := obj.(*configv1.Proxy)
	if !ok {
		return fmt.Errorf("unhandledStructuredItemType: %T", obj)
	}
	c.Items = append(c.Items, *structuredItem)
	return nil
}

func inspectProxyInfo(ctx context.Context, info *resource.Info, o *InspectOptions) error {
	structuredObj, err := toStructuredObject[configv1.Proxy, configv1.ProxyList](info.Object)
	if err != nil {
		return err
	}

	switch castObj := structuredObj.(type) {
	case *configv1.Proxy:
		elideProxy(castObj)

	case *configv1.ProxyList:
		for i := range castObj.Items {
			elideProxy(&castObj.Items[i])
		}
	}

	// save the current object to disk
	dirPath := dirPathForInfo(o.DestDir, info)
	filename := filenameForInfo(info)
	// ensure destination path exists
	if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
		return err
	}
	return o.fileWriter.WriteFromResource(ctx, path.Join(dirPath, filename), structuredObj)
}

// elideProxy obfuscates the sensitive information from proxy object.
func elideProxy(proxy *configv1.Proxy) {
	proxy.Spec.HTTPProxy = proxyRegex.ReplaceAllString(proxy.Spec.HTTPProxy, `${1}${2}:****@`)
	proxy.Spec.HTTPSProxy = proxyRegex.ReplaceAllString(proxy.Spec.HTTPSProxy, `${1}${2}:****@`)
	proxy.Status.HTTPProxy = proxyRegex.ReplaceAllString(proxy.Status.HTTPProxy, `${1}${2}:****@`)
	proxy.Status.HTTPSProxy = proxyRegex.ReplaceAllString(proxy.Status.HTTPSProxy, `${1}${2}:****@`)
}
