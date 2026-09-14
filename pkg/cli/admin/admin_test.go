package admin

import (
	"testing"

	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func TestTransitionCommandFeatureGate(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		present bool
	}{
		{name: "unset"},
		{name: "false", value: "false"},
		{name: "true", value: "true", present: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(transitionTopologyFeatureGate, tc.value)
			cmd := NewCommandAdmin(nil, genericiooptions.NewTestIOStreamsDiscard())

			var transitionFound bool
			for _, subcommand := range cmd.Commands() {
				if subcommand.Name() == "transition" {
					transitionFound = true
					var topologyFound, statusFound bool
					for _, transitionSubcommand := range subcommand.Commands() {
						topologyFound = topologyFound || transitionSubcommand.Name() == "topology"
						statusFound = statusFound || transitionSubcommand.Name() == "status"
					}
					if !topologyFound || !statusFound {
						t.Errorf("transition subcommands topology/status = %t/%t, want true/true", topologyFound, statusFound)
					}
				}
			}
			if transitionFound != tc.present {
				t.Errorf("transition command present = %t, want %t", transitionFound, tc.present)
			}
		})
	}
}
