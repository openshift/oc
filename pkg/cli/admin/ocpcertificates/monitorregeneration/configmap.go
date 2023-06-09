package monitorregeneration

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func (o *MonitorCertificatesRuntime) createConfigMap(obj interface{}, isFirstSync bool) {
	configMap, ok := obj.(*corev1.ConfigMap)
	if !ok {
		fmt.Fprintf(o.IOStreams.ErrOut, "unexpected create obj %T", obj)
		return
	}

	if oldObj, _ := o.interestingConfigMaps.get(configMap.Namespace, configMap.Name); oldObj != nil {
		o.updateConfigMap(obj, oldObj)
		return
	}

	// not all replaces are the same.  we only really want to skip this on the first attempt
	if !isFirstSync {
		o.handleRevisionCreate(configMap)
	}

	o.interestingConfigMaps.upsert(configMap.Namespace, configMap.Name, configMap)
}

func (o *MonitorCertificatesRuntime) updateConfigMap(obj, oldObj interface{}) {
	configMap, ok := obj.(*corev1.ConfigMap)
	if !ok {
		fmt.Fprintf(o.IOStreams.ErrOut, "unexpected update obj %T", obj)
		return
	}
	defer o.interestingConfigMaps.upsert(configMap.Namespace, configMap.Name, configMap)

	_, ok = oldObj.(*corev1.ConfigMap)
	if !ok {
		fmt.Fprintf(o.IOStreams.ErrOut, "unexpected update oldObj %T", oldObj)
		return
	}

}

var nsToOperator = map[string]string{
	"openshift-etcd":                    "etcd",
	"openshift-kube-apiserver":          "kube-apiserver",
	"openshift-kube-controller-manager": "kube-controller-manager",
	"openshift-kube-scheduler":          "kube-scheduler",
}

func (o *MonitorCertificatesRuntime) handleRevisionCreate(configMap *corev1.ConfigMap) {
	if !strings.HasPrefix(configMap.Name, "revision-status-") {
		return
	}
	revisionNumber := configMap.Data["revision"]
	reason := configMap.Data["reason"]

	operatorName := "Unknown"
	if name, ok := nsToOperator[configMap.Namespace]; ok {
		operatorName = name
	}

	fmt.Fprintf(o.IOStreams.Out, "clusteroperators/%v - Revision %v created because %q\n", operatorName, revisionNumber, reason)
}

func (o *MonitorCertificatesRuntime) handleRevisionDelete(configMap *corev1.ConfigMap) {
	if !strings.HasPrefix(configMap.Name, "revision-status-") {
		return
	}
	revisionNumber := configMap.Data["revision"]

	operatorName := "Unknown"
	if name, ok := nsToOperator[configMap.Namespace]; ok {
		operatorName = name
	}

	fmt.Fprintf(o.IOStreams.Out, "clusteroperators/%v - Revision %v pruned\n", operatorName, revisionNumber)
}

func (o *MonitorCertificatesRuntime) deleteConfigMap(obj interface{}) {
	configMap, ok := obj.(*corev1.ConfigMap)
	if !ok {
		fmt.Fprintf(o.IOStreams.ErrOut, "unexpected create obj %T", obj)
		return
	}

	o.handleRevisionDelete(configMap)

	o.interestingConfigMaps.remove(configMap.Namespace, configMap.Name)
}
