package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	githttp "github.com/AaronO/go-git-http"
	"github.com/AaronO/go-git-http/auth"
	"github.com/elazarl/goproxy"
	docker "github.com/fsouza/go-dockerclient"

	kappsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/apitesting"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktypes "k8s.io/apimachinery/pkg/types"
	utilerrs "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	kwatch "k8s.io/apimachinery/pkg/watch"
	krest "k8s.io/client-go/rest"
	clientgotesting "k8s.io/client-go/testing"

	"github.com/openshift/api"
	appsv1 "github.com/openshift/api/apps/v1"
	buildv1 "github.com/openshift/api/build/v1"
	dockerv10 "github.com/openshift/api/image/docker10"
	imagev1 "github.com/openshift/api/image/v1"
	fakeimagev1client "github.com/openshift/client-go/image/clientset/versioned/fake"
	imagev1typedclient "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	fakeroutev1client "github.com/openshift/client-go/route/clientset/versioned/fake"
	faketemplatev1client "github.com/openshift/client-go/template/clientset/versioned/fake"
	"github.com/openshift/library-go/pkg/git"
	dockerregistry "github.com/openshift/library-go/pkg/image/dockerv1client"
	newappapp "github.com/openshift/oc/pkg/cli/newapp"
	"github.com/openshift/oc/pkg/helpers/newapp"
	"github.com/openshift/oc/pkg/helpers/newapp/app"
	apptest "github.com/openshift/oc/pkg/helpers/newapp/app/test"
	"github.com/openshift/oc/pkg/helpers/newapp/cmd"
	"github.com/openshift/oc/pkg/helpers/newapp/dockerfile"
	"github.com/openshift/oc/pkg/helpers/newapp/jenkinsfile"
	"github.com/openshift/oc/pkg/helpers/newapp/source"

	s2igit "github.com/openshift/oc/pkg/helpers/source-to-image/git"
)

func skipExternalGit(t *testing.T) {
	if len(os.Getenv("SKIP_EXTERNAL_GIT")) > 0 {
		t.Skip("external Git tests are disabled")
	}
}

func TestNewAppAddArguments(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "test-newapp")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testDir := filepath.Join(tmpDir, "test/one/two/three")
	err = os.MkdirAll(testDir, 0777)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	tests := map[string]struct {
		args       []string
		env        []string
		parms      []string
		repos      []string
		components []string
		unknown    []string
	}{
		"components": {
			args:       []string{"one", "two+three", "four~five"},
			components: []string{"one", "two+three", "four~five"},
			unknown:    []string{},
		},
		"source": {
			args:    []string{".", testDir, "git://github.com/openshift/origin.git"},
			repos:   []string{".", testDir, "git://github.com/openshift/origin.git"},
			unknown: []string{},
		},
		"source custom ref": {
			args:    []string{"https://github.com/openshift/ruby-hello-world#beta4"},
			repos:   []string{"https://github.com/openshift/ruby-hello-world#beta4"},
			unknown: []string{},
		},
		"env": {
			args:    []string{"first=one", "second=two", "third=three"},
			env:     []string{"first=one", "second=two", "third=three"},
			unknown: []string{},
		},
		"mix 1": {
			args:       []string{"git://github.com/openshift/origin.git", "mysql+ruby~git@github.com/openshift/origin.git", "env1=test", "ruby-helloworld-sample"},
			repos:      []string{"git://github.com/openshift/origin.git"},
			components: []string{"mysql+ruby~git@github.com/openshift/origin.git", "ruby-helloworld-sample"},
			env:        []string{"env1=test"},
			unknown:    []string{},
		},
	}

	for n, c := range tests {
		a := &cmd.AppConfig{}
		a.EnvironmentClassificationErrors = map[string]cmd.ArgumentClassificationError{}
		a.SourceClassificationErrors = map[string]cmd.ArgumentClassificationError{}
		a.TemplateClassificationErrors = map[string]cmd.ArgumentClassificationError{}
		a.ComponentClassificationErrors = map[string]cmd.ArgumentClassificationError{}
		a.ClassificationWinners = map[string]cmd.ArgumentClassificationWinner{}
		unknown := a.AddArguments(c.args)
		if !reflect.DeepEqual(a.Environment, c.env) {
			t.Errorf("%s: Different env variables. Expected: %v, Actual: %v", n, c.env, a.Environment)
		}
		if !reflect.DeepEqual(a.SourceRepositories, c.repos) {
			t.Errorf("%s: Different source repos. Expected: %v, Actual: %v", n, c.repos, a.SourceRepositories)
		}
		if !reflect.DeepEqual(a.Components, c.components) {
			t.Errorf("%s: Different components. Expected: %v, Actual: %v", n, c.components, a.Components)
		}
		if !reflect.DeepEqual(unknown, c.unknown) {
			t.Errorf("%s: Different unknown result. Expected: %v, Actual: %v", n, c.unknown, unknown)
		}
	}

}

func TestNewAppResolve(t *testing.T) {
	tests := []struct {
		name        string
		cfg         cmd.AppConfig
		components  app.ComponentReferences
		expectedErr string
	}{
		{
			name: "Resolver error",
			components: app.ComponentReferences{
				app.ComponentReference(&app.ComponentInput{
					Value: "mysql:invalid",
					Resolver: app.UniqueExactOrInexactMatchResolver{
						Searcher: app.DockerRegistrySearcher{
							Client: dockerregistry.NewClient(10*time.Second, true),
						},
					},
				})},
			expectedErr: `unable to locate any`,
		},
		{
			name: "Successful mysql builder",
			components: app.ComponentReferences{
				app.ComponentReference(&app.ComponentInput{
					Value: "mysql",
					ResolvedMatch: &app.ComponentMatch{
						Builder: true,
					},
				})},
			expectedErr: "",
		},
		{
			name: "Unable to build source code",
			components: app.ComponentReferences{
				app.ComponentReference(&app.ComponentInput{
					Value:         "mysql",
					ExpectToBuild: true,
				})},
			expectedErr: "no resolver",
		},
		{
			name: "Successful docker build",
			cfg: cmd.AppConfig{
				GenerationInputs: cmd.GenerationInputs{
					Strategy: newapp.StrategyDocker,
				},
			},
			components: app.ComponentReferences{
				app.ComponentReference(&app.ComponentInput{
					Value:         "mysql",
					ExpectToBuild: true,
				})},
			expectedErr: "",
		},
	}

	for _, test := range tests {
		err := test.components.Resolve()
		if err != nil {
			if !strings.Contains(err.Error(), test.expectedErr) {
				t.Errorf("%s: Invalid error: Expected %s, got %v", test.name, test.expectedErr, err)
			}
		} else if len(test.expectedErr) != 0 {
			t.Errorf("%s: Expected %s error but got none", test.name, test.expectedErr)
		}
	}
}

func TestNewAppDetectSource(t *testing.T) {
	skipExternalGit(t)
	gitLocalDir, err := s2igit.CreateLocalGitDirectory()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(gitLocalDir)

	dockerSearcher := app.DockerRegistrySearcher{
		Client: dockerregistry.NewClient(10*time.Second, true),
	}
	mocks := MockSourceRepositories(t, gitLocalDir)
	tests := []struct {
		name         string
		cfg          *cmd.AppConfig
		repositories []*app.SourceRepository
		expectedLang string
		expectedErr  string
	}{
		{
			name: "detect source - ruby",
			cfg: &cmd.AppConfig{
				Resolvers: cmd.Resolvers{
					Detector: app.SourceRepositoryEnumerator{
						Detectors:         source.DefaultDetectors,
						DockerfileTester:  dockerfile.NewTester(),
						JenkinsfileTester: jenkinsfile.NewTester(),
					},
					DockerSearcher: dockerSearcher,
				},
			},
			repositories: []*app.SourceRepository{mocks[0]},
			expectedLang: "ruby",
			expectedErr:  "",
		},
	}

	for _, test := range tests {
		err := cmd.DetectSource(test.repositories, test.cfg.Detector, &test.cfg.GenerationInputs)
		if err != nil {
			if !strings.Contains(err.Error(), test.expectedErr) {
				t.Errorf("%s: Invalid error: Expected %s, got %v", test.name, test.expectedErr, err)
			}
		} else if len(test.expectedErr) != 0 {
			t.Errorf("%s: Expected %s error but got none", test.name, test.expectedErr)
		}

		for _, repo := range test.repositories {
			info := repo.Info()
			if info == nil {
				t.Errorf("%s: expected repository info to be populated; it is nil", test.name)
				continue
			}
			if term := strings.Join(info.Terms(), ","); term != test.expectedLang {
				t.Errorf("%s: expected repository info term to be %s; got %s\n", test.name, test.expectedLang, term)
			}
		}
	}
}

func mapContains(a, b map[string]string) bool {
	for k, v := range a {
		if v2, exists := b[k]; !exists || v != v2 {
			return false
		}
	}
	return true
}

// ExactMatchDockerSearcher returns a match with the value that was passed in
// and a march score of 0.0(exact)
type ExactMatchDockerSearcher struct {
	Errs []error
}

func (r *ExactMatchDockerSearcher) Type() string {
	return ""
}

// Search always returns a match for every term passed in
func (r *ExactMatchDockerSearcher) Search(precise bool, terms ...string) (app.ComponentMatches, []error) {
	matches := app.ComponentMatches{}
	for _, value := range terms {
		matches = append(matches, &app.ComponentMatch{
			Value:       value,
			Name:        value,
			Argument:    fmt.Sprintf("--docker-image=%q", value),
			Description: fmt.Sprintf("Docker image %q", value),
			Score:       0.0,
		})
	}
	return matches, r.Errs
}

// Some circular reference detection requires ImageStreams to
// be created with Tag support. The ExactMatchDirectTagDockerSearcher
// creates a Matcher which triggers the logic to enable tag support.
type ExactMatchDirectTagDockerSearcher struct {
	Errs []error
}

func (r *ExactMatchDirectTagDockerSearcher) Type() string {
	return ""
}

func (r *ExactMatchDirectTagDockerSearcher) Search(precise bool, terms ...string) (app.ComponentMatches, []error) {
	matches := app.ComponentMatches{}
	for _, value := range terms {
		matches = append(matches, &app.ComponentMatch{
			Value:       value,
			Name:        value,
			Argument:    fmt.Sprintf("--docker-image=%q", value),
			Description: fmt.Sprintf("Docker image %q", value),
			Score:       0.0,
			DockerImage: &dockerv10.DockerImage{},
			Meta:        map[string]string{"direct-tag": "1"},
		})
	}
	return matches, r.Errs
}

