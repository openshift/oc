package startbuild

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/apitesting"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta/testrestmapper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes/fake"
	restclient "k8s.io/client-go/rest"
	restfake "k8s.io/client-go/rest/fake"
	kclientcmd "k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/kubectl/pkg/scheme"

	"github.com/openshift/api"
	buildv1 "github.com/openshift/api/build/v1"
	fakebuildclientset "github.com/openshift/client-go/build/clientset/versioned/fake"
	buildclientmanual "github.com/openshift/oc/pkg/helpers/build/client/v1"
	"github.com/openshift/oc/pkg/helpers/source-to-image/tar"
)

type missingResourceTest struct {
	name         string
	namespace    string
	strategyType buildv1.BuildStrategyType
	secrets      []string
	vSecrets     []string
	cms          []string
	vCms         []string

	// these will exist in the client
	createSecrets []string
	createCms     []string

	// these will be in the error message
	// if both empty, no error is expected
	missSecrets []string
	missCms     []string
}

type FakeClientConfig struct {
	Raw      clientcmdapi.Config
	Client   *restclient.Config
	NS       string
	Explicit bool
	Err      error
}

func (c *FakeClientConfig) ConfigAccess() kclientcmd.ConfigAccess {
	return nil
}

// RawConfig returns the merged result of all overrides
func (c *FakeClientConfig) RawConfig() (clientcmdapi.Config, error) {
	return c.Raw, c.Err
}

// ClientConfig returns a complete client config
func (c *FakeClientConfig) ClientConfig() (*restclient.Config, error) {
	return c.Client, c.Err
}

// Namespace returns the namespace resulting from the merged result of all overrides
func (c *FakeClientConfig) Namespace() (string, bool, error) {
	return c.NS, c.Explicit, c.Err
}

func TestStartBuildWebHook(t *testing.T) {
	invoked := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		invoked <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &FakeClientConfig{}
	o := &StartBuildOptions{
		IOStreams:    genericclioptions.NewTestIOStreamsDiscard(),
		ClientConfig: cfg.Client,
		FromWebhook:  server.URL + "/webhook",
		Mapper:       testrestmapper.TestOnlyStaticRESTMapper(scheme.Scheme),
	}
	if err := o.Run(context.TODO()); err != nil {
		t.Fatalf("unable to start hook: %v", err)
	}
	<-invoked

	o = &StartBuildOptions{
		IOStreams:      genericclioptions.NewTestIOStreamsDiscard(),
		FromWebhook:    server.URL + "/webhook",
		GitPostReceive: "unknownpath",
	}
	if err := o.Run(context.TODO()); err == nil {
		t.Fatalf("unexpected non-error: %v", err)
	}
}

