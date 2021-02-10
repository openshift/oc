package extract

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestParseMappings test.
func TestParseMappings(t *testing.T) {
	// Default testdata files locations
	testdataPath, err := filepath.Abs("./../../../../testdata/image/extract")
	if err != nil {
		t.Fatalf("cannot make path %s absolute: %v", testdataPath, err)
	}
	fooPath := filepath.Join(testdataPath, "foo")
	barFile := filepath.Join(fooPath, "bar")

	tests := map[string]struct {
		paths        []string
		images       []string
		files        []string
		requireEmpty bool
		expectedErr  string
	}{
		"destination does not exist": {
			paths:        []string{"/foo:/i/dont/exist"},
			images:       []string{"localhost:5000/foobar:latest"},
			files:        nil,
			requireEmpty: false,
			expectedErr:  "destination path does not exist: /i/dont/exist",
		},
		"multiple destination does not exist": {
			paths:        []string{"/foo:" + fooPath, "/foo:/i/dont/exist"},
			images:       []string{"localhost:5000/foobar:latest"},
			files:        nil,
			requireEmpty: false,
			expectedErr:  "destination path does not exist: /i/dont/exist",
		},
		"path is in form of SRC:DST": {
			paths:        []string{"/foo"},
			images:       []string{"localhost:5000/foobar:latest"},
			files:        nil,
			requireEmpty: false,
			expectedErr:  "--paths must be of the form SRC:DST",
		},
		"multiple paths are in form of SRC:DST": {
			paths:        []string{"/foo:" + fooPath, "/bar"},
			images:       []string{"localhost:5000/foobar:latest"},
			files:        nil,
			requireEmpty: false,
			expectedErr:  "--paths must be of the form SRC:DST",
		},
		"check DST path is empty": {
			paths:        []string{"/foo:" + fooPath},
			images:       []string{"localhost:5000/foobar:latest"},
			files:        nil,
			requireEmpty: true,
			expectedErr:  "directory " + fooPath + " must be empty, pass --confirm to overwrite contents of directory",
		},
		"check DST path is directory": {
			paths:        []string{"/foo:" + barFile},
			images:       []string{"localhost:5000/foobar:latest"},
			files:        nil,
			requireEmpty: false,
			expectedErr:  "invalid argument: /foo:" + barFile + " is not a directory",
		},
		"files must NOT end with /": {
			paths:        []string{"/foo:" + fooPath},
			images:       []string{"localhost:5000/foobar:latest"},
			files:        []string{"foobar/"},
			requireEmpty: false,
			expectedErr:  "invalid file: foobar/ must not end with a slash",
		},
		"multiple files must NOT end with /": {
			paths:        []string{"/foo:" + fooPath},
			images:       []string{"localhost:5000/foobar:latest"},
			files:        []string{"foo", "bar/"},
			requireEmpty: false,
			expectedErr:  "invalid file: bar/ must not end with a slash",
		},
		"invalid image reference format": {
			paths:        []string{"/foo:" + fooPath},
			images:       []string{"localhost:5000/foobar/"},
			files:        nil,
			requireEmpty: false,
			expectedErr:  "\"localhost:5000/foobar/\" is not a valid image reference: invalid reference format",
		},
		"missing image ID": {
			paths:        []string{"/foo:" + fooPath},
			images:       []string{"localhost:5000"},
			files:        nil,
			requireEmpty: false,
			expectedErr:  "source image must point to an image ID or image tag",
		},
		"missing image tag": {
			paths:        []string{"/foo:" + fooPath},
			images:       []string{"localhost:5000/foobar"},
			files:        nil,
			requireEmpty: false,
			expectedErr:  "source image must point to an image ID or image tag",
		},
	}
	for testName, test := range tests {
		if _, err := parseMappings(test.images, test.paths, test.files, test.requireEmpty); err != nil {
			if !strings.Contains(err.Error(), test.expectedErr) {
				t.Fatalf("[%s] error not expected: %+v", testName, err)
			}
		} else if len(test.expectedErr) != 0 {
			t.Fatalf("[%s] \ngot: nil\nwant: %v, ", testName, test.expectedErr)
		}
	}
}
