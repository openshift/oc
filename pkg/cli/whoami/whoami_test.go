package whoami

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/google/go-cmp/cmp"

	userv1 "github.com/openshift/api/user/v1"
	userv1fake "github.com/openshift/client-go/user/clientset/versioned/fake"

	v1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	authfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/kubectl/pkg/scheme"
	"sigs.k8s.io/yaml"
)

func TestWhoAmIInternalBothReadyChooseSSR(t *testing.T) {
	var b bytes.Buffer

	fakeAuthClientSet := &authfake.Clientset{}
	fakeUserClientSet := &userv1fake.Clientset{}

	fakeUserClientSet.AddReactor("get", "users",
		func(action core.Action) (handled bool, ret runtime.Object, err error) {
			return true, &userv1.User{
				ObjectMeta: metav1.ObjectMeta{
					Name: "doe.jane",
				},
			}, nil
		})

	fakeAuthClientSet.AddReactor("create", "selfsubjectreviews",
		func(action core.Action) (handled bool, ret runtime.Object, err error) {
			ui := v1.UserInfo{
				Username: "jane.doe",
				UID:      "uniq-id",
			}

			res := &v1.SelfSubjectReview{
				Status: v1.SelfSubjectReviewStatus{
					UserInfo: ui,
				},
			}
			return true, res, nil
		})

	opts := &WhoAmIOptions{
		UserInterface: fakeUserClientSet.UserV1(),
		AuthV1Client:  fakeAuthClientSet.AuthenticationV1(),
		IOStreams: genericiooptions.IOStreams{
			Out:    &b,
			ErrOut: io.Discard,
		},
	}

	user, err := opts.WhoAmI()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	expectedUser := &userv1.User{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "jane.doe",
		},
	}
	if !cmp.Equal(user, expectedUser) {
		t.Errorf("actual user %v must match with the expected %v", user, expectedUser)
	}
}

func TestWhoAmIInternalOauthEnabled(t *testing.T) {
	var b bytes.Buffer

	fakeAuthClientSet := &authfake.Clientset{}
	fakeUserClientSet := &userv1fake.Clientset{}

	fakeUserClientSet.AddReactor("get", "users",
		func(action core.Action) (handled bool, ret runtime.Object, err error) {
			return true, &userv1.User{
				ObjectMeta: metav1.ObjectMeta{
					Name: "jane.doe",
				},
				Groups: []string{"students", "teachers"},
			}, nil
		})

	fakeAuthClientSet.AddReactor("create", "selfsubjectreviews",
		func(action core.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, fmt.Errorf("unknown API")
		})

	opts := &WhoAmIOptions{
		UserInterface: fakeUserClientSet.UserV1(),
		AuthV1Client:  fakeAuthClientSet.AuthenticationV1(),
		IOStreams: genericiooptions.IOStreams{
			Out:    &b,
			ErrOut: io.Discard,
		},
	}

	user, err := opts.WhoAmI()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	expectedUser := &userv1.User{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "jane.doe",
		},
		Groups: []string{"students", "teachers"},
	}
	if !cmp.Equal(user, expectedUser) {
		t.Errorf("actual user %v must match with the expected %v", user, expectedUser)
	}
}

func TestWhoAmISSREnabled(t *testing.T) {
	var b bytes.Buffer

	fakeAuthClientSet := &authfake.Clientset{}
	fakeUserClientSet := &userv1fake.Clientset{}

	fakeUserClientSet.AddReactor("get", "users",
		func(action core.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, apierrors.NewNotFound(schema.GroupResource{
				Group:    "openshift.io",
				Resource: "user",
			}, "")
		})

	fakeAuthClientSet.AddReactor("create", "selfsubjectreviews",
		func(action core.Action) (handled bool, ret runtime.Object, err error) {
			ui := v1.UserInfo{
				Username: "jane.doe",
				UID:      "uniq-id",
				Groups:   []string{"students", "teachers"},
				Extra: map[string]v1.ExtraValue{
					"subjects": {"math", "sports"},
					"skills":   {"reading", "learning"},
				},
			}

			res := &v1.SelfSubjectReview{
				Status: v1.SelfSubjectReviewStatus{
					UserInfo: ui,
				},
			}
			return true, res, nil
		})

	opts := &WhoAmIOptions{
		UserInterface: fakeUserClientSet.UserV1(),
		AuthV1Client:  fakeAuthClientSet.AuthenticationV1(),
		IOStreams: genericiooptions.IOStreams{
			Out:    &b,
			ErrOut: io.Discard,
		},
	}

	user, err := opts.WhoAmI()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	expectedUser := &userv1.User{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "jane.doe",
		},
		Groups: []string{"students", "teachers"},
	}
	if !cmp.Equal(user, expectedUser) {
		t.Errorf("actual user %v must match with the expected %v", user, expectedUser)
	}
}