func TestNewAppRunAll(t *testing.T) {
	skipExternalGit(t)
	dockerSearcher := app.DockerRegistrySearcher{
		Client: dockerregistry.NewClient(10*time.Second, true),
	}
	failImageClient := fakeimagev1client.NewSimpleClientset()
	failImageClient.AddReactor("get", "images", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.NewInternalError(fmt.Errorf(""))
	})
	okTemplateClient := faketemplatev1client.NewSimpleClientset()
	okImageClient := fakeimagev1client.NewSimpleClientset()
	okRouteClient := fakeroutev1client.NewSimpleClientset()
	customScheme, _ := apitesting.SchemeForOrDie(api.Install, api.InstallKube)
	tests := []struct {
		name            string
		config          *cmd.AppConfig
		expected        map[string][]string
		expectedName    string
		expectedErr     error
		errFn           func(error) bool
		expectInsecure  sets.String
		expectedVolumes map[string]string
		checkPort       string
	}{
		{
			name: "successful ruby app generation",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{"https://github.com/openshift/ruby-hello-world"},
				},
				Resolvers: cmd.Resolvers{
					ImageStreamByAnnotationSearcher: app.NewImageStreamByAnnotationSearcher(okImageClient.ImageV1(), okImageClient.ImageV1(), []string{"default"}),
					TemplateSearcher: app.TemplateSearcher{
						Client:     okTemplateClient.TemplateV1(),
						Namespaces: []string{"openshift", "default"},
					},
					DockerSearcher: fakeDockerSearcher(),
					ImageStreamSearcher: app.ImageStreamSearcher{
						Client:     okImageClient.ImageV1(),
						Namespaces: []string{"default"},
					},
					Detector: app.SourceRepositoryEnumerator{
						Detectors:         source.DefaultDetectors,
						DockerfileTester:  dockerfile.NewTester(),
						JenkinsfileTester: jenkinsfile.NewTester(),
					},
				},
				GenerationInputs: cmd.GenerationInputs{
					Strategy: newapp.StrategySource,
				},
				Typer:           customScheme,
				ImageClient:     okImageClient.ImageV1(),
				TemplateClient:  okTemplateClient.TemplateV1(),
				RouteClient:     okRouteClient.RouteV1(),
				OriginNamespace: "default",
			},
			expected: map[string][]string{
				"imageStream": {"ruby-hello-world", "ruby"},
				"buildConfig": {"ruby-hello-world"},
				"deployment":  {"ruby-hello-world"},
				"service":     {"ruby-hello-world"},
			},
			expectedName:    "ruby-hello-world",
			expectedVolumes: nil,
			expectedErr:     nil,
		},
		{
			name: "successful ruby app generation - deployment config",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{"https://github.com/openshift/ruby-hello-world"},
				},
				Resolvers: cmd.Resolvers{
					ImageStreamByAnnotationSearcher: app.NewImageStreamByAnnotationSearcher(okImageClient.ImageV1(), okImageClient.ImageV1(), []string{"default"}),
					TemplateSearcher: app.TemplateSearcher{
						Client:     okTemplateClient.TemplateV1(),
						Namespaces: []string{"openshift", "default"},
					},
					DockerSearcher: fakeDockerSearcher(),
					ImageStreamSearcher: app.ImageStreamSearcher{
						Client:     okImageClient.ImageV1(),
						Namespaces: []string{"default"},
					},
					Detector: app.SourceRepositoryEnumerator{
						Detectors:         source.DefaultDetectors,
						DockerfileTester:  dockerfile.NewTester(),
						JenkinsfileTester: jenkinsfile.NewTester(),
					},
				},
				GenerationInputs: cmd.GenerationInputs{
					Strategy:         newapp.StrategySource,
					DeploymentConfig: true,
				},
				Typer:           customScheme,
				ImageClient:     okImageClient.ImageV1(),
				TemplateClient:  okTemplateClient.TemplateV1(),
				RouteClient:     okRouteClient.RouteV1(),
				OriginNamespace: "default",
			},
			expected: map[string][]string{
				"imageStream":      {"ruby-hello-world", "ruby"},
				"buildConfig":      {"ruby-hello-world"},
				"deploymentConfig": {"ruby-hello-world"},
				"service":          {"ruby-hello-world"},
			},
			expectedName:    "ruby-hello-world",
			expectedVolumes: nil,
			expectedErr:     nil,
		},
		{
			name: "successful ruby app generation with labels",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{"https://github.com/openshift/ruby-hello-world"},
				},

				Resolvers: cmd.Resolvers{
					DockerSearcher: fakeDockerSearcher(),
					ImageStreamSearcher: app.ImageStreamSearcher{
						Client:     okImageClient.ImageV1(),
						Namespaces: []string{"default"},
					},
					ImageStreamByAnnotationSearcher: app.NewImageStreamByAnnotationSearcher(okImageClient.ImageV1(), okImageClient.ImageV1(), []string{"default"}),
					TemplateSearcher: app.TemplateSearcher{
						Client:     okTemplateClient.TemplateV1(),
						Namespaces: []string{"openshift", "default"},
					},
					Detector: app.SourceRepositoryEnumerator{
						Detectors:         source.DefaultDetectors,
						DockerfileTester:  dockerfile.NewTester(),
						JenkinsfileTester: jenkinsfile.NewTester(),
					},
				},

				GenerationInputs: cmd.GenerationInputs{
					Strategy: newapp.StrategySource,
					Labels:   map[string]string{"label1": "value1", "label2": "value2"},
				},
				Typer:           customScheme,
				ImageClient:     okImageClient.ImageV1(),
				TemplateClient:  okTemplateClient.TemplateV1(),
				RouteClient:     okRouteClient.RouteV1(),
				OriginNamespace: "default",
			},
			expected: map[string][]string{
				"imageStream": {"ruby-hello-world", "ruby"},
				"buildConfig": {"ruby-hello-world"},
				"deployment":  {"ruby-hello-world"},
				"service":     {"ruby-hello-world"},
			},
			expectedName:    "ruby-hello-world",
			expectedVolumes: nil,
			expectedErr:     nil,
		},
		{
			name: "successful docker app generation",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{"https://github.com/openshift/ruby-hello-world"},
				},

				Resolvers: cmd.Resolvers{
					DockerSearcher: fakeSimpleDockerSearcher(),
					ImageStreamSearcher: app.ImageStreamSearcher{
						Client:     okImageClient.ImageV1(),
						Namespaces: []string{"default"},
					},
					ImageStreamByAnnotationSearcher: app.NewImageStreamByAnnotationSearcher(okImageClient.ImageV1(), okImageClient.ImageV1(), []string{"default"}),
					TemplateSearcher: app.TemplateSearcher{
						Client:     okTemplateClient.TemplateV1(),
						Namespaces: []string{"openshift", "default"},
					},
					Detector: app.SourceRepositoryEnumerator{
						Detectors:         source.DefaultDetectors,
						DockerfileTester:  dockerfile.NewTester(),
						JenkinsfileTester: jenkinsfile.NewTester(),
					},
				},
				GenerationInputs: cmd.GenerationInputs{
					Strategy: newapp.StrategyDocker,
				},
				Typer:           customScheme,
				ImageClient:     okImageClient.ImageV1(),
				TemplateClient:  okTemplateClient.TemplateV1(),
				RouteClient:     okRouteClient.RouteV1(),
				OriginNamespace: "default",
			},
			checkPort: "8080",
			expected: map[string][]string{
				"imageStream": {"ruby-hello-world", "ruby-25-centos7"},
				"buildConfig": {"ruby-hello-world"},
				"deployment":  {"ruby-hello-world"},
				"service":     {"ruby-hello-world"},
			},
			expectedName: "ruby-hello-world",
			expectedErr:  nil,
		},
		{
			name: "app generation using context dir",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{"https://github.com/openshift/sti-ruby"},
				},
				GenerationInputs: cmd.GenerationInputs{
					ContextDir: "2.0/test/rack-test-app",
				},

				Resolvers: cmd.Resolvers{
					DockerSearcher:                  dockerSearcher,
					ImageStreamSearcher:             fakeImageStreamSearcher(),
					ImageStreamByAnnotationSearcher: app.NewImageStreamByAnnotationSearcher(okImageClient.ImageV1(), okImageClient.ImageV1(), []string{"default"}),
					TemplateSearcher: app.TemplateSearcher{
						Client:     okTemplateClient.TemplateV1(),
						Namespaces: []string{"openshift", "default"},
					},
					Detector: app.SourceRepositoryEnumerator{
						Detectors:         source.DefaultDetectors,
						DockerfileTester:  dockerfile.NewTester(),
						JenkinsfileTester: jenkinsfile.NewTester(),
					},
				},

				Typer:           customScheme,
				ImageClient:     okImageClient.ImageV1(),
				TemplateClient:  okTemplateClient.TemplateV1(),
				RouteClient:     okRouteClient.RouteV1(),
				OriginNamespace: "default",
			},
			expected: map[string][]string{
				"imageStream": {"sti-ruby"},
				"buildConfig": {"sti-ruby"},
				"deployment":  {"sti-ruby"},
				"service":     {"sti-ruby"},
			},
			expectedName:    "sti-ruby",
			expectedVolumes: nil,
			expectedErr:     nil,
		},
		{
			name: "failed app generation using missing context dir",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{"https://github.com/openshift/sti-ruby"},
				},
				GenerationInputs: cmd.GenerationInputs{
					ContextDir: "2.0/test/missing-dir",
				},

				Resolvers: cmd.Resolvers{
					DockerSearcher:                  dockerSearcher,
					ImageStreamSearcher:             fakeImageStreamSearcher(),
					ImageStreamByAnnotationSearcher: app.NewImageStreamByAnnotationSearcher(okImageClient.ImageV1(), okImageClient.ImageV1(), []string{"default"}),
					TemplateSearcher: app.TemplateSearcher{
						Client:     okTemplateClient.TemplateV1(),
						Namespaces: []string{"openshift", "default"},
					},
					Detector: app.SourceRepositoryEnumerator{
						Detectors:         source.DefaultDetectors,
						DockerfileTester:  dockerfile.NewTester(),
						JenkinsfileTester: jenkinsfile.NewTester(),
					},
				},

				Typer:           customScheme,
				ImageClient:     okImageClient.ImageV1(),
				TemplateClient:  okTemplateClient.TemplateV1(),
				RouteClient:     okRouteClient.RouteV1(),
				OriginNamespace: "default",
			},
			expected: map[string][]string{
				"imageStream": {"sti-ruby"},
				"buildConfig": {"sti-ruby"},
				"deployment":  {"sti-ruby"},
				"service":     {"sti-ruby"},
			},
			expectedName:    "sti-ruby",
			expectedVolumes: nil,
			errFn: func(err error) bool {
				return err.Error() == "supplied context directory '2.0/test/missing-dir' does not exist in 'https://github.com/openshift/sti-ruby'"
			},
		},

		{
			name: "insecure registry generation",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					Components:         []string{"myrepo:5000/myco/example"},
					SourceRepositories: []string{"https://github.com/openshift/ruby-hello-world"},
				},
				GenerationInputs: cmd.GenerationInputs{
					Strategy:         newapp.StrategySource,
					InsecureRegistry: true,
				},

				Resolvers: cmd.Resolvers{
					DockerSearcher: app.DockerClientSearcher{
						Client: &apptest.FakeDockerClient{
							Images: []docker.APIImages{{RepoTags: []string{"myrepo:5000/myco/example"}}},
							Image:  dockerBuilderImage(),
						},
						Insecure:         true,
						RegistrySearcher: &ExactMatchDockerSearcher{},
					},
					ImageStreamSearcher: app.ImageStreamSearcher{
						Client:     okImageClient.ImageV1(),
						Namespaces: []string{"default"},
					},
					TemplateSearcher: app.TemplateSearcher{
						Client:     okTemplateClient.TemplateV1(),
						Namespaces: []string{},
					},
					Detector: app.SourceRepositoryEnumerator{
						Detectors:         source.DefaultDetectors,
						DockerfileTester:  dockerfile.NewTester(),
						JenkinsfileTester: jenkinsfile.NewTester(),
					},
				},
				Typer:           customScheme,
				ImageClient:     okImageClient.ImageV1(),
				TemplateClient:  okTemplateClient.TemplateV1(),
				RouteClient:     okRouteClient.RouteV1(),
				OriginNamespace: "default",
			},
			expected: map[string][]string{
				"imageStream": {"example", "ruby-hello-world"},
				"buildConfig": {"ruby-hello-world"},
				"deployment":  {"ruby-hello-world"},
				"service":     {"ruby-hello-world"},
			},
			expectedName:    "ruby-hello-world",
			expectedErr:     nil,
			expectedVolumes: nil,
			expectInsecure:  sets.NewString("example"),
		},
		{
			name: "emptyDir volumes",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					DockerImages: []string{"mysql"},
				},

				Resolvers: cmd.Resolvers{
					DockerSearcher: dockerSearcher,
					ImageStreamSearcher: app.ImageStreamSearcher{
						Client:     okImageClient.ImageV1(),
						Namespaces: []string{"default"},
					},
					TemplateSearcher: app.TemplateSearcher{
						Client:     okTemplateClient.TemplateV1(),
						Namespaces: []string{"openshift", "default"},
					},
					Detector: app.SourceRepositoryEnumerator{
						Detectors:         source.DefaultDetectors,
						DockerfileTester:  dockerfile.NewTester(),
						JenkinsfileTester: jenkinsfile.NewTester(),
					},
				},

				Typer:           customScheme,
				ImageClient:     okImageClient.ImageV1(),
				TemplateClient:  okTemplateClient.TemplateV1(),
				RouteClient:     okRouteClient.RouteV1(),
				OriginNamespace: "default",
			},

			expected: map[string][]string{
				"imageStream":  {"mysql"},
				"deployment":   {"mysql"},
				"service":      {"mysql"},
				"volumeMounts": {"mysql-volume-1"},
			},
			expectedName: "mysql",
			expectedVolumes: map[string]string{
				"mysql-volume-1": "EmptyDir",
			},
			expectedErr: nil,
		},
		{
			name: "Docker build",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{"https://github.com/openshift/ruby-hello-world"},
				},

				Resolvers: cmd.Resolvers{
					DockerSearcher: app.DockerClientSearcher{
						Client: &apptest.FakeDockerClient{
							Images: []docker.APIImages{{RepoTags: []string{"centos/ruby-25-centos7"}}},
							Image:  dockerBuilderImage(),
						},
						Insecure:         true,
						RegistrySearcher: &ExactMatchDockerSearcher{},
					},
					ImageStreamSearcher: app.ImageStreamSearcher{
						Client:     okImageClient.ImageV1(),
						Namespaces: []string{"default"},
					},
					ImageStreamByAnnotationSearcher: app.NewImageStreamByAnnotationSearcher(okImageClient.ImageV1(), okImageClient.ImageV1(), []string{"default"}),
					TemplateSearcher: app.TemplateSearcher{
						Client:     okTemplateClient.TemplateV1(),
						Namespaces: []string{"openshift", "default"},
					},
					Detector: app.SourceRepositoryEnumerator{
						Detectors:         source.DefaultDetectors,
						DockerfileTester:  dockerfile.NewTester(),
						JenkinsfileTester: jenkinsfile.NewTester(),
					},
				},
				Typer:           customScheme,
				ImageClient:     okImageClient.ImageV1(),
				TemplateClient:  okTemplateClient.TemplateV1(),
				RouteClient:     okRouteClient.RouteV1(),
				OriginNamespace: "default",
			},
			expected: map[string][]string{
				"imageStream": {"ruby-hello-world", "ruby-25-centos7"},
				"buildConfig": {"ruby-hello-world"},
				"deployment":  {"ruby-hello-world"},
				"service":     {"ruby-hello-world"},
			},
			expectedName: "ruby-hello-world",
			expectedErr:  nil,
		},
		{
			name: "Docker build with no registry image",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{"https://github.com/openshift/ruby-hello-world"},
				},

				Resolvers: cmd.Resolvers{
					DockerSearcher: app.DockerClientSearcher{
						Client: &apptest.FakeDockerClient{
							Images: []docker.APIImages{{RepoTags: []string{"centos/ruby-25-centos7"}}},
							Image:  dockerBuilderImage(),
						},
						Insecure: true,
					},
					ImageStreamSearcher: app.ImageStreamSearcher{
						Client:     okImageClient.ImageV1(),
						Namespaces: []string{"default"},
					},
					ImageStreamByAnnotationSearcher: app.NewImageStreamByAnnotationSearcher(okImageClient.ImageV1(), okImageClient.ImageV1(), []string{"default"}),
					TemplateSearcher: app.TemplateSearcher{
						Client:     okTemplateClient.TemplateV1(),
						Namespaces: []string{"openshift", "default"},
					},
					Detector: app.SourceRepositoryEnumerator{
						Detectors:         source.DefaultDetectors,
						DockerfileTester:  dockerfile.NewTester(),
						JenkinsfileTester: jenkinsfile.NewTester(),
					},
				},
				Typer:           customScheme,
				ImageClient:     okImageClient.ImageV1(),
				TemplateClient:  okTemplateClient.TemplateV1(),
				RouteClient:     okRouteClient.RouteV1(),
				OriginNamespace: "default",
			},
			expected: map[string][]string{
				"imageStream": {"ruby-hello-world"},
				"buildConfig": {"ruby-hello-world"},
				"deployment":  {"ruby-hello-world"},
				"service":     {"ruby-hello-world"},
			},
			expectedName: "ruby-hello-world",
			expectedErr:  nil,
		},
		{
			name: "custom name",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					DockerImages: []string{"mysql"},
				},
				GenerationInputs: cmd.GenerationInputs{
					Name: "custom",
				},
				Resolvers: cmd.Resolvers{
					DockerSearcher: app.DockerClientSearcher{
						Client: &apptest.FakeDockerClient{
							Images: []docker.APIImages{{RepoTags: []string{"mysql"}}},
							Image: &docker.Image{
								Config: &docker.Config{
									ExposedPorts: map[docker.Port]struct{}{
										"8080/tcp": {},
									},
								},
							},
						},
						RegistrySearcher: &ExactMatchDockerSearcher{},
					},
					ImageStreamSearcher: app.ImageStreamSearcher{
						Client:     okImageClient.ImageV1(),
						Namespaces: []string{"default"},
					},
					TemplateSearcher: app.TemplateSearcher{
						Client:     okTemplateClient.TemplateV1(),
						Namespaces: []string{"openshift", "default"},
					},
				},
				Typer:           customScheme,
				ImageClient:     okImageClient.ImageV1(),
				TemplateClient:  okTemplateClient.TemplateV1(),
				RouteClient:     okRouteClient.RouteV1(),
				OriginNamespace: "default",
			},
			expected: map[string][]string{
				"imageStream": {"custom"},
				"deployment":  {"custom"},
				"service":     {"custom"},
			},
			expectedName: "custom",
			expectedErr:  nil,
		},
		{
			name: "partial matches",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					DockerImages: []string{"mysql"},
				},
				GenerationInputs: cmd.GenerationInputs{
					Name: "custom",
				},
				Resolvers: cmd.Resolvers{
					DockerSearcher: app.DockerClientSearcher{
						RegistrySearcher: &ExactMatchDockerSearcher{Errs: []error{errors.NewInternalError(fmt.Errorf("test error"))}},
					},
					ImageStreamSearcher: app.ImageStreamSearcher{
						Client:     failImageClient.ImageV1(),
						Namespaces: []string{"default"},
					},
					TemplateSearcher: app.TemplateSearcher{
						Client:     okTemplateClient.TemplateV1(),
						Namespaces: []string{"openshift", "default"},
					},
				},
				Typer:           customScheme,
				ImageClient:     okImageClient.ImageV1(),
				TemplateClient:  okTemplateClient.TemplateV1(),
				RouteClient:     okRouteClient.RouteV1(),
				OriginNamespace: "default",
			},
			expected: map[string][]string{
				"imageStream": {"custom"},
				"deployment":  {"custom"},
				"service":     {"custom"},
			},
			expectedName: "custom",
			errFn: func(err error) bool {
				err = err.(utilerrs.Aggregate).Errors()[0]
				match, ok := err.(app.ErrNoMatch)
				if !ok {
					return false
				}
				if match.Value != "mysql" {
					return false
				}
				t.Logf("%#v", match.Errs[0])
				return len(match.Errs) == 1 && strings.Contains(match.Errs[0].Error(), "test error")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.config.Out, test.config.ErrOut = os.Stdout, os.Stderr
			test.config.Deploy = true
			test.config.ImageClient = &NewAppFakeImageClient{
				proxy: test.config.ImageClient,
			}
			res, err := test.config.Run()
			if test.errFn != nil {
				if !test.errFn(err) {
					t.Errorf("%s: Error mismatch! Unexpected error: %#v", test.name, err)
					return
				}
			} else if err != test.expectedErr {
				t.Errorf("%s: Error mismatch! Expected %v, got %v", test.name, test.expectedErr, err)
				return
			}
			if err != nil {
				return
			}
			if res.Name != test.expectedName {
				t.Errorf("%s: Name was not correct: %v", test.name, res.Name)
				return
			}
			imageStreams := []*imagev1.ImageStream{}
			got := map[string][]string{}
			gotVolumes := map[string]string{}
			for _, obj := range res.List.Items {
				switch tp := obj.(type) {
				case *buildv1.BuildConfig:
					got["buildConfig"] = append(got["buildConfig"], tp.Name)
				case *corev1.Service:
					if test.checkPort != "" {
						if len(tp.Spec.Ports) == 0 {
							t.Errorf("%s: did not get any ports in service", test.name)
							break
						}
						expectedPort, _ := strconv.Atoi(test.checkPort)
						if tp.Spec.Ports[0].Port != int32(expectedPort) {
							t.Errorf("%s: did not get expected port in service. Expected: %d. Got %d\n",
								test.name, expectedPort, tp.Spec.Ports[0].Port)
						}
					}
					if test.config.Labels != nil {
						if !mapContains(test.config.Labels, tp.Spec.Selector) {
							t.Errorf("%s: did not get expected service selector. Expected: %v. Got: %v",
								test.name, test.config.Labels, tp.Spec.Selector)
						}
					}
					got["service"] = append(got["service"], tp.Name)
				case *imagev1.ImageStream:
					got["imageStream"] = append(got["imageStream"], tp.Name)
					imageStreams = append(imageStreams, tp)
				case *kappsv1.Deployment:
					got["deployment"] = append(got["deployment"], tp.Name)
					podTemplate := tp.Spec.Template
					for _, volume := range podTemplate.Spec.Volumes {
						if volume.VolumeSource.EmptyDir != nil {
							gotVolumes[volume.Name] = "EmptyDir"
						} else {
							gotVolumes[volume.Name] = "UNKNOWN"
						}
					}
					for _, container := range podTemplate.Spec.Containers {
						for _, volumeMount := range container.VolumeMounts {
							got["volumeMounts"] = append(got["volumeMounts"], volumeMount.Name)
						}
					}
					if test.config.Labels != nil {
						if !mapContains(test.config.Labels, tp.Spec.Template.Labels) {
							t.Errorf("%s: did not get expected deployment r selector. Expected: %v. Got: %v",
								test.name, test.config.Labels, tp.Spec.Selector)
						}
					}
				case *appsv1.DeploymentConfig:
					got["deploymentConfig"] = append(got["deploymentConfig"], tp.Name)
					if podTemplate := tp.Spec.Template; podTemplate != nil {
						for _, volume := range podTemplate.Spec.Volumes {
							if volume.VolumeSource.EmptyDir != nil {
								gotVolumes[volume.Name] = "EmptyDir"
							} else {
								gotVolumes[volume.Name] = "UNKNOWN"
							}
						}
						for _, container := range podTemplate.Spec.Containers {
							for _, volumeMount := range container.VolumeMounts {
								got["volumeMounts"] = append(got["volumeMounts"], volumeMount.Name)
							}
						}
					}
					if test.config.Labels != nil {
						if !mapContains(test.config.Labels, tp.Spec.Selector) {
							t.Errorf("%s: did not get expected deployment config rc selector. Expected: %v. Got: %v",
								test.name, test.config.Labels, tp.Spec.Selector)
						}
					}
				}
			}

			if len(test.expected) != len(got) {
				t.Errorf("%s: Resource kind size mismatch! Expected %d, got %d", test.name, len(test.expected), len(got))
				return
			}
			for k, exp := range test.expected {
				g, ok := got[k]
				if !ok {
					t.Errorf("%s: Didn't find expected kind %s", test.name, k)
				}

				sort.Strings(g)
				sort.Strings(exp)

				if !reflect.DeepEqual(g, exp) {
					t.Errorf("%s: %s resource names mismatch! Expected %v, got %v", test.name, k, exp, g)
					continue
				}
			}

			if len(test.expectedVolumes) != len(gotVolumes) {
				t.Errorf("%s: Volume count mismatch! Expected %d, got %d", test.name, len(test.expectedVolumes), len(gotVolumes))
				return
			}
			for k, exp := range test.expectedVolumes {
				g, ok := gotVolumes[k]
				if !ok {
					t.Errorf("%s: Didn't find expected volume %s", test.name, k)
				}

				if g != exp {
					t.Errorf("%s: Expected volume of type %s, got %s", test.name, g, exp)
				}
			}

			if test.expectedName != res.Name {
				t.Errorf("%s: Unexpected name: %s", test.name, test.expectedName)
			}

			if test.expectInsecure == nil {
				return
			}
			for _, stream := range imageStreams {
				_, hasAnnotation := stream.Annotations[imagev1.InsecureRepositoryAnnotation]
				if test.expectInsecure.Has(stream.Name) && !hasAnnotation {
					t.Errorf("%s: Expected insecure annotation for stream: %s, but did not get one.", test.name, stream.Name)
				}
				if !test.expectInsecure.Has(stream.Name) && hasAnnotation {
					t.Errorf("%s: Got insecure annotation for stream: %s, and was not expecting one.", test.name, stream.Name)
				}
			}
		})

	}
}