func TestStartBuildHookPostReceive(t *testing.T) {
	invoked := make(chan *buildv1.GenericWebHookEvent, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		event := buildv1.GenericWebHookEvent{}
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&event); err != nil {
			t.Errorf("unmarshal failed: %v", err)
		}
		invoked <- &event
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	f, _ := ioutil.TempFile("", "test")
	defer os.Remove(f.Name())
	fmt.Fprintf(f, `0000 2384 refs/heads/master
2548 2548 refs/heads/stage`)
	f.Close()

	testErr := errors.New("not enabled")
	cfg := &FakeClientConfig{
		Err: testErr,
	}
	o := &StartBuildOptions{
		IOStreams:      genericclioptions.NewTestIOStreamsDiscard(),
		ClientConfig:   cfg.Client,
		FromWebhook:    server.URL + "/webhook",
		GitPostReceive: f.Name(),
		Mapper:         testrestmapper.TestOnlyStaticRESTMapper(scheme.Scheme),
	}
	if err := o.Run(context.TODO()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := <-invoked
	if event == nil || event.Git == nil || len(event.Git.Refs) != 1 {
		t.Fatalf("unexpected event: %#v", event)
	}
	if event.Git.Refs[0].Commit != "2384" {
		t.Fatalf("unexpected ref: %#v", event.Git.Refs[0])
	}
}

type FakeBuildConfigs struct {
	t            *testing.T
	expectAsFile bool
}

func (c FakeBuildConfigs) InstantiateBinary(name string, options *buildv1.BinaryBuildRequestOptions, r io.Reader) (result *buildv1.Build, err error) {
	if binary, err := ioutil.ReadAll(r); err != nil {
		c.t.Errorf("Error while reading binary over HTTP: %v", err)
	} else if string(binary) != "hi" {
		c.t.Errorf("Wrong value while reading binary over HTTP: %q", binary)
	}

	if c.expectAsFile && options.AsFile == "" {
		c.t.Errorf("Expecting file, got archive")
	} else if !c.expectAsFile && options.AsFile != "" {
		c.t.Errorf("Expecting archive, got file")
	}

	return &buildv1.Build{}, nil
}

func TestHttpBinary(t *testing.T) {
	tests := []struct {
		description        string
		fromFile           bool // true = --from-file, false = --from-dir/--from-archive
		urlPath            string
		statusCode         int  // server status code, 200 if not set
		contentDisposition bool // will server send Content-Disposition with filename?
		networkError       bool
		tlsBadCert         bool
		expectedError      string
		expectWarning      bool
	}{
		{
			description:        "--from-file, filename in header",
			fromFile:           true,
			urlPath:            "/",
			contentDisposition: true,
		},
		{
			description: "--from-file, filename in URL",
			fromFile:    true,
			urlPath:     "/hi.txt",
		},
		{
			description:   "--from-file, no filename",
			fromFile:      true,
			urlPath:       "",
			expectedError: "unable to determine filename",
		},
		{
			description:   "--from-file, http error",
			fromFile:      true,
			urlPath:       "/",
			statusCode:    404,
			expectedError: "unable to download file",
		},
		{
			description:   "--from-file, network error",
			fromFile:      true,
			urlPath:       "/hi.txt",
			networkError:  true,
			expectedError: "invalid port",
		},
		{
			description:   "--from-file, https with invalid certificate",
			fromFile:      true,
			urlPath:       "/hi.txt",
			tlsBadCert:    true,
			expectedError: "certificate signed by unknown authority",
		},
		{
			description:        "--from-dir, filename in header",
			fromFile:           false,
			contentDisposition: true,
			expectWarning:      true,
		},
		{
			description:   "--from-dir, filename in URL",
			fromFile:      false,
			urlPath:       "/hi.tar.gz",
			expectWarning: true,
		},
		{
			description:   "--from-dir, no filename",
			fromFile:      false,
			expectWarning: true,
		},
		{
			description:   "--from-dir, http error",
			statusCode:    503,
			fromFile:      false,
			expectedError: "unable to download file",
		},
	}

	for _, tc := range tests {
		stdin := bytes.NewReader([]byte{})
		stdout := &bytes.Buffer{}
		options := buildv1.BinaryBuildRequestOptions{}
		handler := func(w http.ResponseWriter, r *http.Request) {
			if tc.contentDisposition {
				w.Header().Add("Content-Disposition", "attachment; filename=hi.txt")
			}
			if tc.statusCode > 0 {
				w.WriteHeader(tc.statusCode)
			}
			w.Write([]byte("hi"))
		}
		var server *httptest.Server
		if tc.tlsBadCert {
			// uses self-signed certificate
			server = httptest.NewTLSServer(http.HandlerFunc(handler))
		} else {
			server = httptest.NewServer(http.HandlerFunc(handler))
		}
		defer server.Close()

		if tc.networkError {
			server.URL = "http://localhost:999999"
		}

		var fromDir, fromFile string
		if tc.fromFile {
			fromFile = server.URL + tc.urlPath
		} else {
			fromDir = server.URL + tc.urlPath
		}

		defaultExclusionPattern := tar.DefaultExclusionPattern.String()

		build, err := streamPathToBuild(nil, stdin, stdout, &FakeBuildConfigs{t: t, expectAsFile: tc.fromFile}, fromDir, fromFile, "", defaultExclusionPattern, &options)

		if len(tc.expectedError) > 0 {
			if err == nil {
				t.Errorf("[%s] Expected error: %q, got success", tc.description, tc.expectedError)
			} else if !strings.Contains(err.Error(), tc.expectedError) {
				t.Errorf("[%s] Expected error: %q, got: %v", tc.description, tc.expectedError, err)
			}
		} else {
			if err != nil {
				t.Errorf("[%s] Unexpected error: %v", tc.description, err)
				continue
			}

			if build == nil {
				t.Errorf("[%s] No error and no build?", tc.description)
			}

			if tc.fromFile && options.AsFile != "hi.txt" {
				t.Errorf("[%s] Wrong asFile: %q", tc.description, options.AsFile)
			} else if !tc.fromFile && options.AsFile != "" {
				t.Errorf("[%s] asFile set when using --from-dir: %q", tc.description, options.AsFile)
			}
		}

		if out := stdout.String(); tc.expectWarning != strings.Contains(out, "may not be an archive") {
			t.Errorf("[%s] Expected archive warning: %v, got: %q", tc.description, tc.expectWarning, out)
		}
	}
}

type logTestCase struct {
	RequestErr     error
	IOErr          error
	ExpectedLogMsg string
	ExpectedErrMsg string
}

type failReader struct {
	Err error
}

func (r *failReader) Read(p []byte) (n int, err error) {
	return 0, r.Err
}

func TestStreamBuildLogs(t *testing.T) {
	cases := []logTestCase{
		{
			ExpectedLogMsg: "hello",
		},
		{
			RequestErr:     errors.New("simulated failure"),
			ExpectedErrMsg: "unable to stream the build logs",
		},
		{
			RequestErr: &kerrors.StatusError{
				ErrStatus: metav1.Status{
					Reason:  metav1.StatusReasonTimeout,
					Message: "timeout",
				},
			},
			ExpectedErrMsg: "unable to stream the build logs",
		},
		{
			IOErr:          errors.New("failed to read"),
			ExpectedErrMsg: "unable to stream the build logs",
		},
	}

	scheme, codecFactory := apitesting.SchemeForOrDie(api.Install)

	for i, tc := range cases {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			out := &bytes.Buffer{}
			build := &buildv1.Build{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-build",
					Namespace: "test-namespace",
				},
			}
			// Set up dummy RESTClient to handle requests
			fakeREST := &restfake.RESTClient{
				NegotiatedSerializer: codecFactory,
				GroupVersion:         buildv1.GroupVersion,
				Client: restfake.CreateHTTPClient(func(*http.Request) (*http.Response, error) {
					if tc.RequestErr != nil {
						return nil, tc.RequestErr
					}
					var body io.Reader
					if tc.IOErr != nil {
						body = &failReader{
							Err: tc.IOErr,
						}
					} else {
						body = bytes.NewBufferString(tc.ExpectedLogMsg)
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       ioutil.NopCloser(body),
					}, nil
				}),
			}

			ioStreams, _, out, _ := genericclioptions.NewTestIOStreams()

			o := &StartBuildOptions{
				IOStreams:      ioStreams,
				BuildLogClient: buildclientmanual.NewBuildLogClient(fakeREST, build.Namespace, scheme),
			}

			err := o.streamBuildLogs(context.TODO(), build)
			if tc.RequestErr == nil && tc.IOErr == nil {
				if err != nil {
					t.Errorf("received unexpected error streaming build logs: %v", err)
				}
				if out.String() != tc.ExpectedLogMsg {
					t.Errorf("expected log \"%s\", got \"%s\"", tc.ExpectedLogMsg, out.String())
				}
			} else {
				if err == nil {
					t.Errorf("no error was received, expected error message: %s", tc.ExpectedErrMsg)
				} else if !strings.Contains(err.Error(), tc.ExpectedErrMsg) {
					t.Errorf("expected error message \"%s\", got \"%s\"", tc.ExpectedErrMsg, err)
				}
			}
		})
	}
}

