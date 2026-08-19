# Architecture

This document explains the design decisions and component relationships of `oc`, the OpenShift CLI. It is not a user guide — see [README.md](README.md) for building and [CONTRIBUTING.md](CONTRIBUTING.md) for development workflow.

## The kubectl Wrapper Taxonomy

Every kubectl command is available in oc. The relationship falls into five categories, each with a different integration mechanism:

**Pure kubectl wrappers** — Commands in `pkg/cli/kubectlwrappers/wrappers.go` that call kubectl's constructor and wrap with `cmdutil.ReplaceCommandName("kubectl", "oc", ...)`. The wrapping is cosmetic — command name substitution in help text. Behavioral changes to these commands belong upstream in `k8s.io/kubectl`.

**Kubectl wrappers with OCP extensions** — Commands that start from kubectl's implementation but add OCP subcommands, resource types, or safety checks. For example, `create` wraps kubectl create then adds OCP subcommands (route, deployment-config, etc.); `scale`/`autoscale` add `deploymentconfig` to ValidArgs; `delete` prompts before removing persistent volumes or claims from a terminal (RFE-8872). Under `oc adm`: drain, cordon, uncordon, taint, certificates are also kubectl wrappers.

**Commands that extend kubectl** — These embed kubectl's implementation struct and add OCP-specific logic. `logs` embeds kubectl's `logs.LogsOptions` and adds Build/DeploymentConfig support. `expose` adds Route creation. `rollout`/`rollback` add DeploymentConfig support. These live in their own packages under `pkg/cli/`, not in `kubectlwrappers/`.

**Commands that diverge from kubectl** — Same concept as upstream but fully reimplemented. `debug` and `set` are entirely oc-native.

**Purely OCP-native commands** — No kubectl equivalent. Build, image, auth/project, connectivity, and operational commands, plus the entire `oc adm` subtree.

## Design Decisions

**Why Complete/Validate/Run.** All commands implement a three-phase lifecycle on an Options struct: `Complete` resolves flags and Factory outputs into a populated struct, `Validate` checks invariants without side effects, `Run` executes. This makes commands testable — tests construct Options directly, bypassing cobra — and ensures validation always happens before mutation.

**Why wrapping kubectl instead of forking.** oc stays current with kubectl automatically for wrapped commands. Forking would create maintenance burden that scales with kubectl's release velocity.

**Why polymorphic helpers.** `shimKubectlForOc()` in `pkg/cli/shim_kubectl.go` replaces kubectl's polymorphic helper functions with OCP-aware versions that wrap the originals — trying the OCP handler first, then falling through to kubectl's default. This is how `oc logs` understands BuildConfigs and `oc rollout` understands DeploymentConfigs without forking those commands.

**Why scheme registration in main().** OCP API types must be registered into the kubectl scheme before any command runs, or serialization of OCP resources fails. This happens in `cmd/oc/oc.go` via `schemehelper.InstallSchemes()`. Note: `new-app` and `set` use their own schemes instead of this global one.

## Upstream Context

- **kubernetes/kubectl** is the upstream for all wrapped commands. Changes to pure-wrapper behavior belong there, not in oc.
- **openshift/api** defines OCP API types. Schema changes happen there.

## Runtime Conventions

**Feature gating.** Experimental commands are gated by environment variables using `kcmdutil.FeatureGate("ENV_VAR").IsEnabled()`. Grep for `FeatureGate` to find current gates.
