package inspect

import (
	"fmt"
	"os"
	"path"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
)

func (o *InspectOptions) gatherPodData(destDir, namespace string, pod *corev1.Pod) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.yaml", pod.Name)
	if err := o.fileWriter.WriteFromResource(path.Join(destDir, "/"+filename), pod); err != nil {
		return err
	}

	errs := []error{}

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
		return fmt.Errorf("one or more errors ocurred while gathering container data for pod %s:\n\n    %v", pod.Name, utilerrors.NewAggregate(errs))
	}
	return nil
}

func (o *InspectOptions) gatherContainerInfo(destDir string, pod *corev1.Pod, container corev1.Container) error {
	if err := o.gatherContainerAllLogs(path.Join(destDir, "/"+container.Name), pod, &container); err != nil {
		return err
	}

	if err := o.gatherContainerDebug(path.Join(destDir, "/"+container.Name, "/debug"), pod, &container); err != nil {
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
		return utilerrors.NewAggregate(errs)
	}
	return nil
}

func filterContainerLogsErrors(err error) error {
	if strings.Contains(err.Error(), "previous terminated container") && strings.HasSuffix(err.Error(), "not found") {
		klog.V(1).Infof("        Unable to gather previous container logs: %v\n", err)
		return nil
	}
	return err
}

func (o *InspectOptions) gatherContainerDebug(destDir string, pod *corev1.Pod, container *corev1.Container) error {
	if !o.withDebug {
		return nil
	}
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}
	port := &RemoteContainerPort{
		Protocol: "https",
		Port:     container.Ports[0].ContainerPort,
	}
	endpoints := []string{"heap", "profile", "trace"}
	for _, endpoint := range endpoints {
		// we need a token in order to access the /debug endpoint
		result, err := o.podUrlGetter.Get("/debug/pprof/"+endpoint, pod, o.RESTConfig, port)
		if err != nil {
			return err
		}
		if err := o.fileWriter.WriteFromSource(path.Join(destDir, endpoint), &TextWriterSource{Text: result}); err != nil {
			return err
		}
	}
	return nil
}

func (o *InspectOptions) gatherContainerLogs(destDir string, pod *corev1.Pod, container *corev1.Container) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}
	errs := []error{}
	wg := sync.WaitGroup{}
	errLock := sync.Mutex{}

	wg.Add(1)
	go func() {
		defer wg.Done()

		innerErrs := []error{}
		logOptions := &corev1.PodLogOptions{
			Container:  container.Name,
			Follow:     false,
			Previous:   false,
			Timestamps: true,
		}
		if len(o.sinceTime) > 0 {
			logOptions.SinceTime = &o.sinceTimestamp
		}
		if o.since != 0 {
			logOptions.SinceSeconds = &o.sinceInt
		}
		filename := "current.log"
		logsReq := o.kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOptions)
		if err := o.fileWriter.WriteFromSource(path.Join(destDir, "/"+filename), logsReq); err != nil {
			innerErrs = append(innerErrs, err)

			// if we had an error, we will try again with an insecure backendproxy flag set
			logOptions.InsecureSkipTLSVerifyBackend = true
			logsReq = o.kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOptions)
			filename = "current.insecure.log"
			if err := o.fileWriter.WriteFromSource(path.Join(destDir, "/"+filename), logsReq); err != nil {
				innerErrs = append(innerErrs, err)
			}
		}

		errLock.Lock()
		defer errLock.Unlock()
		errs = append(errs, innerErrs...)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()

		innerErrs := []error{}
		logOptions := &corev1.PodLogOptions{
			Container:  container.Name,
			Follow:     false,
			Previous:   true,
			Timestamps: true,
		}
		logsReq := o.kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOptions)
		filename := "previous.log"
		if err := o.fileWriter.WriteFromSource(path.Join(destDir, "/"+filename), logsReq); err != nil {
			innerErrs = append(innerErrs, err)

			// if we had an error, we will try again with an insecure backendproxy flag set
			logOptions.InsecureSkipTLSVerifyBackend = true
			logsReq = o.kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOptions)
			filename = "previous.insecure.log"
			if err := o.fileWriter.WriteFromSource(path.Join(destDir, "/"+filename), logsReq); err != nil {
				innerErrs = append(innerErrs, err)
			}
		}

		errLock.Lock()
		defer errLock.Unlock()
		errs = append(errs, innerErrs...)
	}()
	wg.Wait()
	return utilerrors.NewAggregate(errs)
}
