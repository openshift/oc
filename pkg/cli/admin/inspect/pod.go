package inspect

import (
	"context"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	corev1 "k8s.io/api/core/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
)

func (o *InspectOptions) gatherPodData(ctx context.Context, destDir, namespace string, pod *corev1.Pod) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.yaml", pod.Name)
	if err := o.fileWriter.WriteFromResource(ctx, path.Join(destDir, "/"+filename), pod); err != nil {
		return err
	}

	errs := []error{}

	// gather data for each container in the given pod
	for _, container := range pod.Spec.Containers {
		if err := o.gatherContainerInfo(ctx, path.Join(destDir, "/"+container.Name), pod, container); err != nil {
			errs = append(errs, err)
			continue
		}
	}
	for _, container := range pod.Spec.InitContainers {
		if err := o.gatherContainerInfo(ctx, path.Join(destDir, "/"+container.Name), pod, container); err != nil {
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("one or more errors occurred while gathering container data for pod %s:\n\n    %v", pod.Name, utilerrors.NewAggregate(errs))
	}
	return nil
}

func (o *InspectOptions) gatherContainerInfo(ctx context.Context, destDir string, pod *corev1.Pod, container corev1.Container) error {
	if err := o.gatherContainerAllLogs(ctx, path.Join(destDir, "/"+container.Name), pod, &container); err != nil {
		return err
	}

	return nil
}

func (o *InspectOptions) gatherContainerAllLogs(ctx context.Context, destDir string, pod *corev1.Pod, container *corev1.Container) error {
	// ensure destination path exists
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	errs := []error{}
	if err := o.gatherContainerLogs(ctx, path.Join(destDir, "/logs"), pod, container); err != nil {
		errs = append(errs, filterContainerLogsErrors(err))
	}

	if o.rotatedPodLogs {
		if err := o.gatherContainerRotatedLogFiles(ctx, path.Join(destDir, "/logs/rotated"), pod, container); err != nil {
			errs = append(errs, filterContainerLogsErrors(err))
		}
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

func rotatedLogFilename(pod *corev1.Pod) (string, error) {
	if value, exists := pod.Annotations["kubernetes.io/config.source"]; exists && value == "file" {
		hash, exists := pod.Annotations["kubernetes.io/config.hash"]
		if !exists {
			return "", fmt.Errorf("missing 'kubernetes.io/config.hash' annotation for static pod")
		}
		return pod.Namespace + "_" + pod.Name + "_" + hash, nil
	}
	return pod.Namespace + "_" + pod.Name + "_" + string(pod.GetUID()), nil
}

func (o *InspectOptions) gatherContainerRotatedLogFiles(ctx context.Context, destDir string, pod *corev1.Pod, container *corev1.Container) error {
	restClient := o.kubeClient.CoreV1().RESTClient()
	var innerErrs []error

	logFileName, err := rotatedLogFilename(pod)
	if err != nil {
		return err
	}

	// Get all container log files from the node
	containerPath := restClient.Get().
		Name(pod.Spec.NodeName).
		Resource("nodes").
		SubResource("proxy", "logs", "pods", logFileName).
		Suffix(container.Name).URL().Path

	req := restClient.Get().RequestURI(containerPath).
		SetHeader("Accept", "text/plain, */*")
	res, err := req.Stream(ctx)
	if err != nil {
		return err
	}

	doc, err := html.Parse(res)
	if err != nil {
		return err
	}

	// when sinceTime is given we use that to compare the log file name with
	// the provided date.
	// when since is given we subtract the given duration from time.Now(),
	// then use that as the requested time.
	var requestedTime time.Time
	if len(o.sinceTime) > 0 {
		requestedTime = o.sinceTimestamp.Time
	}
	if o.since != 0 {
		requestedTime = time.Now().Add(-o.since)
	}

	// rotated log files have a suffix added at the end of the file name
	// e.g: 0.log.20211027-082023, 0.log.20211027-082023.gz
	reRotatedLog := regexp.MustCompile(`[0-9]+\.log\..+`)
	var downloadRotatedLogs func(*html.Node)
	downloadRotatedLogs = func(n *html.Node) {
		var fileName string
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					fileName = attr.Val
				}
			}
			if !reRotatedLog.MatchString(fileName) {
				return
			}

			if !requestedTime.IsZero() {
				// make sure rotated logs honor --since and --since-time
				// when set by the user.
				if parts := strings.Split(fileName, "."); len(parts) >= 3 {
					if rotatedTime, err := time.Parse("20060102-150405", parts[2]); err == nil {
						if rotatedTime.Before(requestedTime) {
							klog.V(4).Infof(
								"skipping rotated logs for %q because it's outside of user-provided time constraints",
								fileName,
							)
							return
						}
					} else {
						innerErrs = append(innerErrs, err)
					}
				}
			}

			// ensure destination dir exists
			if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
				innerErrs = append(innerErrs, err)
			}

			logsReq := restClient.Get().RequestURI(path.Join(containerPath, fileName)).
				SetHeader("Accept", "text/plain, */*").
				SetHeader("Accept-Encoding", "gzip")

			if err := o.fileWriter.WriteFromSource(ctx, path.Join(destDir, fileName), logsReq); err != nil {
				innerErrs = append(innerErrs, err)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			downloadRotatedLogs(c)
		}
	}
	downloadRotatedLogs(doc)
	return utilerrors.NewAggregate(innerErrs)
}

func (o *InspectOptions) gatherContainerLogs(ctx context.Context, destDir string, pod *corev1.Pod, container *corev1.Container) error {
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
		if err := o.fileWriter.WriteFromSource(ctx, path.Join(destDir, "/"+filename), logsReq); err != nil {
			innerErrs = append(innerErrs, err)

			// if we had an error, we will try again with an insecure backendproxy flag set
			logOptions.InsecureSkipTLSVerifyBackend = true
			logsReq = o.kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOptions)
			filename = "current.insecure.log"
			if err := o.fileWriter.WriteFromSource(ctx, path.Join(destDir, "/"+filename), logsReq); err != nil {
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
		if len(o.sinceTime) > 0 {
			logOptions.SinceTime = &o.sinceTimestamp
		}
		if o.since != 0 {
			logOptions.SinceSeconds = &o.sinceInt
		}
		logsReq := o.kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOptions)
		filename := "previous.log"
		if err := o.fileWriter.WriteFromSource(ctx, path.Join(destDir, "/"+filename), logsReq); err != nil {
			innerErrs = append(innerErrs, err)

			// if we had an error, we will try again with an insecure backendproxy flag set
			logOptions.InsecureSkipTLSVerifyBackend = true
			logsReq = o.kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOptions)
			filename = "previous.insecure.log"
			if err := o.fileWriter.WriteFromSource(ctx, path.Join(destDir, "/"+filename), logsReq); err != nil {
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
