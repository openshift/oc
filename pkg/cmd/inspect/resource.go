package inspect

import (
	"fmt"
	"log"
	"os"
	"path"

	"k8s.io/apimachinery/pkg/runtime/schema"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions/resource"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/must-gather/pkg/util"
)

const (
	clusterScopedResourcesDirname = "cluster-scoped-resources"
	namespaceResourcesDirname     = "namespaces"

	configResourceDataKey   = "/cluster-scoped-resources/config.openshift.io"
	operatorResourceDataKey = "/cluster-scoped-resources/operator.openshift.io"
)

// InspectResource receives an object to gather debugging data for, and a context to keep track of
// already-seen objects when following related-object reference chains.
func InspectResource(info *resource.Info, context *resourceContext, o *InspectOptions) error {
	if context.visited.Has(infoToContextKey(info)) {
		log.Printf("Skipping previously-inspected resource: %q ...", infoToContextKey(info))
		return nil
	}
	context.visited.Insert(infoToContextKey(info))

	switch info.ResourceMapping().Resource.GroupResource() {
	case configv1.GroupVersion.WithResource("clusteroperators").GroupResource():
		unstr, ok := info.Object.(*unstructured.Unstructured)
		if !ok {
			return fmt.Errorf("unexpected type. Expecting %q but got %T", "*unstructured.Unstructured", info.Object)
		}

		// first, gather config.openshift.io resource data
		errs := []error{}
		if err := o.gatherConfigResourceData(path.Join(o.baseDir, "/cluster-scoped-resources/config.openshift.io"), context); err != nil {
			errs = append(errs, err)
		}

		// then, gather operator.openshift.io resource data
		if err := o.gatherOperatorResourceData(path.Join(o.baseDir, "/cluster-scoped-resources/operator.openshift.io"), context); err != nil {
			errs = append(errs, err)
		}

		// save clusteroperator resources to disk
		if err := gatherClusterOperatorResource(o.baseDir, unstr, o.fileWriter); err != nil {
			return err
		}

		// obtain associated objects for the current clusteroperator resources
		relatedObjReferences, err := obtainClusterOperatorRelatedObjects(unstr)
		if err != nil {
			return err
		}

		for _, relatedRef := range relatedObjReferences {
			if context.visited.Has(objectRefToContextKey(relatedRef)) {
				continue
			}

			relatedInfo, err := objectReferenceToResourceInfo(o.configFlags, relatedRef)
			if err != nil {
				errs = append(errs, err)
				continue
			}

			if err := InspectResource(relatedInfo, context, o); err != nil {
				errs = append(errs, err)
				continue
			}
		}

		return errors.NewAggregate(errs)

	case corev1.SchemeGroupVersion.WithResource("namespaces").GroupResource():
		errs := []error{}
		if err := o.gatherNamespaceData(o.baseDir, info.Name); err != nil {
			errs = append(errs, err)
		}
		resourcesToCollect := namespaceResourcesToCollect()
		for _, resource := range resourcesToCollect {
			if context.visited.Has(resourceToContextKey(resource, info.Name)) {
				continue
			}
			resourceInfos, err := groupResourceToInfos(o.configFlags, resource, info.Name)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			for _, resourceInfo := range resourceInfos {
				if err := InspectResource(resourceInfo, context, o); err != nil {
					errs = append(errs, err)
					continue
				}
			}
		}

		return errors.NewAggregate(errs)

	case corev1.SchemeGroupVersion.WithResource("secrets").GroupResource():
		if err := inspectSecretInfo(info, o); err != nil {
			return err
		}
		return nil

	case schema.GroupResource{Group: "route.openshift.io", Resource: "routes"}:
		if err := inspectRouteInfo(info, o); err != nil {
			return err
		}
		return nil
	default:
		// save the current object to disk
		dirPath := dirPathForInfo(o.baseDir, info)
		filename := filenameForInfo(info)
		// ensure destination path exists
		if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
			return err
		}

		return o.fileWriter.WriteFromResource(path.Join(dirPath, filename), info.Object)
	}
}

func gatherClusterOperatorResource(baseDir string, obj *unstructured.Unstructured, fileWriter *util.MultiSourceFileWriter) error {
	log.Printf("Gathering cluster operator resource data...\n")

	// ensure destination path exists
	destDir := path.Join(baseDir, "/"+clusterScopedResourcesDirname, "/"+obj.GroupVersionKind().Group, "/clusteroperators")
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.yaml", obj.GetName())
	return fileWriter.WriteFromResource(path.Join(destDir, "/"+filename), obj)
}

func obtainClusterOperatorRelatedObjects(obj *unstructured.Unstructured) ([]*configv1.ObjectReference, error) {
	// obtain related namespace info for the current clusteroperator
	log.Printf("    Gathering related object reference information for ClusterOperator %q...\n", obj.GetName())

	structuredCO := &configv1.ClusterOperator{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, structuredCO); err != nil {
		return nil, err
	}

	relatedObjs := []*configv1.ObjectReference{}
	for idx, relatedObj := range structuredCO.Status.RelatedObjects {
		relatedObjs = append(relatedObjs, &structuredCO.Status.RelatedObjects[idx])
		log.Printf("    Found related object %q for ClusterOperator %q...\n", objectReferenceToString(&relatedObj), structuredCO.Name)
	}

	return relatedObjs, nil
}
