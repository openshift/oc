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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	authfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
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
