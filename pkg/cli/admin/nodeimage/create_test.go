package nodeimage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes/fake"
	kubernetesscheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/distribution/distribution/v3/manifest/schema2"
	configv1 "github.com/openshift/api/config/v1"
	imageapi "github.com/openshift/api/image/v1"
	configv1fake "github.com/openshift/client-go/config/clientset/versioned/fake"
	"github.com/openshift/library-go/pkg/image/dockerv1client"
	"github.com/openshift/oc/pkg/cli/rsync"
)

var (
	defaultNodesConfigYaml = `hosts:
- hostname: extra-worker-0
  interfaces:
  - name: eth0
    macAddress: 00:b9:9b:c8:ac:f4`
)

func TestValidate(t *testing.T) {
	testCases := []struct {
		name          string
		nodesConfig   *string
		outputName    *string
		expectedError string
	}{
		{
			name:        "default",
			nodesConfig: &defaultNodesConfigYaml,
		},
		{
			name:          "missing configuration file",
			expectedError: "open nodes-config.yaml: file does not exist",
		},
		{
			name:          "invalid configuration file",
			nodesConfig:   strPtr("invalid: yaml\n\tfile"),
			expectedError: "config file nodes-config.yaml is not valid",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeFileSystem := fstest.MapFS{}
			if tc.nodesConfig != nil {
				fakeFileSystem["nodes-config.yaml"] = &fstest.MapFile{
					Data: []byte(*tc.nodesConfig),
				}
			}
			o := &CreateOptions{
				FSys: fakeFileSystem,
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

func strPtr(s string) *string {
	return &s
}

func TestRun(t *testing.T) {
	ClusterVersion_4_16_ObjectFn := func(repo string, manifestDigest string) []runtime.Object {
		cvobj := defaultClusterVersionObjectFn(repo, manifestDigest)
		clusterVersion := cvobj[0].(*configv1.ClusterVersion)
		clusterVersion.Status.Desired.Version = "4.16.6-x86_64"

		return cvobj
	}

	testCases := []struct {
		name             string
		nodesConfig      string
		assetsDir        string
		generatePXEFiles bool

		objects          func(string, string) []runtime.Object
		remoteExecOutput string

		expectedError        string
		expectedPod          func(t *testing.T, pod *corev1.Pod)
		expectedRsyncInclude string
	}{
		{
			name:                 "default",
			nodesConfig:          defaultNodesConfigYaml,
			objects:              defaultClusterVersionObjectFn,
			assetsDir:            "/my-working-dir",
			generatePXEFiles:     false,
			expectedRsyncInclude: "*.iso",
		},
		{
			name:                 "default pxe",
			nodesConfig:          defaultNodesConfigYaml,
			objects:              defaultClusterVersionObjectFn,
			assetsDir:            "/my-working-dir",
			generatePXEFiles:     true,
			expectedRsyncInclude: "boot-artifacts/*",
		},
		{
			name:             "node-joiner tool failure",
			nodesConfig:      defaultNodesConfigYaml,
			objects:          defaultClusterVersionObjectFn,
			remoteExecOutput: "1",
			expectedError:    `image generation error: <nil> (exit code: 1)`,
		},
		{
			name:             "node-joiner unsupported prior to 4.17",
			nodesConfig:      defaultNodesConfigYaml,
			objects:          ClusterVersion_4_16_ObjectFn,
			remoteExecOutput: "1",
			expectedError:    fmt.Sprintf("the 'oc adm node-image' command is only available for OpenShift versions %s and later", nodeJoinerMinimumSupportedVersion),
		},
		{
			name:          "missing cluster connection",
			nodesConfig:   defaultNodesConfigYaml,
			expectedError: `command expects a connection to an OpenShift 4.x server`,
		},
		{
			name:        "use proxy settings when defined",
			nodesConfig: defaultNodesConfigYaml,
			objects: func(repo, manifestDigest string) []runtime.Object {
				objs := defaultClusterVersionObjectFn(repo, manifestDigest)
				return append(objs, &configv1.Proxy{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster",
					},
					Status: configv1.ProxyStatus{
						HTTPProxy:  "http://192.168.111.1:8215",
						HTTPSProxy: "https://192.168.111.1:8215",
						NoProxy:    "172.22.0.0/24,192.168.111.0/24,localhost",
					},
				})
			},
			expectedPod: func(t *testing.T, pod *corev1.Pod) {
				for _, expectedVar := range []struct {
					name  string
					value string
				}{
					{name: "HTTP_PROXY", value: "http://192.168.111.1:8215"},
					{name: "HTTPS_PROXY", value: "https://192.168.111.1:8215"},
					{name: "NO_PROXY", value: "172.22.0.0/24,192.168.111.0/24,localhost"},
				} {
					varFound := false
					for _, e := range pod.Spec.Containers[0].Env {
						if e.Name == expectedVar.name {
							if e.Value != expectedVar.value {
								t.Errorf("expected pod env var '%s' value '%s', but found '%s'", expectedVar.name, expectedVar.value, e.Value)
							}
							varFound = true
							break
						}
					}
					if !varFound {
						t.Errorf("expected pod env var '%s' not found", expectedVar.name)
					}
				}
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeReg, fakeClient, fakeRestConfig, fakeRemoteExec := createFakes(t, nodeJoinerContainer)
			defer fakeReg.Close()

			// Create the fake filesystem, required to provide the command input config file.
			fakeFileSystem := fstest.MapFS{}
			if tc.nodesConfig != "" {
				fakeFileSystem["nodes-config.yaml"] = &fstest.MapFile{
					Data: []byte(tc.nodesConfig),
				}
			}
			// Allow the test case to use the right digest created by the fake registry.
			objs := []runtime.Object{}
			if tc.objects != nil {
				objs = tc.objects(fakeReg.URL()[len("https://"):], fakeReg.fakeManifestDigest)
			}

			if tc.remoteExecOutput != "" {
				fakeRemoteExec.execOut = tc.remoteExecOutput
			}
			// Create another fake for the copy action
			fakeCp := &fakeCopier{}

			// Prepare the command options with all the fakes
			o := &CreateOptions{
				BaseNodeImageCommand: BaseNodeImageCommand{
					IOStreams:      genericiooptions.NewTestIOStreamsDiscard(),
					command:        createCommand,
					ConfigClient:   configv1fake.NewSimpleClientset(objs...),
					Client:         fakeClient,
					Config:         fakeRestConfig,
					remoteExecutor: fakeRemoteExec,
				},
				FSys: fakeFileSystem,
				copyStrategy: func(o *rsync.RsyncOptions) rsync.CopyStrategy {
					fakeCp.options = o
					return fakeCp
				},

				AssetsDir:        tc.assetsDir,
				GeneratePXEFiles: tc.generatePXEFiles,
			}
			// Since the fake registry creates a self-signed cert, let's configure
			// the command options accordingly
			o.SecurityOptions.Insecure = true

			err := o.Run()
			assertContainerImageAndErrors(t, err, fakeReg, fakeClient, tc.expectedError, nodeJoinerContainer)

			// Perform additional checks on the generated node-joiner pod
			if tc.expectedPod != nil {
				pod := getTestPod(fakeClient, nodeJoinerContainer)
				tc.expectedPod(t, pod)
			}

			if tc.expectedError == "" {
				if fakeCp.options.Destination.Path != tc.assetsDir {
					t.Errorf("expected %v, actual %v", fakeCp.options.Destination.Path, tc.assetsDir)
				}
			}

			if tc.expectedRsyncInclude != "" {
				if !slices.Contains(fakeCp.options.RsyncInclude, tc.expectedRsyncInclude) {
					t.Errorf("expected RSyncOptions to include %v, but doesn't", tc.expectedRsyncInclude)
				}
			}
		})
	}
}

// fakeRegistry creates a fake Docker registry configured to serve the minimum
// amount of data required to allow a successfull execution of the command to
// retrieve the release info, and to extract the baremetal-installer pullspec.
type fakeRegistry struct {
	mux    *http.ServeMux
	server *httptest.Server

	fakeManifestDigest         string
	fakeConfigDigest           string
	fakeManifest               dockerv1client.DockerImageManifest
	fakeImageConfig            dockerv1client.DockerImageConfig
	baremetalInstallerPullSpec string
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	fakeRegistry := &fakeRegistry{}
	err := fakeRegistry.init()
	if err != nil {
		t.Fatal(err)
	}
	return fakeRegistry
}

func (fr *fakeRegistry) init() error {
	fr.mux = http.NewServeMux()
	fr.setupFakeRegistryData()

	// Ping handler
	fr.mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
		json.NewEncoder(w).Encode(make(map[string]interface{}))
	})

	// This handler is invoked when retrieving the image manifest
	fr.mux.HandleFunc("/v2/ocp/release/manifests/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", schema2.MediaTypeManifest)
		json.NewEncoder(w).Encode(fr.fakeManifest)
	})

	// Generic blobs handler used to serve both the image config and data
	fr.mux.HandleFunc("/v2/ocp/release/blobs/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		if strings.Contains(r.URL.Path, fr.fakeConfigDigest) {
			json.NewEncoder(w).Encode(fr.fakeImageConfig)
		} else {
			w.Write(fr.makeImageBlob())
		}
	})

	// Catch all
	fr.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	err := fr.newTLSServer(fr.mux.ServeHTTP)
	if err != nil {
		return err
	}
	fr.server.StartTLS()
	return nil
}

