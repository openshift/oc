# Architecture: OpenShift CLI (oc)

## Overview

The OpenShift Client (`oc`) is the official command-line interface for OpenShift Container Platform. It is built on top of `kubectl` and extends it with OpenShift-specific functionality. Every kubectl command is available in oc, making oc a complete superset of kubectl functionality.

oc provides:
- Full kubectl compatibility for Kubernetes resource management
- OpenShift-specific commands for builds, deployments, projects, and developer workflows
- Enhanced resource types (Routes, BuildConfigs, DeploymentConfigs, ImageStreams)
- Integrated authentication and project management
- Developer-focused workflows (new-app, new-build, expose)

## Command Architecture

### Command Categories and kubectl Relationship

oc commands fall into four architectural categories based on their relationship to kubectl:

#### 1. Pure kubectl Wrappers

Commands that are directly proxied to kubectl with minimal wrapping:

| Command Group | Commands |
|--------------|----------|
| **Resource Operations** | get, apply, delete, patch, replace, describe, edit, label, annotate, wait |
| **Workload Management** | run, exec, cp, attach, port-forward, proxy |
| **Cluster Inspection** | explain, diff, api-resources, api-versions, cluster-info, events |
| **Auth & Config** | auth, config, plugin, completion |
| **Advanced** | kustomize |

**Implementation**: Wired via `pkg/cli/kubectlwrappers/wrappers.go` using `cmdutil.ReplaceCommandName("kubectl", "oc", kubectl.NewDefaultKubectlCommand())`. Changes to these commands must go upstream to `k8s.io/kubectl`.

#### 2. kubectl Wrappers with OpenShift Extensions

Commands that use kubectl's implementation but add OpenShift resource types or subcommands:

| Command | Extension |
|---------|-----------|
| **create** | Adds: route, deploymentconfig, clusterquota, user, identity, useridentitymapping, imagestream, imagestreamtag, build subcommands |
| **scale** | Adds: deploymentconfig resource type |
| **autoscale** | Adds: deploymentconfig resource type |
| **config** | Adds: admin-kubeconfig, refresh-ca-bundle subcommands |
| **adm drain/cordon/uncordon/taint** | kubectl wrappers under `oc adm` |
| **adm certificates** | kubectl certificate management |

#### 3. Commands that Extend kubectl

Commands that embed kubectl's implementation but add significant OpenShift-specific logic:

| Command | OpenShift Additions |
|---------|-------------------|
| **logs** | Build, BuildConfig, DeploymentConfig, Jenkins pipeline support; `--version` flag for deployment history |
| **expose** | Route creation with TLS options (edge, passthrough, reencrypt) |
| **rollout** | DeploymentConfig support alongside Kubernetes Deployments |
| **rollback** | DeploymentConfig rollback functionality |
| **debug** | Full custom implementation with OpenShift resource support |
| **set** | Entirely oc-native with OCP-specific subcommands (env, volume, triggers, build-hook, deployment-hook, route-backends, build-secret, image-lookup, probe, data) |

#### 4. Purely OpenShift-Native Commands

Commands with no kubectl equivalent:

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
| **Admin Subtree** | ~30 admin subcommands under `oc adm` |

### Admin Command Subtree

The `oc adm` subtree contains cluster administration commands:

| Category | Commands |
|----------|----------|
| **Cluster Management** | top, toppvc, must-gather, node-logs, inspect, inspect-alerts, catalog |
| **Node Management** | node (logs, copy-to-node), node-image, reboot-machine-config-pool, restart-kubelet, wait-for-node-reboot, per-node-pod |
| **Certificate Management** | ocp-certificates, create-bootstrap-project-template, create-kubeconfig, create-error-template, create-login-template, create-provider-selection-template |
| **Migration & Upgrades** | migrate, release, upgrade, verify-image-signature |
| **Network** | network (join-projects, make-projects-global, isolate-projects) |
| **Policy & Security** | policy (add-scc-to-group, add-cluster-role-to-user, etc.), groups (sync, prune) |
| **Maintenance** | prune (builds, deployments, images), build-chain |
| **Project Management** | project (new-project) |