func TestNewAppRunBuilds(t *testing.T) {
	skipExternalGit(t)
	tests := []struct {
		name   string
		config *cmd.AppConfig

		expected    map[string][]string
		expectedErr func(error) bool
		checkResult func(*cmd.AppResult) error
		checkOutput func(stdout, stderr io.Reader) error
	}{
		{
			name: "successful build from dockerfile",
			config: &cmd.AppConfig{
				GenerationInputs: cmd.GenerationInputs{
					Dockerfile: "FROM openshift/origin:v1.0.6\nUSER foo",
				},
			},
			expected: map[string][]string{
				"buildConfig": {"origin"},
				// There's a single image stream, but different tags: input from
				// openshift/origin:v1.0.6, output to openshift/origin:latest.
				"imageStream": {"origin"},
			},
		},
		{
			name: "successful ruby app generation",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{"https://github.com/openshift/ruby-hello-world"},
					DockerImages:       []string{"centos/ruby-25-centos7", "openshift/nodejs-010-centos7"},
				},
				GenerationInputs: cmd.GenerationInputs{
					OutputDocker: true,
				},
			},
			expected: map[string][]string{
				// TODO: this test used to silently ignore components that were not builders (i.e. user input)
				//   That's bad, so the code should either error in this case or be a bit smarter.
				"buildConfig": {"ruby-hello-world", "ruby-hello-world-1"},
				"imageStream": {"nodejs-010-centos7", "ruby-25-centos7"},
			},
		},
		{
			name: "successful build with no output",
			config: &cmd.AppConfig{
				GenerationInputs: cmd.GenerationInputs{
					Dockerfile: "FROM centos",
					NoOutput:   true,
				},
			},
			expected: map[string][]string{
				"buildConfig": {"centos"},
				"imageStream": {"centos"},
			},
			checkResult: func(res *cmd.AppResult) error {
				for _, item := range res.List.Items {
					switch t := item.(type) {
					case *buildv1.BuildConfig:
						got := t.Spec.Output.To
						want := (*corev1.ObjectReference)(nil)
						if !reflect.DeepEqual(got, want) {
							return fmt.Errorf("build.Spec.Output.To = %v; want %v", got, want)
						}
						return nil
					}
				}
				return fmt.Errorf("BuildConfig not found; got %v", res.List.Items)
			},
		},
		{
			name: "successful build from dockerfile with custom name",
			config: &cmd.AppConfig{
				GenerationInputs: cmd.GenerationInputs{
					Dockerfile: "FROM openshift/origin-base\nUSER foo",
					Name:       "foobar",
				},
			},
			expected: map[string][]string{
				"buildConfig": {"foobar"},
				"imageStream": {"origin-base", "foobar"},
			},
		},
		{
			name: "successful build from dockerfile with --to",
			config: &cmd.AppConfig{
				GenerationInputs: cmd.GenerationInputs{
					Dockerfile: "FROM openshift/origin-base\nUSER foo",
					Name:       "foobar",
					To:         "destination/reference:tag",
				},
			},
			expected: map[string][]string{
				"buildConfig": {"foobar"},
				"imageStream": {"origin-base", "reference"},
			},
		},
		{
			name: "successful build from dockerfile with --to and --to-docker=true",
			config: &cmd.AppConfig{
				GenerationInputs: cmd.GenerationInputs{
					Dockerfile:   "FROM openshift/origin-base\nUSER foo",
					Name:         "foobar",
					To:           "destination/reference:tag",
					OutputDocker: true,
				},
			},
			expected: map[string][]string{
				"buildConfig": {"foobar"},
				"imageStream": {"origin-base"},
			},
			checkResult: func(res *cmd.AppResult) error {
				for _, item := range res.List.Items {
					switch t := item.(type) {
					case *buildv1.BuildConfig:
						got := t.Spec.Output.To
						want := &corev1.ObjectReference{
							Kind: "DockerImage",
							Name: "destination/reference:tag",
						}
						if !reflect.DeepEqual(got, want) {
							return fmt.Errorf("build.Spec.Output.To = %v; want %v", got, want)
						}
						return nil
					}
				}
				return fmt.Errorf("BuildConfig not found; got %v", res.List.Items)
			},
		},
		{
			name: "successful generation of BC with multiple sources: repo + Dockerfile",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{"https://github.com/openshift/ruby-hello-world"},
				},
				GenerationInputs: cmd.GenerationInputs{
					Dockerfile: "FROM centos/ruby-25-centos7\nRUN false",
				},
			},
			expected: map[string][]string{
				"buildConfig": {"ruby-hello-world"},
				"imageStream": {"ruby-25-centos7", "ruby-hello-world"},
			},
			checkResult: func(res *cmd.AppResult) error {
				var bc *buildv1.BuildConfig
				for _, item := range res.List.Items {
					switch v := item.(type) {
					case *buildv1.BuildConfig:
						if bc != nil {
							return fmt.Errorf("want one BuildConfig got multiple: %#v", res.List.Items)
						}
						bc = v
					}
				}
				if bc == nil {
					return fmt.Errorf("want one BuildConfig got none: %#v", res.List.Items)
				}
				var got string
				if bc.Spec.Source.Dockerfile != nil {
					got = *bc.Spec.Source.Dockerfile
				}
				want := "FROM centos/ruby-25-centos7\nRUN false"
				if got != want {
					return fmt.Errorf("bc.Spec.Source.Dockerfile = %q; want %q", got, want)
				}
				return nil
			},
		},
		{
			name: "unsuccessful build from dockerfile due to strategy conflict",
			config: &cmd.AppConfig{
				GenerationInputs: cmd.GenerationInputs{
					Dockerfile: "FROM openshift/origin-base\nUSER foo",
					Strategy:   newapp.StrategySource,
				},
			},
			expectedErr: func(err error) bool {
				return err.Error() == "when directly referencing a Dockerfile, the strategy must must be 'docker'"
			},
		},
		{
			name: "unsuccessful build from dockerfile due to missing FROM instruction",
			config: &cmd.AppConfig{
				GenerationInputs: cmd.GenerationInputs{
					Dockerfile: "USER foo",
					Strategy:   newapp.StrategyDocker,
				},
			},
			expectedErr: func(err error) bool {
				return err.Error() == "the Dockerfile in the repository \"\" has no FROM instruction"
			},
		},
		{
			name: "unsuccessful generation of BC with multiple repos and Dockerfile",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{
						"https://github.com/openshift/ruby-hello-world",
						"https://github.com/sclorg/django-ex",
					},
				},
				GenerationInputs: cmd.GenerationInputs{
					Dockerfile: "FROM centos/ruby-25-centos7\nRUN false",
				},
			},
			expectedErr: func(err error) bool {
				return err.Error() == "--dockerfile cannot be used with multiple source repositories"
			},
		},
		{
			name: "successful input image source build with a repository",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{
						"https://github.com/openshift/ruby-hello-world",
					},
				},
				GenerationInputs: cmd.GenerationInputs{
					SourceImage:     "centos/mongodb-26-centos7",
					SourceImagePath: "/src:dst",
				},
			},
			expected: map[string][]string{
				"buildConfig": {"ruby-hello-world"},
				"imageStream": {"mongodb-26-centos7", "ruby-25-centos7", "ruby-hello-world"},
			},
			checkResult: func(res *cmd.AppResult) error {
				var bc *buildv1.BuildConfig
				for _, item := range res.List.Items {
					switch v := item.(type) {
					case *buildv1.BuildConfig:
						if bc != nil {
							return fmt.Errorf("want one BuildConfig got multiple: %#v", res.List.Items)
						}
						bc = v
					}
				}
				if bc == nil {
					return fmt.Errorf("want one BuildConfig got none: %#v", res.List.Items)
				}
				var got string

				want := "mongodb-26-centos7:latest"
				got = bc.Spec.Source.Images[0].From.Name
				if got != want {
					return fmt.Errorf("bc.Spec.Source.Image.From.Name = %q; want %q", got, want)
				}

				want = "ImageStreamTag"
				got = bc.Spec.Source.Images[0].From.Kind
				if got != want {
					return fmt.Errorf("bc.Spec.Source.Image.From.Kind = %q; want %q", got, want)
				}

				want = "/src"
				got = bc.Spec.Source.Images[0].Paths[0].SourcePath
				if got != want {
					return fmt.Errorf("bc.Spec.Source.Image.Paths[0].SourcePath = %q; want %q", got, want)
				}

				want = "dst"
				got = bc.Spec.Source.Images[0].Paths[0].DestinationDir
				if got != want {
					return fmt.Errorf("bc.Spec.Source.Image.Paths[0].DestinationDir = %q; want %q", got, want)
				}
				return nil
			},
		},
		{
			name: "successful input image source build with no repository",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					Components: []string{"openshift/nodejs-010-centos7"},
				},
				GenerationInputs: cmd.GenerationInputs{
					To:              "outputimage",
					SourceImage:     "centos/mongodb-26-centos7",
					SourceImagePath: "/src:dst",
				},
			},
			expected: map[string][]string{
				"buildConfig": {"outputimage"},
				"imageStream": {"mongodb-26-centos7", "nodejs-010-centos7", "outputimage"},
			},
			checkResult: func(res *cmd.AppResult) error {
				var bc *buildv1.BuildConfig
				for _, item := range res.List.Items {
					switch v := item.(type) {
					case *buildv1.BuildConfig:
						if bc != nil {
							return fmt.Errorf("want one BuildConfig got multiple: %#v", res.List.Items)
						}
						bc = v
					}
				}
				if bc == nil {
					return fmt.Errorf("want one BuildConfig got none: %#v", res.List.Items)
				}
				var got string

				want := "mongodb-26-centos7:latest"
				got = bc.Spec.Source.Images[0].From.Name
				if got != want {
					return fmt.Errorf("bc.Spec.Source.Image.From.Name = %q; want %q", got, want)
				}

				want = "ImageStreamTag"
				got = bc.Spec.Source.Images[0].From.Kind
				if got != want {
					return fmt.Errorf("bc.Spec.Source.Image.From.Kind = %q; want %q", got, want)
				}

				want = "/src"
				got = bc.Spec.Source.Images[0].Paths[0].SourcePath
				if got != want {
					return fmt.Errorf("bc.Spec.Source.Image.Paths[0].SourcePath = %q; want %q", got, want)
				}

				want = "dst"
				got = bc.Spec.Source.Images[0].Paths[0].DestinationDir
				if got != want {
					return fmt.Errorf("bc.Spec.Source.Image.Paths[0].DestinationDir = %q; want %q", got, want)
				}
				return nil
			},
		},
		{
			name: "successful build from source with autodetected jenkinsfile",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{
						"https://github.com/sclorg/nodejs-ex",
					},
				},
				GenerationInputs: cmd.GenerationInputs{
					ContextDir: "openshift/pipeline",
				},
			},
			expected: map[string][]string{
				"buildConfig": {"nodejs-ex"},
			},
			checkResult: func(res *cmd.AppResult) error {
				if len(res.List.Items) != 1 {
					return fmt.Errorf("expected one Item returned")
				}
				bc, ok := res.List.Items[0].(*buildv1.BuildConfig)
				if !ok {
					return fmt.Errorf("expected Item of type *buildv1.BuildConfig")
				}
				if !reflect.DeepEqual(bc.Spec.Output, buildv1.BuildOutput{}) {
					return fmt.Errorf("invalid bc.Spec.Output, got %#v", bc.Spec.Output)
				}
				if !reflect.DeepEqual(bc.Spec.Source, buildv1.BuildSource{
					ContextDir: "openshift/pipeline",
					Git:        &buildv1.GitBuildSource{URI: "https://github.com/sclorg/nodejs-ex"},
					Secrets:    []buildv1.SecretBuildSource{},
					ConfigMaps: []buildv1.ConfigMapBuildSource{},
					Type:       "Git",
				}) {
					return fmt.Errorf("invalid bc.Spec.Source, got %#v", bc.Spec.Source)
				}
				if !reflect.DeepEqual(bc.Spec.Strategy, buildv1.BuildStrategy{JenkinsPipelineStrategy: &buildv1.JenkinsPipelineBuildStrategy{
					Env: []corev1.EnvVar{}},
					Type: "JenkinsPipeline",
				}) {
					return fmt.Errorf("invalid bc.Spec.Strategy, got %#v", bc.Spec.Strategy)
				}
				return nil
			},
		},
		{
			name: "successful build from component with source with pipeline strategy",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					Components: []string{
						"centos/nodejs-4-centos7~https://github.com/sclorg/nodejs-ex",
					},
				},
				GenerationInputs: cmd.GenerationInputs{
					ContextDir: "openshift/pipeline",
					Strategy:   newapp.StrategyPipeline,
				},
			},
			expected: map[string][]string{
				"buildConfig": {"nodejs-ex"},
			},
			checkResult: func(res *cmd.AppResult) error {
				if len(res.List.Items) != 1 {
					return fmt.Errorf("expected one Item returned")
				}
				bc, ok := res.List.Items[0].(*buildv1.BuildConfig)
				if !ok {
					return fmt.Errorf("expected Item of type *buildv1.BuildConfig")
				}
				if !reflect.DeepEqual(bc.Spec.Output, buildv1.BuildOutput{}) {
					return fmt.Errorf("invalid bc.Spec.Output, got %#v", bc.Spec.Output)
				}
				if !reflect.DeepEqual(bc.Spec.Source, buildv1.BuildSource{
					ContextDir: "openshift/pipeline",
					Git:        &buildv1.GitBuildSource{URI: "https://github.com/sclorg/nodejs-ex"},
					Secrets:    []buildv1.SecretBuildSource{},
					ConfigMaps: []buildv1.ConfigMapBuildSource{},
					Type:       "Git",
				}) {
					return fmt.Errorf("invalid bc.Spec.Source, got %#v", bc.Spec.Source.Git)
				}
				if !reflect.DeepEqual(bc.Spec.Strategy, buildv1.BuildStrategy{JenkinsPipelineStrategy: &buildv1.JenkinsPipelineBuildStrategy{
					Env: []corev1.EnvVar{}},
					Type: "JenkinsPipeline",
				}) {
					return fmt.Errorf("invalid bc.Spec.Strategy, got %#v", bc.Spec.Strategy)
				}
				return nil
			},
		},
		{
			name: "successful build from source with jenkinsfile with pipeline strategy",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{
						"https://github.com/sclorg/nodejs-ex",
					},
				},
				GenerationInputs: cmd.GenerationInputs{
					ContextDir: "openshift/pipeline",
					Strategy:   newapp.StrategyPipeline,
				},
			},
			expected: map[string][]string{
				"buildConfig": {"nodejs-ex"},
			},
		},
		{
			name: "failed build from source with jenkinsfile with docker strategy",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{
						"https://github.com/sclorg/nodejs-ex",
					},
				},
				GenerationInputs: cmd.GenerationInputs{
					ContextDir: "openshift/pipeline",
					Strategy:   newapp.StrategyDocker,
				},
			},
			expectedErr: func(err error) bool {
				return strings.HasPrefix(err.Error(), "No Dockerfile was found in the repository")
			},
		},
		{
			name: "failed build from source without jenkinsfile with pipeline strategy",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{
						"https://github.com/sclorg/nodejs-ex",
					},
				},
				GenerationInputs: cmd.GenerationInputs{
					Strategy: newapp.StrategyPipeline,
				},
			},
			expectedErr: func(err error) bool {
				return strings.HasPrefix(err.Error(), "No Jenkinsfile was found in the repository")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr := PrepareAppConfig(test.config)
			test.config.ImageClient = &NewAppFakeImageClient{
				proxy: test.config.ImageClient,
			}

			res, err := test.config.Run()
			if (test.expectedErr == nil && err != nil) || (test.expectedErr != nil && !test.expectedErr(err)) {
				t.Fatalf("%s: unexpected error: %v", test.name, err)
			}
			if err != nil {
				return
			}
			if test.checkOutput != nil {
				if err := test.checkOutput(stdout, stderr); err != nil {
					t.Fatal(err)
				}
			}
			got := map[string][]string{}
			for _, obj := range res.List.Items {
				switch tp := obj.(type) {
				case *buildv1.BuildConfig:
					got["buildConfig"] = append(got["buildConfig"], tp.Name)
				case *imagev1.ImageStream:
					got["imageStream"] = append(got["imageStream"], tp.Name)
				}
			}

			if len(test.expected) != len(got) {
				t.Fatalf("%s: Resource kind size mismatch! Expected %d, got %d", test.name, len(test.expected), len(got))
			}

			for k, exp := range test.expected {
				g, ok := got[k]
				if !ok {
					t.Errorf("%s: Didn't find expected kind %s", test.name, k)
				}

				sort.Strings(g)
				sort.Strings(exp)

				if !reflect.DeepEqual(g, exp) {
					t.Fatalf("%s: Resource names mismatch! Expected %v, got %v", test.name, exp, g)
				}
			}

			if test.checkResult != nil {
				if err := test.checkResult(res); err != nil {
					t.Errorf("%s: unexpected result: %v", test.name, err)
				}
			}
		})
	}
}

