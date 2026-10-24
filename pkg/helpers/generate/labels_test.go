package generate

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantLabels map[string]string
		wantErr    string
	}{
		{
			name:       "single label",
			input:      "app=web",
			wantLabels: map[string]string{"app": "web"},
		},
		{
			name:       "multiple labels",
			input:      "app=web,env=prod",
			wantLabels: map[string]string{"app": "web", "env": "prod"},
		},
		{
			name:       "empty value",
			input:      "app=",
			wantLabels: map[string]string{"app": ""},
		},
		{
			name:       "duplicate key uses last value",
			input:      "app=old,app=new",
			wantLabels: map[string]string{"app": "new"},
		},
		{
			name:       "spaces are preserved",
			input:      " app = web ",
			wantLabels: map[string]string{" app ": " web "},
		},
		{
			name:    "empty input",
			wantErr: "no label spec passed",
		},
		{
			name:    "missing equals sign",
			input:   "app",
			wantErr: "unexpected label spec: app",
		},
		{
			name:    "extra equals sign",
			input:   "app=web=extra",
			wantErr: "unexpected label spec: app=web=extra",
		},
		{
			name:    "empty key",
			input:   "=web",
			wantErr: "unexpected empty label key",
		},
		{
			name:    "invalid label after valid label",
			input:   "app=web,broken",
			wantErr: "unexpected label spec: broken",
		},
		{
			name:    "trailing comma",
			input:   "app=web,",
			wantErr: "unexpected label spec: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels, err := ParseLabels(tt.input)
			if diff := cmp.Diff(tt.wantLabels, labels); diff != "" {
				t.Errorf("ParseLabels(%q) labels mismatch (-want +got):\n%s", tt.input, diff)
			}
			var gotErr string
			if err != nil {
				gotErr = err.Error()
			}
			if diff := cmp.Diff(tt.wantErr, gotErr); diff != "" {
				t.Errorf("ParseLabels(%q) error mismatch (-want +got):\n%s", tt.input, diff)
			}
		})
	}
}
