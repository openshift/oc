## SDN solutions for OpenShift

[![GoDoc](https://godoc.org/github.com/openshift/origin?status.png)](https://godoc.org/github.com/openshift/origin)
[![Travis](https://travis-ci.org/openshift/origin.svg?branch=master)](https://travis-ci.org/openshift/origin)
[![Jenkins](https://ci.openshift.redhat.com/jenkins/buildStatus/icon?job=devenv_ami)](https://ci.openshift.redhat.com/jenkins/job/devenv_ami/)

Currently, this doesn't run as a standalone binary, it works in conjunction with [openshift/origin](https://github.com/openshift/origin).

#### Network Architecture
High level OpenShift SDN architecture can be found [here](https://docs.openshift.org/latest/architecture/additional_concepts/sdn.html).

For more implementation details, refer to [ISOLATION.md](https://github.com/openshift/openshift-sdn/blob/master/ISOLATION.md).

#### How to Contribute
Clone openshift origin and openshift-sdn repositories:
	
	$ git clone https://github.com/openshift/origin
	$ git clone https://github.com/openshift/openshift-sdn

Make changes to openshift-sdn repository:
	
	$ cd openshift-sdn
	Patch files...
        
Run unit tests in openshift-sdn repository:

	$ cd openshift-sdn
	$ hack/test.sh

Synchronize your changes to origin repository:

	$ cd openshift-sdn
	$ hack/sync-to-origin.sh -r ../origin/

Create openshift cluster with your sdn changes:

If you have downloaded the client tools, place the included binaries in your PATH.

* For a quick install of Origin, see the [Getting Started Install guide](https://docs.openshift.org/latest/getting_started/administrators.html).
* For an advanced installation using [Ansible](https://github.com/openshift/openshift-ansible), follow the [Advanced Installation guide](https://docs.openshift.org/latest/install_config/install/advanced_install.html)
* To build and run from source, see [CONTRIBUTING.adoc](CONTRIBUTING.adoc)

Validate your changes and test cases on the openshift cluster and submit corresponding pull requests to [openshift/openshift-sdn](https://github.com/openshift/openshift-sdn) and/or [openshift/origin](https://github.com/openshift/origin) repositories.