func TestNewAppBuildOutputCycleDetection(t *testing.T) {
	skipExternalGit(t)
	tests := []struct {
		name   string
		config *cmd.AppConfig

		expected    map[string][]string
		expectedErr func(error) bool
		checkOutput func(stdout, stderr io.Reader) error
	}{
		{
			name: "successful build with warning that output docker-image may trigger input ImageStream change; legacy ImageStream without tags",
			config: &cmd.AppConfig{
				GenerationInputs: cmd.GenerationInputs{
					OutputDocker: true,
					To:           "centos/ruby-25-centos7",
					Dockerfile:   "FROM centos/ruby-25-centos7:latest",
				},
			},
			expected: map[string][]string{
				"buildConfig": {"ruby-25-centos7"},
				"imageStream": {"ruby-25-centos7"},
			},
			checkOutput: func(stdout, stderr io.Reader) error {
				got, err := ioutil.ReadAll(stderr)
				if err != nil {
					return err
				}
				want := "--> WARNING: output image of \"centos/ruby-25-centos7:latest\" should be different than input\n"
				if string(got) != want {
					return fmt.Errorf("stderr: got %q; want %q", got, want)
				}
				return nil
			},
		},
		{
			name: "successful build from dockerfile with identical input and output image references with warning(1)",
			config: &cmd.AppConfig{
				GenerationInputs: cmd.GenerationInputs{
					Dockerfile: "FROM centos\nRUN yum install -y httpd",
					To:         "centos",
				},
			},
			expected: map[string][]string{
				"buildConfig": {"centos"},
				"imageStream": {"centos"},
			},
			checkOutput: func(stdout, stderr io.Reader) error {
				got, err := ioutil.ReadAll(stderr)
				if err != nil {
					return err
				}
				want := "--> WARNING: output image of \"centos:latest\" should be different than input\n"
				if string(got) != want {
					return fmt.Errorf("stderr: got %q; want %q", got, want)
				}
				return nil
			},
		},
		{
			name: "successful build from dockerfile with identical input and output image references with warning(2)",
			config: &cmd.AppConfig{
				GenerationInputs: cmd.GenerationInputs{
					Dockerfile: "FROM centos/ruby-25-centos7\nRUN yum install -y httpd",
					To:         "ruby-25-centos7",
				},
			},
			expected: map[string][]string{
				"buildConfig": {"ruby-25-centos7"},
				"imageStream": {"ruby-25-centos7"},
			},
			checkOutput: func(stdout, stderr io.Reader) error {
				got, err := ioutil.ReadAll(stderr)
				if err != nil {
					return err
				}
				want := "--> WARNING: output image of \"centos/ruby-25-centos7:latest\" should be different than input\n"
				if string(got) != want {
					return fmt.Errorf("stderr: got %q; want %q", got, want)
				}
				return nil
			},
		},
		{
			name: "unsuccessful build from dockerfile due to identical input and output image references(1)",
			config: &cmd.AppConfig{
				GenerationInputs: cmd.GenerationInputs{
					Dockerfile: "FROM centos\nRUN yum install -y httpd",
				},
			},
			expectedErr: func(err error) bool {
				e := app.CircularOutputReferenceError{
					Reference: "centos:latest",
				}
				return err.Error() == fmt.Errorf("%v, set a different tag with --to", e).Error()
			},
		},
		{
			name: "unsuccessful build from dockerfile due to identical input and output image references(2)",
			config: &cmd.AppConfig{
				GenerationInputs: cmd.GenerationInputs{
					Dockerfile: "FROM centos/ruby-25-centos7\nRUN yum install -y httpd",
				},
			},
			expectedErr: func(err error) bool {
				e := app.CircularOutputReferenceError{
					Reference: "centos/ruby-25-centos7:latest",
				}
				return err.Error() == fmt.Errorf("%v, set a different tag with --to", e).Error()
			},
		},
		{
			name: "successful build with warning that output docker-image may trigger input ImageStream change",
			config: &cmd.AppConfig{
				GenerationInputs: cmd.GenerationInputs{
					OutputDocker: true,
					To:           "centos/ruby-25-centos7",
					Dockerfile:   "FROM centos/ruby-25-centos7",
				},
				Resolvers: cmd.Resolvers{
					DockerSearcher: app.DockerClientSearcher{
						Client:           &apptest.FakeDockerClient{},
						Insecure:         true,
						RegistrySearcher: &ExactMatchDirectTagDockerSearcher{},
					},
				},
			},
			expected: map[string][]string{
				"buildConfig": {"ruby-25-centos7"},
				"imageStream": {"ruby-25-centos7"},
			},
			checkOutput: func(stdout, stderr io.Reader) error {
				got, err := ioutil.ReadAll(stderr)
				if err != nil {
					return err
				}
				want := "--> WARNING: output image of \"centos/ruby-25-centos7:latest\" should be different than input\n"
				if string(got) != want {
					return fmt.Errorf("stderr: got %q; want %q", got, want)
				}
				return nil
			},
		},
		{
			name: "successful build with warning that output docker-image may trigger input ImageStream change; latest variation",
			config: &cmd.AppConfig{
				GenerationInputs: cmd.GenerationInputs{
					OutputDocker: true,
					To:           "centos/ruby-25-centos7",
					Dockerfile:   "FROM centos/ruby-25-centos7:latest",
				},
				Resolvers: cmd.Resolvers{
					DockerSearcher: app.DockerClientSearcher{
						Client:           &apptest.FakeDockerClient{},
						Insecure:         true,
						RegistrySearcher: &ExactMatchDirectTagDockerSearcher{},
					},
				},
			},
			expected: map[string][]string{
				"buildConfig": {"ruby-25-centos7"},
				"imageStream": {"ruby-25-centos7"},
			},
			checkOutput: func(stdout, stderr io.Reader) error {
				got, err := ioutil.ReadAll(stderr)
				if err != nil {
					return err
				}
				want := "--> WARNING: output image of \"centos/ruby-25-centos7:latest\" should be different than input\n"
				if string(got) != want {
					return fmt.Errorf("stderr: got %q; want %q", got, want)
				}
				return nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr := PrepareAppConfig(test.config)
			test.config.ImageClient = &NewAppFakeImageClient{
				proxy: test.config.ImageClient,
			}

			res, err := test.config.Run()
			if (test.expectedErr == nil && err != nil) || (test.expectedErr != nil && !test.expectedErr(err)) {
				t.Fatalf("%s: unexpected error: %v", test.name, err)
			}
			if err != nil {
				return
			}
			if test.checkOutput != nil {
				if err := test.checkOutput(stdout, stderr); err != nil {
					t.Fatalf("Error during test %q: %v", test.name, err)
				}
			}
			got := map[string][]string{}
			for _, obj := range res.List.Items {
				switch tp := obj.(type) {
				case *buildv1.BuildConfig:
					got["buildConfig"] = append(got["buildConfig"], tp.Name)
				case *imagev1.ImageStream:
					got["imageStream"] = append(got["imageStream"], tp.Name)
				}
			}

			if len(test.expected) != len(got) {
				t.Fatalf("%s: Resource kind size mismatch! Expected %d, got %d", test.name, len(test.expected), len(got))
			}

			for k, exp := range test.expected {
				g, ok := got[k]
				if !ok {
					t.Errorf("%s: Didn't find expected kind %s", test.name, k)
				}

				sort.Strings(g)
				sort.Strings(exp)

				if !reflect.DeepEqual(g, exp) {
					t.Fatalf("%s: Resource names mismatch! Expected %v, got %v", test.name, exp, g)
				}
			}
		})
	}

}

