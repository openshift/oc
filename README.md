## `service-catalog`

[![Build Status](https://travis-ci.org/kubernetes-incubator/service-catalog.svg?branch=master)](https://travis-ci.org/kubernetes-incubator/service-catalog "Travis")
[![Build Status](https://service-catalog-jenkins.appspot.com/buildStatus/icon?job=service-catalog-master-testing)](https://service-catalog-jenkins.appspot.com/job/service-catalog-master-testing/ "Jenkins")
[![Go Report Card](https://goreportcard.com/badge/github.com/kubernetes-incubator/service-catalog)](https://goreportcard.com/report/github.com/kubernetes-incubator/service-catalog)

### Introduction

The service-catalog project is in incubation to bring integration with service
brokers to the Kubernetes ecosystem via the [Open Service Broker API](https://github.com/openservicebrokerapi/servicebroker).

A _service broker_ is an endpoint that manages a set of software offerings
called _services_. The end-goal of the service-catalog project is to provide
a way for Kubernetes users to consume services from brokers and easily
configure their applications to use those services, without needing detailed
knowledge about how those services are created or managed.

As an example:

Most applications need a datastore of some kind.  The service-catalog allows
Kubernetes applications to consume services like databases that exist
_somewhere_ in a simple way:

1.  A user wanting to consume a database in their application browses a list of
    available services in the catalog
2.  The user asks for a new instance of that service to be _provisioned_

     _Provisioning_ means that the broker somehow creates a new instance of a
    service.  This could mean basically anything that results in a new instance
    of the service becoming available.  Possibilities include: creating a new
    set of Kubernetes resources in another namespace in the same Kubernetes
    cluster as the consumer or a different cluster, or even creating a new
    tenant in a multi-tenant SaaS system.  The point is that the
    consumer doesn't have to be aware of or care at all about the details.
3.  The user requests a _binding_ to use the service instance in their application

    Credentials are delivered to users in normal Kubernetes secrets and
    contain information necessary to connect to and authenticate to the
    service instance.

For more introduction, including installation and self-guided demo 
instructions, please see the [introduction](./docs/introduction.md) doc.

For more details about the design and features of this project see the
[design](docs/design.md) doc.

#### Video links

- [Service Catalog Basic Concepts](https://goo.gl/6xINOa)
- [Service Catalog Basic Demo](https://goo.gl/IJ6CV3)
- [SIG Service Catalog Meeting Playlist](https://goo.gl/ZmLNX9)

---

### Overall Status

We are currently working toward a beta-quality release to be used in conjunction with
Kubernetes 1.8. See the
[milestones list](https://github.com/kubernetes-incubator/service-catalog/milestones?direction=desc&sort=due_date&state=open) 
for information about the issues and PRs in current and future milestones.

The project [roadmap](https://github.com/kubernetes-incubator/service-catalog/wiki/Roadmap)
contains information about our high-level goals for future milestones.

We are currently making weekly releases; see the
[release process](https://github.com/kubernetes-incubator/service-catalog/wiki/Release-Process)
for more information.

### Documentation

Our goal is to have extensive use-case and functional documentation.

See [here](./docs/v1) for detailed documentation.

See [here](https://github.com/kubernetes-incubator/service-catalog/wiki/Examples) for examples and
[here](https://github.com/openservicebrokerapi/servicebroker/blob/master/gettingStarted.md) for
broker servers that are compatible with this software.

### Terminology

This project's problem domain contains a few inconvenient but unavoidable
overloads with other Kubernetes terms.  Check out our [terminology page](./terminology.md)
for definitions of terms as they are used in this project.

### Contributing

Interested in contributing?  Check out the [contributing documentation](./CONTRIBUTING.md).

Also see the [developer's guide](./docs/devguide.md) for information on how to
build and test the code.

We have weekly meetings - see
[Kubernetes SIGs](https://github.com/kubernetes/community/blob/master/sig-list.md)
(search for "Service Catalog") for the exact date and time. Our agenda/notes
doc can be found
[here](https://docs.google.com/document/d/17xlpkoEbPR5M6P5VDzNx17q6-IPFxKyebEekCGYiIKM/edit)

Previous Agenda notes are also available:
[2016-08-29 through 2017-09-17](https://docs.google.com/document/d/10VsJjstYfnqeQKCgXGgI43kQWnWFSx8JTH7wFh8CmPA/edit).

### Kubernetes Incubator

This is a [Kubernetes Incubator project](https://github.com/kubernetes/community/blob/master/incubator.md).
The project was established 2016-Sept-12.  The incubator team for the project is:

- Sponsor: Brian Grant ([@bgrant0607](https://github.com/bgrant0607))
- Champion: Paul Morie ([@pmorie](https://github.com/pmorie))
- SIG: [sig-service-catalog](https://github.com/kubernetes/community/tree/master/sig-service-catalog)

For more information about sig-service-catalog such as meeting times and agenda,
check out the [community site](https://github.com/kubernetes/community/tree/master/sig-service-catalog).

### Code of Conduct

OpenShift runs with the following security policy by default:

  * Containers run as a non-root unique user that is separate from other system users
    * They cannot access host resources, run privileged, or become root
    * They are given CPU and memory limits defined by the system administrator
    * Any persistent storage they access will be under a unique SELinux label, which prevents others from seeing their content
    * These settings are per project, so containers in different projects cannot see each other by default
  * Regular users can run Docker, source, and custom builds
    * By default, Docker builds can (and often do) run as root. You can control who can create Docker builds through the `builds/docker` and `builds/custom` policy resource.
  * Regular users and project admins cannot change their security quotas.

Many Docker containers expect to run as root (and therefore edit all the contents of the filesystem). The [Image Author's guide](https://docs.openshift.org/latest/creating_images/guidelines.html#openshift-specific-guidelines) gives recommendations on making your image more secure by default:

    * Don't run as root
    * Make directories you want to write to group-writable and owned by group id 0
    * Set the net-bind capability on your executables if they need to bind to ports < 1024

If you are running your own cluster and want to run a container as root, you can grant that permission to the containers in your current project with the following command:

    # Gives the default service account in the current project access to run as UID 0 (root)
    oc adm add-scc-to-user anyuid -z default 

See the [security documentation](https://docs.openshift.org/latest/admin_guide/manage_scc.html) more on confining applications.


Support for Kubernetes Alpha Features
-----------------------------------------

Some features from upstream Kubernetes are not yet enabled in OpenShift, for reasons including supportability, security, or limitations in the upstream feature.

Kubernetes Definitions:

* Alpha
  * The feature is available, but no guarantees are made about backwards compatibility or whether data is preserved when feature moves to Beta.
  * The feature may have significant bugs and is suitable for testing and prototyping.
  * The feature may be replaced or significantly redesigned in the future.
  * No migration to Beta is generally provided other than documentation of the change.
* Beta
  * The feature is available and generally agreed to solve the desired solution, but may need stabilization or additional feedback.
  * The feature is potentially suitable for limited production use under constrained circumstances.
  * The feature is unlikely to be replaced or removed, although it is still possible for feature changes that require migration.

OpenShift uses these terms in the same fashion as Kubernetes, and adds four more:

* Not Yet Secure
  * Features which are not yet enabled because they have significant security or stability risks to the cluster
  * Generally this applies to features which may allow escalation or denial-of-service behavior on the platform
  * In some cases this is applied to new features which have not had time for full security review
* Potentially Insecure
  * Features that require additional work to be properly secured in a multi-user environment
  * These features are only enabled for cluster admins by default and we do not recommend enabling them for untrusted users
  * We generally try to identify and fix these within 1 release of their availability
* Tech Preview
  * Features that are considered unsupported for various reasons are known as 'tech preview' in our documentation
  * Kubernetes Alpha and Beta features are considered tech preview, although occasionally some features will be graduated early
  * Any tech preview feature is not supported in OpenShift Container Platform except through exemption
* Disabled Pending Migration
  * These are features that are new in Kubernetes but which originated in OpenShift, and thus need migrations for existing users
  * We generally try to minimize the impact of features introduced upstream to Kubernetes on OpenShift users by providing seamless
    migration for existing clusters.
  * Generally these are addressed within 1 Kubernetes release

The list of features that qualify under these labels is described below, along with additional context for why.

Feature | Kubernetes | OpenShift | Justification
------- | ---------- | --------- | -------------
Third Party Resources | Alpha (1.4, 1.5) | Not Yet Secure | Third party resources are still under active development upstream.<br>Known issues include failure to clean up resources in etcd, which may result in a denial of service attack against the cluster.<br>We are considering enabling them for development environments only.
Garbage Collection | Alpha (1.3)<br>Beta (1.4, 1.5) | Tech Preview (1.4, 1.5) | Garbage collection will automatically delete related resources on the server, and thus given the potential for data loss we are waiting for GC to graduate to beta and have a full release cycle of testing before enabling it in Origin.
Stateful Sets | Alpha (1.3, 1.4)<br>Beta (1.5) | Tech Preview (1.3, 1.4, 1.5) | Stateful Sets are still being actively developed and no backwards compatibility is guaranteed until 1.5 is released. Starting in 1.5, Stateful Sets will be enabled by default and some backwards compatibility will be guaranteed.
Init Containers | Alpha (1.3, 1.4)<br>Beta(1.5) | Tech Preview (1.3, 1.4, 1.5) | Init containers are properly secured, but will not be officially supported until 1.6.
Federated Clusters | Alpha (1.3)<br>Beta (1.4, 1.5) | Tech Preview (1.3, 1.4, 1.5) | A Kubernetes federation server may be used against Origin clusters with the appropriate credentials today.<br>Known issues include tenant support in federation and the ability to have consistent access control between federation and normal clusters.<br>No Origin specific binary is being distributed for federation at this time.
Deployment | Beta (1.3, 1.4, 1.5) | Tech Preview (1.3, 1.4, 1.5) | OpenShift launched with DeploymentConfigs, a more fully featured Deployment object. DeploymentConfigs are more appropriate for developer flows where you want to push code and have it automatically be deployed, and also provide more advanced hooks and custom deployments.  Use Kubernetes Deployments when you are managing change outside of OpenShift.
Replica Sets | Beta (1.3, 1.4, 1.5) | Tech Preview (1.3, 1.4, 1.5) | Replica Sets perform the same function as Replication Controllers, but have a more powerful label syntax. Both ReplicationControllers and ReplicaSets can be used.  
Ingress | Beta (1.2, 1.3, 1.4, 1.5) | Tech Preview (1.3, 1.4, 1.5) | OpenShift launched with Routes, a more full featured Ingress object. In 1.5, Ingress rules can be read by the router (disabled by default), but because Ingress objects reference secrets you must grant the routers a very level of access to your cluster to run with them.  Future changes will likely reduce the security impact of enabling Ingress.
PodSecurityPolicy | Beta (1.3, 1.4, 1.5) | Tech Preview (1.3, 1.4, 1.5) | OpenShift launched with SecurityContextConstraints, and then upstreamed them as PodSecurityPolicy. We plan to enable upstream PodSecurityPolicy so as to automatically migrate existing SecurityContextConstraints. PodSecurityPolicy has not yet completed a full security review, which will be part of the criteria for tech preview. <br>SecurityContextConstraints are a superset of PodSecurityPolicy features.
PodAntiAffinitySelectors | Beta (1.3, 1.4, 1.5) | Not Yet Secure (1.3)<br>Tech Preview (1.4, 1.5) | End users are not allowed to set PodAntiAffinitySelectors that are not the node name due to the possibility of attacking the scheduler via denial of service.
NetworkPolicy | Beta (1.3, 1.4, 1.5) | Tech Preview (1.3, 1.4, 1.5) | Starting with 1.5, OpenShift SDN will expose an experimental mode that uses network policy to restrict access to pods.  Future releases will expand this support.

Please contact us if this list omits a feature supported in Kubernetes which does not run in Origin.


Contributing
------------

You can develop [locally on your host](CONTRIBUTING.adoc#develop-locally-on-your-host) or with a [virtual machine](CONTRIBUTING.adoc#develop-on-virtual-machine-using-vagrant), or if you want to just try out Origin [download the latest Linux server, or Windows and Mac OS X client pre-built binaries](CONTRIBUTING.adoc#download-from-github).

First, **get up and running with the** [**Contributing Guide**](CONTRIBUTING.adoc).

All contributions are welcome - Origin uses the Apache 2 license and does not require any contributor agreement to submit patches.  Please open issues for any bugs or problems you encounter, ask questions on the OpenShift IRC channel (#openshift-dev on freenode), or get involved in the [Kubernetes project](https://github.com/kubernetes/kubernetes) at the container runtime layer.

See [HACKING.md](https://github.com/openshift/origin/blob/master/HACKING.md) for more details on developing on Origin including how different tests are setup.

If you want to run the test suite, make sure you have your environment set up, and from the `origin` directory run:

```
# run the verifiers, unit tests, and command tests
$ make check

# run a command-line integration test suite
$ hack/test-cmd.sh

# run the integration server test suite
$ hack/test-integration.sh

# run the end-to-end test suite
$ hack/test-end-to-end.sh

# run all of the tests above
$ make test
```

You'll need [etcd](https://github.com/coreos/etcd) installed and on your path for the integration and end-to-end tests to run, and Docker must be installed to run the end-to-end tests.  To install etcd you should be able to run:

```
$ hack/install-etcd.sh
```

Some of the components of Origin run as Docker images, including the builders and deployment tools in `images/builder/docker/*` and `images/deploy/*`.  To build them locally run

```
$ hack/build-images.sh
```

To hack on the web console, check out the [assets/README.md](assets/README.md) file for instructions on testing the console and building your changes.

Security Response
-----------------
If you've found a security issue that you'd like to disclose confidentially
please contact Red Hat's Product Security team. Details at
https://access.redhat.com/security/team/contact


License
-------

OpenShift is licensed under the [Apache License, Version 2.0](http://www.apache.org/licenses/).