## Command Implementation Pattern

All commands follow the **Complete/Validate/Run** pattern, a three-phase initialization model:

```go
type CommandOptions struct {
    genericiooptions.IOStreams  // stdin, stdout, stderr
    
    // Configuration from flags
    namespace string
    selector  string
    
    // Computed/resolved state
    client    kubernetes.Interface
    config    *rest.Config
}

func NewCmdExample(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
    o := &CommandOptions{IOStreams: streams}
    
    cmd := &cobra.Command{
        Use:   "example [options]",
        Short: "Brief description",
        Long:  templates.LongDesc(`...`),
        Example: templates.Examples(`...`),
        Run: func(cmd *cobra.Command, args []string) {
            kcmdutil.CheckErr(o.Complete(f, cmd, args))
            kcmdutil.CheckErr(o.Validate())
            kcmdutil.CheckErr(o.Run())
        },
    }
    
    // Add flags
    cmd.Flags().StringVarP(&o.namespace, "namespace", "n", "", "namespace")
    return cmd
}

// Complete - resolve dependencies, build clients, process arguments
func (o *CommandOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
    var err error
    o.config, err = f.ToRESTConfig()
    if err != nil {
        return err
    }
    o.client, err = kubernetes.NewForConfig(o.config)
    return err
}

// Validate - validate options, check preconditions
func (o *CommandOptions) Validate() error {
    if o.namespace == "" {
        return fmt.Errorf("namespace is required")
    }
    return nil
}

// Run - execute the command logic
func (o *CommandOptions) Run() error {
    // Implementation
    return nil
}
```

**Phase Responsibilities:**

| Phase | Purpose | Examples |
|-------|---------|----------|
| **Complete** | Resolve dependencies, build clients from Factory, parse arguments | `f.ToRESTConfig()`, `clientset.NewForConfig()`, parse positional args |
| **Validate** | Check preconditions, validate flag combinations, verify resources exist | Required flags set, valid enums, namespace exists |
| **Run** | Execute command logic, interact with API server, write output | Create resources, list objects, stream logs |

## Key Subsystems

### 1. Client Factory (`kcmdutil.Factory`)

The Factory interface abstracts Kubernetes client creation and configuration:

```go
type Factory interface {
    ToRESTConfig() (*rest.Config, error)
    ToRawKubeConfigLoader() clientcmd.ClientConfig
    ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error)
    ToRESTMapper() (meta.RESTMapper, error)
    ClientSet() (kubernetes.Interface, error)
}
```

Commands use the factory to obtain:
- REST clients for API communication
- Discovery clients for API resource enumeration
- REST mappers for GVK ↔ GVR translation
- Kubeconfig loaders for authentication

### 2. Scheme Registration (`pkg/helpers/scheme/`)

The oc scheme registers both Kubernetes and OpenShift API types:

```go
import (
    "github.com/openshift/oc/pkg/helpers/scheme"
)

// Scheme includes:
// - All Kubernetes core and extension types
// - All OpenShift types (Build, DeploymentConfig, Route, ImageStream, etc.)
// - All versioned and internal types
```

Used for:
- Encoding/decoding API objects
- Type conversion between API versions
- Default value population
- Validation

### 3. Template Helpers (`pkg/cli/templates/`)

Consistent help text formatting:

```go
templates.LongDesc(`
    Long description with automatic
    text wrapping and indentation normalization.
`)

templates.Examples(`
    # Comment-style examples (# required, // forbidden)
    oc command --flag=value

    # Multiple examples
    oc command resource-name
`)
```

The `tools/clicheck` tool enforces that examples use `#` for comments, not `//`.

### 4. Generic CLI Options (`k8s.io/cli-runtime/pkg/genericclioptions/`)

Reusable flag sets for common patterns:

