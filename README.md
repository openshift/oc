# OpenShift Client - oc

With OpenShift Client CLI (oc), you can create applications and manage OpenShift
resources.  It is built on top of [kubectl](https://github.com/kubernetes/kubectl/)
which means it provides its full capabilities to connect with any kubernetes
compliant cluster, and on top adds commands simplifying interaction with an
OpenShift cluster.

## Documentation

- [CONTRIBUTING.md](CONTRIBUTING.md) — code conventions, testing, PR process, CI, review expectations (shared across OpenShift Control Plane repos)
- [ARCHITECTURE.md](ARCHITECTURE.md) — design decisions, component relationships, the kubectl wrapper taxonomy
- [AGENTS.md](AGENTS.md) — instructions for AI coding agents

## Building

To build oc invoke `make oc`. At any time you can invoke `make help` and you'll
get a list of all supported make sub-commands.

By default `make oc` builds the executable without debugging symbols. To include
debugging symbols, run `make STRIP_DEBUGGING_SYMBOLS=false oc`.

In order to build `oc`, you will need the GSSAPI sources and also a C compiler.
On a Fedora/CentOS/RHEL workstation, install them with:

```bash
dnf install krb5-devel gpgme-devel libassuan-devel gcc
```

For macOS, install build dependencies with:

```bash
brew install heimdal gpgme
```

## Testing

All PRs must pass automated checks — `go fmt`, `go vet`, unit tests, and e2e tests against a real cluster.

Locally you can run verification and unit tests with:

```bash
make verify
make test
```

## Dependencies

Dependencies are managed through [Go Modules](https://github.com/golang/go/wiki/Modules).
When updating any dependency:

1. `go mod tidy`
2. `go mod vendor`

## Key Dependencies

| Repository | Role |
|---|---|
| [kubernetes/kubectl](https://github.com/kubernetes/kubectl) | Upstream CLI — oc wraps and extends it |
| [k8s.io/client-go](https://github.com/kubernetes/client-go) | Kubernetes API client library |
| [openshift/api](https://github.com/openshift/api) | OCP API type definitions |
| [openshift/client-go](https://github.com/openshift/client-go) | Typed clients for OCP resources |
| [openshift/library-go](https://github.com/openshift/library-go) | Shared OCP library code |
| [openshift/build-machinery-go](https://github.com/openshift/build-machinery-go) | Standard Makefile targets |

## Security

If you've found a security issue that you'd like to disclose confidentially, please contact Red Hat's Product Security team. Details at https://access.redhat.com/security/team/contact

Do not file security issues as public GitHub issues.

## License

oc is licensed under the [Apache License, Version 2.0](http://www.apache.org/licenses/).
