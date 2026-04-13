# OpenShift CLI (oc)

The OpenShift Client (`oc`) is the official CLI for OpenShift Container Platform. It is built on top of `kubectl` and extends it with OpenShift-specific functionality.

## Architecture: oc and kubectl Relationship

kubectl is a subset of oc — every kubectl command is available in oc. The relationship between oc and kubectl commands falls into these categories:

**Pure kubectl wrappers** — Commands wired via `pkg/cli/kubectlwrappers/wrappers.go` using `cmdutil.ReplaceCommandName("kubectl", "oc", ...)`. Includes: get, apply, delete, exec, run, patch, replace, describe, edit, label, annotate, cp, wait, events, port-forward, attach, proxy, explain, diff, auth, api-resources, api-versions, cluster-info, kustomize, completion, plugin. Changes to these should go upstream to `k8s.io/kubectl`.

**Kubectl wrappers with OCP extensions** — `create` (adds route, deployment-config, user, identity, image-stream, build subcommands), `scale`/`autoscale` (adds deploymentconfig resource), `config` (adds admin-kubeconfig, refresh-ca-bundle subcommands). Under `oc adm`: drain, cordon, uncordon, taint, certificates are also kubectl wrappers.

**Commands that extend kubectl** — These embed kubectl's implementation but add OCP-specific logic:
- `logs` — embeds kubectl's `logs.LogsOptions`, adds Build, BuildConfig, DeploymentConfig, Jenkins pipeline support, and `--version` flag
- `expose` — extends kubectl expose with OpenShift Route creation
- `rollout`/`rollback` — extends with DeploymentConfig support

**Commands that diverge from kubectl** — These have their own oc implementation:
- `debug` — full custom implementation with OpenShift resource support
- `set` — entirely oc-native with OCP-specific subcommands (env, volume, triggers, build-hook, deployment-hook, route-backends, etc.)

**Purely OCP-native commands** — No kubectl equivalent: new-app, new-build, start-build, cancel-build, import-image, tag, login, logout, whoami, project, projects, request-project, rsh, rsync, status, process, extract, observe, idle, image, registry, policy, secrets, service-accounts, get-token, and the entire `oc adm` subtree (~30 admin subcommands).

## Build and Test

```bash
make oc                              # Fast build, ~5-10s (strips debug symbols by default)
make build                           # Full build (includes oc-tests-ext, tools)
make test                            # Unit tests (~2-5 min for full suite)
make verify                          # Formatting, linting, CLI convention checks (~30-60s)
make verify-cli-conventions          # CLI structure validation via tools/clicheck
make update-generated-completions    # Regenerate shell completions
make verify-generated-completions    # Verify completions are up-to-date
```

Go version: see `go.mod` for the required version.

### Running Tests for a Single Package

```bash
# Linux
go test -tags 'include_gcs include_oss containers_image_openpgp gssapi' ./pkg/cli/admin/policy/...

# macOS / Windows (omit gssapi)
go test -tags 'include_gcs include_oss containers_image_openpgp' ./pkg/cli/admin/policy/...
```

System dependencies for building:
- Fedora/CentOS/RHEL: `krb5-devel` (GSSAPI/Kerberos), `gpgme-devel`, `libassuan-devel`
- macOS: `heimdal`, `gpgme` (via brew)

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

## Command Implementation Pattern

All commands follow the Complete/Validate/Run pattern:

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

## Key Conventions

- **Error handling:** `fmt.Errorf("context: %w", err)`, `kcmdutil.CheckErr()` for command-level errors, `utilerrors.Aggregate` for batch errors
- **Logging:** `k8s.io/klog/v2` with verbosity levels (`klog.V(4).Infof()`)
- **Clients:** obtained via `kcmdutil.Factory` -> `ToRESTConfig()` -> typed clients (`kubernetes.NewForConfig`, `buildv1client.NewForConfig`, etc.)
- **Flags:** `cmd.Flags().TypeVar()`, config flags via `genericclioptions.ConfigFlags`
- **Output formats:** `genericclioptions.PrintFlags` for json/yaml support
- **Scheme registration:** `pkg/helpers/scheme/scheme.go` for OpenShift/Kubernetes API types
- **Help text:** `templates.LongDesc()` for long descriptions, `templates.Examples()` for examples. Use `#` for comments in examples, not `//`. Enforced by `tools/clicheck`.
- **Command groups:** registered via `ktemplates.CommandGroups` in `pkg/cli/cli.go`

## Contributing Rules

- **Do not modify `pkg/cli/cli.go`** unless it is part of a kubectl rebase process (to reflect changes from `kubectl/cmd.go`).
- **Do not diverge from wrapped kubectl commands.** If a command is a kubectl wrapper, behavioral changes belong upstream in `k8s.io/kubectl`.
- **Do not modify files under `vendor/`.** Regenerate via `go mod tidy && go mod vendor`.
- **Do not edit generated files.** `contrib/completions/` and `docs/generated/` are generated — use `make update-generated-completions` to regenerate.
- **Write unit tests for every change.** Some commands do not easily support unit tests without dramatic refactoring — those may be excluded, but test coverage is expected by default. Test fixtures go in `testdata/` subdirectories co-located with tests.
- **Never remove commands, flags, or options without a deprecation notice.** Backwards compatibility is the most important aspect of this tool. Deprecate first, remove later. Use cobra's built-in deprecation: `cmd.Deprecated = "Use X instead"`.

## Dependencies

Go Modules with vendoring. Workflow: `go mod tidy` then `go mod vendor`.

Key dependencies: `spf13/cobra`, `k8s.io/client-go`, `k8s.io/kubectl`, `openshift/api`, `openshift/client-go`, `openshift/library-go`, `containers/image/v5`.

## Testing

- **Unit tests:** co-located `*_test.go` files, table-driven tests, standard `testing.T`
- **E2E tests:** `test/e2e/` using Ginkgo v2 + Gomega
- **Assertions:** assemble the expected object and compare with the actual using `google/go-cmp`, rather than checking individual fields with if statements
- **Test fixtures:** use typed API struct builders (e.g. `rbacv1.ClusterRole{...}`), not raw YAML strings
- **Fake clients:** use `fake.NewClientset()` with action recording to verify API call options
- **OTE framework:** `oc-tests-ext` binary for test suite execution (`make build` then `./oc-tests-ext run-suite openshift/oc/all`)
- **CLI conventions:** `make verify-cli-conventions` validates command structure via `tools/clicheck`
