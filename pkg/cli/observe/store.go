package observe

import (
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/tools/cache"
)

type restListWatcher struct {
	*resource.Helper
	namespace string
	selector  string
}

func (lw restListWatcher) List(opt metav1.ListOptions) (runtime.Object, error) {
	opt.LabelSelector = lw.selector
	return lw.Helper.List(lw.namespace, "", &opt)
}

func (lw restListWatcher) Watch(opt metav1.ListOptions) (watch.Interface, error) {
	opt.LabelSelector = lw.selector
	return lw.Helper.Watch(lw.namespace, opt.ResourceVersion, &opt)
}

type knownObjects interface {
	cache.KeyListerGetter

	ListKeysError() error
	Put(key string, value interface{})
	Remove(key string)
}

type objectArguments struct {
	key       string
	arguments []string
	output    []byte
}

func objectArgumentsKeyFunc(obj interface{}) (string, error) {
	if args, ok := obj.(objectArguments); ok {
		return args.key, nil
	}
	return cache.MetaNamespaceKeyFunc(obj)
}

type objectArgumentsStore struct {
	keyFn func() ([]string, error)

	lock      sync.Mutex
	arguments map[string]interface{}
	err       error
}

func (r *objectArgumentsStore) ListKeysError() error {
	r.lock.Lock()
	defer r.lock.Unlock()
	return r.err
}

func (r *objectArgumentsStore) ListKeys() []string {
	r.lock.Lock()
	defer r.lock.Unlock()

	if r.keyFn != nil {
		var keys []string
		keys, r.err = r.keyFn()
		return keys
	}

	keys := make([]string, 0, len(r.arguments))
	for k := range r.arguments {
		keys = append(keys, k)
	}
	return keys
}

func (r *objectArgumentsStore) GetByKey(key string) (interface{}, bool, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	args := r.arguments[key]
	return args, true, nil
}

func (r *objectArgumentsStore) Put(key string, arguments interface{}) {
	r.lock.Lock()
	defer r.lock.Unlock()
	if r.arguments == nil {
		r.arguments = make(map[string]interface{})
	}
	r.arguments[key] = arguments
}

func (r *objectArgumentsStore) Remove(key string) {
	r.lock.Lock()
	defer r.lock.Unlock()
	delete(r.arguments, key)
}
