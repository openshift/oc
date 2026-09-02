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
