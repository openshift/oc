package generate

import (
	"fmt"
	"strings"
)

// ParseLabels turns a string representation of a label set into a map[string]string
//
// Copied from https://github.com/kubernetes/kubernetes/blob/5c8f26f48032899031760e5b75ad259d23d312b2/staging/src/k8s.io/kubectl/pkg/cmd/expose/expose.go#L578
func ParseLabels(labelSpec string) (map[string]string, error) {
	if len(labelSpec) == 0 {
		return nil, fmt.Errorf("no label spec passed")
	}
	labels := map[string]string{}
	labelSpecs := strings.Split(labelSpec, ",")
	for ix := range labelSpecs {
		labelSpec := strings.Split(labelSpecs[ix], "=")
		if len(labelSpec) != 2 {
			return nil, fmt.Errorf("unexpected label spec: %s", labelSpecs[ix])
		}
		if len(labelSpec[0]) == 0 {
			return nil, fmt.Errorf("unexpected empty label key")
		}
		labels[labelSpec[0]] = labelSpec[1]
	}
	return labels, nil
}
