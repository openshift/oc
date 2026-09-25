package release

import (
	"bytes"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_copyAndReplace(t *testing.T) {
	buffer := 4
	tests := []struct {
		name         string
		input        string
		replacements []replacement
		expected     string
		error        string
	}{
		{
			name:  "buffer too small",
			input: "1234",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aaaaa"),
					value:  "A",
				},
			},
			error: "the buffer size must be greater than 5 bytes to find rep-A",
		},
		{
			name:     "buffer too large",
			input:    "123",
			expected: "123",
		},
		{
			name:  "value too large",
			input: "1234",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "AA",
				},
			},
			error: "the rep-A value has 2 bytes, but the maximum replacement length is 1",
		},
		{
			name:     "A beginning of file",
			input:    "aa345678",
			expected: "A\x00345678",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
			},
		},
		{
			name:     "A end of buffer",
			input:    "12aa5678",
			expected: "12A\x005678",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
			},
		},
		{
			name:     "A cross buffer",
			input:    "123aa678",
			expected: "123A\x00678",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
			},
		},
		{
			name:     "A beginning of buffer",
			input:    "1234aa78",
			expected: "1234A\x0078",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
			},
		},
		{
			name:     "A end of file",
			input:    "123456aa",
			expected: "123456A\x00",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
			},
		},
		{
			name:     "A buffer too large",
			input:    "12345aa",
			expected: "12345A\x00",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
			},
		},
		{
			name:     "AB beginning of file",
			input:    "aabb5678",
			expected: "A\x00B\x005678",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
				{
					name:   "rep-B",
					marker: []byte("bb"),
					value:  "B",
				},
			},
		},
		{
			name:     "BA beginning of file",
			input:    "bbaa5678",
			expected: "B\x00A\x005678",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
				{
					name:   "rep-B",
					marker: []byte("bb"),
					value:  "B",
				},
			},
		},
		{
			name:     "AB end of buffer",
			input:    "1234aabb",
			expected: "1234A\x00B\x00",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
				{
					name:   "rep-B",
					marker: []byte("bb"),
					value:  "B",
				},
			},
		},
		{
			name:     "AB cross buffer",
			input:    "123aa6bb",
			expected: "123A\x006B\x00",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
				{
					name:   "rep-B",
					marker: []byte("bb"),
					value:  "B",
				},
			},
		},
		{
			name:     "AB end of file",
			input:    "1234aabb",
			expected: "1234A\x00B\x00",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
				{
					name:   "rep-B",
					marker: []byte("bb"),
					value:  "B",
				},
			},
		},
		{
			name:     "BA end of file",
			input:    "1234bbaa",
			expected: "1234B\x00A\x00",
			replacements: []replacement{
				{
					name:   "rep-A",
					marker: []byte("aa"),
					value:  "A",
				},
				{
					name:   "rep-B",
					marker: []byte("bb"),
					value:  "B",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bytes.NewReader([]byte(tt.input))
			w := &bytes.Buffer{}
			err := copyAndReplace(nil, w, r, buffer, tt.replacements, "test")
			if (err == nil && tt.error != "") || (err != nil && err.Error() != tt.error) {
				t.Fatalf("unexpected error: %v != %v", err, tt.error)
			}
			actual := w.String()
			if actual != tt.expected {
				t.Fatalf("unexpected response body: %q != %q", actual, tt.expected)
			}
		})
	}
}

func TestSelectExtractTargets(t *testing.T) {
	available := []extractTarget{
		{Command: "oc"},
		{Command: "openshift-install"},
		{Command: "openshift-install-fips"},
		{Command: "openshift-baremetal-install", Optional: true},
		{Command: "ccoctl"},
	}

	commandNames := func(targets []extractTarget) []string {
		names := make([]string, 0, len(targets))
		for _, target := range targets {
			names = append(names, target.Command)
		}
		return names
	}

	tests := []struct {
		name     string
		command  string
		expected []string
	}{
		{
			name:     "tools includes non-optional commands",
			command:  "",
			expected: []string{"oc", "openshift-install", "openshift-install-fips", "ccoctl"},
		},
		{
			name:     "command selects matching targets including optional ones",
			command:  "openshift-baremetal-install",
			expected: []string{"openshift-baremetal-install"},
		},
		{
			name:     "command selects install fips",
			command:  "openshift-install-fips",
			expected: []string{"openshift-install-fips"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commandNames(selectExtractTargets(available, tt.command))
			if diff := cmp.Diff(tt.expected, got); diff != "" {
				t.Fatalf("selectExtractTargets() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDefaultExtractTargetsIncludeInstallFipsInTools(t *testing.T) {
	type fipsTarget struct {
		OS, Arch, Command, Image, From, ArchiveFormat string
		Optional                                      bool
		InjectReleaseImage                            bool
		InjectReleaseVersion                          bool
		InjectReleaseArchitecture                     bool
	}

	var got *fipsTarget
	for _, target := range defaultExtractTargets() {
		if target.Command == "openshift-install-fips" {
			got = &fipsTarget{
				OS:                        target.OS,
				Arch:                      target.Arch,
				Command:                   target.Command,
				Image:                     target.Mapping.Image,
				From:                      target.Mapping.From,
				ArchiveFormat:             target.ArchiveFormat,
				Optional:                  target.Optional,
				InjectReleaseImage:        target.InjectReleaseImage,
				InjectReleaseVersion:      target.InjectReleaseVersion,
				InjectReleaseArchitecture: target.InjectReleaseArchitecture,
			}
			break
		}
	}
	if got == nil {
		t.Fatal("expected openshift-install-fips extract target")
	}

	want := &fipsTarget{
		OS:                        "linux",
		Arch:                      targetReleaseArch,
		Command:                   "openshift-install-fips",
		Image:                     "baremetal-installer",
		From:                      "usr/bin/openshift-install",
		ArchiveFormat:             "openshift-install-fips-%s.tar.gz",
		InjectReleaseImage:        true,
		InjectReleaseVersion:      true,
		InjectReleaseArchitecture: true,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("openshift-install-fips target mismatch (-want +got):\n%s", diff)
	}

	toolsCommands := make([]string, 0)
	for _, target := range selectExtractTargets(defaultExtractTargets(), "") {
		toolsCommands = append(toolsCommands, target.Command)
	}
	found := false
	for _, command := range toolsCommands {
		if command == "openshift-install-fips" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected --tools to include openshift-install-fips, got %v", toolsCommands)
	}
}
