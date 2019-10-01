package inspect

import (
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// gatherNamespaceEvents will gather all events for given namespace
func (o *InspectOptions) gatherNamespaceEvents(targetFileName, namespace string) error {
	events, err := o.kubeClient.CoreV1().Events(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	var formattedEvents []string
	sortedEvents := events.Items
	// sort by LastTimestamp
	sort.Slice(sortedEvents, func(i, j int) bool {
		return sortedEvents[i].LastTimestamp.Time.Before(sortedEvents[j].LastTimestamp.Time)
	})
	// use simplified format for events to save space and make events more readable
	for _, event := range sortedEvents {
		formattedEvents = append(formattedEvents, fmt.Sprintf("%s [%s] %s", event.LastTimestamp, event.Type[0:1], event.Message))
	}
	return o.fileWriter.WriteFromSource(targetFileName, &TextWriterSource{Text: strings.Join(formattedEvents, "\n")})
}
