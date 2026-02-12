---
description: Review a pull request by running build, tests, verification, and applying Go style improvements
argument-hint: [base-git-branch]
---

## Name

pr-review

## Synopsis

```
/pr-review [base-git-branch]
```

## Description

Activate the PR Review skill and perform a comprehensive review of the current changes.

This will:
1. Run `make oc` to verify the code compiles (faster than `make build`)
2. Run `make test` to execute the test suite (known failure in `pkg/cli` can be ignored)
3. Run `make verify` to check formatting, linting, conventions, and generated code
4. Review the code and apply Go best practices from Effective Go
5. Provide a detailed summary of findings and any fixes applied

The review process is specifically tailored for oc (OpenShift CLI), which is based on kubectl and provides both kubectl commands and OpenShift-specific functionality.

## Arguments

- **$1** (base-git-branch): Optional. The base branch to review PR changes against. This is `main` by default.
