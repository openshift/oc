package inspect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/rest"
)

func (o *InspectOptions) gatherPodData(destDir, namespace string, pod *corev1.Pod) error {
	if pod.Status.Phase != corev1.PodRunning {
		log.Printf("        Skipping container data collection for pod %q: Pod not running\n", pod.Name)
		return nil
	}

	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.yaml", pod.Name)
	if err := o.fileWriter.WriteFromResource(path.Join(destDir, "/"+filename), pod); err != nil {
		return err
	}

	errs := []error{}

	// skip gathering container data if containers are no longer running
	if running, err := PodRunningReady(pod); err != nil {
		return err
	} else if !running {
		log.Printf("        Skipping container data collection for pod %q: Pod not running\n", pod.Name)
		return nil
	}

	// gather data for each container in the given pod
	for _, container := range pod.Spec.Containers {
		if err := o.gatherContainerInfo(path.Join(destDir, "/"+container.Name), pod, container); err != nil {
			errs = append(errs, err)
			continue
		}
	}
	for _, container := range pod.Spec.InitContainers {
		if err := o.gatherContainerInfo(path.Join(destDir, "/"+container.Name), pod, container); err != nil {
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("one or more errors ocurred while gathering container data for pod %s:\n\n    %v", pod.Name, errors.NewAggregate(errs))
	}
	return nil
}

func (o *InspectOptions) gatherContainerInfo(destDir string, pod *corev1.Pod, container corev1.Container) error {
	if err := o.gatherContainerAllLogs(path.Join(destDir, "/"+container.Name), pod, &container); err != nil {
		return err
	}

	if len(container.Ports) == 0 {
		log.Printf("        Skipping container endpoint collection for pod %q container %q: No ports\n", pod.Name, container.Name)
		return nil
	}
	port := &RemoteContainerPort{
		Protocol: "https",
		Port:     container.Ports[0].ContainerPort,
	}

	if err := o.gatherContainerEndpoints(path.Join(destDir, "/"+container.Name), pod, &container, port); err != nil {
		return err
	}

	return nil
}

func (o *InspectOptions) gatherContainerAllLogs(destDir string, pod *corev1.Pod, container *corev1.Container) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	errs := []error{}
	if err := o.gatherContainerLogs(path.Join(destDir, "/logs"), pod, container); err != nil {
		errs = append(errs, filterContainerLogsErrors(err))
	}

	if len(errs) > 0 {
		return errors.NewAggregate(errs)
	}
	return nil
}

func (o *InspectOptions) gatherContainerEndpoints(destDir string, pod *corev1.Pod, container *corev1.Container, metricsPort *RemoteContainerPort) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	errs := []error{}
	if err := o.gatherContainerHealthz(path.Join(destDir, "/healthz"), pod, metricsPort); err != nil {
		errs = append(errs, fmt.Errorf("unable to gather container /healthz: %v", err))
	}
	if err := o.gatherContainerVersion(destDir, pod, metricsPort); err != nil {
		errs = append(errs, fmt.Errorf("unable to gather container /version: %v", err))
	}
	if err := o.gatherContainerMetrics(destDir, pod, metricsPort); err != nil {
		errs = append(errs, fmt.Errorf("unable to gather container /metrics: %v", err))
	}

	if len(errs) > 0 {
		return errors.NewAggregate(errs)
	}
	return nil
}

func filterContainerLogsErrors(err error) error {
	if strings.Contains(err.Error(), "previous terminated container") && strings.HasSuffix(err.Error(), "not found") {
		log.Printf("        Unable to gather previous container logs: %v\n", err)
		return nil
	}
	return err
}

func (o *InspectOptions) gatherContainerVersion(destDir string, pod *corev1.Pod, metricsPort *RemoteContainerPort) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	hasVersionPath := false

	// determine if a /version endpoint exists
	paths, err := getAvailablePodEndpoints(o.podUrlGetter, pod, o.restConfig, metricsPort)
	if err != nil {
		return err
	}
	for _, p := range paths {
		if p != "/version" {
			continue
		}
		hasVersionPath = true
		break
	}
	if !hasVersionPath {
		log.Printf("        Skipping /version info gathering for pod %q. Endpoint not found...\n", pod.Name)
		return nil
	}

	result, err := o.podUrlGetter.Get("/version", pod, o.restConfig, metricsPort)

	return o.fileWriter.WriteFromSource(path.Join(destDir, "version.json"), &TextWriterSource{Text: result})
}

