package util

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type defaultPortForwarder struct {
	restConfig *rest.Config

	StopChannel  chan struct{}
	ReadyChannel chan struct{}
}

func NewDefaultPortForwarder(adminConfig *rest.Config) *defaultPortForwarder {
	return &defaultPortForwarder{
		restConfig:   adminConfig,
		StopChannel:  make(chan struct{}, 1),
		ReadyChannel: make(chan struct{}, 1),
	}
}

func (f *defaultPortForwarder) ForwardPortsAndExecute(pod *corev1.Pod, ports []string, toExecute func()) error {
	if len(ports) < 1 {
		return fmt.Errorf("at least 1 PORT is required for port-forward")
	}

	restClient, err := corev1client.NewForConfig(f.restConfig)
	if err != nil {
		return err
	}

	if pod.Status.Phase != corev1.PodRunning {
		return fmt.Errorf("unable to forward port because pod is not running. Current status=%v", pod.Status.Phase)
	}

	stdout := bytes.NewBuffer(nil)
	req := restClient.RESTClient().Post().
		Resource("pods").
		Namespace(pod.Namespace).
		Name(pod.Name).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(f.restConfig)
	if err != nil {
		return err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())
	fw, err := portforward.New(dialer, ports, f.StopChannel, f.ReadyChannel, stdout, ioutil.Discard)
	if err != nil {
		return err
	}

	go func() {
		if f.StopChannel != nil {
			defer close(f.StopChannel)
		}

		<-f.ReadyChannel
		toExecute()
	}()

	return fw.ForwardPorts()
}

type PortForwardURLGetter struct {
	Protocol   string
	Host       string
	RemotePort string
	LocalPort  string
}

func (c *PortForwardURLGetter) Get(urlPath string, pod *corev1.Pod, config *rest.Config) (*rest.Request, error) {
	var result *rest.Request
	var lastErr error
	forwarder := NewDefaultPortForwarder(config)

	if err := forwarder.ForwardPortsAndExecute(pod, []string{c.LocalPort + ":" + c.RemotePort}, func() {
		restClient, err := kubernetes.NewForConfig(config)
		if err != nil {
			lastErr = err
			return
		}

		result = restClient.RESTClient().Get().RequestURI(urlPath)
	}); err != nil {
		return nil, err
	}
	return result, lastErr
}
