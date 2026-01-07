# OpenShift Client - oc

With OpenShift Client CLI (oc), you can create applications and manage OpenShift
resources.  It is built on top of [kubectl](https://github.com/kubernetes/kubectl/)
which means it provides its full capabilities to connect with any kubernetes
compliant cluster, and on top adds commands simplifying interaction with an
OpenShift cluster.


# Contributing

All contributions are welcome - oc uses the Apache 2 license and does not require
any contributor agreement to submit patches.  Please open issues for any bugs
or problems you encounter. You can also get involved with the [kubectl](https://github.com/kubernetes/kubectl)
and the [Kubernetes project](https://github.com/kubernetes/kubernetes).

## Building

To build oc invoke `make oc`. At any time you can invoke `make help` and you'll
get a list of all supported make sub-commands.

In order to build `oc`, you will need the GSSAPI sources. On a Fedora/CentOS/RHEL
workstation, install them with:

```
dnf install krb5-devel
```

Also:

```
dnf install gpgme-devel
dnf install libassuan-devel
```

For MacOS you'll need to install a few brew packages before building locally. Install them with:
```
brew install heimdal
brew install gpgme
```
## Testing

All PRs will have to pass a series of automated tests starting from go tools
such as `go fmt` and `go vet`, through unit tests, up to e2e against a real cluster.

Locally you can invoke the initial verification and unit test through `make verify`
and `make test`, accordingly.

## Dependencies

Dependencies are managed through [Go Modules](https://github.com/golang/go/wiki/Modules).
When updating any dependency the suggested workflow is:

1. `go mod tidy`
2. `go mod vendor`


# Security Response

If you've found a security issue that you'd like to disclose confidentially
please contact Red Hat's Product Security team. Details at
https://access.redhat.com/security/team/contact

## Tests

This repository is compatible with the "OpenShift Tests Extension (OTE)" framework.

### Building the test binary
```bash
make build
```

### Running test suites and tests
```bash
# Run a specific test suite or test
./oc-tests-ext run-suite openshift/oc/all
./oc-tests-ext run-test "test-name"

# Run with JUnit output
./oc-tests-ext run-suite openshift/oc/all --junit-path=/tmp/junit-results/junit.xml
./oc-tests-ext run-test "test-name" --junit-path=/tmp/junit-results/junit.xml
```

### Listing available tests and suites
```bash
# List all test suites
./oc-tests-ext list-suites

# List tests in a specific suite
./oc-tests-ext list-tests openshift/oc/all
```

The test extension binary is included in the production image for CI/CD integration.

# License

oc is licensed under the [Apache License, Version 2.0](http://www.apache.org/licenses/).