// gatherContainerMetrics invokes an asynchronous network call
func (o *InspectOptions) gatherContainerMetrics(destDir string, pod *corev1.Pod, metricsPort *RemoteContainerPort) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	// we need a token in order to access the /metrics endpoint
	result, err := o.podUrlGetter.Get("/metrics", pod, o.restConfig, metricsPort)
	if err != nil {
		return err
	}

	return o.fileWriter.WriteFromSource(path.Join(destDir, "metrics.json"), &TextWriterSource{Text: result})
}

func (o *InspectOptions) gatherContainerHealthz(destDir string, pod *corev1.Pod, metricsPort *RemoteContainerPort) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	paths, err := getAvailablePodEndpoints(o.podUrlGetter, pod, o.restConfig, metricsPort)
	if err != nil {
		return err
	}

	healthzSeparator := "/healthz"
	healthzPaths := []string{}
	for _, p := range paths {
		if !strings.HasPrefix(p, healthzSeparator) {
			continue
		}
		healthzPaths = append(healthzPaths, p)
	}
	if len(healthzPaths) == 0 {
		return fmt.Errorf("unable to find any available /healthz paths hosted in pod %q", pod.Name)
	}

	for _, healthzPath := range healthzPaths {
		result, err := o.podUrlGetter.Get(path.Join("/", healthzPath), pod, o.restConfig, metricsPort)
		if err != nil {
			// TODO: aggregate errors
			return err
		}

		if len(healthzSeparator) > len(healthzPath) {
			continue
		}
		filename := healthzPath[len(healthzSeparator):]
		if len(filename) == 0 {
			filename = "index"
		} else {
			filename = strings.TrimPrefix(filename, "/")
		}

		filenameSegs := strings.Split(filename, "/")
		if len(filenameSegs) > 1 {
			// ensure directory structure for nested paths exists
			filenameSegs = filenameSegs[:len(filenameSegs)-1]
			if err := os.MkdirAll(path.Join(destDir, "/"+strings.Join(filenameSegs, "/")), os.ModePerm); err != nil {
				return err
			}
		}

		if err := o.fileWriter.WriteFromSource(path.Join(destDir, filename), &TextWriterSource{Text: result}); err != nil {
			return err
		}
	}
	return nil
}

func getAvailablePodEndpoints(urlGetter *PortForwardURLGetter, pod *corev1.Pod, config *rest.Config, port *RemoteContainerPort) ([]string, error) {
	result, err := urlGetter.Get("/", pod, config, port)
	if err != nil {
		return nil, err
	}

	resultBuffer := bytes.NewBuffer([]byte(result))
	pathInfo := map[string][]string{}

	// first, unmarshal result into json object and obtain all available /healthz endpoints
	if err := json.Unmarshal(resultBuffer.Bytes(), &pathInfo); err != nil {
		return nil, err
	}
	paths, ok := pathInfo["paths"]
	if !ok {
		return nil, fmt.Errorf("unable to extract path information for pod %q", pod.Name)
	}

	return paths, nil
}

func (o *InspectOptions) gatherContainerLogs(destDir string, pod *corev1.Pod, container *corev1.Container) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	logOptions := &corev1.PodLogOptions{
		Container:  container.Name,
		Follow:     false,
		Previous:   false,
		Timestamps: true,
	}
	// first, retrieve current logs
	logsReq := o.kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOptions)

	filename := fmt.Sprintf("%s.log", "current")

	if err := o.fileWriter.WriteFromSource(path.Join(destDir, "/"+filename), logsReq); err != nil {
		return err
	}

	// then, retrieve previous logs
	logOptions.Previous = true
	logsReqPrevious := o.kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOptions)

	filename = fmt.Sprintf("%s.log", "previous")
	return o.fileWriter.WriteFromSource(path.Join(destDir, "/"+filename), logsReqPrevious)
}