| Option Type | Purpose | Flags |
|------------|---------|-------|
| **ConfigFlags** | Kubeconfig and context | `--kubeconfig`, `--context`, `--namespace`, `--server` |
| **PrintFlags** | Output format | `--output`, `-o`, `--show-labels`, `--show-managed-fields` |
| **ResourceBuilderFlags** | Resource selection | `--all`, `--selector`, `--filename`, `--recursive` |
| **IOStreams** | Standard streams | `In`, `Out`, `ErrOut` for testability |

### 5. Error Handling

Consistent error wrapping and aggregation:

```go
// Wrap errors with context
return fmt.Errorf("failed to create deployment: %w", err)

// Command-level error handling
kcmdutil.CheckErr(err)  // Prints error and exits with code 1

// Aggregate multiple errors
errs := []error{}
for _, item := range items {
    if err := process(item); err != nil {
        errs = append(errs, err)
    }
}
return utilerrors.NewAggregate(errs)
```

### 6. Logging (`k8s.io/klog/v2`)

Verbosity-based logging:

```go
klog.V(2).Infof("Processing %d items", len(items))  // -v=2
klog.V(4).Infof("Item details: %+v", item)          // -v=4
klog.V(6).Infof("Full API response: %s", raw)       // -v=6
```

## Build System

### Makefile Structure

The build system uses OpenShift's build-machinery-go framework:

```makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
    golang.mk \
    targets/openshift/images.mk \
    targets/openshift/rpm.mk \
    targets/openshift/deps-gomod.mk \
)
```

### Build Targets

| Target | Output | Duration | Purpose |
|--------|--------|----------|---------|
| `make oc` | `oc` binary | ~5-10s | Fast dev build (stripped symbols) |
| `make build` | `oc`, `oc-tests-ext`, tools | ~30-60s | Full build including tests |
| `make test` | Test results | ~2-5m | Unit tests across all packages |
| `make verify` | Verification status | ~30-60s | Linting, formatting, CLI conventions |
| `make cross-build` | Multi-platform binaries | ~3-5m | All supported OS/arch combinations |

### Build Flags

```makefile
GO_BUILD_FLAGS := -tags 'include_gcs include_oss containers_image_openpgp gssapi'
```

**Build tags:**
- `include_gcs` - Google Cloud Storage support
- `include_oss` - Alibaba Cloud OSS support
- `containers_image_openpgp` - Image signature verification
- `gssapi` - Kerberos authentication (Linux only)

**Strip symbols control:**
```bash
make STRIP_DEBUGGING_SYMBOLS=false oc  # Include debug symbols
```

### Cross-Platform Support

| Platform | GOOS | GOARCH | Notes |
|----------|------|--------|-------|
| macOS (Intel) | darwin | amd64 | |
| macOS (Apple Silicon) | darwin | arm64 | |
| Windows | windows | amd64 | Includes version info via `oc.syso` |
| Linux (x86-64) | linux | amd64 | |
| Linux (ARM64) | linux | arm64 | |
| Linux (PowerPC) | linux | ppc64le | |
| Linux (s390x) | linux | s390x | Mainframe |

## Testing Framework

### Unit Tests

**Location**: Colocated `*_test.go` files

**Test tags**: Must match build tags for the package:

```go
//go:build include_gcs && include_oss && containers_image_openpgp && gssapi
// +build include_gcs,include_oss,containers_image_openpgp,gssapi
```

**Running tests**:
```bash
# Linux
go test -tags 'include_gcs include_oss containers_image_openpgp gssapi' ./pkg/...

# macOS/Windows (no gssapi)
go test -tags 'include_gcs include_oss containers_image_openpgp' ./pkg/...
```

**Testing patterns**:
- Table-driven tests with `t.Run()` subtests
- Fake clients with action recording: `fake.NewSimpleClientset()`
- Typed API objects, not raw YAML strings
- `google/go-cmp` for deep equality comparisons
- `testdata/` directories for fixtures

### E2E Tests (OTE Framework)

**Location**: `test/e2e/`

**Framework**: Ginkgo v2 + Gomega

