package certregen

import (
	"context"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
)

var (
	secretKind = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
)

type objectPrinter struct {
	printer printers.ResourcePrinter
	out     io.Writer
}

func (p *objectPrinter) printObject(obj runtime.Object) error {
	return p.printer.PrintObj(obj, p.out)
}

type CertificateSecretRegenerateFunc func(objPrinter *objectPrinter, kubeClient kubernetes.Interface, secret *corev1.Secret, dryRun bool) error

type RegenerateCertsRuntime struct {
	ResourceFinder genericclioptions.ResourceFinder
	KubeClient     kubernetes.Interface

	ValidBefore *time.Time
	DryRun      bool

	regenerateSecretFn CertificateSecretRegenerateFunc

	Printer printers.ResourcePrinter
	genericiooptions.IOStreams
}

func (o *RegenerateCertsRuntime) Run(ctx context.Context) error {
	visitor := o.ResourceFinder.Do()

	// TODO need to wire context through the visitorFns
	err := visitor.Visit(o.forceRegenerationOnResourceInfo)
	if err != nil {
		return err
	}
	return nil
}

// ought to find some way to test this.
func (o *RegenerateCertsRuntime) forceRegenerationOnResourceInfo(info *resource.Info, err error) error {
	if err != nil {
		return err
	}

	if secretKind != info.Object.GetObjectKind().GroupVersionKind() {
		return fmt.Errorf("command must only be pointed at secrets")
	}

	uncastObj, ok := info.Object.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("not unstructured: %w", err)
	}
	secret := &corev1.Secret{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uncastObj.Object, secret); err != nil {
		return fmt.Errorf("not a secret: %w", err)
	}

	return o.regenerateSecretFn(&objectPrinter{printer: o.Printer, out: o.Out}, o.KubeClient, secret, o.DryRun)
}
