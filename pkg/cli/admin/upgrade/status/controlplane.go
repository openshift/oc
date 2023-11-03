package status

import (
	"io"
	"strings"
	"text/template"

	v1 "github.com/openshift/api/config/v1"
)

type operators struct {
	Total       int
	Available   int
	Progressing int
	Degraded    int
}

type controlPlaneStatusDisplayData struct {
	Operators operators
}

func assessControlPlaneStatus(cv *v1.ClusterVersion, operators []v1.ClusterOperator) controlPlaneStatusDisplayData {
	var displayData controlPlaneStatusDisplayData

	targetVersion := cv.Status.Desired.Version

	for _, operator := range operators {
		var isPlatformOperator bool
		for annotation := range operator.Annotations {
			if strings.HasPrefix(annotation, "exclude.release.openshift.io/") ||
				strings.HasPrefix(annotation, "include.release.openshift.io/") {
				isPlatformOperator = true
				break
			}
		}
		if !isPlatformOperator {
			continue
		}

		var updated bool
		for _, version := range operator.Status.Versions {
			if version.Name == "operator" && version.Version == targetVersion {
				updated = true
			}
		}
		displayData.Operators.Total++
		for _, condition := range operator.Status.Conditions {
			switch {
			case condition.Type == v1.OperatorAvailable && condition.Status == v1.ConditionTrue:
				displayData.Operators.Available++
			case condition.Type == v1.OperatorProgressing && condition.Status == v1.ConditionTrue && !updated:
				displayData.Operators.Progressing++
			case condition.Type == v1.OperatorDegraded && condition.Status == v1.ConditionTrue:
				displayData.Operators.Degraded++
			}
		}
	}

	return displayData
}

var controlPlaneStatusTemplate = template.Must(template.New("controlPlaneStatus").Parse(controlPlaneStatusTemplateRaw))

func (d *controlPlaneStatusDisplayData) Write(f io.Writer) error {
	return controlPlaneStatusTemplate.Execute(f, d)
}

const controlPlaneStatusTemplateRaw = `= Control Plane =
Operator Status: {{ .Operators.Total }} Total, {{ .Operators.Available }} Available, {{ .Operators.Progressing }} Progressing, {{ .Operators.Degraded }} Degraded
`
