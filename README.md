# OpenShift Client - oc

With OpenShift Client CLI (oc), you can create applications and manage OpenShift
resources.  It is built on top of [kubectl](https://github.com/kubernetes/kubectl/)
which means it provides its full capabilities to connect with any kubernetes
compliant cluster, and on top adds commands simplifying interaction with an
OpenShift cluster.


# Contributing

All contributions are welcome - oc uses the Apache 2 license and does not require
any contributor agreement to submit patches.  Please open issues for any bugs
or problems you encounter, ask questions on the OpenShift IRC channel
(#openshift-dev on freenode), or get involved in the [kubectl](https://github.com/kubernetes/kubectl)
and [kubernetes project](https://github.com/kubernetes/kubernetes) at the container
runtime layer.

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