func TestNewAppNewBuildEnvVars(t *testing.T) {
	skipExternalGit(t)
	dockerSearcher := app.DockerRegistrySearcher{
		Client: dockerregistry.NewClient(10*time.Second, true),
	}

	okTemplateClient := faketemplatev1client.NewSimpleClientset()
	okImageClient := fakeimagev1client.NewSimpleClientset()
	okRouteClient := fakeroutev1client.NewSimpleClientset()
	customScheme, _ := apitesting.SchemeForOrDie(api.Install, api.InstallKube)

	tests := []struct {
		name        string
		config      *cmd.AppConfig
		expected    []corev1.EnvVar
		expectedErr error
	}{
		{
			name: "explicit environment variables for buildConfig and deploymentConfig",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{"https://github.com/openshift/ruby-hello-world"},
					DockerImages:       []string{"centos/ruby-25-centos7", "openshift/nodejs-010-centos7"},
				},
				GenerationInputs: cmd.GenerationInputs{
					OutputDocker:     true,
					BuildEnvironment: []string{"BUILD_ENV_1=env_value_1", "BUILD_ENV_2=env_value_2"},
				},

				Resolvers: cmd.Resolvers{
					DockerSearcher: dockerSearcher,
					Detector: app.SourceRepositoryEnumerator{
						Detectors:         source.DefaultDetectors,
						DockerfileTester:  dockerfile.NewTester(),
						JenkinsfileTester: jenkinsfile.NewTester(),
					},
				},
				Typer:           customScheme,
				ImageClient:     okImageClient.ImageV1(),
				TemplateClient:  okTemplateClient.TemplateV1(),
				RouteClient:     okRouteClient.RouteV1(),
				OriginNamespace: "default",
			},
			expected: []corev1.EnvVar{
				{Name: "BUILD_ENV_1", Value: "env_value_1"},
				{Name: "BUILD_ENV_2", Value: "env_value_2"},
			},
			expectedErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.config.Out, test.config.ErrOut = os.Stdout, os.Stderr
			test.config.ExpectToBuild = true
			res, err := test.config.Run()
			if err != test.expectedErr {
				t.Fatalf("%s: Error mismatch! Expected %v, got %v", test.name, test.expectedErr, err)
			}
			got := []corev1.EnvVar{}
			for _, obj := range res.List.Items {
				switch tp := obj.(type) {
				case *buildv1.BuildConfig:
					got = tp.Spec.Strategy.SourceStrategy.Env
					break
				}
			}

			if !reflect.DeepEqual(test.expected, got) {
				t.Fatalf("%s: unexpected output. Expected: %#v, Got: %#v", test.name, test.expected, got)
			}
		})
	}
}

