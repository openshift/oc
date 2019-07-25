#!/usr/bin/env bash

# This script generates release zips and RPMs into _output/releases.
# tito and other build dependencies are required on the host. We will
# be running `hack/build-cross.sh` under the covers, so we transitively
# consume all of the relevant envars.
source "$(dirname "${BASH_SOURCE}")/lib/init.sh"

function cleanup() {
        return_code=$?
        os::util::describe_return_code "${return_code}"
        exit "${return_code}"
}
trap "cleanup" EXIT

os::build::setup_env

function build() {
	go install -tags "${GO_TAGS}" -ldflags "${GO_LD_EXTRAFLAGS}" github.com/openshift/oc/cmd/oc
	go install -tags "${GO_TAGS}" -ldflags "${GO_LD_EXTRAFLAGS}" github.com/openshift/oc/tools/genman
}

GO_LD_EXTRAFLAGS="-X github.com/openshift/oc/vendor/k8s.io/kubernetes/pkg/version.gitMajor='1' \
                   -X github.com/openshift/oc/vendor/k8s.io/kubernetes/pkg/version.gitMinor='14' \
                   -X github.com/openshift/oc/vendor/k8s.io/kubernetes/pkg/version.gitVersion='v1.14.0+724e12f93f' \
                   -X github.com/openshift/oc/vendor/k8s.io/kubernetes/pkg/version.gitCommit=$(git rev-parse --short "HEAD^{commit}" 2>/dev/null) \
                   -X github.com/openshift/oc/vendor/k8s.io/kubernetes/pkg/version.buildDate=$(date -u +'%Y-%m-%dT%H:%M:%SZ') \
                   -X github.com/openshift/oc/vendor/k8s.io/kubernetes/pkg/version.gitTreeState=clean"

export GOARCH=amd64
GO_TAGS="include_gcs include_oss containers_image_openpgp gssapi"
build
export GOOS=linux; build
GO_TAGS=""
export GOOS=darwin; build
export GOOS=windows; build


