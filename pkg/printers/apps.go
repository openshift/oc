package printers

import (
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	kprinters "k8s.io/kubernetes/pkg/printers"

	appsv1 "github.com/openshift/api/apps/v1"
)

func AddAppsOpenShiftHandlers(h kprinters.PrintHandler) {
	deploymentConfigColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Revision", Type: "string", Description: appsv1.DeploymentConfigStatus{}.SwaggerDoc()["latestVersion"]},
		{Name: "Desired", Type: "string", Description: appsv1.DeploymentConfigSpec{}.SwaggerDoc()["replicas"]},
		{Name: "Current", Type: "string", Description: appsv1.DeploymentConfigStatus{}.SwaggerDoc()["updatedReplicas"]},
		{Name: "Triggered By", Type: "string", Description: appsv1.DeploymentConfigSpec{}.SwaggerDoc()["triggers"]},
	}
	if err := h.TableHandler(deploymentConfigColumnDefinitions, printDeploymentConfigList); err != nil {
		panic(err)
	}
	if err := h.TableHandler(deploymentConfigColumnDefinitions, printDeploymentConfig); err != nil {
		panic(err)
	}
}

func printDeploymentConfig(dc *appsv1.DeploymentConfig, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: dc},
	}

	var desired string
	if dc.Spec.Test {
		desired = fmt.Sprintf("%d (during test)", dc.Spec.Replicas)
	} else {
		desired = fmt.Sprintf("%d", dc.Spec.Replicas)
	}

	containers := sets.NewString()
	if dc.Spec.Template != nil {
		for _, c := range dc.Spec.Template.Spec.Containers {
			containers.Insert(c.Name)
		}
	}
	//names := containers.List()
	referencedContainers := sets.NewString()

	triggers := sets.String{}
	for _, trigger := range dc.Spec.Triggers {
		switch t := trigger.Type; t {
		case appsv1.DeploymentTriggerOnConfigChange:
			triggers.Insert("config")
		case appsv1.DeploymentTriggerOnImageChange:
			if p := trigger.ImageChangeParams; p != nil && p.Automatic {
				var prefix string
				if len(containers) != 1 && !containers.HasAll(p.ContainerNames...) {
					sort.Sort(sort.StringSlice(p.ContainerNames))
					prefix = strings.Join(p.ContainerNames, ",") + ":"
				}
				referencedContainers.Insert(p.ContainerNames...)
				switch p.From.Kind {
				case "ImageStreamTag":
					triggers.Insert(fmt.Sprintf("image(%s%s)", prefix, p.From.Name))
				default:
					triggers.Insert(fmt.Sprintf("%s(%s%s)", p.From.Kind, prefix, p.From.Name))
				}
			}
		default:
			triggers.Insert(string(t))
		}
	}

	name := formatResourceName(options.Kind, dc.Name, options.WithKind)
	trigger := strings.Join(triggers.List(), ",")

	row.Cells = append(row.Cells, name, dc.Status.LatestVersion, desired, dc.Status.UpdatedReplicas, trigger)

	return []metav1.TableRow{row}, nil
}

func printDeploymentConfigList(list *appsv1.DeploymentConfigList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printDeploymentConfig(&list.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}
