package preflight

import (
	"context"
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
)

// TopologyState represents the control plane and infrastructure topology modes
type TopologyState struct {
	ControlPlane   configv1.TopologyMode
	Infrastructure configv1.TopologyMode
}

// Validator validates whether a topology transition is possible.
// This interface allows swapping between client-side validation (current)
// and status-published validation (future) without changing command code.
type Validator interface {
	// Validate checks if transition to target topology is possible.
	// Returns validation results for each check and overall availability.
	Validate(ctx context.Context, current, target TopologyState) (*ValidationResult, error)
}

// CheckSeverity indicates the severity of a validation check failure.
type CheckSeverity string

const (
	// CheckSeverityError indicates a check failure that blocks the transition.
	// Cannot be bypassed with --allow-transition-with-warnings.
	// Example: Unsupported transition (HA → SNO).
	CheckSeverityError CheckSeverity = "Error"

	// CheckSeverityWarning indicates a check failure that can be bypassed.
	// Can proceed with --allow-transition-with-warnings.
	// Example: Insufficient node count, etcd quorum issues.
	CheckSeverityWarning CheckSeverity = "Warning"
)

// CheckStatus represents the outcome of a validation check.
type CheckStatus string

const (
	// CheckStatusPassed indicates the check succeeded
	CheckStatusPassed CheckStatus = "Passed"
	// CheckStatusFailed indicates the check failed (known issue blocking transition)
	CheckStatusFailed CheckStatus = "Failed"
	// CheckStatusUnknown indicates the check could not complete (API error, etc.)
	CheckStatusUnknown CheckStatus = "Unknown"
)

// ValidationStatus represents the overall outcome of validation.
type ValidationStatus string

const (
	// ValidationStatusReady indicates the transition can proceed (all checks passed)
	ValidationStatusReady ValidationStatus = "Ready"
	// ValidationStatusNotReady indicates the transition cannot proceed (checks failed)
	ValidationStatusNotReady ValidationStatus = "Not Ready"
	// ValidationStatusUnknown indicates validation could not complete (API errors, etc.)
	ValidationStatusUnknown ValidationStatus = "Unknown"
)

// ValidationResult contains the outcome of preflight validation checks
// for a topology transition.
type ValidationResult struct {
	// Current topology state of the cluster
	Current TopologyState

	// Target topology state being validated
	Target TopologyState

	// Status indicates the overall validation outcome
	Status ValidationStatus

	// Checks contains individual validation check results
	Checks []CheckResult
}

// NewValidationResult creates a new validation result with status initialized to Unknown
func NewValidationResult(current, target TopologyState) *ValidationResult {
	return &ValidationResult{
		Current: current,
		Target:  target,
		Status:  ValidationStatusUnknown, // Unknown until checks run
		Checks:  []CheckResult{},
	}
}

// CheckResult represents the outcome of a single validation check.
type CheckResult struct {
	// Name is the human-readable name of the check
	Name string

	// Severity indicates whether a check failure blocks the transition
	Severity CheckSeverity

	// Status indicates whether the check passed, failed, or could not complete
	Status CheckStatus

	// Message provides details about the check result.
	// Empty for passed checks, contains error details for failed/unknown checks.
	Message string
}

// AddCheck appends a check result to the validation result and updates overall status
func (vr *ValidationResult) AddCheck(check CheckResult) {
	// Check if this is the first check (to distinguish initial Unknown from API error Unknown)
	isFirstCheck := len(vr.Checks) == 0

	vr.Checks = append(vr.Checks, check)

	// Update overall status based on check results
	// Priority: Unknown (API error) > Not Ready (check failed) > Ready (all passed)
	switch check.Status {
	case CheckStatusPassed:
		// Set to Ready if this is the first check (initial Unknown state)
		// Keep Ready if already Ready
		// Do NOT transition from Unknown to Ready if Unknown came from a previous API error
		if isFirstCheck && vr.Status == ValidationStatusUnknown {
			vr.Status = ValidationStatusReady
		} else if vr.Status == ValidationStatusReady {
			// Keep Ready status
		}
		// else: keep current status (Not Ready/Unknown from previous checks)

	case CheckStatusFailed:
		// Set to Not Ready if this is the first check (initial Unknown state) OR status is Ready
		// Keep Unknown if it came from a previous API error (len(vr.Checks) > 1 && status is Unknown)
		if isFirstCheck || vr.Status == ValidationStatusReady {
			vr.Status = ValidationStatusNotReady
		}
		// else: keep Unknown status from previous API error

	case CheckStatusUnknown:
		// Unknown (API error) takes precedence over everything
		vr.Status = ValidationStatusUnknown
	}
}

