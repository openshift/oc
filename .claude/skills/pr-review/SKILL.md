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

### 1. Dependencies Verification

Ensure Go dependencies are consistent by running:

- `go mod tidy -diff`
  - This command ensures that `go.mod` and `go.sum` are consistent and match the source code in the module.

### 2. Build Verification

Run the build to ensure code compiles:

```bash
make oc
```

- If build fails, report errors and stop the review
- If build succeeds, proceed to testing
- Note: Use `make oc` instead of `make build` to avoid building for all architectures (faster)

### 3. Code Verification

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

### 4. Test Execution

Run the test suite to verify functionality:

```bash
make test
```

- Report any test failures with details
- If critical tests fail, flag for immediate attention
- Proceed even if some tests fail (document them)
- **Known Issue**: Test failure in `github.com/openshift/oc/pkg/cli` (kubeconfig error) can be ignored

### 5. Code Review & Go Style Application

After running the above checks, review the changed code and apply Go best practices.
Start by:

- Load changes against the base branch by using `git diff`.
  The base branch is `main` by default, but it can be overwritten by `[base-git-branch]`
  argument when this skill is invoked using `pr-review` command directly.
- Understand the scope of the changes.

Then proceed to review. Follow these steps:

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

- **Breaking Changes**:
  - Ensure that the command line API is backwards-compatible
    - Check for CLI flag removals or renames
    - Check for changes in command line arguments

- **Code Quality**:
  - Look for potential race conditions
  - Check for resource leaks (unclosed files, connections, goroutine leaks)
    - Goroutine leak patterns to watch:
      - Goroutines without context cancellation handling
      - Missing `select` with `ctx.Done()` case
      - Unbounded channel operations without timeouts
      - `go func()` without proper lifecycle management
      - Use `errgroup` or `sync.WaitGroup` for coordinated goroutines
  - Verify proper context propagation
  - Ensure appropriate logging levels

- **Documentation**:
  - All exported functions/types should have doc comments
  - CLI command help text should be clear and complete
  - Complex logic should have explanatory comments

### 6. Apply Fixes

Based on the review:
- Fix any linting issues automatically where safe
- Apply `gofmt` and `goimports` formatting
- Suggest or implement idiomatic Go improvements
- Document any issues that require manual review

### 7. Summary

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