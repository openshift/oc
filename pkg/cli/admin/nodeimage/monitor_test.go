package nodeimage

import (
	"bytes"
	"strings"
	"testing"

	configv1fake "github.com/openshift/client-go/config/clientset/versioned/fake"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	cmdlogs "k8s.io/kubectl/pkg/cmd/logs"
)

func TestMonitorValidate(t *testing.T) {
	testCases := []struct {
		name                 string
		IPAddressesToMonitor string
		expectedError        string
	}{
		{
			name:                 "default",
			IPAddressesToMonitor: "192.168.111.83",
		},
		{
			name:          "no IP addresses",
			expectedError: "--ip-addresses cannot be empty",
		},
		{
			name:                 "invalid IP address",
			IPAddressesToMonitor: "192.168.111.8e",
			expectedError:        "192.168.111.8e is not valid IP address",
		},
		{
			name:                 "multiple IP addresses",
			IPAddressesToMonitor: "192.168.111.83,192.168.111.84",
		},
		{
			name:                 "IPv6 addresses",
			IPAddressesToMonitor: "2001:db8::1234:5678,2001:db8:3333:4444:5555:6666:7777:8888",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			o := &MonitorOptions{
				IPAddressesToMonitor: tc.IPAddressesToMonitor,
			}

			err := o.Validate()

			if tc.expectedError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error not received: %s", tc.expectedError)
				}
				if !strings.Contains(err.Error(), tc.expectedError) {
					t.Fatalf("expected error: %s, actual: %v", tc.expectedError, err.Error())
				}
			}
		})
	}
}

func TestMonitorRun(t *testing.T) {
	testCases := []struct {
		name      string
		assetsDir string

		objects          func(string, string) []runtime.Object
		remoteExecOutput string

		expectedError string
	}{
		{
			name:    "default",
			objects: defaultClusterVersionObjectFn,
		},
		{
			name:          "missing cluster connection",
			expectedError: `command expects a connection to an OpenShift 4.x server`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeReg, fakeClient, fakeRestConfig, fakeRemoteExec := createFakes(t, nodeJoinerMonitorContainer)
			defer fakeReg.Close()

			// Allow the test case to use the right digest created by the fake registry.
			objs := []runtime.Object{}
			if tc.objects != nil {
				objs = tc.objects(fakeReg.URL()[len("https://"):], fakeReg.fakeManifestDigest)
			}

			if tc.remoteExecOutput != "" {
				fakeRemoteExec.execOut = tc.remoteExecOutput
			}

			// Prepare the command options with all the fakes
			o := &MonitorOptions{
				BaseNodeImageCommand: BaseNodeImageCommand{
					IOStreams:      genericiooptions.NewTestIOStreamsDiscard(),
					command:        createCommand,
					ConfigClient:   configv1fake.NewSimpleClientset(objs...),
					Client:         fakeClient,
					Config:         fakeRestConfig,
					remoteExecutor: fakeRemoteExec,
				},
			}

			var logContents bytes.Buffer
			o.Out = &logContents
			fakeLogContent := "fake log content"

			o.updateLogsFn = func(opts *cmdlogs.LogsOptions) error {
				logContents.WriteString(fakeLogContent)
				return nil
			}

			// Since the fake registry creates a self-signed cert, let's configure
			// the command options accordingly
			o.SecurityOptions.Insecure = true

			err := o.Run()
			assertContainerImageAndErrors(t, err, fakeReg, fakeClient, -1, tc.expectedError, nodeJoinerMonitorContainer)
			if tc.expectedError == "" {
				if fakeLogContent != logContents.String() {
					t.Errorf("expected %v, actual %v", fakeLogContent, logContents.String())
				}
			}
		})
	}
}
