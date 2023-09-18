package rebootmachineconfigpool

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/dynamic"
)

const (
	MasterRebootingMachineConfigName = "95-oc-initiated-reboot-master"
	WorkerRebootingMachineConfigName = "95-oc-initiated-reboot-worker"
)

var (
	machineConfigPoolKind     = schema.GroupVersionKind{Group: "machineconfiguration.openshift.io", Version: "v1", Kind: "MachineConfigPool"}
	machineConfigPoolResource = schema.GroupVersionResource{Group: "machineconfiguration.openshift.io", Version: "v1", Resource: "machineconfigpools"}

	machineConfigKind     = schema.GroupVersionKind{Group: "machineconfiguration.openshift.io", Version: "v1", Kind: "MachineConfig"}
	MachineConfigResource = schema.GroupVersionResource{Group: "machineconfiguration.openshift.io", Version: "v1", Resource: "machineconfigs"}

	//go:embed restart-template.json
	restartTemplateJSON []byte
	restartTemplate     = readMachineConfigOrDie(restartTemplateJSON)
)

func readMachineConfigOrDie(objBytes []byte) *MachineConfig {
	unstructuredObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, objBytes)
	if err != nil {
		panic(err)
	}

	uncastObj, ok := unstructuredObj.(*unstructured.Unstructured)
	if !ok {
		panic(fmt.Sprintf("unexpected type %T", unstructuredObj))
	}
	machineConfig := &MachineConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uncastObj.Object, machineConfig); err != nil {
		panic(err)
	}

	return machineConfig
}

const (
	RebootMachineConfigPoolFieldManager = "reboot-machine-config-pool"
)

type RebootMachineConfigPoolRuntime struct {
	ResourceFinder genericclioptions.ResourceFinder
	DynamicClient  dynamic.Interface

	dryRun bool

	Printer printers.ResourcePrinter

	genericiooptions.IOStreams
}

func (r *RebootMachineConfigPoolRuntime) Run(ctx context.Context) error {
	visitor := r.ResourceFinder.Do()

	// TODO need to wire context through the visitorFns
	err := visitor.Visit(r.rebootFromResourceInfo)
	if err != nil {
		return err
	}
	return nil
}

func (r *RebootMachineConfigPoolRuntime) rebootFromResourceInfo(info *resource.Info, err error) error {
	if err != nil {
		return err
	}

	if machineConfigPoolKind != info.Object.GetObjectKind().GroupVersionKind() {
		return fmt.Errorf("command must only be pointed at machineconfigpools")
	}

	uncastObj, ok := info.Object.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("not unstructured: %w", err)
	}
	machineConfigPool := &MachineConfigPool{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uncastObj.Object, machineConfigPool); err != nil {
		return fmt.Errorf("not a secret: %w", err)
	}

	return r.rebootMachineConfigPool(context.TODO(), machineConfigPool)
}

func (r *RebootMachineConfigPoolRuntime) createOptions() metav1.CreateOptions {
	if r.dryRun {
		return metav1.CreateOptions{
			DryRun:       []string{metav1.DryRunAll},
			FieldManager: RebootMachineConfigPoolFieldManager,
		}
	}
	return metav1.CreateOptions{
		FieldManager: RebootMachineConfigPoolFieldManager,
	}
}

func (r *RebootMachineConfigPoolRuntime) updateOptions() metav1.UpdateOptions {
	if r.dryRun {
		return metav1.UpdateOptions{
			DryRun:       []string{metav1.DryRunAll},
			FieldManager: RebootMachineConfigPoolFieldManager,
		}
	}
	return metav1.UpdateOptions{
		FieldManager: RebootMachineConfigPoolFieldManager,
	}
}

