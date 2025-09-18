package inspect

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/resource"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

type customResourceDefinitionList struct {
	*apiextensionsv1.CustomResourceDefinitionList
}

func (c *customResourceDefinitionList) addItem(obj interface{}) error {
	structuredItem, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
	if !ok {
		return fmt.Errorf("unhandledStructuredItemType: %T", obj)
	}
	c.Items = append(c.Items, *structuredItem)
	return nil
}

func gatherCustomResourceDefinition(ctx context.Context, resourceCtx *resourceContext, info *resource.Info, o *InspectOptions) error {
	structuredObj, err := toStructuredObject[apiextensionsv1.CustomResourceDefinition, apiextensionsv1.CustomResourceDefinitionList](info.Object)
	if err != nil {
		return gatherGenericObject(ctx, resourceCtx, info, o)
	}

	errs := []error{}
	switch castObj := structuredObj.(type) {
	case *apiextensionsv1.CustomResourceDefinition:
		if err := gatherCustomResourceDefinitionRelated(ctx, resourceCtx, o, castObj); err != nil {
			errs = append(errs, err)
		}

	case *apiextensionsv1.CustomResourceDefinitionList:
		for _, webhook := range castObj.Items {
			if err := gatherCustomResourceDefinitionRelated(ctx, resourceCtx, o, &webhook); err != nil {
				errs = append(errs, err)
			}
		}

	}

	if err := gatherGenericObject(ctx, resourceCtx, info, o); err != nil {
		errs = append(errs, err)
	}
	return errors.NewAggregate(errs)
}

func gatherCustomResourceDefinitionRelated(ctx context.Context, resourceCtx *resourceContext, o *InspectOptions, crd *apiextensionsv1.CustomResourceDefinition) error {
	if crd.Spec.Conversion == nil {
		return nil
	}
	if crd.Spec.Conversion.Webhook == nil {
		return nil
	}
	if crd.Spec.Conversion.Webhook.ClientConfig == nil {
		return nil
	}
	if crd.Spec.Conversion.Webhook.ClientConfig.Service == nil {
		return nil
	}

	return gatherNamespaces(ctx, resourceCtx, o, crd.Spec.Conversion.Webhook.ClientConfig.Service.Namespace)
}