func TestWhoAmIInternalEnabledUnauthorized(t *testing.T) {
	var b bytes.Buffer

	fakeAuthClientSet := &authfake.Clientset{}
	fakeUserClientSet := &userv1fake.Clientset{}

	fakeUserClientSet.AddReactor("get", "users",
		func(action core.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, apierrors.NewUnauthorized("unauthorized")
		})

	fakeAuthClientSet.AddReactor("create", "selfsubjectreviews",
		func(action core.Action) (handled bool, ret runtime.Object, err error) {
			ui := v1.UserInfo{
				Username: "jane.doe",
				UID:      "uniq-id",
				Groups:   []string{"students", "teachers"},
				Extra: map[string]v1.ExtraValue{
					"subjects": {"math", "sports"},
					"skills":   {"reading", "learning"},
				},
			}

			res := &v1.SelfSubjectReview{
				Status: v1.SelfSubjectReviewStatus{
					UserInfo: ui,
				},
			}
			return true, res, nil
		})

	opts := &WhoAmIOptions{
		UserInterface: fakeUserClientSet.UserV1(),
		AuthV1Client:  fakeAuthClientSet.AuthenticationV1(),
		IOStreams: genericiooptions.IOStreams{
			Out:    &b,
			ErrOut: io.Discard,
		},
	}

	user, err := opts.WhoAmI()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	expectedUser := &userv1.User{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "jane.doe",
		},
		Groups: []string{"students", "teachers"},
	}
	if !cmp.Equal(user, expectedUser) {
		t.Errorf("actual user %v must match with the expected %v", user, expectedUser)
	}
}

func TestWhoAmIInternalDisabledNotFound(t *testing.T) {
	var b bytes.Buffer

	fakeAuthClientSet := &authfake.Clientset{}
	fakeUserClientSet := &userv1fake.Clientset{}

	fakeUserClientSet.AddReactor("get", "users",
		func(action core.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, apierrors.NewNotFound(schema.GroupResource{
				Group:    "openshift.io",
				Resource: "user",
			}, "")
		})

	fakeAuthClientSet.AddReactor("create", "selfsubjectreviews",
		func(action core.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, apierrors.NewUnauthorized("unauthorized request")
		})

	opts := &WhoAmIOptions{
		UserInterface: fakeUserClientSet.UserV1(),
		AuthV1Client:  fakeAuthClientSet.AuthenticationV1(),
		IOStreams: genericiooptions.IOStreams{
			Out:    &b,
			ErrOut: io.Discard,
		},
	}

	_, err := opts.WhoAmI()
	if !apierrors.IsNotFound(err) {
		t.Errorf("expected unauthorized error but not got different %v", err)
	}
}

func TestWhoAmIOutputJSON(t *testing.T) {
	var b bytes.Buffer

	fakeAuthClientSet := &authfake.Clientset{}
	fakeUserClientSet := &userv1fake.Clientset{}

	fakeAuthClientSet.AddReactor("create", "selfsubjectreviews",
		func(action core.Action) (handled bool, ret runtime.Object, err error) {
			ui := v1.UserInfo{
				Username: "jane.doe",
				UID:      "uniq-id",
				Groups:   []string{"developers", "admins"},
			}

			res := &v1.SelfSubjectReview{
				Status: v1.SelfSubjectReviewStatus{
					UserInfo: ui,
				},
			}
			return true, res, nil
		})

	printFlags := genericclioptions.NewPrintFlags("").WithTypeSetter(scheme.Scheme).WithDefaultOutput("json")
	if err := userv1.Install(scheme.Scheme); err != nil {
		t.Fatalf("failed to register user types: %v", err)
	}
	printer, err := printFlags.ToPrinter()
	if err != nil {
		t.Fatalf("failed to create printer: %v", err)
	}

	opts := &WhoAmIOptions{
		UserInterface:       fakeUserClientSet.UserV1(),
		AuthV1Client:        fakeAuthClientSet.AuthenticationV1(),
		resourcePrinterFunc: printer.PrintObj,
		IOStreams: genericiooptions.IOStreams{
			Out:    &b,
			ErrOut: io.Discard,
		},
	}

	_, err = opts.WhoAmI()
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if b.Len() == 0 {
		t.Fatalf("expected JSON output, but received empty response")
	}
	var actualUser userv1.User
	if err := json.Unmarshal(b.Bytes(), &actualUser); err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v\nOutput: %s", err, b.String())
	}

	expectedUser := userv1.User{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "user.openshift.io/v1",
			Kind:       "User",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "jane.doe",
		},
		Groups: []string{"developers", "admins"},
	}

	if diff := cmp.Diff(expectedUser, actualUser); diff != "" {
		t.Errorf("User mismatch (-expected +actual):\n%s", diff)
	}
}