func (fr *fakeRegistry) newTLSServer(handler http.HandlerFunc) error {
	fr.server = httptest.NewUnstartedServer(handler)
	cert, err := fr.generateSelfSignedCert()
	if err != nil {
		return fmt.Errorf("error configuring server cert: %s", err)
	}
	fr.server.TLS = &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
	return nil
}

func (fr *fakeRegistry) Close() {
	fr.server.Close()
}

func (fr *fakeRegistry) URL() string {
	return fr.server.URL
}

func (fr *fakeRegistry) generateSelfSignedCert() (tls.Certificate, error) {
	// Generate the private key
	pk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	// Generate the serial number
	sn, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return tls.Certificate{}, err
	}
	// Create the certificate template
	template := x509.Certificate{
		SerialNumber: sn,
		Subject: pkix.Name{
			Organization: []string{"Day2 AddNodes Tester & Co"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(1 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &pk.PublicKey, pk)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(pk)})
	return tls.X509KeyPair(certPEM, keyPEM)
}

func (fr *fakeRegistry) setupFakeRegistryData() {
	fr.fakeManifestDigest = "sha256:378301736412173c8c195695a44e9f8f4b4cfc2af2acb6c419e46e5807ff427b"
	fr.fakeConfigDigest = "sha256:f2be4387ceaa5d23d2d5c8c2179964b1e77644cbb43441d623edf7c805a75220"

	fr.fakeManifest = dockerv1client.DockerImageManifest{
		SchemaVersion: 2,
		MediaType:     schema2.MediaTypeManifest,
		Config: dockerv1client.Descriptor{
			MediaType: schema2.MediaTypeImageConfig,
			Digest:    fr.fakeConfigDigest,
		},
		Layers: []dockerv1client.Descriptor{
			{
				MediaType: schema2.MediaTypeLayer,
				Digest:    "sha256:a000a000a000a000f000f000f000f000f000f000f000f000f000f000f000f000",
			},
		},
	}

	fr.fakeImageConfig = dockerv1client.DockerImageConfig{
		Architecture: "amd64",
		OS:           "linux",
	}

	fr.baremetalInstallerPullSpec = "ocp-fake-test.io/ocp-v4.0-art-dev@sha256:0a80d59b317a88fd807d1f2b1a6db634ba5e5dfcdc0ec84298ae0971f3780dca"
}

