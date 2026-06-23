# AI Agent Instructions for OpenShift CLI (oc)

Before making changes, check these files for additional context:
- **`ARCHITECTURE.md`** — Command architecture, subsystems, design decisions, directory structure
- **`CONTRIBUTING.md`** — Contribution guidelines, PR process, testing requirements, CI infrastructure
- **`README.md`** — Project overview and quick start

## What This Repo Is

This is the official command-line interface for OpenShift Container Platform. `oc` is built on top of `kubectl` and extends it with OpenShift-specific functionality. Every kubectl command is available in oc, making oc a complete superset of kubectl.

The CLI handles:
- Full kubectl compatibility for Kubernetes resource management
- OpenShift-specific commands for builds, deployments, projects, and developer workflows
- Enhanced resource types (Routes, BuildConfigs, DeploymentConfigs, ImageStreams)
- Integrated authentication and project management
- Developer-focused workflows (new-app, new-build, expose)

## Critical Rules

1. **Do not modify `pkg/cli/cli.go`** unless it is part of a kubectl rebase process. This file mirrors kubectl's command tree and should only change during kubectl version updates.
2. **Do not diverge from wrapped kubectl commands.** If a command is a pure kubectl wrapper (see list below), behavioral changes belong upstream in `k8s.io/kubectl`.
3. **Do not modify files under `vendor/`.** The vendor directory is managed by `go mod tidy && go mod vendor`. Never hand-edit anything under `vendor/`.
4. **Do not edit generated files.** `contrib/completions/` shell completions are generated — use `make update-generated-completions` to regenerate.
5. **Do not modify `OWNERS` or `OWNERS_ALIASES` files** without explicit direction.
6. **Never remove commands, flags, or options without deprecation.** Backwards compatibility is paramount. Use cobra's deprecation: `cmd.Deprecated = "Use X instead"`.
7. **Run `make verify` before considering any change complete.** This runs gofmt, govet, CLI conventions, and completion checks (~30-60s).

## Repository Structure

```
cmd/
├── oc/                     # Main CLI entry point
└── oc-tests-ext/           # OTE (OpenShift Tests Extension) e2e test harness

pkg/
├── cli/                    # Command implementations (44 packages)
│   ├── cli.go              # Root command assembly (mirrors kubectl structure)
│   ├── admin/              # Admin subcommands (31 packages: mustgather, release, upgrade, etc.)
│   ├── kubectlwrappers/    # Pure kubectl wrappers (get, apply, delete, etc.)
│   ├── create/             # OpenShift-specific create subcommands
│   ├── set/                # OpenShift-specific set subcommands
│   └── ...                 # OpenShift-native commands (login, new-app, etc.)
├── helpers/                # Shared utilities (32 packages: scheme, newapp, image, route, etc.)
└── version/                # Version information

hack/                       # Build and verification scripts
├── update-generated-completions.sh
├── verify-generated-completions.sh
└── verify-kube-version.sh

tools/                      # Development tools
├── clicheck/               # CLI convention validator
├── gendocs/                # Documentation generator
└── genman/                 # Man page generator

test/
└── e2e/                    # End-to-end tests (Ginkgo v2)

contrib/completions/        # Generated shell completions (bash, zsh)
images/                     # Container image definitions
vendor/                     # Vendored dependencies (Go modules)
```

## Understanding Command Categories

Before modifying any command, understand which category it falls into:

### 1. Pure kubectl Wrappers
Commands wired via `pkg/cli/kubectlwrappers/wrappers.go` that directly proxy to kubectl:

**DO NOT modify behavior** — changes belong upstream in `k8s.io/kubectl`:
- Resource operations: get, apply, delete, patch, replace, describe, edit, label, annotate, wait
- Workload management: run, exec, cp, attach, port-forward, proxy
- Cluster inspection: explain, diff, api-resources, api-versions, cluster-info, events
- Auth & config: auth, config (base), plugin, completion
- Advanced: kustomize

**Implementation**: Uses `cmdutil.ReplaceCommandName("kubectl", "oc", kubectl.NewCmd*())` to swap branding only.

### 2. kubectl Wrappers with OpenShift Extensions
Commands that use kubectl's base implementation but add OpenShift resource types:

