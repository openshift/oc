package inspect

import (
	"fmt"
	"path"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericclioptions/resource"

	configv1 "github.com/openshift/api/config/v1"
)

// resourceContext is used to keep track of previously seen objects
type resourceContext struct {
	visited sets.String
}

func NewResourceContext() *resourceContext {
	return &resourceContext{
		visited: sets.NewString(),
	}
}

func objectReferenceToResourceInfo(clientGetter genericclioptions.RESTClientGetter, ref *configv1.ObjectReference) (*resource.Info, error) {
	b := resource.NewBuilder(clientGetter).
		Unstructured().
		ResourceTypeOrNameArgs(false, fmt.Sprintf("%s/%s", ref.Resource, ref.Name)).
		NamespaceParam(ref.Namespace).
		Flatten().
		Latest()

	infos, err := b.Do().Infos()
	if err != nil {
		return nil, err
	}

	return infos[0], nil
}

func groupResourceToInfos(clientGetter genericclioptions.RESTClientGetter, ref schema.GroupResource, namespace string) ([]*resource.Info, error) {
	resourceString := ref.Resource
	if len(ref.Group) > 0 {
		resourceString = resourceString + "." + ref.Group
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