func (fr *fakeRegistry) makeImageBlob() []byte {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	imageReferences := imageapi.ImageStream{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ImageStream",
			APIVersion: "image.openshift.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"io.openshift.build.versions": "0.0.1",
			},
		},
		Spec: imageapi.ImageStreamSpec{
			// Just configure the only required image pull spec
			Tags: []imageapi.TagReference{
				{
					Name: "baremetal-installer",
					From: &corev1.ObjectReference{
						Name: fr.baremetalInstallerPullSpec,
					},
				},
			},
		},
	}
	data, _ := json.Marshal(&imageReferences)
	header := &tar.Header{
		Name: "release-manifests/image-references",
		Mode: 0600,
		Size: int64(len(data)),
	}

	tw.WriteHeader(header)
	tw.Write(data)
	tw.Close()
	gw.Close()
	return buf.Bytes()
}

// fakeCopier is used to mock the remote copy
type fakeCopier struct {
	options *rsync.RsyncOptions
}

func (f *fakeCopier) Copy(source, destination *rsync.PathSpec, out, errOut io.Writer) error {
	return nil
}
func (f *fakeCopier) Validate() error {
	return nil
}
func (f *fakeCopier) String() string {
	return ""
}

// fakeRemoteExecutor is used to simulate the remote execution
type fakeRemoteExecutor struct {
	url     *url.URL
	execErr error
	execOut string
}

func (f *fakeRemoteExecutor) Execute(url *url.URL, config *restclient.Config, stdin io.Reader, stdout, stderr io.Writer, tty bool, terminalSizeQueue remotecommand.TerminalSizeQueue) error {
	f.url = url
	stdout.Write([]byte(f.execOut))
	return f.execErr
}

func createFakes(t *testing.T, podName string) (*fakeRegistry, *fake.Clientset, *restclient.Config, *fakeRemoteExecutor) {
	// Create the fake registry. It will provide the required manifests and image data,
	// when looking for the baremetal-installer image pullspec.
	fakeReg := newFakeRegistry(t)

	fakeClient := fake.NewSimpleClientset()
	// When creating a pod, it's necessary to set a propert name. Also, to simulate the pod execution, its container status
	// is moved to a terminal state.
	fakeClient.PrependReactor("create", "pods", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
		createAction, _ := action.(clientgotesting.CreateAction)
		pod := createAction.GetObject().(*corev1.Pod)
		pod.SetName(podName)
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{
			Name: podName,
			State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{},
			},
		})
		return false, pod, nil
	})
	// Create a fake rest config. Required by the exec command.
	fakeRestConfig := &restclient.Config{
		Host: fakeReg.URL(),
		ContentConfig: restclient.ContentConfig{
			GroupVersion:         &configv1.GroupVersion,
			NegotiatedSerializer: kubernetesscheme.Codecs,
		},
	}
	// Create a fake remote executor, with a default success result
	fakeRemoteExec := &fakeRemoteExecutor{
		execOut: "0",
	}

	return fakeReg, fakeClient, fakeRestConfig, fakeRemoteExec
}

