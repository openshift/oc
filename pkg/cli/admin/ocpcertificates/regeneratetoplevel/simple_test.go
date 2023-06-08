package regeneratetoplevel

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/utils/diff"
)

func AllTestsInDir(directory string) ([]RegenerateTest, error) {
	ret := []RegenerateTest{}
	err := filepath.WalkDir(directory, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}

		if containsDirectory, err := containsDir(path); err != nil {
			return err
		} else if containsDirectory {
			return nil
		}

		// so now we have only leave nodes
		relativePath, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}

		currTest, err := readTestInDir(relativePath, path)
		if err != nil {
			return err
		}
		ret = append(ret, currTest)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func readTestInDir(testName, directory string) (RegenerateTest, error) {
	ret := RegenerateTest{
		Name: testName,
	}

	existingSecretFile := filepath.Join(directory, "existing.yaml")
	existingBytes, err := os.ReadFile(existingSecretFile)
	if err != nil {
		return RegenerateTest{}, err
	}
	if len(existingBytes) > 0 {
		ret.ExistingSecret = resourceread.ReadSecretV1OrDie(existingBytes)
	}

	argsFile := filepath.Join(directory, "args.yaml")
	argsBytes, err := os.ReadFile(argsFile)
	if err != nil {
		return RegenerateTest{}, err
	}
	args := &Args{}
	if err := yaml.Unmarshal(argsBytes, args); err != nil {
		return RegenerateTest{}, err
	}
	ret.Args = *args

	optionalExpectedFile := filepath.Join(directory, "expected.yaml")
	expectedBytes, err := os.ReadFile(optionalExpectedFile)
	if err != nil && !os.IsNotExist(err) {
		return RegenerateTest{}, err
	}
	if len(expectedBytes) > 0 {
		ret.ExpectedSecret = resourceread.ReadSecretV1OrDie(expectedBytes)
	}

	optionalExpectedErrorsFile := filepath.Join(directory, "errors.txt")
	expectedErrorsBytes, err := os.ReadFile(optionalExpectedErrorsFile)
	if err != nil && !os.IsNotExist(err) {
		return RegenerateTest{}, err
	}
	if len(expectedErrorsBytes) > 0 {
		expectedErrors := []string{}
		scanner := bufio.NewScanner(bytes.NewBuffer(expectedErrorsBytes))
		for scanner.Scan() {
			expectedErrors = append(expectedErrors, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return RegenerateTest{}, err
		}
		if len(expectedErrors) > 1 {
			return RegenerateTest{}, fmt.Errorf("too many errors")
		}
		ret.ExpectedError = expectedErrors[0]
	}

	return ret, nil
}

func containsDir(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return true, nil
		}
	}
	return false, nil
}

// RegenerateTest represents the directory style test we have.
type RegenerateTest struct {
	Name           string
	ExistingSecret *corev1.Secret
	Args           Args

	ExpectedSecret *corev1.Secret
	ExpectedError  string
}

type Args struct {
	ValidBefore string `yaml:"ValidBefore"`
	DryRun      bool   `yaml:"DryRun"`
}

type simpleRegenerateTest struct {
	regenerateTest RegenerateTest
}

func (tc *simpleRegenerateTest) Test(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(tc.regenerateTest.ExistingSecret)

	testPrinter := func(runtime.Object, io.Writer) error {
		return nil
	}
	o := &RegenerateTopLevelRuntime{
		KubeClient: fakeClient,
		DryRun:     tc.regenerateTest.Args.DryRun,
		Printer:    printers.ResourcePrinterFunc(testPrinter),
		IOStreams:  genericclioptions.NewTestIOStreamsDiscard(),
	}
	if len(tc.regenerateTest.Args.ValidBefore) > 0 {
		validBefore, err := time.Parse(time.RFC3339, tc.regenerateTest.Args.ValidBefore)
		if err != nil {
			t.Fatal(err)
		}
		o.ValidBefore = &validBefore
	}

	actualErr := o.forceRegenerationOnSecret(tc.regenerateTest.ExistingSecret)
	tc.regenerateTest.Test(t, fakeClient, actualErr)
}

func (tc *RegenerateTest) Test(t *testing.T, fakeClient *fake.Clientset, actualErr error) {
	switch {
	case len(tc.ExpectedError) == 0 && actualErr == nil:
	case len(tc.ExpectedError) == 0 && actualErr != nil:
		t.Fatalf("no error expected, got %v", actualErr)
	case len(tc.ExpectedError) != 0 && actualErr == nil:
		t.Fatalf("expected some errors: %v, got none", tc.ExpectedError)
	case len(tc.ExpectedError) != 0 && actualErr != nil:
		if !reflect.DeepEqual(tc.ExpectedError, actualErr.Error()) {
			t.Fatalf("expected some error: %v, got different error: %v", tc.ExpectedError, actualErr.Error())
		}
	}

	if len(fakeClient.Actions()) > 1 {
		t.Fatalf("too many actions: %v", fakeClient.Actions())
	}

	if tc.ExpectedSecret == nil {
		if len(fakeClient.Actions()) != 0 {
			t.Fatalf("no action expected, but got: %v", fakeClient.Actions())
		}
		return
	}

	if len(fakeClient.Actions()) == 0 {
		t.Fatalf("missing expected action")
	}

	action := fakeClient.Actions()[0].(clienttesting.PatchAction)
	if action.GetPatchType() != types.ApplyPatchType {
		t.Fatalf("wrong patch type: %v", action.GetPatchType())
	}
	actualSecret := resourceread.ReadSecretV1OrDie(action.GetPatch())
	if !equality.Semantic.DeepEqual(tc.ExpectedSecret, actualSecret) {
		t.Logf("actual %v", actualSecret)
		t.Fatalf("unexpected diff: %v", diff.ObjectDiff(tc.ExpectedSecret, actualSecret))
	}

	// TODO capture options.  needs client-go update.
}
