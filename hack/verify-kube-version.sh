#!/usr/bin/env bash

source "$(dirname "${BASH_SOURCE}")/lib/init.sh"

KUBECTL_GO_MOD_VERSION=$(grep "k8s.io/kubectl" go.mod | sed 's/[\t]k8s.io\/kubectl v0.//')
KUBECTL_GIT_VERSION="${KUBE_GIT_VERSION//v1./}"

if [[ "${KUBECTL_GO_MOD_VERSION}" != "${KUBECTL_GIT_VERSION}" ]]; then
  os::log::warning "kubernetes version and kubectl version in go.mod must be equal, please update KUBE_GIT_VERSION"
  exit 1
fi