---
name: PR Review
description: "Comprehensive PR review for oc (OpenShift CLI). Runs build, tests, and linting, then applies Go style improvements and provides detailed code review feedback."
---

# PR Review

Perform a comprehensive review of pull requests for the oc repository, which is a CLI tool based on kubectl that provides kubectl commands plus OpenShift-specific functionality.

## When to Apply

Use this skill when:
- Reviewing a pull request
- User asks to review code changes
- User requests `/pr-review` or similar commands

## Review Process

Follow these steps in order:

### 1. Build Verification
Run the build to ensure code compiles:
```bash
make oc
```

- If build fails, report errors and stop the review
- If build succeeds, proceed to testing
- Note: Use `make oc` instead of `make build` to avoid building for all architectures (faster)

### 2. Test Execution
Run the test suite to verify functionality:
```bash
make test
```

- Report any test failures with details
- If critical tests fail, flag for immediate attention
- Proceed even if some tests fail (document them)
- **Known Issue**: Test failure in `github.com/openshift/oc/pkg/cli` (kubeconfig error) can be ignored

### 3. Verification
Run verification checks to catch style and potential issues:
```bash
make verify
```

This runs multiple verification targets including:
- `verify-gofmt` - Go formatting checks
- `verify-golint` - Linting checks
- `verify-govet` - Go vet checks
- `verify-cli-conventions` - CLI-specific conventions
- `verify-generated-completions` - Generated code verification

- Report any verification errors or warnings
- Note any patterns that need addressing

### 4. Code Review & Go Style Application

After running the above checks, review the changed code and apply Go best practices:

- **Effective Go Principles**: Apply the Effective Go skill automatically
  - Use `gofmt` for formatting
  - Follow Go naming conventions (MixedCaps/mixedCaps, no underscores)
  - Ensure proper error handling (no ignored errors)
  - Check for idiomatic Go patterns

- **oc-Specific Considerations**:
  - Ensure kubectl compatibility is maintained
  - Verify OpenShift-specific commands follow existing patterns
  - Check that CLI output follows consistent formatting
  - Validate flag definitions match kubectl conventions where applicable

- **Code Quality**:
  - Look for potential race conditions
  - Check for resource leaks (unclosed files, connections)
  - Verify proper context propagation
  - Ensure appropriate logging levels

- **Documentation**:
  - All exported functions/types should have doc comments
  - CLI command help text should be clear and complete
  - Complex logic should have explanatory comments

### 5. Apply Fixes

Based on the review:
- Fix any linting issues automatically where safe
- Apply `gofmt` and `goimports` formatting
- Suggest or implement idiomatic Go improvements
- Document any issues that require manual review

### 6. Summary

Provide a structured summary:
- ✅ Build status
- ✅ Test results (pass/fail counts)
- ✅ Linting status
- 📝 Code quality observations
- 🔧 Changes applied (if any)
- ⚠️  Issues requiring attention

## Key Checks for oc

Since oc is built on kubectl:
- Verify upstream kubectl compatibility
- Check for proper use of kubectl libraries
- Ensure OpenShift-specific features are clearly separated
- Validate that CLI behavior matches kubectl conventions

## References

- [Effective Go](https://go.dev/doc/effective_go)
- [oc Repository](https://github.com/openshift/oc)
- [kubectl Conventions](https://kubernetes.io/docs/reference/kubectl/conventions/)