**Test binary**: `oc-tests-ext` (OpenShift Tests Extension)

**Test suite structure**:
```go
var _ = g.Describe("[sig-cli] oc command", func() {
    g.It("should create a route [Serial]", func() {
        // Test implementation
    })
})
```

**Test labels**:
- `[Serial]` - Run sequentially (e.g., cluster-wide state changes)
- `[Parallel]` - Safe to run concurrently
- `[sig-cli]` - Special Interest Group tag

**Running e2e tests**:
```bash
./oc-tests-ext run-suite openshift/oc/all
./oc-tests-ext run-test "[sig-cli] oc new-app"
./oc-tests-ext list-suites
./oc-tests-ext list-tests openshift/oc/all
```

### CLI Convention Verification

**Tool**: `tools/clicheck`

**Checks**:
- Examples use `#` for comments (not `//`)
- Help text formatting consistency
- Command group organization
- Flag naming conventions
- Required vs. optional flag documentation

**Running**:
```bash
make verify-cli-conventions
```

### Shell Completion Tests

**Generated files**: `contrib/completions/bash/`, `contrib/completions/zsh/`

**Workflow**:
```bash
make update-generated-completions  # Regenerate from current command tree
make verify-generated-completions  # Ensure generated files are current
```

Generated completions must be committed. CI fails if they're stale.

## Dependency Map

```text
oc
├── Embeds
│   └── kubectl (k8s.io/kubectl/pkg/cmd)
│       ├── All kubectl commands
│       └── kubectl plugin infrastructure
│
├── Kubernetes Dependencies
│   ├── k8s.io/client-go          (clients, informers, auth)
│   ├── k8s.io/apimachinery        (meta, runtime, schema)
│   ├── k8s.io/api                 (Kubernetes API types)
│   ├── k8s.io/cli-runtime         (genericclioptions, resource builders)
│   └── k8s.io/component-base      (version info, logging)
│
├── OpenShift Dependencies
│   ├── github.com/openshift/api              (OpenShift CRD types)
│   ├── github.com/openshift/client-go        (OpenShift clients)
│   ├── github.com/openshift/library-go       (common utilities)
│   └── github.com/openshift/build-machinery-go (build system)
│
├── CLI Framework
│   ├── github.com/spf13/cobra     (command structure)
│   └── github.com/spf13/pflag     (POSIX flags)
│
├── Container/Image Operations
│   ├── github.com/containers/image/v5         (image pull/push/inspect)
│   ├── github.com/containers/storage          (local image storage)
│   └── github.com/opencontainers/go-digest   (content addressing)
│
├── Authentication
│   ├── github.com/apcera/gssapi   (Kerberos/GSSAPI)
│   └── golang.org/x/oauth2        (OAuth flows)
│
└── Cloud SDKs
    ├── github.com/aws/aws-sdk-go-v2          (S3 operations)
    ├── github.com/Azure/azure-sdk-for-go     (Azure operations)
    └── cloud.google.com/go                   (GCP operations)
```

## Directory Structure