**Changes to base behavior belong upstream; OpenShift extensions stay here**:
- `create` — Adds subcommands: route, deploymentconfig, clusterquota, user, identity, useridentitymapping, imagestream, imagestreamtag, build
- `scale` / `autoscale` — Add deploymentconfig support
- `config` — Adds: admin-kubeconfig, refresh-ca-bundle
- `adm drain/cordon/uncordon/taint/certificates` — kubectl wrappers under admin

### 3. Commands that Extend kubectl
Commands embedding kubectl's implementation with significant OpenShift additions:

**Can modify OpenShift-specific portions**:
- `logs` — Adds Build, BuildConfig, DeploymentConfig, Jenkins pipeline support; `--version` flag
- `expose` — Adds Route creation with TLS options (edge, passthrough, reencrypt)
- `rollout` / `rollback` — Add DeploymentConfig support alongside Deployments
- `debug` — Full custom implementation with OpenShift resource support
- `set` — Entirely oc-native (env, volume, triggers, build-hook, deployment-hook, route-backends, build-secret, image-lookup, probe, data)

### 4. Purely OpenShift-Native Commands
No kubectl equivalent — fully modifiable:

| Category | Commands |
|----------|----------|
| **Application Management** | new-app, new-build, start-build, cancel-build, import-image, tag |
| **Authentication** | login, logout, whoami, get-token |
| **Project Management** | project, projects, request-project |
| **Developer Tools** | rsh, rsync, status, process, extract, observe, idle |
| **Image Operations** | image (info, append, extract, mirror) |
| **Registry** | registry (info, login) |
| **Policy** | policy (add-role-to-user, etc.) |
| **Service Accounts** | serviceaccounts, secrets (link, unlink, new-*) |
| **Admin** | ~31 admin subcommands (see ARCHITECTURE.md for full list) |

## Key Patterns to Follow

### Key Patterns to Follow

### Command Implementation Pattern

All commands follow the **Complete/Validate/Run** three-phase pattern. See `ARCHITECTURE.md` for full details and examples.

**Phase responsibilities:**
- **Complete**: Resolve Factory dependencies, build clients, parse args
- **Validate**: Check preconditions, validate flag combinations
- **Run**: Execute command logic, interact with API server, write output

### Help Text Conventions

**Critical**: Use `#` for comments in examples, not `//`. Enforced by `tools/clicheck`.

```go
Long: templates.LongDesc(`...`),
Example: templates.Examples(`
    # Shell-style comments using #
    oc command --flag=value
`),
```

### Error Handling, Clients, Output

- **Error handling**: `fmt.Errorf("context: %w", err)`, `kcmdutil.CheckErr()`
- **Clients**: Obtain via `f.ToRESTConfig()` -> `kubernetes.NewForConfig()`
- **Output**: Use `genericclioptions.PrintFlags` for json/yaml support
- **Logging**: `klog.V(2).Infof()` with verbosity levels

See `ARCHITECTURE.md` for detailed code examples.

## Important Constraints

### Build Tags
All builds and tests must use the correct build tags:

**Linux**:
```bash
-tags 'include_gcs include_oss containers_image_openpgp gssapi'
```

**macOS/Windows** (no gssapi):
```bash
-tags 'include_gcs include_oss containers_image_openpgp'
```

### System Dependencies
Required for building:
- Fedora/CentOS/RHEL: `krb5-devel`, `gpgme-devel`, `libassuan-devel`
- macOS: `heimdal`, `gpgme` (via brew)

### Backwards Compatibility
- Never remove commands, flags, or change default behavior without deprecation
- Deprecate first using `cmd.Deprecated = "Use X instead"`
- Wait at least one release cycle before removal
- This is the **most important** constraint — breaking changes harm users

### Generated Files
- Shell completions in `contrib/completions/` are generated
- After command changes: `make update-generated-completions`
- Before committing: `make verify-generated-completions`
- CI will fail if completions are stale

### Vendor Directory
- Checked into git
- Managed by `go mod tidy && go mod vendor`
- CI fails if vendor is out of sync with go.mod/go.sum
- Commit go.mod, go.sum, and vendor/ together

## Build and Test

### Build Commands

```bash
make oc                              # Fast build (~5-10s, strips debug symbols)
make STRIP_DEBUGGING_SYMBOLS=false oc  # Build with debug symbols
make build                           # Full build (oc + oc-tests-ext + tools)
make cross-build                     # Build for all platforms
make help                            # List all available targets
```

Output: Binaries go to `_output/bin/` (not `./oc`)

### Test Commands

