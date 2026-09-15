package preflight

/*
================================================================================
PREFLIGHT TYPES TESTS
================================================================================

This file tests the types and interfaces used by the preflight validation system.
These types form the abstraction layer that allows swapping between client-side
and status-published validation implementations.

--------------------------------------------------------------------------------
TEST COVERAGE AT-A-GLANCE
--------------------------------------------------------------------------------

VALIDATION RESULT CONSTRUCTION
  - NewValidationResult creates result with current and target
  - NewValidationResult initializes status to Unknown
  - NewValidationResult initializes empty checks slice

VALIDATION RESULT METHODS
  - AddCheckResult appends to checks slice
  - AddCheckResult maintains order
  - Status becomes Available when all checks pass
  - Status becomes Unavailable when a check fails
  - Status becomes Unknown when a check is unknown

CHECK RESULT CONSTRUCTION
  - CheckResult fields set correctly

CHECK RESULT METHODS
  - String() formats passed check correctly
  - String() formats failed check with message
  - String() formats unknown check with message

VALIDATOR INTERFACE (CONTRACT TESTS)
  - ClientSideValidator implements Validator interface
  - (FUTURE) StatusPublishedValidator implements Validator interface

--------------------------------------------------------------------------------
*/

import (
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

// TestNewValidationResult tests ValidationResult construction
// TestValidationResult_AddCheckResult tests adding check results
// TestValidationResult_Status_AllPass tests status when all checks pass
func TestValidationResult_Status_AllPass(t *testing.T) {
	current := TopologyState{ControlPlane: configv1.SingleReplicaTopologyMode, Infrastructure: configv1.SingleReplicaTopologyMode}
	target := TopologyState{ControlPlane: configv1.HighlyAvailableTopologyMode, Infrastructure: configv1.HighlyAvailableTopologyMode}
	result := NewValidationResult(current, target)

	result.AddCheck(CheckResult{Name: "Check 1", Severity: CheckSeverityWarning, Status: CheckStatusPassed})
	result.AddCheck(CheckResult{Name: "Check 2", Severity: CheckSeverityError, Status: CheckStatusPassed})

	if result.Status != ValidationStatusReady {
		t.Errorf("expected status=Ready when all checks pass, got %s", result.Status)
	}
}

// TestValidationResult_Status_SomeFail tests status when checks fail
func TestValidationResult_Status_SomeFail(t *testing.T) {
	current := TopologyState{ControlPlane: configv1.SingleReplicaTopologyMode, Infrastructure: configv1.SingleReplicaTopologyMode}
	target := TopologyState{ControlPlane: configv1.HighlyAvailableTopologyMode, Infrastructure: configv1.HighlyAvailableTopologyMode}
	result := NewValidationResult(current, target)

	result.AddCheck(CheckResult{Name: "Check 1", Severity: CheckSeverityWarning, Status: CheckStatusPassed})
	result.AddCheck(CheckResult{Name: "Check 2", Severity: CheckSeverityError, Status: CheckStatusFailed, Message: "failed"})

	if result.Status != ValidationStatusNotReady {
		t.Errorf("expected status=Not Ready when a check fails, got %s", result.Status)
	}
}

// TestValidationResult_Status_Unknown tests status when a check is unknown
// TestCheckResult_Construction tests CheckResult construction
// TestCheckResult_String_Passed tests formatting of passed check
// TestCheckResult_String_Failed tests formatting of failed check with message
// TestCheckResult_String_Unknown tests formatting of unknown check
// TestClientSideValidator_ImplementsInterface tests interface compliance
func TestClientSideValidator_ImplementsInterface(t *testing.T) {
	// Compile-time check that ClientSideValidator implements Validator
	var _ Validator = (*ClientSideValidator)(nil)
}

func TestValidationResultFailureHelpers(t *testing.T) {
	tests := []struct {
		name        string
		checks      []CheckResult
		errorFailed bool
		warnFailed  bool
		errorText   []string
	}{
		{
			name: "all passed",
			checks: []CheckResult{
				{Name: "error", Severity: CheckSeverityError, Status: CheckStatusPassed},
				{Name: "warning", Severity: CheckSeverityWarning, Status: CheckStatusPassed},
			},
		},
		{
			name:        "error failed and warning unknown",
			errorFailed: true,
			warnFailed:  true,
			checks: []CheckResult{
				{Name: "unsupported", Severity: CheckSeverityError, Status: CheckStatusFailed, Message: "not supported"},
				{Name: "nodes", Severity: CheckSeverityWarning, Status: CheckStatusUnknown, Message: "API unavailable"},
			},
			errorText: []string{"1 error(s):", "unsupported: not supported", "1 warning(s) - could not complete:", "nodes: API unavailable"},
		},
		{
			name:        "error unknown and warning failed",
			errorFailed: true,
			warnFailed:  true,
			checks: []CheckResult{
				{Name: "feature gate", Severity: CheckSeverityError, Status: CheckStatusUnknown, Message: "read failed"},
				{Name: "etcd", Severity: CheckSeverityWarning, Status: CheckStatusFailed, Message: "no quorum"},
			},
			errorText: []string{"1 error(s) - could not complete:", "feature gate: read failed", "1 warning(s):", "etcd: no quorum"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := NewValidationResult(TopologyState{}, TopologyState{})
			for _, check := range tc.checks {
				result.AddCheck(check)
			}

			if got := result.HasErrorCheckFailures(); got != tc.errorFailed {
				t.Errorf("HasErrorCheckFailures() = %t, want %t", got, tc.errorFailed)
			}
			if got := result.HasWarningCheckFailures(); got != tc.warnFailed {
				t.Errorf("HasWarningCheckFailures() = %t, want %t", got, tc.warnFailed)
			}

			err := result.Error()
			if len(tc.errorText) == 0 {
				if err != nil {
					t.Fatalf("Error() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Error() = nil, want aggregate error")
			}
			for _, expected := range tc.errorText {
				if !strings.Contains(err.Error(), expected) {
					t.Errorf("Error() = %q, want it to contain %q", err, expected)
				}
			}
		})
	}
}

func TestValidationResultUnknownPrecedence(t *testing.T) {
	for _, checks := range [][]CheckResult{
		{
			{Name: "unknown", Severity: CheckSeverityWarning, Status: CheckStatusUnknown},
			{Name: "failed", Severity: CheckSeverityWarning, Status: CheckStatusFailed},
		},
		{
			{Name: "failed", Severity: CheckSeverityWarning, Status: CheckStatusFailed},
			{Name: "unknown", Severity: CheckSeverityWarning, Status: CheckStatusUnknown},
		},
	} {
		result := NewValidationResult(TopologyState{}, TopologyState{})
		for _, check := range checks {
			result.AddCheck(check)
		}
		if result.Status != ValidationStatusUnknown {
			t.Errorf("status = %s, want %s", result.Status, ValidationStatusUnknown)
		}
	}
}