func TestBuildConfigWithMissingResources(t *testing.T) {
	secretsPattern := regexp.MustCompile("Secrets: (.+)")
	cmPattern := regexp.MustCompile("Config Maps: (.+)")

	tests := []missingResourceTest{
		{
			name:      "missing-volumes-and-cms",
			namespace: "nsname",

			secrets:  []string{"secret-name-1", "secret-name-2", "secret-name-3"},
			cms:      []string{"config-map-1", "config-map-2", "config-map-3"},
			vSecrets: []string{"volume-secret-1", "volume-secret-2"},
			vCms:     []string{"volume-cm-1", "volume-cm-2"},

			createSecrets: []string{"secret-name-1", "secret-name-2", "volume-secret-1"},
			createCms:     []string{"config-map-1", "config-map-2", "volume-cm-1"},

			missSecrets: []string{"secret-name-3", "volume-secret-2"},
			missCms:     []string{"config-map-3", "volume-cm-2"},
		},
		{
			name:      "no-errors",
			namespace: "nsname",

			secrets:  []string{"secret-name-1", "secret-name-2", "secret-name-3"},
			cms:      []string{"config-map-1", "config-map-2", "config-map-3"},
			vSecrets: []string{"volume-secret-1", "volume-secret-2"},
			vCms:     []string{"volume-cm-1", "volume-cm-2"},

			createSecrets: []string{"secret-name-1", "secret-name-2", "secret-name-3", "volume-secret-1", "volume-secret-2"},
			createCms:     []string{"config-map-1", "config-map-2", "config-map-3", "volume-cm-1", "volume-cm-2"},

			missSecrets: []string{},
			missCms:     []string{},
		},
		{
			name:      "only-miss-secrets",
			namespace: "nsname",

			secrets:  []string{"secret-name-1", "secret-name-2", "secret-name-3"},
			cms:      []string{"config-map-1", "config-map-2", "config-map-3"},
			vSecrets: []string{"volume-secret-1", "volume-secret-2"},
			vCms:     []string{"volume-cm-1", "volume-cm-2"},

			createSecrets: []string{"secret-name-1", "secret-name-2", "volume-secret-1"},
			createCms:     []string{"config-map-1", "config-map-2", "config-map-3", "volume-cm-1", "volume-cm-2"},

			missSecrets: []string{"secret-name-3", "volume-secret-2"},
			missCms:     []string{},
		},
		{
			name:      "only-miss-config-maps",
			namespace: "nsname",

			secrets:  []string{"secret-name-1", "secret-name-2", "secret-name-3"},
			cms:      []string{"config-map-1", "config-map-2", "config-map-3"},
			vSecrets: []string{"volume-secret-1", "volume-secret-2"},
			vCms:     []string{"volume-cm-1", "volume-cm-2"},

			createSecrets: []string{"secret-name-1", "secret-name-2", "secret-name-3", "volume-secret-1", "volume-secret-2"},
			createCms:     []string{"config-map-1", "config-map-2", "volume-cm-1"},

			missSecrets: []string{},
			missCms:     []string{"config-map-3", "volume-cm-2"},
		},
	}

	allTests := []missingResourceTest{}

	for _, t := range tests {
		dockerTest, sourceTest := t, t

		dockerTest.name += "-docker"
		sourceTest.name += "-source"

		dockerTest.strategyType = buildv1.DockerBuildStrategyType
		sourceTest.strategyType = buildv1.SourceBuildStrategyType

		allTests = append(allTests, sourceTest, dockerTest)
	}

	for _, test := range allTests {
		bc := initializeBc(
			test.name,
			test.namespace,
			test.strategyType,
			test.secrets,
			test.cms,
			test.vSecrets,
			test.vCms)

		kubeObjects := initializeRuntimeObjects(
			test.namespace,
			test.createSecrets,
			test.createCms,
		)

		kubeClient := fake.NewSimpleClientset(kubeObjects...)
		buildClient := fakebuildclientset.NewSimpleClientset(bc).BuildV1()
		options := &StartBuildOptions{
			Name:        test.name,
			Namespace:   test.namespace,
			KubeClient:  kubeClient,
			BuildClient: buildClient,
		}

		err := options.checkNonExistantResources(context.TODO())
		if err != nil && len(test.missSecrets) == 0 && len(test.missCms) == 0 {
			t.Errorf("Unexpected error in bc %q: %v", test.name, err)
		}

		// if error exists and we are not expecting to miss any secrets
		// then error should not contain anything about them
		if err != nil && len(test.missSecrets) == 0 {
			secretsErr := secretsPattern.FindAllString(err.Error(), -1)
			if len(secretsErr) > 0 {
				t.Errorf("In bc %q error contains secrets which should not be missing: %v", test.name, err)
			}
		}
		// same situation as before, but with config maps here
		if err != nil && len(test.missCms) == 0 {
			cmsErr := cmPattern.FindAllString(err.Error(), -1)
			if len(cmsErr) > 0 {
				t.Errorf("In bc %q error contains ConfigMaps which should not be missing: %v", test.name, err)
			}
		}

		// we are expecting to be informed about missing secrets in the error
		if len(test.missSecrets) > 0 {
			found := secretsPattern.FindAllStringSubmatch(err.Error(), -1)
			if len(found) == 0 {
				t.Errorf("Secrets not found in bc %q, expected: %v", test.name, test.missSecrets)
			}

			// remove spaces, remove comas
			foundSecrets := strings.ReplaceAll(found[0][1], " ", "")
			fs := strings.Split(foundSecrets, ",")
			if len(fs) == 0 {
				// this is weird and should never happen
				// but we'll check anyways
				t.Errorf("Empty secrets list in bc %q, error: %v", test.name, err)
			}

			// just make sure that error contains all secrets that we expect it to contain and doesn't contain any extras
			if len(fs) != len(test.missSecrets) {
				t.Errorf("Wrong missing secrets in bc %q, expected %v, actual %v", test.name, test.missSecrets, err)
			}
			for _, sec := range test.missSecrets {
				if !findValueInSlice(sec, fs) {
					t.Errorf("In bc %q secret %q is not missing in error %v", test.name, sec, err)
				}
			}

		}

		// this is basically the same as secrets, but for config maps
		if len(test.missCms) > 0 {
			found := cmPattern.FindAllStringSubmatch(err.Error(), -1)
			if len(found) == 0 {
				t.Errorf("ConfigMaps not found in bc %q, expected: %v", test.name, test.missCms)
			}

			foundCms := strings.ReplaceAll(found[0][1], " ", "")
			fcm := strings.Split(foundCms, ",")
			if len(fcm) == 0 {
				t.Errorf("Empty ConfigMaps list in bc %q, error: %v", test.name, err)
			}
			if len(fcm) != len(test.missCms) {
				t.Errorf("Wrong missing ConfigMaps in bc %q, expected %v, actual %v", test.name, test.missCms, err)
			}

			for _, cm := range test.missCms {
				if !findValueInSlice(cm, fcm) {
					t.Errorf("In bc %q ConfigMap %q is not missing in error %v", test.name, cm, err)
				}
			}

		}
	}
}

