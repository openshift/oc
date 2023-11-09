package status

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func compareWithFixture(t *testing.T, actualOut []byte, cvPath string) {
	t.Helper()
	expectedOutPath := strings.Replace(cvPath, "-cv.yaml", ".output", 1)

	if update := os.Getenv("UPDATE"); update != "" {
		if err := os.WriteFile(expectedOutPath, actualOut, 0644); err != nil {
			t.Fatalf("Error when writing output fixture: %v", err)
		}
		return
	}

	expectedOut, err := os.ReadFile(expectedOutPath)
	if err != nil {
		t.Fatalf("Error when reading output fixture: %v", err)
	}
	if diff := cmp.Diff(expectedOut, actualOut); diff != "" {
		t.Errorf("Output differs from expected:\n%s", diff)
	}
}

func TestExamples(t *testing.T) {
	cvs, err := filepath.Glob("examples/*-cv.yaml")
	if err != nil {
		t.Fatalf("Error when listing examples: %v", err)
	}

	for _, cv := range cvs {
		cv := cv
		t.Run(cv, func(t *testing.T) {
			t.Parallel()
			co := strings.Replace(cv, "-cv.yaml", "-co.yaml", 1)

			opts := &options{
				mockCvPath:        cv,
				mockOperatorsPath: co,
			}
			if err := opts.Complete(nil, nil, nil); err != nil {
				t.Fatalf("Error when completing options: %v", err)
			}

			var stdout, stderr bytes.Buffer
			opts.Out = &stdout
			opts.ErrOut = &stderr

			if err := opts.Run(context.Background()); err != nil {
				t.Fatalf("Error when running: %v", err)
			}

			compareWithFixture(t, stdout.Bytes(), cv)
		})
	}
}