// HasErrorCheckFailures returns true if any Error-severity checks failed or are unknown.
// Error-severity checks cannot be bypassed with --allow-transition-with-warnings.
func (vr *ValidationResult) HasErrorCheckFailures() bool {
	for _, check := range vr.Checks {
		if check.Severity == CheckSeverityError && check.Status != CheckStatusPassed {
			return true
		}
	}
	return false
}

// HasWarningCheckFailures returns true if any Warning-severity checks failed or are unknown.
// Warning-severity checks can be bypassed with --allow-transition-with-warnings.
func (vr *ValidationResult) HasWarningCheckFailures() bool {
	for _, check := range vr.Checks {
		if check.Severity == CheckSeverityWarning && check.Status != CheckStatusPassed {
			return true
		}
	}
	return false
}

// Error returns an aggregate error if the validation status is not Available.
// Returns nil if status is Available.
func (vr *ValidationResult) Error() error {
	if vr.Status == ValidationStatusReady {
		return nil
	}

	var errorFailed, errorUnknown []string
	var warningFailed, warningUnknown []string

	for _, check := range vr.Checks {
		var dest *[]string
		switch check.Status {
		case CheckStatusFailed:
			if check.Severity == CheckSeverityError {
				dest = &errorFailed
			} else {
				dest = &warningFailed
			}
		case CheckStatusUnknown:
			if check.Severity == CheckSeverityError {
				dest = &errorUnknown
			} else {
				dest = &warningUnknown
			}
		default:
			continue
		}
		*dest = append(*dest, fmt.Sprintf("%s: %s", check.Name, check.Message))
	}

	var parts []string

	// Error-severity checks listed first (cannot be bypassed)
	if len(errorFailed) > 0 {
		parts = append(parts, fmt.Sprintf("%d error(s):\n  - %s", len(errorFailed), strings.Join(errorFailed, "\n  - ")))
	}
	if len(errorUnknown) > 0 {
		parts = append(parts, fmt.Sprintf("%d error(s) - could not complete:\n  - %s", len(errorUnknown), strings.Join(errorUnknown, "\n  - ")))
	}

	// Warning-severity checks listed second (can be bypassed)
	if len(warningFailed) > 0 {
		parts = append(parts, fmt.Sprintf("%d warning(s):\n  - %s", len(warningFailed), strings.Join(warningFailed, "\n  - ")))
	}
	if len(warningUnknown) > 0 {
		parts = append(parts, fmt.Sprintf("%d warning(s) - could not complete:\n  - %s", len(warningUnknown), strings.Join(warningUnknown, "\n  - ")))
	}

	if len(parts) == 0 {
		return nil
	}

	return fmt.Errorf("%s", strings.Join(parts, "\n\n"))
}

// String returns a formatted string representation of the check result
func (cr CheckResult) String() string {
	switch cr.Status {
	case CheckStatusPassed:
		return fmt.Sprintf("  %s: passed", cr.Name)
	case CheckStatusFailed:
		return fmt.Sprintf("  %s: %s", cr.Name, cr.Message)
	case CheckStatusUnknown:
		return fmt.Sprintf("  %s: unknown (%s)", cr.Name, cr.Message)
	default:
		return fmt.Sprintf("  %s: %s", cr.Name, cr.Message)
	}
}