func (r *RebootMachineConfigPoolRuntime) rebootMachineConfigPool(ctx context.Context, machineConfigPool *MachineConfigPool) error {
	switch machineConfigPool.Name {
	case "master":
	case "worker":
	default:
		return fmt.Errorf("only master or worker machineconfigpools are accepted, not: %q", machineConfigPool.Name)
	}

	if machineConfigPool.Spec.MachineConfigSelector == nil {
		return fmt.Errorf("machineconfigpools/%v cannot be rebooted with this command due to missing .spec.machineConfigSelector", machineConfigPool.Name)
	}
	if len(machineConfigPool.Spec.MachineConfigSelector.MatchExpressions) > 0 {
		return fmt.Errorf("machineconfigpools/%v cannot be rebooted with this command due to usage of .spec.machineConfigSelector.matchExpressions", machineConfigPool.Name)
	}
	if len(machineConfigPool.Spec.MachineConfigSelector.MatchLabels) == 0 {
		return fmt.Errorf("machineconfigpools/%v cannot be rebooted with this command due to missing .spec.machineConfigSelector.matchLabels", machineConfigPool.Name)
	}

	machineConfig := restartTemplate.DeepCopy()
	if machineConfig.Labels == nil {
		machineConfig.Labels = map[string]string{}
	}
	machineConfig.Name = fmt.Sprintf("95-oc-initiated-reboot-%s", machineConfigPool.Name)
	for k, v := range machineConfigPool.Spec.MachineConfigSelector.MatchLabels {
		machineConfig.Labels[k] = v
	}

	finalObject := &MachineConfig{}
	currMachineConfigUnstructured, err := r.DynamicClient.Resource(MachineConfigResource).Get(ctx, machineConfig.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		machineConfigUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(machineConfig)
		if err != nil {
			return fmt.Errorf("not a secret: %w", err)
		}
		toCreate := &unstructured.Unstructured{Object: machineConfigUnstructured}

		finalUnstructured, err := r.DynamicClient.Resource(MachineConfigResource).Create(ctx, toCreate, r.createOptions())
		if err != nil {
			return err
		}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(finalUnstructured.Object, finalObject); err != nil {
			return err
		}

	case err != nil:
		return err

	default:
		currMachineConfig := &MachineConfig{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(currMachineConfigUnstructured.Object, currMachineConfig); err != nil {
			return err
		}
		machineConfig.ResourceVersion = currMachineConfig.ResourceVersion

		machineConfigUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(machineConfig)
		if err != nil {
			return fmt.Errorf("not a secret: %w", err)
		}
		toUpdate := &unstructured.Unstructured{Object: machineConfigUnstructured}

		// update the file to increment the reboot counter.
		if nextContent := getNextCount(currMachineConfigUnstructured); len(nextContent) > 0 {
			// we just did this, it will work
			fileList, _, _ := unstructured.NestedSlice(toUpdate.Object, "spec", "config", "storage", "files")
			if err := unstructured.SetNestedField(fileList[0].(map[string]interface{}), nextContent, "contents", "source"); err != nil {
				return err
			}
			if err := unstructured.SetNestedSlice(toUpdate.Object, fileList, "spec", "config", "storage", "files"); err != nil {
				return err
			}
		}

		finalUnstructured, err := r.DynamicClient.Resource(MachineConfigResource).Update(ctx, toUpdate, r.updateOptions())
		if err != nil {
			return err
		}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(finalUnstructured.Object, finalObject); err != nil {
			return err
		}
	}

	finalObject.Kind = "MachineConfig"
	finalObject.APIVersion = "machineconfiguration.openshift.io/v1"
	if err := r.Printer.PrintObj(finalObject, r.Out); err != nil {
		return err
	}

	machineConfigPool.Kind = "MachineConfigPool"
	machineConfigPool.APIVersion = "machineconfiguration.openshift.io/v1"
	if err := r.Printer.PrintObj(machineConfigPool, r.Out); err != nil {
		return err
	}

	return nil
}

func GetRebootNumber(currMachineConfigUnstructured *unstructured.Unstructured) (int, error) {
	fileList, ok, err := unstructured.NestedSlice(currMachineConfigUnstructured.Object, "spec", "config", "storage", "files")
	if err != nil {
		return 0, fmt.Errorf("failed to get files: %w", err)
	}
	if !ok {
		return 0, fmt.Errorf("files content missing: %w", err)
	}

	existingContent := ""
	for _, file := range fileList {
		filename, ok, err := unstructured.NestedString(file.(map[string]interface{}), "path")
		if !ok || err != nil {
			continue
		}
		// this is the file we use to indicate reboot numbers
		if filename != "/etc/kubernetes/machine-config-operator-oc-initiated-reboot-number" {
			continue
		}

		existingContent, ok, err = unstructured.NestedString(file.(map[string]interface{}), "contents", "source")
		if err != nil {
			return 0, fmt.Errorf("failed to get source: %w", err)
		}
		if !ok {
			return 0, fmt.Errorf("source content missing: %w", err)
		}
	}

	// we write like `data:,1%0A`
	if len(existingContent) < len(`data:,1%0A`) {
		return 0, fmt.Errorf("source content unexpected %q", existingContent)
	}

	existingCountString := existingContent[6 : len(existingContent)-3]
	existingCount, err := strconv.ParseInt(existingCountString, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("source content unexpected %q: %w", existingContent, err)
	}

	return int(existingCount), nil
}

func getNextCount(currMachineConfigUnstructured *unstructured.Unstructured) string {
	existingCount, err := GetRebootNumber(currMachineConfigUnstructured)
	if err != nil {
		return ""
	}
	newCount := existingCount + 1

	return fmt.Sprintf("data:,%d%%0A", newCount)
}