```bash
make test                            # Unit tests (~2-5 min for full suite)
make verify                          # All verification checks (~30-60s)
make verify-cli-conventions          # CLI structure validation
make verify-generated-completions    # Check completions are current
```

### Running Tests for a Single Package

**Linux**:
```bash
go test -tags 'include_gcs include_oss containers_image_openpgp gssapi' ./pkg/cli/admin/policy/...
```

**macOS/Windows**:
```bash
go test -tags 'include_gcs include_oss containers_image_openpgp' ./pkg/cli/admin/policy/...
```

### E2E Tests (OTE Framework)

```bash
make build                           # Build oc-tests-ext binary
./oc-tests-ext list-suites           # List available test suites
./oc-tests-ext list-tests openshift/oc/all  # List tests in suite
./oc-tests-ext run-suite openshift/oc/all   # Run all tests
./oc-tests-ext run-test "test-name"  # Run specific test
```

E2E tests require a real OpenShift cluster. Use Ginkgo v2 + Gomega.

## Testing Best Practices

- **Table-driven tests** with `t.Run()` subtests
- **Typed API structs** (e.g., `rbacv1.ClusterRole{...}`), not raw YAML strings
- **Fake clients** with action recording: `fake.NewSimpleClientset()`
- **go-cmp** for assertions: `cmp.Diff(expected, actual)`
- **Test fixtures** in `testdata/` subdirectories co-located with tests

See `CONTRIBUTING.md` for detailed testing patterns and examples.

## What NOT to Do

- **Do not modify kubectl wrapper behavior** — submit changes to `k8s.io/kubectl` upstream
- **Do not hand-edit vendor/** — use `go mod tidy && go mod vendor`
- **Do not edit generated completions** — use `make update-generated-completions`
- **Do not skip `make verify`** — CI will fail if you do
- **Do not remove commands/flags without deprecation** — backwards compatibility is critical
- **Do not use `//` for comments in examples** — use `#` (enforced by clicheck)
- **Do not build with `go build`** — use `make oc` (injects version info)
- **Do not assume kubeconfig is at default location** — use Factory.ToRESTConfig()
- **Do not log secrets or sensitive data** — tokens, passwords, credentials
- **Do not introduce command injection vulnerabilities** — validate all user inputs

## kubectl Rebase Process

When updating to a new kubectl version (aligned with Kubernetes releases, quarterly):

1. Update dependencies in go.mod (all k8s.io versions matching)
2. Update `pkg/cli/cli.go` — ONLY file that should change (mirrors kubectl's command tree)
3. Test extensively: `make verify && make test` + manual testing
4. Update completions: `make update-generated-completions`
5. Do not touch other files unless fixing compatibility issues

See `ARCHITECTURE.md` for detailed step-by-step rebase procedure.

## Common Command Additions

### Adding a New OpenShift-Native Command

1. Create package under `pkg/cli/mycommand/`
2. Implement Complete/Validate/Run pattern
3. Register in `pkg/cli/cli.go`
4. Add unit tests
5. Regenerate completions: `make update-generated-completions`
6. Verify: `make verify && make test`

### Adding a Subcommand to `oc create`

1. Create subcommand in `pkg/cli/create/`
2. Register in `pkg/cli/kubectlwrappers/wrappers.go` via `cmd.AddCommand()`
3. Add tests, regenerate completions, verify

See `ARCHITECTURE.md` for detailed code examples and patterns.

## Dependencies

**Workflow**: 
```bash
go mod tidy
go mod vendor
git add go.mod go.sum vendor/
git commit -m "Update dependencies"
```

**Key dependencies**:
- `github.com/spf13/cobra` — CLI framework
- `k8s.io/kubectl` — kubectl foundation (embedded)
- `k8s.io/client-go` — Kubernetes client
- `k8s.io/cli-runtime` — Generic CLI options, printers
- `github.com/openshift/api` — OpenShift CRD types
- `github.com/openshift/client-go` — OpenShift clients
- `github.com/openshift/library-go` — OpenShift utilities
- `github.com/containers/image/v5` — Container image operations

## Additional Context

- **See ARCHITECTURE.md** for detailed architectural overview
- **See CONTRIBUTING.md** for contribution guidelines
- **Check `go.mod`** for required Go version (do not hardcode)
- **CI runs via Prow/ci-operator** — all checks must pass for merge
- **License**: Apache 2.0
- **No CLA required** for contributions
