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

#### Try it out

##### Use vagrant, pre-define a cluster, and bring it up

Create an openshift cluster on your desktop using vagrant:

	$ git clone https://github.com/openshift/origin
	$ cd origin
	$ make clean
	$ export OPENSHIFT_DEV_CLUSTER=1
	$ export OPENSHIFT_NUM_MINIONS=2
	$ export OPENSHIFT_SDN=ovs-simple
	$ vagrant up

##### Manually add minions to a master

Steps to create manually create an OpenShift cluster with openshift-sdn. This requires that each machine (master, minions) have compiled `openshift` and `openshift-sdn` already. Check [here](https://github.com/openshift/origin) for OpenShift instructions. Ensure 'openvswitch' is installed and running (`yum install -y openvswitch && systemctl enable openvswitch && systemctl start openvswitch`). Also verify that the `DOCKER_OPTIONS` variable is unset in your environment, or set to a known-working value (e.g. `DOCKER_OPTIONS='-b=lbr0 --mtu=1450 --selinux-enabled'`). If you don't know what to put there, it's probably best to leave it unset. :)

Security Warning!!!
-------------------
OpenShift is a system that runs Docker containers on your machine.  In some cases (build operations and the registry service) it does so using privileged containers.  Those containers access your host's Docker daemon and perform `docker build` and `docker push` operations.  As such, you should be aware of the inherent security risks associated with performing `docker run` operations on arbitrary images as they have effective root access.  This is particularly relevant when running the OpenShift as a node directly on your laptop or primary workstation.  Only run code you trust.

	$ openshift start master [--nodes=node1]  # start the master openshift server (also starts the etcd server by default) with an optional list of nodes
	$ openshift-sdn           # assumes etcd is running at localhost:4001

To add a node to the cluster, do the following on the node:

	$ openshift-sdn -etcd-endpoints=http://openshift-master:4001 -minion -public-ip=<10.10....> -hostname <hostname>
	where, 
		-etcd-endpoints	: reach the etcd db here
		-minion 	: run it in minion mode (will watch etcd servers for new minion subnets)
		-public-ip	: use this field for suggesting the publicly reachable IP address of this minion
		-hostname	: the name that will be used to register the minion with openshift-master
	$ openshift start node --master=https://openshift-master:8443

Back on the master, to finally register the node:

	Create a json file for the new minion resource
        $ cat <<EOF > minion-1.json
	{
		"kind":"Minion", 
		"id":"openshift-minion-1",
	 	"apiVersion":"v1beta1"
	}
	EOF
	where, openshift-minion-1 is a hostname that is resolvable from the master (or, create an entry in /etc/hosts and point it to the public-ip of the minion).
	$ openshift cli create -f minion-1.json

### Fedora 21
RPMs for Docker 1.6 are available for Fedora 21 in the updates yum repository.

### CentOS 7
RPMs for Docker 1.6 are available for CentOS 7 in the extras yum repository.


Getting Started
---------------
The simplest way to run OpenShift Origin is in a Docker container:

    $ sudo docker run -d --name "openshift-origin" --net=host --privileged \
        -v /var/run/docker.sock:/var/run/docker.sock \
        openshift/origin start


##### OpenShift? PaaS? Can I have a 'plain setup' just for Docker?

    $ sudo docker exec -it openshift-origin bash
    $ osc --help

If you just want to experiment with the API without worrying about security privileges, you can disable authorization checks by running this from the host system.  This command grants full access to anyone.

    $ sudo docker exec -it openshift-origin bash -c "osadm policy add-cluster-role-to-group cluster-admin system:authenticated system:unauthenticated --config=/var/lib/openshift/openshift.local.config/master/admin.kubeconfig"


### Start Developing

You can develop [locally on your host](CONTRIBUTING.adoc#develop-locally-on-your-host) or with a [virtual machine](CONTRIBUTING.adoc#develop-on-virtual-machine-using-vagrant), or if you want to just try out OpenShift [download the latest Linux server, or Windows and Mac OS X client pre-built binaries](CONTRIBUTING.adoc#download-from-github).

First, **get up and running with the** [**Contributing Guide**](CONTRIBUTING.adoc).

Once setup with a Go development environment and Docker, you can:

1.  Build the source code

        $ make clean build

2.  Start the OpenShift server

        $ make run

3.  In another terminal window, switch to the directory and start an app:

        $ cd $GOPATH/src/github.com/openshift/origin
        $ export OPENSHIFTCONFIG=`pwd`/openshift.local.config/master/admin.kubeconfig 
        $ _output/local/go/bin/osc create -f examples/hello-openshift/hello-pod.json

In your browser, go to [http://localhost:6061](http://localhost:6061) and you should see 'Welcome to OpenShift'.


### What's Just Happened?

The example above starts the ['openshift/hello-openshift' Docker image](https://github.com/openshift/origin/blob/266ce4cfc8785b0633c24a46cd0aff927b8e90b2/examples/hello-openshift/hello-pod.json#L7) inside a Docker container, but managed by OpenShift and Kubernetes.

* At the Docker level, that image [listens on port 8080](https://github.com/openshift/origin/blob/266ce4cfc8785b0633c24a46cd0aff927b8e90b2/examples/hello-openshift/hello_openshift.go#L16) within a container and [prints out a simple 'Hello OpenShift' message on access](https://github.com/openshift/origin/blob/266ce4cfc8785b0633c24a46cd0aff927b8e90b2/examples/hello-openshift/hello_openshift.go#L9).
* At the Kubernetes level, we [map that bound port in the container](https://github.com/openshift/origin/blob/266ce4cfc8785b0633c24a46cd0aff927b8e90b2/examples/hello-openshift/hello-pod.json#L11) [to port 6061 on the host](https://github.com/openshift/origin/blob/266ce4cfc8785b0633c24a46cd0aff927b8e90b2/examples/hello-openshift/hello-pod.json#L12) so that we can access it via the host browser.
* When you created the container, Kubernetes decided which host to place the container on by looking at the available hosts and selecting one with available space.  The agent that runs on each node (part of the OpenShift all-in-one binary, called the Kubelet) saw that it was now supposed to run the container and instructed Docker to start the container.

OpenShift brings all of these pieces (and the client) together in a single, easy to use binary.  The following examples show the other OpenShift specific features that live above the Kubernetes runtime like image building and deployment flows.


### Next Steps

We highly recommend trying out the [OpenShift walkthrough](https://github.com/openshift/origin/blob/master/examples/sample-app/README.md), which shows some of the lower level pieces of of OpenShift that will be the foundation for user applications.  The walkthrough is accompanied by a blog series on [blog.openshift.com](https://blog.openshift.com/openshift-v3-deep-dive-docker-kubernetes/) that goes into more detail.  It's a great place to start, albeit at a lower level than OpenShift 2.

Both OpenShift and Kubernetes have a strong focus on documentation - see the following for more information about them:

* [OpenShift Documentation](http://docs.openshift.org/latest/welcome/index.html)
* [Kubernetes Getting Started](https://github.com/GoogleCloudPlatform/kubernetes/blob/master/README.md)
* [Kubernetes Documentation](https://github.com/GoogleCloudPlatform/kubernetes/blob/master/docs/README.md)

You can see some other examples of using Kubernetes at a lower level - stay tuned for more high level OpenShift examples as well:

* [Kubernetes walkthrough](https://github.com/GoogleCloudPlatform/kubernetes/tree/master/examples/walkthrough)
* [Kubernetes guestbook](https://github.com/GoogleCloudPlatform/kubernetes/tree/master/examples/guestbook)

Steps:

1. Run etcd somewhere, and run the openshift-sdn master to watch it in sync mode. 

		$ systemctl start etcd
		$ openshift-sdn -master -sync  # use -etcd-endpoints=http://target:4001 if etcd is not running locally

2. To add a node, make sure the 'hostname/dns' is reachable from the machine that is running 'openshift-sdn master'. Then start the openshift-sdn in minion mode with sync flag.

		$ openshift-sdn -minion -sync -etcd-endpoints=http://master-host:4001 -hostname=minion-1-dns -public-ip=<public ip that the hostname resolves to>

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

The OpenShift APIs are exposed at `https://localhost:8443/osapi/v1beta1/*`.

* Builds
 * `https://localhost:8443/osapi/v1beta1/builds`
 * `https://localhost:8443/osapi/v1beta1/buildConfigs`
 * `https://localhost:8443/osapi/v1beta1/buildLogs`
 * `https://localhost:8443/osapi/v1beta1/buildConfigHooks`
* Deployments
 * `https://localhost:8443/osapi/v1beta1/deployments`
 * `https://localhost:8443/osapi/v1beta1/deploymentConfigs`
* Images
 * `https://localhost:8443/osapi/v1beta1/images`
 * `https://localhost:8443/osapi/v1beta1/imageRepositories`
 * `https://localhost:8443/osapi/v1beta1/imageRepositoryMappings`
* Templates
 * `https://localhost:8443/osapi/v1beta1/templateConfigs`
* Routes
 * `https://localhost:8443/osapi/v1beta1/routes`
* Projects
 * `https://localhost:8443/osapi/v1beta1/projects`
* Users
 * `https://localhost:8443/osapi/v1beta1/users`
 * `https://localhost:8443/osapi/v1beta1/userIdentityMappings`
* OAuth
 * `https://localhost:8443/osapi/v1beta1/accessTokens`
 * `https://localhost:8443/osapi/v1beta1/authorizeTokens`
 * `https://localhost:8443/osapi/v1beta1/clients`
 * `https://localhost:8443/osapi/v1beta1/clientAuthorizations`

The Kubernetes APIs are exposed at `https://localhost:8443/api/v1beta1/*`:

* `https://localhost:8443/api/v1beta1/pods`
* `https://localhost:8443/api/v1beta1/services`
* `https://localhost:8443/api/v1beta1/replicationControllers`
* `https://localhost:8443/api/v1beta1/operations`

OpenShift and Kubernetes integrate with the [Swagger 2.0 API framework](http://swagger.io) which aims to make it easier to document and write clients for RESTful APIs.  When you start OpenShift, the Swagger API endpoint is exposed at `https://localhost:8443/swaggerapi`. The Swagger UI makes it easy to view your documentation - to view the docs for your local version of OpenShift start the server with CORS enabled:

    $ openshift start --cors-allowed-origins=.*

and then browse to http://openshift3swagger-claytondev.rhcloud.com (which runs a copy of the Swagger UI that points to localhost:8080 by default).  Expand the operations available on v1beta1 to see the schemas (and to try the API directly).

Web Console
-----------

The OpenShift API server also hosts a web console. You can try it out at [https://localhost:8443/console](https://localhost:8443/console).

For more information on the console [checkout the README](assets/README.md) and the [docs](http://docs.openshift.org/latest/dev_guide/console.html).

![Web console overview](docs/screenshots/console_overview.png?raw=true)

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

    OpenShift is designed to run any existing Docker images.  In addition you can define builds that will produce new Docker images from a Dockerfile.  However the real magic of OpenShift can be seen when using [Source-To-Image](https://github.com/openshift/source-to-image)(STI) builds which allow you to simply supply an application source repository which will be combined with an existing STI-enabled Docker image to produce a new runnable image that runs your application.  We are continuing to grow the ecosystem of STI-enabled images and documenting them [here](https://ci.openshift.redhat.com/openshift-docs-master-testing/latest/openshift_sti_images/overview.html).  We also have a few more experimental images available:

    * [Wildfly](https://github.com/openshift/wildfly-8-centos)

Contributing
------------

All contributions are welcome - OpenShift uses the Apache 2 license and does not require any contributor agreement to submit patches.  Please open issues for any bugs or problems you encounter, ask questions on the OpenShift IRC channel (#openshift-dev on freenode), or get involved in the [Kubernetes project](https://github.com/GoogleCloudPlatform/kubernetes) at the container runtime layer.

See [HACKING.md](https://github.com/openshift/origin/blob/master/HACKING.md) for more details on developing on OpenShift including how different tests are setup.

If you want to run the test suite, make sure you have your environment from above set up, and from the origin directory run:

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

The optimized solution is available for use with OpenShift/Kubernetes only. Use '-kube' option with openshift-sdn on all hosts. And use the network_plugin for OpenShift/Kubernetes as 'redhat/openshift-ovs-subet'.

#### TODO

 - Network isolation between groups of containers