func TestNewAppBuildConfigEnvVarsAndSecrets(t *testing.T) {
	skipExternalGit(t)
	dockerSearcher := app.DockerRegistrySearcher{
		Client: dockerregistry.NewClient(10*time.Second, true),
	}
	okTemplateClient := faketemplatev1client.NewSimpleClientset()
	okImageClient := fakeimagev1client.NewSimpleClientset()
	okRouteClient := fakeroutev1client.NewSimpleClientset()
	customScheme, _ := apitesting.SchemeForOrDie(api.Install, api.InstallKube)

	tests := []struct {
		name               string
		config             *cmd.AppConfig
		expected           []corev1.EnvVar
		expectedSecrets    map[string]string
		expectedConfigMaps map[string]string
		expectedErr        error
	}{
		{
			name: "explicit environment variables for buildConfig and deploymentConfig",
			config: &cmd.AppConfig{
				ComponentInputs: cmd.ComponentInputs{
					SourceRepositories: []string{"https://github.com/openshift/ruby-hello-world"},
					DockerImages:       []string{"centos/ruby-25-centos7", "centos/mongodb-26-centos7"},
				},
				GenerationInputs: cmd.GenerationInputs{
					OutputDocker: true,
					Environment:  []string{"BUILD_ENV_1=env_value_1", "BUILD_ENV_2=env_value_2"},
					Secrets:      []string{"foo:/var", "bar"},
					ConfigMaps:   []string{"this:/tmp", "that"},
				},

				Resolvers: cmd.Resolvers{
					DockerSearcher: dockerSearcher,
					Detector: app.SourceRepositoryEnumerator{
						Detectors:         source.DefaultDetectors,
						DockerfileTester:  dockerfile.NewTester(),
						JenkinsfileTester: jenkinsfile.NewTester(),
					},
				},
				Typer:           customScheme,
				ImageClient:     okImageClient.ImageV1(),
				TemplateClient:  okTemplateClient.TemplateV1(),
				RouteClient:     okRouteClient.RouteV1(),
				OriginNamespace: "default",
			},
			expected:           []corev1.EnvVar{},
			expectedSecrets:    map[string]string{"foo": "/var", "bar": "."},
			expectedConfigMaps: map[string]string{"this": "/tmp", "that": "."},
			expectedErr:        nil,
		},
	}

	for _, test := range tests {
		test.config.Out, test.config.ErrOut = os.Stdout, os.Stderr
		test.config.Deploy = true
		res, err := test.config.Run()
		if err != test.expectedErr {
			t.Errorf("%s: Error mismatch! Expected %v, got %v", test.name, test.expectedErr, err)
			continue
		}
		got := []corev1.EnvVar{}
		gotSecrets := []buildv1.SecretBuildSource{}
		gotConfigMaps := []buildv1.ConfigMapBuildSource{}
		for _, obj := range res.List.Items {
			switch tp := obj.(type) {
			case *buildv1.BuildConfig:
				got = tp.Spec.Strategy.SourceStrategy.Env
				gotSecrets = tp.Spec.Source.Secrets
				gotConfigMaps = tp.Spec.Source.ConfigMaps
				break
			}
		}

		for secretName, destDir := range test.expectedSecrets {
			found := false
			for _, got := range gotSecrets {
				if got.Secret.Name == secretName && got.DestinationDir == destDir {
					found = true
					continue
				}
			}
			if !found {
				t.Errorf("expected secret %q and destination %q, got %#v", secretName, destDir, gotSecrets)
				continue
			}
		}

		for configName, destDir := range test.expectedConfigMaps {
			found := false
			for _, got := range gotConfigMaps {
				if got.ConfigMap.Name == configName && got.DestinationDir == destDir {
					found = true
					continue
				}
			}
			if !found {
				t.Errorf("expected configMap %q and destination %q, got %#v", configName, destDir, gotConfigMaps)
				continue
			}
		}

		if !reflect.DeepEqual(test.expected, got) {
			t.Errorf("%s: unexpected output. Expected: %#v, Got: %#v", test.name, test.expected, got)
			continue
		}
	}
}