func findValueInSlice(value string, where []string) bool {
	for _, elem := range where {
		if value == elem {
			return true
		}
	}

	return false
}

// initializeBCForTest creates BuildConfig and required Kubernetes resources for the
// TestBuildConfigWithMissingResources test
// It takes slices of secret and config map names as arguments and returns prepared objects
func initializeBc(name string, namespace string, strategyType buildv1.BuildStrategyType, secrets []string, cms []string, vSecrets []string, vCms []string) *buildv1.BuildConfig {
	// Create the skeleton BuildConfig
	buildConfig := &buildv1.BuildConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: buildv1.BuildConfigSpec{
			CommonSpec: buildv1.CommonSpec{
				Source:   buildv1.BuildSource{},
				Strategy: buildv1.BuildStrategy{},
			},
		},
	}

	// Create Secret and ConfigMap reference objects for the name provided to the function
	for _, s := range secrets {
		secret := buildv1.SecretBuildSource{
			Secret: corev1.LocalObjectReference{
				Name: s,
			},
		}
		buildConfig.Spec.Source.Secrets = append(buildConfig.Spec.Source.Secrets, secret)
	}
	for _, cmName := range cms {
		cm := buildv1.ConfigMapBuildSource{
			ConfigMap: corev1.LocalObjectReference{
				Name: cmName,
			},
		}
		buildConfig.Spec.Source.ConfigMaps = append(buildConfig.Spec.Source.ConfigMaps, cm)
	}

	// only need to set strategy for the test if we are going to work with volumes
	if len(vSecrets) > 0 || len(vCms) > 0 {
		switch strategyType {
		case buildv1.DockerBuildStrategyType:
			buildConfig.Spec.Strategy.Type = buildv1.DockerBuildStrategyType
			buildConfig.Spec.Strategy.DockerStrategy = &buildv1.DockerBuildStrategy{}
		case buildv1.SourceBuildStrategyType:
			buildConfig.Spec.Strategy.Type = buildv1.SourceBuildStrategyType
			buildConfig.Spec.Strategy.SourceStrategy = &buildv1.SourceBuildStrategy{}
		}
	}

	// Create BuildVolumes for BuildConfig
	for _, vs := range vSecrets {
		volume := buildv1.BuildVolume{
			Name: vs,
			Source: buildv1.BuildVolumeSource{
				Type: buildv1.BuildVolumeSourceTypeSecret,
				Secret: &corev1.SecretVolumeSource{
					SecretName: vs,
				},
			},
		}

		switch strategyType {
		case buildv1.DockerBuildStrategyType:
			buildConfig.Spec.CommonSpec.Strategy.DockerStrategy.Volumes =
				append(buildConfig.Spec.CommonSpec.Strategy.DockerStrategy.Volumes, volume)
		case buildv1.SourceBuildStrategyType:
			buildConfig.Spec.CommonSpec.Strategy.SourceStrategy.Volumes =
				append(buildConfig.Spec.CommonSpec.Strategy.SourceStrategy.Volumes, volume)
		}
	}

	// Add all the build volumes to the BuildConfig
	for _, vcm := range vCms {
		volume := buildv1.BuildVolume{
			Name: vcm,
			Source: buildv1.BuildVolumeSource{
				Type: buildv1.BuildVolumeSourceTypeConfigMap,
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: vcm,
					},
				},
			},
		}

		switch strategyType {
		case buildv1.DockerBuildStrategyType:
			buildConfig.Spec.CommonSpec.Strategy.DockerStrategy.Volumes =
				append(buildConfig.Spec.CommonSpec.Strategy.DockerStrategy.Volumes, volume)
		case buildv1.SourceBuildStrategyType:
			buildConfig.Spec.CommonSpec.Strategy.SourceStrategy.Volumes =
				append(buildConfig.Spec.CommonSpec.Strategy.SourceStrategy.Volumes, volume)
		}
	}

	return buildConfig
}

func initializeRuntimeObjects(namespace string, secrets []string, configMaps []string) []runtime.Object {
	runtimeObjects := []runtime.Object{}

	// Create Kube objects for secrets and config maps. We don't want to create all of them.
	// Still need some false positives, so we will not create last elements of each slice
	for _, s := range secrets {
		runtimeObjects = append(runtimeObjects, createSecret(s, namespace))
	}
	for _, cmName := range configMaps {
		runtimeObjects = append(runtimeObjects, createConfigMap(cmName, namespace))
	}

	return runtimeObjects
}

func createSecret(secret string, namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{"key": []byte("some value")},
	}
}

func createConfigMap(cm string, namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cm,
			Namespace: namespace,
		},
		Data: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}
}