| Directory | Purpose | Entry Points |
|-----------|---------|--------------|
| `cmd/oc/` | Main entry point | `main.go` |
| `cmd/oc-tests-ext/` | E2E test harness | `main.go` |
| `pkg/cli/` | Command implementations (~44 packages) | `cli.go` (root command assembly) |
| `pkg/cli/admin/` | Admin subcommands (31 packages) | `admin.go`, includes: mustgather, inspect, release, upgrade, top, migrate, prune, etc. |
| `pkg/cli/kubectlwrappers/` | kubectl proxies | `wrappers.go` |
| `pkg/helpers/` | Shared utilities (32 packages) | `scheme/`, `groupsync/`, `tokencmd/`, `newapp/`, `image/`, `route/`, `env/`, etc. |
| `hack/` | Build/verify scripts | `update-generated-completions.sh`, `verify-generated-completions.sh`, `verify-kube-version.sh` |
| `tools/` | Dev tools | `clicheck/`, `gendocs/`, `genman/` |
| `test/e2e/` | E2E tests | Ginkgo test suites (3 test files) |
| `contrib/completions/` | Shell completions | bash/, zsh/ (generated and checked in) |
| `vendor/` | Vendored dependencies | Go modules cache |

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Embed kubectl, don't fork | Guarantees kubectl compatibility, reduces maintenance burden, allows upstream contributions |
| Separate kubectl wrappers package | Clear boundary between pure kubectl and OpenShift extensions, enables kubectl rebases |
| Complete/Validate/Run pattern | Separates dependency resolution, validation, and execution; improves testability |
| Vendored dependencies | Ensures reproducible builds, required for disconnected builds, matches OpenShift conventions |
| Stripped symbols by default | Faster builds for development (5-10s vs 30-60s), users can opt-in with `STRIP_DEBUGGING_SYMBOLS=false` |
| OTE test framework | Enables test reuse across OpenShift repos, supports test suite composition, JUnit output for CI |
| `#` for example comments | Shell-style comments match actual usage, enforced by `clicheck` to prevent confusion |
| Single root `cli.go` file | Central command tree assembly, kubectl rebase changes are isolated to this file |
| Platform auto-detection for builds | Correct build tags per platform (gssapi on Linux, not macOS/Windows) |
| Generated completions checked in | Allows users to install completions without building oc, CI ensures freshness |

## kubectl Rebase Process

When rebasing to a new kubectl version:

1. **Update `pkg/cli/cli.go`** - This is the ONLY file that should change during a rebase
2. **Match kubectl's command tree structure** - Mirror `kubectl/cmd.go`
3. **Preserve OpenShift command groups** - Maintain OCP-specific command organization
4. **Update go.mod** - Bump `k8s.io/kubectl`, `k8s.io/client-go`, etc. to matching versions
5. **Run vendor** - `go mod tidy && go mod vendor`
6. **Test** - `make verify && make test`
7. **Manual verification** - Test wrapped commands: `oc get`, `oc apply`, etc.

**Rebase frequency**: Aligned with Kubernetes releases (quarterly)

## Special Topics

### Authentication Flow

```text
oc login <server>
  ↓
1. Discover OAuth endpoints (/.well-known/oauth-authorization-server)
  ↓
2. Obtain OAuth token:
   - Interactive: browser-based OAuth flow
   - Non-interactive: username/password or token flag
  ↓
3. Create/update kubeconfig:
   - Add cluster (server, CA cert)
   - Add user (token)
   - Add context (cluster + user + namespace)
   - Set current-context
  ↓
4. Subsequent commands use token from kubeconfig
```

**Token storage**: `~/.kube/config` (same as kubectl)

**Token refresh**: Handled by `k8s.io/client-go` transport layer

### Project vs. Namespace

OpenShift Projects are Kubernetes Namespaces with additional metadata and RBAC:

```text
oc new-project myapp
  ↓
1. Create Namespace "myapp"
2. Create Project "myapp" (metadata wrapper)
3. Create default RoleBindings (admin, edit, view)
4. Switch current context to "myapp"
```

**`oc project`** switches context (like `kubectl config set-context --current --namespace=...`)

**`oc projects`** lists projects user has access to (filtered list of namespaces)

### Resource Printing

oc supports multiple output formats:

| Format | Flag | Use Case |
|--------|------|----------|
| Table (default) | (none) | Human-readable columns |
| JSON | `-o json` | API inspection, jq processing |
| YAML | `-o yaml` | Configuration export |
| JSONPath | `-o jsonpath=...` | Extracting specific fields |
| Go template | `-o template=...` | Custom formatting |
| Name only | `-o name` | Piping to other commands |
| Wide | `-o wide` | Additional columns |

Implemented via `k8s.io/cli-runtime/pkg/printers` and `genericclioptions.PrintFlags`.

---

**Document Maintenance**: Update this file when:
- Adding new command categories
- Changing the command implementation pattern
- Modifying the build system
- Adding new subsystems or architectural layers
- Changing kubectl rebase procedures
