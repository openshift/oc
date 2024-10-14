package recommend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func compareWithFixture(t *testing.T, actualOut []byte, cvPath string, outputSuffix string) {
	t.Helper()
	expectedOutPath := strings.Replace(cvPath, "-cv.yaml", outputSuffix, 1)

	if update := os.Getenv("UPDATE"); update != "" {
		if err := os.WriteFile(expectedOutPath, actualOut, 0644); err != nil {
			t.Fatalf("Error when writing output fixture: %v", err)
		}
		return
	}

	expectedOut, err := os.ReadFile(expectedOutPath)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("Error when reading output fixture: %v", err)
		} else {
			t.Fatalf("Output file %s does not exist. You may rerun this test with UPDATE=true to create output file with the following actual output:\n%s", expectedOutPath, actualOut)
		}
	}

	if diff := cmp.Diff(string(expectedOut), string(actualOut)); diff != "" {
		t.Errorf("Output differs from expected (%s):\n%s", filepath.Base(expectedOutPath), diff)
	}
}

func TestExamples(t *testing.T) {
	cvs, err := filepath.Glob("examples/*-cv.yaml")
	if err != nil {
		t.Fatalf("Error when listing examples: %v", err)
	}

	variants := []struct {
		name                 string
		showOutdatedReleases bool
		version              string
		outputSuffix         string
	}{
		{
			name:                 "normal output",
			showOutdatedReleases: false,
			outputSuffix:         ".output",
		},
		{
			name:                 "show outdated releases",
			showOutdatedReleases: true,
			outputSuffix:         ".show-outdated-releases-output",
		},
		{
			name:         "specific version",
			version:      "4.12.51",
			outputSuffix: ".version-4.12.51-output",
		},
	}

	for _, cv := range cvs {
		cv := cv
		for _, variant := range variants {
			variant := variant
			t.Run(fmt.Sprintf("%s-%s", cv, variant.name), func(t *testing.T) {
				t.Parallel()
				opts := &options{
					mockData:             mockData{cvPath: cv},
					showOutdatedReleases: variant.showOutdatedReleases,
					rawVersion:           variant.version,
				}
				if err := opts.Complete(nil, nil, nil); err != nil {
					t.Fatalf("Error when completing options: %v", err)
				}

				var stdout, stderr bytes.Buffer
				opts.Out = &stdout
				opts.ErrOut = &stderr

				if err := opts.Run(context.Background()); err != nil {
					compareWithFixture(t, bytes.Join([][]byte{stdout.Bytes(), []byte("\nerror: "), []byte(err.Error()), []byte("\n")}, []byte{}), cv, variant.outputSuffix)
				} else {
					compareWithFixture(t, stdout.Bytes(), cv, variant.outputSuffix)
				}
			})
		}
	}
}