func TestNewAppSourceAuthRequired(t *testing.T) {

	tests := []struct {
		name               string
		passwordProtected  bool
		useProxy           bool
		expectAuthRequired bool
	}{
		{
			name:               "no auth",
			passwordProtected:  false,
			useProxy:           false,
			expectAuthRequired: false,
		},
		{
			name:               "basic auth",
			passwordProtected:  true,
			useProxy:           false,
			expectAuthRequired: true,
		},
		{
			name:               "proxy required",
			passwordProtected:  false,
			useProxy:           true,
			expectAuthRequired: true,
		},
		{
			name:               "basic auth and proxy required",
			passwordProtected:  true,
			useProxy:           true,
			expectAuthRequired: true,
		},
	}

	for _, test := range tests {
		url, tempRepoDir := setupLocalGitRepo(t, test.passwordProtected, test.useProxy)

		sourceRepo, err := app.NewSourceRepository(url, newapp.StrategySource)
		if err != nil {
			t.Fatalf("%v", err)
		}

		detector := app.SourceRepositoryEnumerator{
			Detectors:         source.DefaultDetectors,
			DockerfileTester:  dockerfile.NewTester(),
			JenkinsfileTester: jenkinsfile.NewTester(),
		}

		if err = sourceRepo.Detect(detector, true); err != nil {
			t.Fatalf("%v", err)
		}

		_, sourceRef, err := app.StrategyAndSourceForRepository(sourceRepo, nil)
		if err != nil {
			t.Fatalf("%v", err)
		}

		if test.expectAuthRequired != sourceRef.RequiresAuth {
			t.Errorf("%s: unexpected auth required result. Expected: %v. Actual: %v", test.name, test.expectAuthRequired, sourceRef.RequiresAuth)
		}
		os.RemoveAll(tempRepoDir)
	}
}

func TestNewAppListAndSearch(t *testing.T) {
	tests := []struct {
		name           string
		options        newappapp.AppOptions
		expectedOutput string
	}{
		{
			name: "search, no oldversion",
			options: newappapp.AppOptions{
				ObjectGeneratorOptions: &newappapp.ObjectGeneratorOptions{
					Config: &cmd.AppConfig{
						ComponentInputs: cmd.ComponentInputs{
							ImageStreams: []string{"ruby"},
						},
						AsSearch: true,
					}},
			},
			expectedOutput: "Image streams (arvan paas new-app --image-stream=<image-stream> [--code=<source>])\n-----\nruby\n  Project: default\n  Tags:    latest\n\n",
		},
		{
			name: "list, no oldversion",
			options: newappapp.AppOptions{
				ObjectGeneratorOptions: &newappapp.ObjectGeneratorOptions{
					Config: &cmd.AppConfig{
						AsList: true,
					}},
			},
			expectedOutput: "Image streams (arvan paas new-app --image-stream=<image-stream> [--code=<source>])\n-----\nruby\n  Project: default\n  Tags:    latest\n\n",
		},
	}
	for _, test := range tests {
		stdout, stderr := PrepareAppConfig(test.options.Config)
		test.options.Action.Out, test.options.ErrOut = stdout, stderr
		test.options.CommandName = "new-app"

		err := test.options.RunNewApp()
		if err != nil {
			t.Errorf("expected err == nil, got err == %v", err)
		}
		if stderr.Len() > 0 {
			t.Errorf("expected stderr == %q, got stderr == %q", "", stderr.Bytes())
		}
		if string(stdout.Bytes()) != test.expectedOutput {
			t.Errorf("expected stdout == %q, got stdout == %q", test.expectedOutput, stdout.Bytes())
		}
	}
}

func setupLocalGitRepo(t *testing.T, passwordProtected bool, requireProxy bool) (string, string) {
	// Create test directories
	testDir, err := ioutil.TempDir("", "gitauth")
	if err != nil {
		t.Fatalf("%v", err)
	}
	initialRepoDir := filepath.Join(testDir, "initial-repo")
	if err = os.Mkdir(initialRepoDir, 0755); err != nil {
		t.Fatalf("%v", err)
	}
	gitHomeDir := filepath.Join(testDir, "git-home")
	if err = os.Mkdir(gitHomeDir, 0755); err != nil {
		t.Fatalf("%v", err)
	}
	testRepoDir := filepath.Join(gitHomeDir, "test-repo")
	if err = os.Mkdir(testRepoDir, 0755); err != nil {
		t.Fatalf("%v", err)
	}
	userHomeDir := filepath.Join(testDir, "user-home")
	if err = os.Mkdir(userHomeDir, 0755); err != nil {
		t.Fatalf("%v", err)
	}

	// Set initial repo contents
	gitRepo := git.NewRepositoryWithEnv([]string{
		"GIT_AUTHOR_NAME=developer",
		"GIT_AUTHOR_EMAIL=developer@example.com",
		"GIT_COMMITTER_NAME=developer",
		"GIT_COMMITTER_EMAIL=developer@example.com",
	})
	if err = gitRepo.Init(initialRepoDir, false); err != nil {
		t.Fatalf("%v", err)
	}
	if err = ioutil.WriteFile(filepath.Join(initialRepoDir, "Dockerfile"), []byte("FROM mysql\nLABEL mylabel=myvalue\n"), 0644); err != nil {
		t.Fatalf("%v", err)
	}
	if err = gitRepo.Add(initialRepoDir, "."); err != nil {
		t.Fatalf("%v", err)
	}
	if err = gitRepo.Commit(initialRepoDir, "initial commit"); err != nil {
		t.Fatalf("%v", err)
	}

	// Clone to repository inside gitHomeDir
	if err = gitRepo.CloneBare(testRepoDir, initialRepoDir); err != nil {
		t.Fatalf("%v", err)
	}

	// Initialize test git server
	var gitHandler http.Handler
	gitHandler = githttp.New(gitHomeDir)

	// If password protected, set handler to require password
	user := "gituser"
	password := "gitpass"
	if passwordProtected {
		authenticator := auth.Authenticator(func(info auth.AuthInfo) (bool, error) {
			if info.Username != user && info.Password != password {
				return false, nil
			}
			return true, nil
		})
		gitHandler = authenticator(gitHandler)
	}
	gitServer := httptest.NewServer(gitHandler)
	gitURLString := fmt.Sprintf("%s/%s", gitServer.URL, "test-repo")

	var proxyServer *httptest.Server

	// If proxy required, create a simple proxy server that will forward any host to the git server
	if requireProxy {
		gitURL, err := url.Parse(gitURLString)
		if err != nil {
			t.Fatalf("%v", err)
		}
		proxy := goproxy.NewProxyHttpServer()
		proxy.OnRequest().DoFunc(
			func(r *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
				r.URL.Host = gitURL.Host
				return r, nil
			})
		gitURLString = "http://example.com/test-repo"
		proxyServer = httptest.NewServer(proxy)
	}

	gitConfig := `
[user]
name = developer
email = developer@org.org
`
	if passwordProtected {
		authSection := `
[url %q]
insteadOf = %s
		`
		urlWithAuth, err := url.Parse(gitURLString)
		if err != nil {
			t.Fatalf("%v", err)
		}
		urlWithAuth.User = url.UserPassword(user, password)
		authSection = fmt.Sprintf(authSection, urlWithAuth.String(), gitURLString)
		gitConfig += authSection
	}

	if requireProxy {
		proxySection := `
[http]
	proxy = %s
`
		proxySection = fmt.Sprintf(proxySection, proxyServer.URL)
		gitConfig += proxySection
	}

	if err = ioutil.WriteFile(filepath.Join(userHomeDir, ".gitconfig"), []byte(gitConfig), 0644); err != nil {
		t.Fatalf("%v", err)
	}
	os.Setenv("HOME", userHomeDir)
	os.Setenv("GIT_ASKPASS", "true")

	return gitURLString, testDir

}

