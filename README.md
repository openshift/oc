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

Docker 1.6
----------
OpenShift now requires at least Docker 1.6. Here's how to get it:

### Fedora 21
RPMs for Docker 1.6 are available for Fedora 21 in the updates yum repository.

### CentOS 7
Docker 1.6 is not yet available in the CentOS 7 Extras yum repository yet. In the meantime, you will need to install it from https://mirror.openshift.com/pub/openshift-v3/dependencies/centos7/x86_64/. Create `/etc/yum.repos.d/openshift-v3-dependencies.repo` with these contents

    [openshift-v3-dependencies]
    name=OpenShift V3 Dependencies
    baseurl=https://mirror.openshift.com/pub/openshift-v3/dependencies/centos7/x86_64/
    enabled=1
    metadata_expire=7d
    gpgcheck=0

You will now be able to `yum install` or `yum update` Docker to 1.6.


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

#### Performance Note

The current design has a long path for packets directed for the overlay network.
There are two veth-pairs, a linux bridge, and then the OpenVSwitch, that cause a drop in performance of about 40%

An optimzed solution that eliminates the long-path to just a single veth-pair bring the performance close to the wire. The performance has been measured using sockperf.

    $ docker exec -it openshift-origin bash
    $ osc --help

If you just want to experiment with the API without worrying about security privileges, you can disable authorization checks by running this from the host system.  This command grants full access to anyone.

    $ docker exec -it openshift-origin bash -c "openshift admin policy add-role-to-group cluster-admin system:authenticated system:unauthenticated --config=/var/lib/openshift/openshift.local.certificates/admin/.kubeconfig"

The optimized solution is available for use with OpenShift/Kubernetes only. Use '-kube' option with openshift-sdn on all hosts. And use the network_plugin for OpenShift/Kubernetes as 'redhat/openshift-ovs-subet'.

#### TODO

 - Network isolation between groups of containers