func TestWhoAmIOutputYAML(t *testing.T) {
	var b bytes.Buffer

	fakeAuthClientSet := &authfake.Clientset{}
	fakeUserClientSet := &userv1fake.Clientset{}

	fakeAuthClientSet.AddReactor("create", "selfsubjectreviews",
		func(action core.Action) (handled bool, ret runtime.Object, err error) {
			ui := v1.UserInfo{
				Username: "john.smith",
				UID:      "unique-id-123",
				Groups:   []string{"developers", "admins"},
			}

			res := &v1.SelfSubjectReview{
				Status: v1.SelfSubjectReviewStatus{
					UserInfo: ui,
				},
			}
			return true, res, nil
		})

	printFlags := genericclioptions.NewPrintFlags("").WithTypeSetter(scheme.Scheme).WithDefaultOutput("yaml")
	if err := userv1.Install(scheme.Scheme); err != nil {
		t.Fatalf("failed to register user types: %v", err)
	}
	printer, err := printFlags.ToPrinter()
	if err != nil {
		t.Fatalf("failed to create printer: %v", err)
	}

	opts := &WhoAmIOptions{
		UserInterface:       fakeUserClientSet.UserV1(),
		AuthV1Client:        fakeAuthClientSet.AuthenticationV1(),
		resourcePrinterFunc: printer.PrintObj,
		IOStreams: genericiooptions.IOStreams{
			Out:    &b,
			ErrOut: io.Discard,
		},
	}

	_, err = opts.WhoAmI()
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if b.Len() == 0 {
		t.Fatalf("expected YAML output, but received empty response")
	}
	var actualUser userv1.User
	if err := yaml.Unmarshal(b.Bytes(), &actualUser); err != nil {
		t.Fatalf("failed to unmarshal YAML output: %v\nOutput: %s", err, b.String())
	}

	expectedUser := userv1.User{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "user.openshift.io/v1",
			Kind:       "User",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "john.smith",
		},
		Groups: []string{"developers", "admins"},
	}

	if diff := cmp.Diff(expectedUser, actualUser); diff != "" {
		t.Errorf("User mismatch (-expected +actual):\n%s", diff)
	}
}

func TestWhoAmIOutputJSONFallbackToUserAPI(t *testing.T) {
	var b bytes.Buffer

	fakeAuthClientSet := &authfake.Clientset{}
	fakeUserClientSet := &userv1fake.Clientset{}

	fakeUserClientSet.AddReactor("get", "users",
		func(action core.Action) (handled bool, ret runtime.Object, err error) {
			return true, &userv1.User{
				ObjectMeta: metav1.ObjectMeta{
					Name: "legacy.user",
				},
				Groups: []string{"legacy-group"},
			}, nil
		})

	fakeAuthClientSet.AddReactor("create", "selfsubjectreviews",
		func(action core.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, fmt.Errorf("unknown API")
		})

	printFlags := genericclioptions.NewPrintFlags("").WithTypeSetter(scheme.Scheme).WithDefaultOutput("json")
	if err := userv1.Install(scheme.Scheme); err != nil {
		t.Fatalf("failed to register user types: %v", err)
	}
	printer, err := printFlags.ToPrinter()
	if err != nil {
		t.Fatalf("failed to create printer: %v", err)
	}

	opts := &WhoAmIOptions{
		UserInterface:       fakeUserClientSet.UserV1(),
		AuthV1Client:        fakeAuthClientSet.AuthenticationV1(),
		resourcePrinterFunc: printer.PrintObj,
		IOStreams: genericiooptions.IOStreams{
			Out:    &b,
			ErrOut: io.Discard,
		},
	}

	_, err = opts.WhoAmI()
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if b.Len() == 0 {
		t.Fatalf("expected JSON output, but received empty response")
	}
	var actualUser userv1.User
	if err := json.Unmarshal(b.Bytes(), &actualUser); err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v\nOutput: %s", err, b.String())
	}

	expectedUser := userv1.User{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "user.openshift.io/v1",
			Kind:       "User",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "legacy.user",
		},
		Groups: []string{"legacy-group"},
	}

	if diff := cmp.Diff(expectedUser, actualUser); diff != "" {
		t.Errorf("User mismatch (-expected +actual):\n%s", diff)
	}
}