func builderImageStream() *imagev1.ImageStream {
	return &imagev1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "ruby",
			Namespace:       "default",
			ResourceVersion: "1",
		},
		Spec: imagev1.ImageStreamSpec{
			Tags: []imagev1.TagReference{
				{
					Name: "oldversion",
					Annotations: map[string]string{
						"tags": "hidden",
					},
				},
			},
		},
		Status: imagev1.ImageStreamStatus{
			Tags: []imagev1.NamedTagEventList{
				{
					Tag: "latest",
					Items: []imagev1.TagEvent{
						{
							Image: "the-image-id",
						},
					},
				},
				{
					Tag: "oldversion",
					Items: []imagev1.TagEvent{
						{
							Image: "the-image-id",
						},
					},
				},
			},
			DockerImageRepository: "example/ruby:latest",
		},
	}

}

func builderImageStreams() *imagev1.ImageStreamList {
	return &imagev1.ImageStreamList{
		Items: []imagev1.ImageStream{*builderImageStream()},
	}
}

func builderImage() *imagev1.ImageStreamImage {
	return &imagev1.ImageStreamImage{
		Image: imagev1.Image{
			DockerImageReference: "example/ruby:latest",
			DockerImageMetadata: runtime.RawExtension{
				Object: &dockerv10.DockerImage{
					Config: &dockerv10.DockerConfig{
						Env: []string{
							"STI_SCRIPTS_URL=http://repo/git/ruby",
						},
						ExposedPorts: map[string]struct{}{
							"8080/tcp": {},
						},
					},
				},
			},
		},
	}
}

func dockerBuilderImage() *docker.Image {
	return &docker.Image{
		ID: "ruby",
		Config: &docker.Config{
			Env: []string{
				"STI_SCRIPTS_URL=http://repo/git/ruby",
			},
			ExposedPorts: map[docker.Port]struct{}{
				"8080/tcp": {},
			},
		},
	}
}

func fakeImageStreamSearcher() app.Searcher {
	client := fakeimagev1client.Clientset{Fake: clientgotesting.Fake{}}
	client.AddReactor("get", "imagestreams", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, builderImageStream(), nil
	})
	client.AddReactor("list", "imagestreams", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, builderImageStreams(), nil
	})
	client.AddReactor("get", "imagestreamimages", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, builderImage(), nil
	})

	return app.ImageStreamSearcher{
		Client:     client.ImageV1(),
		Namespaces: []string{"default"},
	}
}

func fakeDockerSearcher() app.Searcher {
	return app.DockerClientSearcher{
		Client: &apptest.FakeDockerClient{
			Images: []docker.APIImages{{RepoTags: []string{"library/ruby:latest"}}},
			Image:  dockerBuilderImage(),
		},
		Insecure:         true,
		RegistrySearcher: &ExactMatchDockerSearcher{},
	}
}

func fakeSimpleDockerSearcher() app.Searcher {
	return app.DockerClientSearcher{
		Client: &apptest.FakeDockerClient{
			Images: []docker.APIImages{{RepoTags: []string{"centos/ruby-25-centos7"}}},
			Image: &docker.Image{
				ID: "ruby",
				Config: &docker.Config{
					Env: []string{},
				},
			},
		},
		RegistrySearcher: &ExactMatchDockerSearcher{},
	}
}

// MockSourceRepositories is a set of mocked source repositories used for
// testing
func MockSourceRepositories(t *testing.T, file string) []*app.SourceRepository {
	var b []*app.SourceRepository
	for _, location := range []string{
		"https://github.com/openshift/ruby-hello-world.git",
		file,
	} {
		s, err := app.NewSourceRepository(location, newapp.StrategySource)
		if err != nil {
			t.Fatal(err)
		}
		b = append(b, s)
	}
	return b
}

// PrepareAppConfig sets fields in config appropriate for running tests. It
// returns two buffers bound to stdout and stderr.
func PrepareAppConfig(config *cmd.AppConfig) (stdout, stderr *bytes.Buffer) {
	config.ExpectToBuild = true
	stdout, stderr = new(bytes.Buffer), new(bytes.Buffer)
	config.Out, config.ErrOut = stdout, stderr

	okTemplateClient := faketemplatev1client.NewSimpleClientset()
	okImageClient := fakeimagev1client.NewSimpleClientset()
	okRouteClient := fakeroutev1client.NewSimpleClientset()

	config.Detector = app.SourceRepositoryEnumerator{
		Detectors:         source.DefaultDetectors,
		DockerfileTester:  dockerfile.NewTester(),
		JenkinsfileTester: jenkinsfile.NewTester(),
	}
	if config.DockerSearcher == nil {
		config.DockerSearcher = app.DockerRegistrySearcher{
			Client: dockerregistry.NewClient(10*time.Second, true),
		}
	}
	config.ImageStreamByAnnotationSearcher = fakeImageStreamSearcher()
	config.ImageStreamSearcher = fakeImageStreamSearcher()
	config.OriginNamespace = "default"

	config.ImageClient = okImageClient.ImageV1()
	config.TemplateClient = okTemplateClient.TemplateV1()
	config.RouteClient = okRouteClient.RouteV1()

	config.TemplateSearcher = app.TemplateSearcher{
		Client:     okTemplateClient.TemplateV1(),
		Namespaces: []string{"openshift", "default"},
	}

	customScheme, _ := apitesting.SchemeForOrDie(api.Install, api.InstallKube)
	config.Typer = customScheme
	return
}

// NewAppFakeImageClient implements ImageClient interface and overrides some of
// the default fake client behavior around default, empty imagestreams
type NewAppFakeImageClient struct {
	proxy imagev1typedclient.ImageV1Interface
}

func (c *NewAppFakeImageClient) Images() imagev1typedclient.ImageInterface {
	return c.proxy.Images()
}

func (c *NewAppFakeImageClient) ImageSignatures() imagev1typedclient.ImageSignatureInterface {
	return c.proxy.ImageSignatures()
}

func (c *NewAppFakeImageClient) ImageStreams(namespace string) imagev1typedclient.ImageStreamInterface {
	return &NewAppFakeImageStreams{
		proxy: c.proxy.ImageStreams(namespace),
	}
}

func (c *NewAppFakeImageClient) ImageStreamImages(namespace string) imagev1typedclient.ImageStreamImageInterface {
	return c.proxy.ImageStreamImages(namespace)
}

func (c *NewAppFakeImageClient) ImageStreamImports(namespace string) imagev1typedclient.ImageStreamImportInterface {
	return c.proxy.ImageStreamImports(namespace)
}

func (c *NewAppFakeImageClient) ImageStreamMappings(namespace string) imagev1typedclient.ImageStreamMappingInterface {
	return c.proxy.ImageStreamMappings(namespace)
}

func (c *NewAppFakeImageClient) ImageStreamTags(namespace string) imagev1typedclient.ImageStreamTagInterface {
	return c.proxy.ImageStreamTags(namespace)
}

func (c *NewAppFakeImageClient) ImageTags(namespace string) imagev1typedclient.ImageTagInterface {
	return c.proxy.ImageTags(namespace)
}

func (c *NewAppFakeImageClient) RESTClient() krest.Interface {
	return c.proxy.RESTClient()
}

// NewAppFakeImageStreams implements the ImageStreamInterface  and overrides some of the
// default fake client behavior round default, empty imagestreams
type NewAppFakeImageStreams struct {
	proxy imagev1typedclient.ImageStreamInterface
}

func (c *NewAppFakeImageStreams) Get(ctx context.Context, name string, options metav1.GetOptions) (result *imagev1.ImageStream, err error) {
	result, err = c.proxy.Get(ctx, name, options)
	if err != nil {
		return nil, err
	}
	if len(result.Name) == 0 {
		// the default faker will return an empty image stream struct if it
		// cannot find an entry for the given name ... we want nil for our tests,
		// just like the real client
		return nil, nil
	}
	return result, nil
}

func (c *NewAppFakeImageStreams) List(ctx context.Context, opts metav1.ListOptions) (result *imagev1.ImageStreamList, err error) {
	return c.proxy.List(ctx, opts)
}

func (c *NewAppFakeImageStreams) Watch(ctx context.Context, opts metav1.ListOptions) (kwatch.Interface, error) {
	return c.proxy.Watch(ctx, opts)
}

func (c *NewAppFakeImageStreams) Create(ctx context.Context, imageStream *imagev1.ImageStream, opts metav1.CreateOptions) (result *imagev1.ImageStream, err error) {
	return c.proxy.Create(ctx, imageStream, opts)
}

func (c *NewAppFakeImageStreams) Update(ctx context.Context, imageStream *imagev1.ImageStream, opts metav1.UpdateOptions) (result *imagev1.ImageStream, err error) {
	return c.proxy.Update(ctx, imageStream, opts)
}

func (c *NewAppFakeImageStreams) UpdateStatus(ctx context.Context, imageStream *imagev1.ImageStream, opts metav1.UpdateOptions) (*imagev1.ImageStream, error) {
	return c.proxy.UpdateStatus(ctx, imageStream, opts)
}

func (c *NewAppFakeImageStreams) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.proxy.Delete(ctx, name, opts)
}

func (c *NewAppFakeImageStreams) DeleteCollection(ctx context.Context, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	return c.proxy.DeleteCollection(ctx, options, listOptions)
}

func (c *NewAppFakeImageStreams) Patch(ctx context.Context, name string, pt ktypes.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *imagev1.ImageStream, err error) {
	return c.proxy.Patch(ctx, name, pt, data, opts, subresources...)
}

func (c *NewAppFakeImageStreams) Secrets(ctx context.Context, imageStreamName string, opts metav1.GetOptions) (result *corev1.SecretList, err error) {
	return c.proxy.Secrets(ctx, imageStreamName, opts)
}

func (c *NewAppFakeImageStreams) Layers(ctx context.Context, imageStreamName string, opts metav1.GetOptions) (result *imagev1.ImageStreamLayers, err error) {
	return c.proxy.Layers(ctx, imageStreamName, opts)
}
