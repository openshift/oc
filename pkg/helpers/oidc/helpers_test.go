package oidc

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestExternalOIDCExtraArgIsSet(t *testing.T) {
	tests := []struct {
		extraArgs     []string
		param         string
		expectedFound bool
		expectedValue string
	}{
		{
			extraArgs: []string{
				"--parameter-1=value-1",
				"--parameter-2=value-2",
			},
			param:         "--parameter-1",
			expectedFound: true,
			expectedValue: "value-1",
		},
		{
			extraArgs: []string{
				"--parameter-1=value-1",
				"--parameter-2=value-2",
			},
			param:         "--parameter-3",
			expectedFound: false,
			expectedValue: "",
		},
		{
			extraArgs: []string{
				"--parameter-1=value-1",
				"--parameter-2=value-2",
			},
			param:         "--parameter-1=value-1",
			expectedFound: false,
			expectedValue: "",
		},
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			actualFound, actualValue := ExternalOIDCExtraArgIsSet(test.extraArgs, test.param)
			if test.expectedFound != actualFound {
				t.Errorf("expected found %t value does not match with the actual %t", test.expectedFound, actualFound)
			}

			if test.expectedValue != actualValue {
				t.Errorf("expected parameter value %s does not match with the actual parameter value %s", test.expectedValue, actualValue)
			}
		})
	}
}

func TestExternalOIDCSetOrOverrideArgs(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		extraArgs    []string
		disallowed   map[string]struct{}
		expectedArgs []string
	}{
		{
			name: "change all values",
			args: []string{
				"--parameter-1=value-1",
				"--parameter-2=value-2",
				"--parameter-3=value-3",
			},
			extraArgs: []string{
				"--parameter-3=value-changed",
				"--parameter-4=new-value",
			},
			disallowed: map[string]struct{}{
				"--parameter-2": {},
			},
			expectedArgs: []string{
				"--parameter-1=value-1",
				"--parameter-2=value-2",
				"--parameter-3=value-changed",
				"--parameter-4=new-value",
			},
		},
		{
			name: "discard disallowed ones",
			args: []string{
				"--parameter-1=value-1",
				"--parameter-2=value-2",
				"--parameter-3=value-3",
			},
			extraArgs: []string{
				"--parameter-2=value-changed",
				"--parameter-4=new-value",
			},
			disallowed: map[string]struct{}{
				"--parameter-2": {},
			},
			expectedArgs: []string{
				"--parameter-1=value-1",
				"--parameter-2=value-2",
				"--parameter-3=value-3",
				"--parameter-4=new-value",
			},
		},
		{
			name: "add new one",
			args: []string{
				"--parameter-1=value-1",
				"--parameter-2=value-2",
				"--parameter-3=value-3",
			},
			extraArgs: []string{
				"--parameter-4=new-value",
			},
			disallowed: map[string]struct{}{
				"--parameter-2": {},
			},
			expectedArgs: []string{
				"--parameter-1=value-1",
				"--parameter-2=value-2",
				"--parameter-3=value-3",
				"--parameter-4=new-value",
			},
		},
		{
			name: "new without value and invalid disallowed",
			args: []string{
				"--parameter-1=value-1",
				"--parameter-2=value-2",
				"--parameter-3=value-3",
			},
			extraArgs: []string{
				"--parameter-4",
				"--parameter-2===",
			},
			disallowed: map[string]struct{}{
				"--parameter-2": {},
			},
			expectedArgs: []string{
				"--parameter-1=value-1",
				"--parameter-2=value-2",
				"--parameter-3=value-3",
				"--parameter-4",
			},
		},
		{
			name: "invalid new and disallowed",
			args: []string{
				"--parameter-1=value-1",
				"--parameter-2=value-2",
				"--parameter-3=value-3",
			},
			extraArgs: []string{
				"--parameter-4!",
				"--parameter-2!",
			},
			disallowed: map[string]struct{}{
				"--parameter-2": {},
			},
			expectedArgs: []string{
				"--parameter-1=value-1",
				"--parameter-2=value-2",
				"--parameter-3=value-3",
				"--parameter-4!",
				"--parameter-2!",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actualArgs := ExternalOIDCSetOrOverrideArgs(test.args, test.extraArgs, test.disallowed)
			if !cmp.Equal(test.expectedArgs, actualArgs) {
				t.Errorf("expected arguments %v does not match with the actual arguments %v", test.expectedArgs, actualArgs)
			}
		})
	}
}