func TestFetchUser(t *testing.T) {
	tests := []struct {
		name         string
		ssrFunc      func(action core.Action) (bool, runtime.Object, error)
		userFunc     func(action core.Action) (bool, runtime.Object, error)
		expectedUser *userv1.User
		expectErr    bool
	}{
		{
			name: "SSR succeeds without printing to Out",
			ssrFunc: func(action core.Action) (bool, runtime.Object, error) {
				return true, &v1.SelfSubjectReview{
					Status: v1.SelfSubjectReviewStatus{
						UserInfo: v1.UserInfo{
							Username: "jane.doe",
							Groups:   []string{"developers", "admins"},
						},
					},
				}, nil
			},
			userFunc: func(action core.Action) (bool, runtime.Object, error) {
				t.Errorf("unexpected fallback to Users().Get")
				return true, nil, nil
			},
			expectedUser: &userv1.User{
				ObjectMeta: metav1.ObjectMeta{
					Name: "jane.doe",
				},
				Groups: []string{"developers", "admins"},
			},
		},
		{
			name: "SSR fails, falls back to user API without printing to Out",
			ssrFunc: func(action core.Action) (bool, runtime.Object, error) {
				return true, nil, fmt.Errorf("SSR not supported")
			},
			userFunc: func(action core.Action) (bool, runtime.Object, error) {
				return true, &userv1.User{
					ObjectMeta: metav1.ObjectMeta{
						Name: "legacy.user",
					},
					Groups: []string{"legacy-group"},
				}, nil
			},
			expectedUser: &userv1.User{
				ObjectMeta: metav1.ObjectMeta{
					Name: "legacy.user",
				},
				Groups: []string{"legacy-group"},
			},
		},
		{
			name: "both SSR and user API fail",
			ssrFunc: func(action core.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewUnauthorized("unauthorized")
			},
			userFunc: func(action core.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewNotFound(schema.GroupResource{Group: "openshift.io", Resource: "user"}, "~")
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			fakeAuthClientSet := &authfake.Clientset{}
			fakeUserClientSet := &userv1fake.Clientset{}

			fakeAuthClientSet.AddReactor("create", "selfsubjectreviews", tc.ssrFunc)
			fakeUserClientSet.AddReactor("get", "users", tc.userFunc)

			opts := &WhoAmIOptions{
				UserInterface: fakeUserClientSet.UserV1(),
				AuthV1Client:  fakeAuthClientSet.AuthenticationV1(),
				IOStreams: genericiooptions.IOStreams{
					Out:    &b,
					ErrOut: io.Discard,
				},
			}

			user, err := opts.fetchUser()
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if diff := cmp.Diff(tc.expectedUser, user); diff != "" {
				t.Errorf("user mismatch (-expected +actual):\n%s", diff)
			}
			if b.Len() != 0 {
				t.Errorf("expected fetchUser not to write to Out, but got %q", b.String())
			}
		})
	}
}

func TestWhoAmIValidateShowAll(t *testing.T) {
	outputNotSpecified := &genericclioptions.PrintFlags{
		OutputFlagSpecified: func() bool { return false },
	}
	outputSpecified := &genericclioptions.PrintFlags{
		OutputFlagSpecified: func() bool { return true },
	}
	tests := []struct {
		name        string
		opts        *WhoAmIOptions
		expectedErr string
	}{
		{
			name: "valid --show-all alone",
			opts: &WhoAmIOptions{
				ShowAll:    true,
				PrintFlags: outputNotSpecified,
				ClientConfig: &rest.Config{
					Host: "https://api.example.com:6443",
				},
			},
		},
		{
			name: "--show-all with --output",
			opts: &WhoAmIOptions{
				ShowAll:    true,
				PrintFlags: outputSpecified,
			},
			expectedErr: "--output cannot be used with --show-token, --show-context, --show-server, --show-console, or --show-all",
		},
		{
			name: "--show-all with --show-token",
			opts: &WhoAmIOptions{
				ShowAll:    true,
				ShowToken:  true,
				PrintFlags: outputNotSpecified,
			},
			expectedErr: "--show-all should not be used together with individual --show-* flags",
		},
		{
			name: "--show-all with --show-context",
			opts: &WhoAmIOptions{
				ShowAll:     true,
				ShowContext: true,
				PrintFlags:  outputNotSpecified,
			},
			expectedErr: "--show-all should not be used together with individual --show-* flags",
		},
		{
			name: "--show-all with --show-server",
			opts: &WhoAmIOptions{
				ShowAll:    true,
				ShowServer: true,
				PrintFlags: outputNotSpecified,
			},
			expectedErr: "--show-all should not be used together with individual --show-* flags",
		},
		{
			name: "--show-all with --show-console",
			opts: &WhoAmIOptions{
				ShowAll:        true,
				ShowConsoleUrl: true,
				PrintFlags:     outputNotSpecified,
			},
			expectedErr: "--show-all should not be used together with individual --show-* flags",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if tc.expectedErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q, got nil", tc.expectedErr)
			}
			if diff := cmp.Diff(tc.expectedErr, err.Error()); diff != "" {
				t.Errorf("error mismatch (-expected +actual):\n%s", diff)
			}
		})
	}
}

