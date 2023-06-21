package certregen

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func newFakeClientSet(objects ...runtime.Object) *fakeKubeWrapper {
	scheme := runtime.NewScheme()

	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Version: "v1"})
	utilruntime.Must(fake.AddToScheme(scheme))

	codecs := serializer.NewCodecFactory(scheme)
	o := clienttesting.NewObjectTracker(scheme, codecs.UniversalDecoder())
	for _, obj := range objects {
		if err := o.Add(obj); err != nil {
			panic(err)
		}
	}

	cs := &fakeKubeWrapper{Clientset: &fake.Clientset{}, tracker: o}
	cs.discovery = &fakediscovery.FakeDiscovery{Fake: &cs.Fake}
	cs.AddReactor("*", "*", clienttesting.ObjectReaction(o))
	cs.AddWatchReactor("*", func(action clienttesting.Action) (handled bool, ret watch.Interface, err error) {
		gvr := action.GetResource()
		ns := action.GetNamespace()
		watch, err := o.Watch(gvr, ns)
		if err != nil {
			return false, nil, err
		}
		return true, watch, nil
	})

	return cs
}

type fakeKubeWrapper struct {
	*fake.Clientset
	tracker   clienttesting.ObjectTracker
	discovery *fakediscovery.FakeDiscovery
}

// the following two methods return our own objects as these are private in the
// wrapped object
func (w *fakeKubeWrapper) Discovery() discovery.DiscoveryInterface {
	return w.discovery
}

func (w *fakeKubeWrapper) Tracker() clienttesting.ObjectTracker {
	return w.tracker
}

func (w *fakeKubeWrapper) PrependReactor(verb, resource string, reaction clienttesting.ReactionFunc) {
	w.Clientset.PrependReactor(verb, resource, objectReactionWrapper(reaction))
}

func (w *fakeKubeWrapper) AddReactor(verb, resource string, reaction clienttesting.ReactionFunc) {
	w.Clientset.AddReactor(verb, resource, objectReactionWrapper(reaction))
}

var (
	_ kubernetes.Interface     = &fakeKubeWrapper{}
	_ clienttesting.FakeClient = &fakeKubeWrapper{}
)

func objectReactionWrapper(delegate clienttesting.ReactionFunc) clienttesting.ReactionFunc {
	return func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		switch a := action.(type) {
		case clienttesting.PatchActionImpl:
			if a.GetPatchType() == "application/apply-patch+yaml" { // this is what upstream did in later versions
				a.PatchType = types.StrategicMergePatchType
				action = a
			}
		}
		return delegate(action)
	}
}
