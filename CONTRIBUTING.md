# Contributing to OpenShift Client (oc)

Thank you for your interest in contributing to the OpenShift Client! This document provides guidelines and information for contributors.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Building oc](#building-oc)
- [Testing](#testing)
- [Making Changes](#making-changes)
- [Pull Request Process](#pull-request-process)
- [Code Conventions](#code-conventions)
- [Project Structure](#project-structure)
- [Dependencies](#dependencies)
- [Security](#security)
- [Getting Help](#getting-help)

## Code of Conduct

All contributors are expected to follow professional and respectful conduct when participating in this project. We are committed to providing a welcoming and inclusive environment for everyone.

## Getting Started

All contributions are welcome! The OpenShift Client uses the Apache 2.0 license and does not require any contributor agreement to submit patches. You can contribute by:

- Reporting bugs
- Suggesting new features
- Improving documentation
- Submitting code changes
- Reviewing pull requests

Please open issues for any bugs or problems you encounter. You can also get involved with the upstream [kubectl](https://github.com/kubernetes/kubectl) and [Kubernetes project](https://github.com/kubernetes/kubernetes).

## Development Setup

### Prerequisites

**Required Go Version:** Check `go.mod` for the current required version.

**System Dependencies:**

Fedora/CentOS/RHEL:
```bash
dnf install krb5-devel gpgme-devel libassuan-devel
```

macOS:
```bash
brew install heimdal gpgme
```

Windows users should use WSL2 with the Linux dependencies.

### Clone the Repository

```bash
git clone https://github.com/openshift/oc.git
cd oc
```

## Building oc

### Fast Build (Default)

```bash
make oc
```

This builds the `oc` binary in ~5-10 seconds. By default, debugging symbols are stripped for faster builds.

### Build with Debug Symbols

```bash
make STRIP_DEBUGGING_SYMBOLS=false oc
```

### Full Build (Including Tests and Tools)

```bash
make build
```

This builds:
- `oc` CLI binary
- `oc-tests-ext` test extension binary
- Development tools (clicheck, gendocs, genman)

### Cross-Platform Builds

```bash
# Build for specific platforms
make cross-build-darwin-amd64
make cross-build-darwin-arm64
make cross-build-linux-amd64
make cross-build-linux-arm64
make cross-build-windows-amd64

# Build for all platforms
make cross-build
```

Built binaries are placed in `_output/bin/<platform>`.

### View All Available Targets

```bash
make help
```

## Testing

All pull requests must pass automated tests including formatting checks, unit tests, and end-to-end tests.

### Running Tests Locally

**Unit Tests:**
```bash
make test
```

This runs the full unit test suite (~2-5 minutes).

**Verification (Formatting, Linting, CLI Conventions):**
```bash
make verify
```

This runs (~30-60 seconds):
- `go fmt` and `go vet` checks
- CLI convention validation
- Shell completion verification
- Kubernetes version alignment check

**Run Tests for a Single Package:**

Linux:
```bash
go test -tags 'include_gcs include_oss containers_image_openpgp gssapi' ./pkg/cli/admin/policy/...
```

macOS/Windows:
```bash
go test -tags 'include_gcs include_oss containers_image_openpgp' ./pkg/cli/admin/policy/...
```

### CLI Convention Checks

```bash
make verify-cli-conventions
```

This validates command structure using `tools/clicheck`.

### Shell Completions

After modifying commands, regenerate and verify completions:

```bash
make update-generated-completions
make verify-generated-completions
```

### End-to-End Tests (OTE Framework)

The repository uses the OpenShift Tests Extension (OTE) framework.

**Build the test binary:**
```bash
make build
```

**Run test suites:**
```bash
# Run all oc tests
./oc-tests-ext run-suite openshift/oc/all

# Run a specific test
./oc-tests-ext run-test "test-name"

# Run with JUnit output
./oc-tests-ext run-suite openshift/oc/all --junit-path=/tmp/junit-results/junit.xml
```

**List available tests:**
```bash
# List all test suites
./oc-tests-ext list-suites

# List tests in a specific suite
./oc-tests-ext list-tests openshift/oc/all
```

## Making Changes

### Understanding oc and kubectl Relationship

kubectl is a subset of oc — every kubectl command is available in oc. Commands fall into these categories:

**1. Pure kubectl wrappers** (wired via `pkg/cli/kubectlwrappers/wrappers.go`):
- Examples: get, apply, delete, exec, run, patch, replace, describe, edit, label, annotate, cp, wait, events, port-forward, attach, proxy, explain, diff, auth, api-resources, api-versions, cluster-info, kustomize, completion, plugin
- **Changes to these commands belong upstream in `k8s.io/kubectl`**

**2. kubectl wrappers with OpenShift extensions:**
- `create` (adds route, deployment-config, user, identity, image-stream, build)
- `scale`/`autoscale` (adds deploymentconfig support)
- `config` (adds admin-kubeconfig, refresh-ca-bundle)
- Admin commands: drain, cordon, uncordon, taint, certificates

**3. Commands that extend kubectl:**
- `logs` — adds Build, BuildConfig, DeploymentConfig, Jenkins pipeline support
- `expose` — adds OpenShift Route creation
- `rollout`/`rollback` — adds DeploymentConfig support

**4. Purely OpenShift-native commands:**
- Examples: new-app, new-build, start-build, cancel-build, import-image, tag, login, logout, whoami, project, projects, request-project, rsh, rsync, status, process, extract, observe, idle, image, registry, policy, secrets, service-accounts, get-token, and the entire `oc adm` subtree

### Command Implementation Pattern

All commands follow the **Complete/Validate/Run** pattern:

```go
type ExampleOptions struct {
    genericiooptions.IOStreams
    // command-specific fields
}

func NewExampleOptions(streams genericiooptions.IOStreams) *ExampleOptions {
    return &ExampleOptions{IOStreams: streams}
}

func NewCmdExample(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
    o := NewExampleOptions(streams)
    cmd := &cobra.Command{
        Use:     "example",
        Short:   "Brief description",
        Long:    templates.LongDesc(`...`),
        Example: templates.Examples(`...`),
        Run: func(cmd *cobra.Command, args []string) {
            kcmdutil.CheckErr(o.Complete(f, cmd, args))
            kcmdutil.CheckErr(o.Validate())
            kcmdutil.CheckErr(o.Run())
        },
    }
    // register flags
    return cmd
}

func (o *ExampleOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error { ... }
func (o *ExampleOptions) Validate() error { ... }
func (o *ExampleOptions) Run() error { ... }
```

### Critical Rules

**DO NOT:**

1. **Modify `pkg/cli/cli.go`** unless it is part of a kubectl rebase process
2. **Diverge from wrapped kubectl commands** — behavioral changes belong upstream
3. **Modify files under `vendor/`** — regenerate via `go mod tidy && go mod vendor`
4. **Edit generated files** — use `make update-generated-completions` to regenerate
5. **Remove commands, flags, or options without deprecation** — backwards compatibility is paramount

**DO:**

1. **Write unit tests for every change** — test coverage is expected by default
2. **Use cobra's deprecation mechanism** before removal: `cmd.Deprecated = "Use X instead"`
3. **Follow the Complete/Validate/Run pattern** for all new commands
4. **Place test fixtures in `testdata/` subdirectories** co-located with tests

## Pull Request Process

### Before Submitting

1. **Run verification:**
   ```bash
   make verify
   ```

2. **Run unit tests:**
   ```bash
   make test
   ```

3. **Test your changes manually** with a built `oc` binary

4. **Update documentation** if adding/modifying commands

5. **Regenerate completions** if modifying CLI structure:
   ```bash
   make update-generated-completions
   ```

### Submitting Your PR

1. **Fork the repository** and create a feature branch from `main`
2. **Keep PRs focused** — one logical change per PR
3. **Make your changes** following the code conventions
4. **Write clear commit messages** describing what and why. Reference Jira tickets where applicable (e.g., `OCPBUGS-12345: fix completion generation`)
5. **Push to your fork** and create a pull request
6. **Fill out the PR template** completely
7. **Respond to review feedback** promptly

### PR Requirements

- All CI checks must pass (formatting, unit tests, e2e tests)
- At least one approver listed in the `OWNERS` file must approve
- No merge conflicts with the base branch
- Commit messages follow project conventions
- The vendor directory must be up-to-date (CI will fail if stale)

## Code Conventions

### Error Handling

```go
// Wrap errors with context
return fmt.Errorf("failed to create resource: %w", err)

// Use CheckErr for command-level errors
kcmdutil.CheckErr(o.Run())

// Use Aggregate for batch errors
utilerrors.Aggregate(errs)
```

### Logging

```go
// Use k8s.io/klog/v2 with verbosity levels
klog.V(4).Infof("processing resource: %s", name)
```

### Clients

```go
// Obtain clients via Factory
config, err := f.ToRESTConfig()
kubeClient := kubernetes.NewForConfig(config)
buildClient := buildv1client.NewForConfig(config)
```

### Flags

```go
// Use typed flag setters
cmd.Flags().StringVar(&o.namespace, "namespace", "", "namespace to use")

// Use genericclioptions for common flag sets
configFlags := genericclioptions.NewConfigFlags(true)
printFlags := genericclioptions.NewPrintFlags("")
```

### Help Text

```go
// Use templates for long descriptions and examples
Long: templates.LongDesc(`
    Long description here.
    Can span multiple lines.
`),
Example: templates.Examples(`
    # Example command
    oc example-command --flag=value

    # Another example with comment (use # not //)
    oc example-command resource-name
`),
```

**Note:** Use `#` for comments in examples, not `//`. This is enforced by `tools/clicheck`.

### Testing

**Write table-driven unit tests:**

```go
func TestExample(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
        wantErr  bool
    }{
        {
            name:     "valid input",
            input:    "test",
            expected: "test-result",
            wantErr:  false,
        },
        // more test cases...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := Example(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("Example() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if result != tt.expected {
                t.Errorf("Example() = %v, want %v", result, tt.expected)
            }
        })
    }
}
```

**Use typed API struct builders:**

```go
// Good: typed structs
expectedRole := &rbacv1.ClusterRole{
    ObjectMeta: metav1.ObjectMeta{Name: "test-role"},
    Rules: []rbacv1.PolicyRule{...},
}

// Avoid: raw YAML strings
```

**Use fake clients with action recording:**

```go
client := fake.NewClientset()
// ... perform operations
actions := client.Actions()
// verify actions
```

**Use go-cmp for assertions:**

```go
if diff := cmp.Diff(expected, actual); diff != "" {
    t.Errorf("mismatch (-want +got):\n%s", diff)
}
```

## Project Structure

| Directory                  | Purpose                                                                    |
|----------------------------|----------------------------------------------------------------------------|
| `cmd/oc/`                  | Main entry point                                                           |
| `cmd/oc-tests-ext/`        | OTE (OpenShift Test Extension) entry point                                 |
| `pkg/cli/`                 | Command implementations (~37 top-level commands)                           |
| `pkg/cli/admin/`           | Admin subcommands (~27 directories)                                        |
| `pkg/cli/kubectlwrappers/` | kubectl command wrappers                                                   |
| `pkg/helpers/`             | Shared utilities (scheme, errors, auth, bulk ops)                          |
| `hack/`                    | Build and verification scripts (`update-*` regenerates, `verify-*` checks) |
| `tools/`                   | clicheck, gendocs, genman                                                  |
| `test/e2e/`                | End-to-end tests (Ginkgo v2)                                               |
| `images/`                  | Container image definitions                                                |
| `vendor/`                  | Vendored dependencies (Go modules)                                         |

## Dependencies

This project uses **Go Modules with vendoring**.

### Updating Dependencies

The vendor directory is checked in. CI will fail if vendor is stale.

```bash
# Update go.mod
go mod tidy

# Vendor dependencies
go mod vendor
```

After updating dependencies, commit both the `go.mod`, `go.sum`, and `vendor/` changes together.

### Key Dependencies

- `github.com/spf13/cobra` — CLI framework
- `k8s.io/client-go` — Kubernetes client
- `k8s.io/kubectl` — kubectl foundation
- `github.com/openshift/api` — OpenShift API types
- `github.com/openshift/client-go` — OpenShift clients
- `github.com/openshift/library-go` — OpenShift libraries
- `github.com/containers/image/v5` — Container image operations

## Continuous Integration

CI runs via OpenShift's CI infrastructure (Prow / ci-operator). All checks must pass for a PR to merge:

| Check | What it validates |
|-------|------------------|
| `make verify` | Formatting (gofmt), linting (govet), CLI conventions, dependency checks |
| `make test` | Unit tests across all packages |
| E2E tests | End-to-end tests on real OpenShift clusters |
| Vendor check | Ensures `vendor/` is up-to-date with `go.mod` |

You can run the same checks locally before pushing to catch issues early.

## Security

### Reporting Security Issues

If you've found a security issue that you'd like to disclose confidentially, please contact Red Hat's Product Security team. Details at:

**https://access.redhat.com/security/team/contact**

Do not open public GitHub issues for security vulnerabilities.

### Security Best Practices

When contributing code:
- Avoid command injection vulnerabilities
- Prevent XSS in any output that might be rendered
- Never log sensitive information (tokens, passwords, secrets)
- Validate all user inputs
- Be mindful of OWASP Top 10 vulnerabilities

## Getting Help

### Documentation

- [README.md](README.md) — Project overview and quick start
- [AGENTS.md](AGENTS.md) — Detailed architecture and conventions
- [OpenShift CLI Reference](https://docs.openshift.com/container-platform/latest/cli_reference/openshift_cli/getting-started-cli.html)

### Community

- **GitHub Issues:** For bugs and feature requests
- **Pull Requests:** For code review and discussion
- **Kubernetes Slack:** #openshift-dev channel

### Claude Code

This repository is set up for use with [Claude Code](https://docs.anthropic.com/en/docs/claude-code). Available agents in `.claude/agents/`:

- `code-reviewer` — PR code review for Go style, breaking changes, and oc-specific concerns
- `tester` — Build, lint, and test runner to validate changes

Available skills in `.claude/skills/`:

- `learn-session` — End-of-session knowledge extraction
- `learn-history` — Deep analysis of all past sessions

## License

oc is licensed under the [Apache License, Version 2.0](http://www.apache.org/licenses/). By contributing to this project, you agree to license your contributions under the same license.

---

Thank you for contributing to the OpenShift Client! Your contributions help make OpenShift better for everyone.