func TestWhoAmIShowAll(t *testing.T) {
	tests := []struct {
		name           string
		username       string
		groups         []string
		currentContext string
		host           string
		bearerToken    string
		consoleCM      *corev1.ConfigMap
		consoleErr     error
		expectedOutput string
	}{
		{
			name:           "full session with groups, console URL, and token",
			username:       "jane.doe",
			groups:         []string{"developers", "system:authenticated"},
			currentContext: "default/api-example-com:6443/jane.doe",
			host:           "https://api.example.com:6443",
			bearerToken:    "secret-bearer-token",
			consoleCM: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      consolePublicConfigMap,
					Namespace: openShiftConfigManagedNamespaceName,
				},
				Data: map[string]string{
					"consoleURL": "https://console-openshift-console.apps.example.com",
				},
			},
			expectedOutput: "" +
				"User:            jane.doe\n" +
				"Groups:          developers, system:authenticated\n" +
				"Current Context: default/api-example-com:6443/jane.doe\n" +
				"Server URL:      https://api.example.com:6443\n" +
				"Console URL:     https://console-openshift-console.apps.example.com\n" +
				"Token:           <present>\n",
		},
		{
			name:           "session without groups or token, and console unavailable",
			username:       "system:admin",
			groups:         nil,
			currentContext: "admin-context",
			host:           "https://api.example.com:6443",
			bearerToken:    "",
			consoleErr:     apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, consolePublicConfigMap, fmt.Errorf("forbidden")),
			expectedOutput: "" +
				"User:            system:admin\n" +
				"Current Context: admin-context\n" +
				"Server URL:      https://api.example.com:6443\n" +
				"Console URL:     <unavailable>\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer

			var initObjs []runtime.Object
			if tc.consoleCM != nil {
				initObjs = append(initObjs, tc.consoleCM)
			}
			fakeKubeClient := authfake.NewSimpleClientset(initObjs...)
			fakeUserClientSet := &userv1fake.Clientset{}

			fakeKubeClient.PrependReactor("get", "configmaps",
				func(action core.Action) (bool, runtime.Object, error) {
					if tc.consoleErr != nil {
						return true, nil, tc.consoleErr
					}
					return false, nil, nil
				})

			fakeKubeClient.PrependReactor("create", "selfsubjectreviews",
				func(action core.Action) (bool, runtime.Object, error) {
					return true, &v1.SelfSubjectReview{
						Status: v1.SelfSubjectReviewStatus{
							UserInfo: v1.UserInfo{
								Username: tc.username,
								Groups:   tc.groups,
							},
						},
					}, nil
				})

			opts := &WhoAmIOptions{
				UserInterface: fakeUserClientSet.UserV1(),
				AuthV1Client:  fakeKubeClient.AuthenticationV1(),
				KubeClient:    fakeKubeClient,
				ClientConfig: &rest.Config{
					Host:        tc.host,
					BearerToken: tc.bearerToken,
				},
				RawConfig: api.Config{
					CurrentContext: tc.currentContext,
				},
				ShowAll:    true,
				PrintFlags: genericclioptions.NewPrintFlags(""),
				IOStreams: genericiooptions.IOStreams{
					Out:    &b,
					ErrOut: io.Discard,
				},
			}

			if err := opts.Run(); err != nil {
				t.Fatalf("unexpected error running --show-all: %v", err)
			}

			if diff := cmp.Diff(tc.expectedOutput, b.String()); diff != "" {
				t.Errorf("output mismatch (-expected +actual):\n%s", diff)
			}
		})
	}
}
