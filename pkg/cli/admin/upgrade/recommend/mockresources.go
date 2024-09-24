package recommend

import (
	"fmt"
	"os"

	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

type mockData struct {
	cvPath         string
	clusterVersion *configv1.ClusterVersion
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
	if err := configv1.Install(scheme); err != nil {
		return err
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return err
	}
	decoder := codecs.UniversalDecoder(configv1.GroupVersion, corev1.SchemeGroupVersion)

	cvBytes, err := os.ReadFile(o.cvPath)
	if err != nil {
		return err
	}
	cvObj, err := runtime.Decode(decoder, cvBytes)
	if err != nil {
		return err
	}
	switch cvObj := cvObj.(type) {
	case *configv1.ClusterVersion:
		o.clusterVersion = cvObj
	case *configv1.ClusterVersionList:
		o.clusterVersion = &cvObj.Items[0]
	case *corev1.List:
		cvs, err := asResourceList[configv1.ClusterVersion](cvObj, decoder)
		if err != nil {
			return fmt.Errorf("error while parsing file %s: %w", o.cvPath, err)
		}
		o.clusterVersion = &cvs[0]
	default:
		return fmt.Errorf("unexpected object type %T in --mock-clusterversion=%s content", cvObj, o.cvPath)
	}

	return nil
}
