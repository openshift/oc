package monitorregeneration

import (
	"context"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	configv1 "github.com/openshift/api/config/v1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
)

type MonitorCertificatesRuntime struct {
	KubeClient   kubernetes.Interface
	ConfigClient configclient.Interface

	genericiooptions.IOStreams

	interestingConfigMaps       *namespacedCache
	interestingSecrets          *namespacedCache
	interestingClusterOperators *unnamespacedCache
}

type namespacedCache struct {
	cache map[string]*unnamespacedCache
	lock  sync.Mutex
}

func newNamespacedCache() *namespacedCache {
	return &namespacedCache{
		cache: map[string]*unnamespacedCache{},
	}
}

func (c *namespacedCache) get(namespace, name string) (runtime.Object, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()
	next, ok := c.cache[namespace]
	if !ok {
		return nil, false
	}
	return next.get(name)
}

func (c *namespacedCache) upsert(namespace, name string, obj runtime.Object) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if _, ok := c.cache[namespace]; !ok {
		c.cache[namespace] = newUnnamespacedCache()
	}
	c.cache[namespace].upsert(name, obj)
}

func (c *namespacedCache) remove(namespace, name string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.cache[namespace].remove(name)
}

type unnamespacedCache struct {
	cache map[string]runtime.Object
	lock  sync.Mutex
}

func newUnnamespacedCache() *unnamespacedCache {
	return &unnamespacedCache{
		cache: map[string]runtime.Object{},
	}
}

func (c *unnamespacedCache) get(name string) (runtime.Object, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()
	ret, ok := c.cache[name]
	return ret, ok
}

func (c *unnamespacedCache) upsert(name string, obj runtime.Object) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.cache[name] = obj
}

func (c *unnamespacedCache) remove(name string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.cache, name)
}

func (o *MonitorCertificatesRuntime) Run(ctx context.Context) error {
	interestingNamespaces := sets.NewString("kube-node-lease", "kube-public", "kube-system", "default", "openshift")
	namespaces, err := o.KubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, namespace := range namespaces.Items {
		if strings.HasPrefix(namespace.Name, "openshift-") {
			interestingNamespaces.Insert(namespace.Name)
		}
	}

	// we want to see every change, so we will wire up directly to a reflector (NOT to an informer) with a
	// synthentic store that will allow us to watch.
	// we create a separate list/watch for every namespace so that in large clusters we don't produce a surge of
	// "list secrets in all namespaces".
	// We expect connection drops as the kube-apiserver restarts, but we expect rapid reconnection.

	for _, namespace := range interestingNamespaces.List() {
		listWatch := cache.NewListWatchFromClient(o.KubeClient.CoreV1().RESTClient(), "secrets", namespace, fields.Everything())
		customStore := newMonitoringStore(
			[]objCreateFunc{o.createSecret},
			[]objUpdateFunc{o.updateSecret},
			[]objDeleteFunc{o.deleteSecret},
		)
		reflector := cache.NewReflector(listWatch, &corev1.Secret{}, customStore, 0)
		go reflector.Run(ctx.Done())
	}

	for _, namespace := range interestingNamespaces.List() {
		listWatch := cache.NewListWatchFromClient(o.KubeClient.CoreV1().RESTClient(), "configmaps", namespace, fields.Everything())
		customStore := newMonitoringStore(
			[]objCreateFunc{o.createConfigMap},
			[]objUpdateFunc{o.updateConfigMap},
			[]objDeleteFunc{o.deleteConfigMap},
		)
		reflector := cache.NewReflector(listWatch, &corev1.ConfigMap{}, customStore, 0)
		go reflector.Run(ctx.Done())
	}

	listWatch := cache.NewListWatchFromClient(o.ConfigClient.ConfigV1().RESTClient(), "clusteroperators", "", fields.Everything())
	customStore := newMonitoringStore(
		[]objCreateFunc{o.createClusterOperator},
		[]objUpdateFunc{o.updateClusterOperator},
		[]objDeleteFunc{o.deleteClusterOperator},
	)
	reflector := cache.NewReflector(listWatch, &configv1.ClusterOperator{}, customStore, 0)
	go reflector.Run(ctx.Done())

	<-ctx.Done()
	return nil
}
