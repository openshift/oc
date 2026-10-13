package inspect

import (
	"context"
	"fmt"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/resource"
)

type mutatingWebhookConfigList struct {
	*admissionregistrationv1.MutatingWebhookConfigurationList
}

func (c *mutatingWebhookConfigList) addItem(obj interface{}) error {
	structuredItem, ok := obj.(*admissionregistrationv1.MutatingWebhookConfiguration)
	if !ok {
		return fmt.Errorf("unhandledStructuredItemType: %T", obj)
	}
	c.Items = append(c.Items, *structuredItem)
	return nil
}

func gatherMutatingAdmissionWebhook(ctx context.Context, resourceCtx *resourceContext, info *resource.Info, o *InspectOptions) error {
	structuredObj, err := toStructuredObject[admissionregistrationv1.MutatingWebhookConfiguration, admissionregistrationv1.MutatingWebhookConfigurationList](info.Object)
	if err != nil {
		return gatherGenericObject(ctx, resourceCtx, info, o)
	}

	errs := []error{}
	switch castObj := structuredObj.(type) {
	case *admissionregistrationv1.MutatingWebhookConfiguration:
		if err := gatherMutatingAdmissionWebhookRelated(ctx, resourceCtx, o, castObj); err != nil {
			errs = append(errs, err)
		}

	case *admissionregistrationv1.MutatingWebhookConfigurationList:
		for _, webhook := range castObj.Items {
			if err := gatherMutatingAdmissionWebhookRelated(ctx, resourceCtx, o, &webhook); err != nil {
				errs = append(errs, err)
			}
		}

	}

	if err := gatherGenericObject(ctx, resourceCtx, info, o); err != nil {
		errs = append(errs, err)
	}
	return errors.NewAggregate(errs)
}

func gatherMutatingAdmissionWebhookRelated(ctx context.Context, resourceCtx *resourceContext, o *InspectOptions, webhookConfig *admissionregistrationv1.MutatingWebhookConfiguration) error {
	errs := []error{}
	for _, webhook := range webhookConfig.Webhooks {
		if webhook.ClientConfig.Service == nil {
			continue
		}
		if err := gatherNamespaces(ctx, resourceCtx, o, webhook.ClientConfig.Service.Namespace); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.NewAggregate(errs)
}

type validatingWebhookConfigList struct {
	*admissionregistrationv1.ValidatingWebhookConfigurationList
}

func (c *validatingWebhookConfigList) addItem(obj interface{}) error {
	structuredItem, ok := obj.(*admissionregistrationv1.ValidatingWebhookConfiguration)
	if !ok {
		return fmt.Errorf("unhandledStructuredItemType: %T", obj)
	}
	c.Items = append(c.Items, *structuredItem)
	return nil
}

func gatherValidatingAdmissionWebhook(ctx context.Context, resourceCtx *resourceContext, info *resource.Info, o *InspectOptions) error {
	structuredObj, err := toStructuredObject[admissionregistrationv1.ValidatingWebhookConfiguration, admissionregistrationv1.ValidatingWebhookConfigurationList](info.Object)
	if err != nil {
		return gatherGenericObject(ctx, resourceCtx, info, o)
	}

	errs := []error{}
	switch castObj := structuredObj.(type) {
	case *admissionregistrationv1.ValidatingWebhookConfiguration:
		if err := gatherValidatingAdmissionWebhookRelated(ctx, resourceCtx, o, castObj); err != nil {
			errs = append(errs, err)
		}

	case *admissionregistrationv1.ValidatingWebhookConfigurationList:
		for _, webhook := range castObj.Items {
			if err := gatherValidatingAdmissionWebhookRelated(ctx, resourceCtx, o, &webhook); err != nil {
				errs = append(errs, err)
			}
		}

	}

	if err := gatherGenericObject(ctx, resourceCtx, info, o); err != nil {
		errs = append(errs, err)
	}
	return errors.NewAggregate(errs)
}

func gatherValidatingAdmissionWebhookRelated(ctx context.Context, resourceCtx *resourceContext, o *InspectOptions, webhookConfig *admissionregistrationv1.ValidatingWebhookConfiguration) error {
	errs := []error{}
	for _, webhook := range webhookConfig.Webhooks {
		if webhook.ClientConfig.Service == nil {
			continue
		}
		if err := gatherNamespaces(ctx, resourceCtx, o, webhook.ClientConfig.Service.Namespace); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.NewAggregate(errs)
}
