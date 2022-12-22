#!/bin/sh
"${OC:-oc}" adm release extract --file=image-references quay.io/openshift-release-dev/ocp-release:4.11.0-x86_64
