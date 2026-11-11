package transition

import (
	"strings"
	"testing"

	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// TestValidateStatusOutput checks the status command accepts only json and
// yaml output formats.
func TestValidateStatusOutput(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		wantErr string
	}{
		{name: "empty"},
		{name: "json", format: "json"},
		{name: "yaml", format: "yaml"},
		{name: "invalid format", format: "wide", wantErr: "must be json or yaml"},
		{name: "name rejected", format: "name", wantErr: "must be json or yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := newStatusOptions(genericclioptions.NewTestIOStreamsDiscard())
			*o.printFlags.OutputFormat = tt.format

			err := o.validate()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("got %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}
