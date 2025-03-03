package status

import (
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	updatev1alpha1 "github.com/openshift/api/update/v1alpha1"
)

type mockData struct {
	updateStatusPath string
	updateStatus     *updatev1alpha1.UpdateStatus
}

func asResourceList[T any](objects *corev1.List, decoder runtime.Decoder) ([]T, error) {
	var outputItems []T
	for i, item := range objects.Items {
		obj, err := runtime.Decode(decoder, item.Raw)
		if err != nil {
			return nil, err
		}
		typedObj, ok := any(obj).(*T)
		if !ok {
			return nil, fmt.Errorf("unexpected object type %T in List content at index %d", obj, i)
		}
		outputItems = append(outputItems, *typedObj)
	}
	return outputItems, nil
}

func (o *mockData) load() error {
	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)
	if err := updatev1alpha1.Install(scheme); err != nil {
		return err
	}
	decoder := codecs.UniversalDecoder(updatev1alpha1.GroupVersion)

	updateStatusBytes, err := os.ReadFile(o.updateStatusPath)
	if err != nil {
		return err
	}
	usObj, err := runtime.Decode(decoder, updateStatusBytes)
	if err != nil {
		return err
	}
	switch usObj := usObj.(type) {
	case *updatev1alpha1.UpdateStatus:
		o.updateStatus = usObj
	case *updatev1alpha1.UpdateStatusList:
		o.updateStatus = &usObj.Items[0]
	case *corev1.List:
		cvs, err := asResourceList[updatev1alpha1.UpdateStatus](usObj, decoder)
		if err != nil {
			return fmt.Errorf("error while parsing file %s: %w", o.updateStatusPath, err)
		}
		o.updateStatus = &cvs[0]
	default:
		return fmt.Errorf("unexpected object type %T in --mock-updatestatus=%s content", usObj, o.updateStatusPath)
	}

	return nil
}