var defaultClusterVersionObjectFn = func(repo string, manifestDigest string) []runtime.Object {
	return []runtime.Object{
		&configv1.ClusterVersion{
			ObjectMeta: metav1.ObjectMeta{
				Name: "version",
			},
			Status: configv1.ClusterVersionStatus{
				Desired: configv1.Release{
					Image:   fmt.Sprintf("%s/ocp/release@%s", repo, manifestDigest),
					Version: "4.18.6-x86_64",
				},
			},
		},
	}
}

func getTestPod(fakeClient *fake.Clientset, podName string) *corev1.Pod {
	pod, _ := fakeClient.CoreV1().Pods("").Get(context.Background(), podName, metav1.GetOptions{})
	return pod
}

func assertContainerImageAndErrors(t *testing.T, runErr error, fakeReg *fakeRegistry, fakeClient *fake.Clientset, expectedError, podName string) {
	if expectedError == "" {
		if runErr != nil {
			t.Fatalf("unexpected error: %v", runErr)
		}
		pod := getTestPod(fakeClient, podName)
		// In case of success, let's verify that the image pullspec used was effectively the one served by the
		// fake registry.
		if fakeReg.baremetalInstallerPullSpec != pod.Spec.Containers[0].Image {
			t.Errorf("expected %v, actual %v", fakeReg.baremetalInstallerPullSpec, pod.Spec.Containers[0].Image)
		}
	} else {
		if runErr == nil {
			t.Fatalf("expected error not received: %s", expectedError)
		}
		if !strings.Contains(runErr.Error(), expectedError) {
			t.Fatalf("expected error: %s, actual: %v", expectedError, runErr.Error())
		}
	}
}

func TestCreateConfigFileFromFlags(t *testing.T) {
	sshKeyContents := "ssh-key"
	networkConfigContents := "interfaces:"
	expectedConfigFile := `hosts:
- cpuArchitecture: arm64
  hostname: server1.example.org
  interfaces:
  - macAddress: fe:b1:7d:5b:86:e7
    name: eth0
  networkConfig:
    interfaces: null
  rootDeviceHints:
    deviceName: /dev/sda
  sshKey: ssh-key
`
	testCases := []struct {
		name                    string
		singleNodeCreateOptions *singleNodeCreateOptions
		expectedConfigFile      string
		expectedError           string
	}{
		{
			name: "default",
			singleNodeCreateOptions: &singleNodeCreateOptions{
				MacAddress:        "fe:b1:7d:5b:86:e7",
				CPUArchitecture:   "arm64",
				SSHKeyPath:        "ssh-key.pub",
				Hostname:          "server1.example.org",
				RootDeviceHints:   "deviceName:/dev/sda",
				NetworkConfigPath: "network-config.yaml",
			},
			expectedConfigFile: expectedConfigFile,
		},
		{
			name: "ssh-key-path missing or incorrect",
			singleNodeCreateOptions: &singleNodeCreateOptions{
				SSHKeyPath: "wrong-ssh-key-file-name",
			},
			expectedError: "open wrong-ssh-key-file-name: file does not exist",
		},
		{
			name: "network config path is missing or incorrect",
			singleNodeCreateOptions: &singleNodeCreateOptions{
				NetworkConfigPath: "wrong-network-config-file-name",
			},
			expectedError: "open wrong-network-config-file-name: file does not exist",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeFileSystem := fstest.MapFS{}
			if tc.singleNodeCreateOptions.SSHKeyPath != "" {
				fakeFileSystem["ssh-key.pub"] = &fstest.MapFile{
					Data: []byte(sshKeyContents),
				}
			}
			if tc.singleNodeCreateOptions.NetworkConfigPath != "" {
				fakeFileSystem["network-config.yaml"] = &fstest.MapFile{
					Data: []byte(networkConfigContents),
				}
			}
			o := &CreateOptions{
				FSys:           fakeFileSystem,
				SingleNodeOpts: tc.singleNodeCreateOptions,
			}

			configFileBytes, err := o.createConfigFileFromFlags()

			if tc.expectedError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if !bytes.Equal(configFileBytes, []byte(tc.expectedConfigFile)) {
					t.Fatalf("generated config file does not match expected: %v, actual: %v", tc.expectedConfigFile, string(configFileBytes))
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
