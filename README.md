OKD: The Origin Community Distribution of Kubernetes
=======================================================

[![Build Status](https://travis-ci.org/kubernetes-incubator/service-catalog.svg?branch=master)](https://travis-ci.org/kubernetes-incubator/service-catalog "Travis")
[![Build Status](https://service-catalog-jenkins.appspot.com/buildStatus/icon?job=service-catalog-master-testing)](https://service-catalog-jenkins.appspot.com/job/service-catalog-master-testing/ "Jenkins")
[![Go Report Card](https://goreportcard.com/badge/github.com/kubernetes-incubator/service-catalog)](https://goreportcard.com/report/github.com/kubernetes-incubator/service-catalog)

***OKD*** is the Origin community distribution of [Kubernetes](https://kubernetes.io) optimized for continuous application development and multi-tenant deployment.  OKD adds developer and operations-centric tools on top of Kubernetes to enable rapid application development, easy deployment and scaling, and long-term lifecycle maintenance for small and large teams. ***OKD*** is also referred to as ***Origin*** in github and in the documentation.

The service-catalog project is in incubation to bring integration with service
brokers to the Kubernetes ecosystem via the [Open Service Broker API](https://github.com/openservicebrokerapi/servicebroker).

A _service broker_ is an endpoint that manages a set of software offerings
called _services_. The end-goal of the service-catalog project is to provide
a way for Kubernetes users to consume services from brokers and easily
configure their applications to use those services, without needing detailed
knowledge about how those services are created or managed.

As an example:

Most applications need a datastore of some kind. The service-catalog allows
Kubernetes applications to consume services like databases that exist
_somewhere_ in a simple way:

* **[Public Documentation](https://docs.okd.io/latest/welcome/)**
  * **[API Documentation](https://docs.okd.io/latest/rest_api/index.html)**
* Our **[Trello Roadmap](https://ci.openshift.redhat.com/roadmap_overview.html)** covers the epics and stories being worked on (click through to individual items)

    _Provisioning_ means that the broker somehow creates a new instance of a
   service. This could mean basically anything that results in a new instance
   of the service becoming available. Possibilities include: creating a new
   set of Kubernetes resources in another namespace in the same Kubernetes
   cluster as the consumer or a different cluster, or even creating a new
   tenant in a multi-tenant SaaS system. The point is that the
   consumer doesn't have to be aware of or care at all about the details.
3. The user requests a _binding_ to use the service instance in their application

    Credentials are delivered to users in normal Kubernetes secrets and
    contain information necessary to connect to and authenticate to the
    service instance.

For more introduction, including installation and self-guided demo
instructions, please see the [introduction](./docs/introduction.md) doc.

For more details about the design and features of this project see the
[design](docs/design.md) doc.

* On any system with a Docker engine installed, you can run `oc cluster up` to get started immediately.  Try it out now!
* For a full cluster installation using [Ansible](https://github.com/openshift/openshift-ansible), follow the [Advanced Installation guide](https://docs.okd.io/latest/install_config/install/advanced_install.html)
* To build and run from source, see [CONTRIBUTING.adoc](CONTRIBUTING.adoc)

The latest OKD Origin images are published to the Docker Hub under the `openshift` account at https://hub.docker.com/u/openshift/. We use a rolling tag system as of v3.9, where the `:latest` tag always points to the most recent alpha release on `master`, the `v3.X` tag points to the most recent build for that release (pre-release and post-release), and `v3.X.Y` is a stable tag for patches to a release.

### Concepts

OKD builds a developer-centric workflow around Docker containers and Kubernetes runtime concepts.  An **Image Stream** lets you easily tag, import, and publish Docker images from the integrated registry.  A **Build Config** allows you to launch Docker builds, build directly from source code, or trigger Jenkins Pipeline jobs whenever an image stream tag is updated.  A **Deployment Config** allows you to use custom deployment logic to rollout your application, and Kubernetes workflow objects like **DaemonSets**, **Deployments**, or **StatefulSets** are upgraded to automatically trigger when new images are available.  **Routes** make it trivial to expose your Kubernetes services via a public DNS name. As an administrator, you can enable your developers to request new **Projects** which come with predefined roles, quotas, and security controls to fairly divide access.

For more on the underlying concepts of OKD, please see the [documentation site](https://docs.okd.io/latest/welcome/index.html).

### OKD API

The OKD API is located on each server at `https://<host>:8443/apis`. OKD adds its own API groups alongside the Kubernetes APIs. For more, [see the API documentation](https://docs.okd.io/latest/rest_api).

We are currently making weekly releases; see the
[release process](https://github.com/kubernetes-incubator/service-catalog/wiki/Release-Process)
for more information.

OKD extends Kubernetes with security and other developer centric concepts.  Each OKD release ships slightly after the Kubernetes release has stabilized. Version numbers are aligned - OKD v3.9 is Kubernetes v1.9.

If you're looking for more information about using Kubernetes or the lower level concepts that OKD depends on, see the following:

* [Kubernetes Getting Started](https://kubernetes.io/docs/tutorials/kubernetes-basics/)
* [Kubernetes Documentation](https://kubernetes.io/docs/)
* [Kubernetes API](https://docs.okd.io/latest/rest_api)

For details on broker servers that are compatible with this software, see the
Open Service Broker API project's [Getting Started guide](https://github.com/openservicebrokerapi/servicebroker/blob/master/gettingStarted.md).

### What can I run on OKD?

OKD is designed to run any existing Docker images.  Additionally, you can define builds that will produce new Docker images using a `Dockerfile`.

### Contributing

You can see the [full list of Source-to-Image builder images](https://docs.okd.io/latest/using_images/s2i_images/overview.html) and it's straightforward to [create your own](https://blog.openshift.com/create-s2i-builder-image/).  Some of our available images include:

Also see the [developer's guide](./docs/devguide.md) for information on how to
build and test the code.

Your application image can be easily extended with a database service with our [database images](https://docs.okd.io/latest/using_images/db_images/overview.html):

Previous meeting notes are also available:
[2016-08-29 through 2017-09-17](https://docs.google.com/document/d/10VsJjstYfnqeQKCgXGgI43kQWnWFSx8JTH7wFh8CmPA/edit).

### Kubernetes Incubator

OKD runs with the following security policy by default:

- Sponsor: Brian Grant ([@bgrant0607](https://github.com/bgrant0607))
- Champion: Paul Morie ([@pmorie](https://github.com/pmorie))
- SIG: [sig-service-catalog](https://github.com/kubernetes/community/tree/master/sig-service-catalog)

Many Docker containers expect to run as root (and therefore edit all the contents of the filesystem). The [Image Author's guide](https://docs.okd.io/latest/creating_images/guidelines.html#openshift-specific-guidelines) gives recommendations on making your image more secure by default:

### Code of Conduct

Participation in the Kubernetes community is governed by the
[Kubernetes Code of Conduct](./code-of-conduct.md).

    # Gives the default service account in the current project access to run as UID 0 (root)
    oc adm add-scc-to-user anyuid -z default

See the [security documentation](https://docs.okd.io/latest/admin_guide/manage_scc.html) more on confining applications.


Support for Kubernetes Alpha Features
-----------------------------------------

Some features from upstream Kubernetes are not yet enabled in OKD, for reasons including supportability, security, or limitations in the upstream feature.

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

OKD uses these terms in the same fashion as Kubernetes, and adds four more:

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
  * Any tech preview feature is not supported in OKD except through exemption
* Disabled Pending Migration
  * These are features that are new in Kubernetes but which originated in OKD, and thus need migrations for existing users
  * We generally try to minimize the impact of features introduced upstream to Kubernetes on OKD users by providing seamless
    migration for existing clusters.
  * Generally these are addressed within 1 Kubernetes release

The list of features that qualify under these labels is described below, along with additional context for why.

Feature | Kubernetes | OKD       | Justification
------- | ---------- | --------- | -------------
Custom Resource Definitions | GA (1.9) | GA (3.9) |
Stateful Sets | GA (1.9) | GA (3.9) |
Deployment | GA (1.9) | GA (1.9) |
Replica Sets | GA (1.9) | GA (3.9) | Replica Sets perform the same function as Replication Controllers, but have a more powerful label syntax. Both ReplicationControllers and ReplicaSets can be used.
Ingress | Beta (1.9) | Tech Preview (3.9) | OKD launched with Routes, a more full featured Ingress object. Ingress rules can be read by the router (disabled by default), but because Ingress objects reference secrets you must grant the routers access to your secrets manually.  Ingress is still beta in upstream Kubernetes.
PodSecurityPolicy | Beta (1.9) | Tech Preview (3.9) | OKD launched with SecurityContextConstraints, and then upstreamed them as PodSecurityPolicy. We plan to enable upstream PodSecurityPolicy so as to automatically migrate existing SecurityContextConstraints. PodSecurityPolicy has not yet completed a full security review, which will be part of the criteria for tech preview. <br>SecurityContextConstraints are a superset of PodSecurityPolicy features.
NetworkPolicy | GA (1.6) | GA (3.7) |

Please contact us if this list omits a feature supported in Kubernetes which does not run in Origin.


Contributing
------------

You can develop [locally on your host](CONTRIBUTING.adoc#develop-locally-on-your-host) or with a [virtual machine](CONTRIBUTING.adoc#develop-on-virtual-machine-using-vagrant), or if you want to just try out Origin [download the latest Linux server, or Windows and Mac OS X client pre-built binaries](CONTRIBUTING.adoc#download-from-github).

First, **get up and running with the** [**Contributing Guide**](CONTRIBUTING.adoc).

All contributions are welcome - OKD uses the Apache 2 license and does not require any contributor agreement to submit patches.  Please open issues for any bugs or problems you encounter, ask questions on the OpenShift IRC channel (#openshift-dev on freenode), or get involved in the [Kubernetes project](https://github.com/kubernetes/kubernetes) at the container runtime layer.

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

OKD is licensed under the [Apache License, Version 2.0](http://www.apache.org/licenses/).