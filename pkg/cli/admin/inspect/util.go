package inspect

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sync"

	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
)

// resourceContext is used to keep track of previously seen objects
type resourceContext struct {
	lock sync.Mutex

	alreadyVisited sets.String
}

func NewResourceContext() *resourceContext {
	return &resourceContext{
		alreadyVisited: sets.NewString(),
	}
}

// visited returns whether or not an item already has already been visited and adds it to the list
func (r *resourceContext) visited(resource string) bool {
	r.lock.Lock()
	defer r.lock.Unlock()

	ret := r.alreadyVisited.Has(resource)
	r.alreadyVisited.Insert(resource)
	return ret
}

// visited returns whether or not an item already has already been visited and does NOT add it to the list
func (r *resourceContext) peekVisited(resource string) bool {
	r.lock.Lock()
	defer r.lock.Unlock()

	return r.alreadyVisited.Has(resource)
}

func objectReferenceToString(ref *configv1.ObjectReference) string {
	resource := ref.Resource
	group := ref.Group
	name := ref.Name
	if len(name) > 0 {
		name = "/" + name
	}
	if len(group) > 0 {
		group = "." + group
	}
	return resource + group + name
}

func unstructuredToString(obj *unstructured.Unstructured) string {
	resource := obj.GetKind()
	var group string
	if gv, err := schema.ParseGroupVersion(obj.GetAPIVersion()); err != nil {
		group = gv.Group
	}
	name := obj.GetName()
	if len(name) > 0 {
		name = "/" + name
	}
	if len(group) > 0 {
		group = "." + group
	}
	return resource + group + name

}

func objectReferenceToResourceInfos(clientGetter genericclioptions.RESTClientGetter, ref *configv1.ObjectReference) ([]*resource.Info, error) {
	b := resource.NewBuilder(clientGetter).
		Unstructured().
		ResourceTypeOrNameArgs(true, objectReferenceToString(ref)).
		NamespaceParam(ref.Namespace).DefaultNamespace().AllNamespaces(len(ref.Namespace) == 0).
		Flatten().
		Latest()

	infos, err := b.Do().Infos()
	if err != nil {
		return nil, err
	}

	return infos, nil
}

func groupResourceToInfos(clientGetter genericclioptions.RESTClientGetter, ref schema.GroupResource, namespace string) ([]*resource.Info, error) {
	resourceString := ref.Resource
	if len(ref.Group) > 0 {
		resourceString = fmt.Sprintf("%s.%s", resourceString, ref.Group)
	}
	b := resource.NewBuilder(clientGetter).
		Unstructured().
		ResourceTypeOrNameArgs(false, resourceString).
		SelectAllParam(true).
		NamespaceParam(namespace).
		Latest()

	return b.Do().Infos()
}

// infoToContextKey receives a resource.Info and returns a unique string for use in keeping track of objects previously seen
func infoToContextKey(info *resource.Info) string {
	name := info.Name
	if meta.IsListType(info.Object) {
		name = "*"
	}
	return fmt.Sprintf("%s/%s/%s/%s", info.Namespace, info.ResourceMapping().GroupVersionKind.Group, info.ResourceMapping().Resource.Resource, name)
}

// objectRefToContextKey is a variant of infoToContextKey that receives a configv1.ObjectReference and returns a unique string for use in keeping track of object references previously seen
func objectRefToContextKey(objRef *configv1.ObjectReference) string {
	return fmt.Sprintf("%s/%s/%s/%s", objRef.Namespace, objRef.Group, objRef.Resource, objRef.Name)
}

func resourceToContextKey(resource schema.GroupResource, namespace string) string {
	return fmt.Sprintf("%s/%s/%s/%s", namespace, resource.Group, resource.Resource, "*")
}

// dirPathForInfo receives a *resource.Info and returns a relative path
// corresponding to the directory location of that object on disk
func dirPathForInfo(baseDir string, info *resource.Info) string {
	groupName := "core"
	if len(info.Mapping.GroupVersionKind.Group) > 0 {
		groupName = info.Mapping.GroupVersionKind.Group
	}

	groupPath := path.Join(baseDir, namespaceResourcesDirname, info.Namespace, groupName)
	if len(info.Namespace) == 0 {
		groupPath = path.Join(baseDir, clusterScopedResourcesDirname, "/"+groupName)
	}
	if meta.IsListType(info.Object) {
		return groupPath
	}

	objPath := path.Join(groupPath, info.ResourceMapping().Resource.Resource)
	if len(info.Namespace) == 0 {
		objPath = path.Join(groupPath, info.ResourceMapping().Resource.Resource)
	}
	return objPath
}

// filenameForInfo receives a *resource.Info and returns the basename
func filenameForInfo(info *resource.Info) string {
	if meta.IsListType(info.Object) {
		return info.ResourceMapping().Resource.Resource + ".yaml"
	}

	return info.Name + ".yaml"
}

// getAllEventsRecursive returns a union (not deconflicted) or all events under a directory
func getAllEventsRecursive(rootDir string) (*corev1.EventList, error) {
	// now gather all the events into a single file and produce a unified file
	eventLists := &corev1.EventList{}
	err := filepath.Walk(rootDir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.Name() != "events.yaml" {
				return nil
			}
			eventBytes, err := ioutil.ReadFile(path)
			if err != nil {
				return err
			}
			events, err := readEvents(eventBytes)
			if err != nil {
				return err
			}
			eventLists.Items = append(eventLists.Items, events.Items...)
			return nil
		})
	if err != nil {
		return nil, err
	}

	return eventLists, nil
}

// CreateEventFilterPage reads all events in rootDir recursively, produces a single file, and produces a webpage
// that can be viewed locally to filter the events.
func CreateEventFilterPage(rootDir string) error {
	events, err := getAllEventsRecursive(rootDir)
	if err != nil {
		return err
	}
	jsonSerializer := json.NewSerializerWithOptions(json.DefaultMetaFactory, coreScheme, coreScheme, json.SerializerOptions{Pretty: true})
	unifiedEventBytes, err := runtime.Encode(jsonSerializer, events)
	if err != nil {
		return err
	}

	alleventsFile, err := os.OpenFile(filepath.Join(rootDir, "all-events.json.js"), os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer alleventsFile.Close()

	_, err = alleventsFile.WriteString("var allEvents = ")
	if err != nil {
		return err
	}

	_, err = alleventsFile.Write(bytes.ReplaceAll(unifiedEventBytes, []byte("\\\\n"), []byte("\\n+")))
	if err != nil {
		return err
	}

	// produce jqgrid based event filter file
	err = ioutil.WriteFile(filepath.Join(rootDir, "event-filter.html"), eventFilterHtml, 0644)
	if err != nil {
		return err
	}

	return nil
}

var (
	coreScheme = runtime.NewScheme()
	coreCodecs = serializer.NewCodecFactory(coreScheme)
)

func init() {
	if err := corev1.AddToScheme(coreScheme); err != nil {
		panic(err)
	}
}

func readEvents(objBytes []byte) (*corev1.EventList, error) {
	requiredObj, err := runtime.Decode(coreCodecs.UniversalDecoder(corev1.SchemeGroupVersion), objBytes)
	if err != nil {
		return nil, err
	}
	return requiredObj.(*corev1.EventList), nil
}
