## SDN solutions for Openshift

Software to get an overlay network up and running for a docker cluster. This is still a work in progress. Do not use it in production.

This is the source repository for [OpenShift 3](https://openshift.github.io), based on top of [Docker](https://www.docker.io) containers and the
[Kubernetes](https://github.com/GoogleCloudPlatform/kubernetes) container cluster manager.
OpenShift adds developer and operational centric tools on top of Kubernetes to enable rapid application development,
easy deployment and scaling, and long-term lifecycle maintenance for small and large teams and applications.

	$ git clone https://github.com/openshift/openshift-sdn
	$ cd openshift-sdn
	$ make clean        # optional
	$ make              # build
	$ make install      # installs in /usr/bin

* Build web-scale applications with integrated service discovery, DNS, load balancing, failover, health checking, persistent storage, and fast scaling
* Push source code to your Git repository and have image builds and deployments automatically occur
* Easy to use client tools for building web applications from source code
  * Templatize the components of your system, reuse them, and iteratively deploy them over time
* Centralized administration and management of application component libraries
  * Roll out changes to software stacks to your entire organization in a controlled fashion
* Team and user isolation of containers, builds, and network communication in an easy multi-tenancy system
  * Allow developers to run containers securely by preventing root access and isolating containers with SELinux
  * Limit, track, and manage the resources teams are using

##### Use vagrant, pre-define a cluster, and bring it up

* **[OpenShift Public Documentation](http://docs.openshift.org/latest/welcome/index.html)**
* The **[Trello Roadmap](https://ci.openshift.redhat.com/roadmap_overview.html)** covers the epics and stories being worked on (click through to individual items)
* **[Technical Architecture Presentation](https://docs.google.com/presentation/d/1Isp5UeQZTo3gh6e59FMYmMs_V9QIQeBelmbyHIJ1H_g/pub?start=false&loop=false&delayms=3000)**
* **[System Architecture](https://github.com/openshift/openshift-pep/blob/master/openshift-pep-013-openshift-3.md)** design document

	$ git clone https://github.com/openshift/origin
	$ cd origin
	$ make clean
	$ export OPENSHIFT_DEV_CLUSTER=1
	$ export OPENSHIFT_NUM_MINIONS=2
	$ export OPENSHIFT_SDN=ovs-simple
	$ vagrant up

##### Manually add nodes to a master

Steps to create manually create an OpenShift cluster with openshift-sdn. This requires that each machine (master, nodes) have compiled `openshift` and `openshift-sdn` already. Check [here](https://github.com/openshift/origin) for OpenShift instructions. Ensure 'openvswitch' is installed and running (`yum install -y openvswitch && systemctl enable openvswitch && systemctl start openvswitch`). Also verify that the `DOCKER_OPTIONS` variable is unset in your environment, or set to a known-working value (e.g. `DOCKER_OPTIONS='-b=lbr0 --mtu=1450 --selinux-enabled'`). If you don't know what to put there, it's probably best to leave it unset. :)

Security!!!
-------------------
OpenShift runs with the following security policy by default:

* Containers run as a non-root unique user that is separate from other system users
  * They cannot access host resources, run privileged, or become root
  * They are given CPU and memory limits defined by the system administrator
  * Any persistent storage they access will be under a unique SELinux label, which prevents others from seeing their content
  * These settings are per project, so containers in different projects cannot see each other by default
* Regular users can run Docker, source, and custom builds
  * By default, Docker builds can (and often do) run as root. You can control who can create Docker builds through the `builds/docker` and `builds/custom` policy resource.
* Regular users and project admins cannot change their security quotas.

See the [security documentation](https://docs.openshift.org/latest/admin_guide/manage_scc.html) for more on managing these restrictions.

	$ openshift-sdn -etcd-endpoints=http://openshift-master:4001 -node -public-ip=<10.10....> -hostname <hostname>
	where, 
		-etcd-endpoints	: reach the etcd db here
		-node 	        : run it in node mode (will watch etcd servers for new node subnets)
		-public-ip	: use this field for suggesting the publicly reachable IP address of this node
		-hostname	: the name that will be used to register the node with openshift-master
	$ openshift start node --master=https://openshift-master:8443

You'll need to configure the Docker daemon on your host to trust the Docker registry service you'll be starting.

To do this, you need to add "--insecure-registry 172.30.0.0/16" to the Docker daemon invocation, eg:

    $ docker -d --insecure-registry 172.30.0.0/16

If you are running Docker as a service via `systemd`, you can add this argument to the options value in `/etc/sysconfig/docker`

This will instruct the Docker daemon to trust any Docker registry on the 172.30.0.0/16 subnet,
rather than requiring the registry to have a verifiable certificate.

**Important!**: Docker on non-RedHat distributions (Ubuntu, Debian, boot2docker) has mount propagation PRIVATE, which [breaks](https://github.com/openshift/origin/issues/3072) running OpenShift inside a container. Please use the [Vagrant](CONTRIBUTING.adoc#develop-on-virtual-machine-using-vagrant) or binary installation paths on those distributions.

	Create a json file for the new node resource
        $ cat <<EOF > node-1.json
	{
		"kind":"Node",
		"id":"openshift-minion-1",
		"apiVersion":"v1"
	}
	EOF
	where, openshift-minion-1 is a hostname that is resolvable from the master (or, create an entry in /etc/hosts and point it to the public-ip of the node).
	$ openshift cli create -f node-1.json

Done. Repeat last two pieces to add more nodes. Create new pods from the master (or just docker containers on the nodes), and see that the pods are indeed reachable from each other.

Once the container is started, you can jump into a console inside the container and run the CLI.

    $ sudo docker exec -it origin bash

    # Start the OpenShift integrated registry in a container
    $ oadm registry --credentials=./openshift.local.config/master/openshift-registry.kubeconfig

    # Use the CLI to login, create a project, and then create your app.
    $ oc --help
    $ oc login
    Username: test
    Password: test
    $ oc new-project test
    $ oc new-app -f https://raw.githubusercontent.com/openshift/origin/master/examples/sample-app/application-template-stibuild.json

    # See everything you just created!
    $ oc status

Any username and password are accepted by default (with no credential system configured).  You can view the webconsole at [https://localhost:8443/console](https://localhost:8443/console) in your browser - login with the same credentials you used above and you'll see the application you just created.

![Web console overview](docs/screenshots/console_overview.png?raw=true)

You can also use the Docker container to run our CLI (`sudo docker exec -it origin cli --help`) or download the `oc` command-line client from the [releases](https://github.com/openshift/origin/releases) page for Mac, Windows, or Linux and login from your host with `oc login`.

You can reset your server by stopping the `origin` container and then removing it via Docker. The contents of `/var/lib/openshift` can then be removed. See the [public docs](http://docs.openshift.org/latest/welcome/index.html) for more about running a permanent installation of OpenShift.


### Next Steps

We highly recommend trying out the [OpenShift walkthrough](https://github.com/openshift/origin/blob/master/examples/sample-app/README.md), which shows some of the lower level pieces of of OpenShift that will be the foundation for user applications.  The walkthrough is accompanied by a blog series on [blog.openshift.com](https://blog.openshift.com/openshift-v3-deep-dive-docker-kubernetes/) that goes into more detail.  It's a great place to start, albeit at a lower level than OpenShift 2.

Both OpenShift and Kubernetes have a strong focus on documentation - see the following for more information about them:

* [OpenShift Documentation](http://docs.openshift.org/latest/welcome/index.html)
* [Kubernetes Getting Started](https://github.com/GoogleCloudPlatform/kubernetes/blob/master/README.md)
* [Kubernetes Documentation](https://github.com/GoogleCloudPlatform/kubernetes/blob/master/docs/README.md)


Steps:

1. Run etcd somewhere, and run the openshift-sdn master to watch it in sync mode.

		$ systemctl start etcd
		$ openshift-sdn -master -sync  # use -etcd-endpoints=http://target:4001 if etcd is not running locally

2. To add a node, make sure the 'hostname/dns' is reachable from the machine that is running 'openshift-sdn master'. Then start the openshift-sdn in node mode with sync flag.

		$ openshift-sdn -node -sync -etcd-endpoints=http://master-host:4001 -hostname=node-1-dns -public-ip=<public ip that the hostname resolves to>

Done. Add more nodes by repeating step 2. All nodes should have a docker bridge (lbr0) that is part of the overlay network.

#### Gotchas..

Some requirements, some silly errors.

 - openshift-sdn fails with errors around ovs-vsctl.. 
	yum -y install openvswitch && systemctl enable openvswitch && systemctl start openvswitch
 - openshift-sdn fails to start with errors relating to 'network not up' etc.
	systemctl stop NetworkManager # that fella is nosy, does not like mint new bridges
 - openshift-sdn fails to start saying cannot reach etcd endpoints
	etcd not running really or not listening on public interface? That machine not reachable possibly? -etcd-endpoints=https?? without ssl being supplied? Remove the trailing '/' from the url maybe?
 - openshift-sdn is up, I think I got the subnet, but my pings do not work
	It may take a while for the ping to work (blame the docker linux bridge, optimizations coming). Check that all nodes' hostnames on master are resolvable and to the correct IP addresses. Last, but not the least - firewalld (switch it off and check, and then punch a hole for vxlan please).

The OpenShift APIs are exposed at `https://localhost:8443/oapi/v1/*`.

To experiment with the API, you can get a token to act as a user:

    $ sudo docker exec -it openshift-origin bash
    $ oc login
    Username: test
    Password: test
    $ oc whoami -t
    <prints a token>
    $ exit
    # from your host
    $ curl -H "Authorization: bearer <token>" https://localhost:8443/oapi/v1/...


### API Documentation

The API documentation can be found [here](http://docs.openshift.org/latest/rest_api/openshift_v1.html).


FAQ
---

1. How does OpenShift relate to Kubernetes?

    OpenShift embeds Kubernetes and adds additional functionality to offer a simple, powerful, and
    easy-to-approach developer and operator experience for building applications in containers.
    Kubernetes today is focused around composing containerized applications - OpenShift adds
    building images, managing them, and integrating them into deployment flows.  Our goal is to do
    most of that work upstream, with integration and final packaging occurring in OpenShift.  As we
    iterate through the next few months, you'll see this repository focus more on integration and
    plugins, with more and more features becoming part of Kubernetes.

2. What can I run on OpenShift?

    OpenShift is designed to run any existing Docker images.  In addition you can define builds that will produce new Docker images from a Dockerfile.  However the real magic of OpenShift can be seen when using [Source-To-Image](https://github.com/openshift/source-to-image) builds which allow you to simply supply an application source repository which will be combined with an existing Source-To-Image enabled Docker image to produce a new runnable image that runs your application.  We are continuing to grow the ecosystem of Source-To-Image enabled images and documenting them [here](http://docs.openshift.org/latest/using_images/s2i_images/overview.html). Our available images are:

    * [Ruby](https://github.com/openshift/sti-ruby)
    * [Python](https://github.com/openshift/sti-python)
    * [NodeJS](https://github.com/openshift/sti-nodejs)
    * [PHP](https://github.com/openshift/sti-php)
    * [Perl](https://github.com/openshift/sti-perl)
    * [Wildfly](https://github.com/openshift/wildfly-8-centos)

    Your application image can be easily extended with a database service with our [database images](http://docs.openshift.org/latest/using_images/db_images/overview.html). Our available database images are:

    * [MySQL](https://github.com/openshift/mysql)
    * [MongoDB](https://github.com/openshift/mongodb)
    * [PostgreSQL](https://github.com/openshift/postgresql)

Contributing
------------

You can develop [locally on your host](CONTRIBUTING.adoc#develop-locally-on-your-host) or with a [virtual machine](CONTRIBUTING.adoc#develop-on-virtual-machine-using-vagrant), or if you want to just try out OpenShift [download the latest Linux server, or Windows and Mac OS X client pre-built binaries](CONTRIBUTING.adoc#download-from-github).

First, **get up and running with the** [**Contributing Guide**](CONTRIBUTING.adoc).

All contributions are welcome - OpenShift uses the Apache 2 license and does not require any contributor agreement to submit patches.  Please open issues for any bugs or problems you encounter, ask questions on the OpenShift IRC channel (#openshift-dev on freenode), or get involved in the [Kubernetes project](https://github.com/GoogleCloudPlatform/kubernetes) at the container runtime layer.

See [HACKING.md](https://github.com/openshift/origin/blob/master/HACKING.md) for more details on developing on OpenShift including how different tests are setup.

If you want to run the test suite, make sure you have your environment set up, and from the `origin` directory run:

```
# run the unit tests
$ make check

# run a simple server integration test
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

If you just want to experiment with the API without worrying about security privileges, you can disable authorization checks by running this from the host system.  This command grants full access to anyone.

    $ docker exec -it openshift-origin bash -c "openshift admin policy add-role-to-group cluster-admin system:authenticated system:unauthenticated --config=/var/lib/openshift/openshift.local.certificates/admin/.kubeconfig"

To hack on the web console, check out the [assets/README.md](assets/README.md) file for instructions on testing the console and building your changes.


#### TODO

 - Network isolation between groups of containers
