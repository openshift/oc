all: build
.PHONY: all

GO_LD_EXTRAFLAGS :=-X github.com/openshift/oc/vendor/k8s.io/kubernetes/pkg/version.gitMajor="1" \
                   -X github.com/openshift/oc/vendor/k8s.io/kubernetes/pkg/version.gitMinor="14" \
                   -X github.com/openshift/oc/vendor/k8s.io/kubernetes/pkg/version.gitVersion="v1.14.0+724e12f93f" \
                   -X github.com/openshift/oc/vendor/k8s.io/kubernetes/pkg/version.gitCommit="$(SOURCE_GIT_COMMIT)" \
                   -X github.com/openshift/oc/vendor/k8s.io/kubernetes/pkg/version.buildDate="$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')" \
                   -X github.com/openshift/oc/vendor/k8s.io/kubernetes/pkg/version.gitTreeState="clean"


# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/library-go/alpha-build-machinery/make/, \
	golang.mk \
	targets/openshift/deps.mk \
	targets/openshift/images.mk \
	targets/openshift/rpm.mk \
)

GO_BUILD_PACKAGES :=$(strip \
	./cmd/... \
	$(wildcard ./tools/*) \
)
# These tags make sure we can statically link and avoid shared dependencies
GO_BUILD_FLAGS :=-tags 'include_gcs include_oss containers_image_openpgp no_openssl gssapi'
GO_BUILD_FLAGS_DARWIN :=-tags 'include_gcs include_oss containers_image_openpgp no_openssl'
GO_BUILD_FLAGS_WINDOWS :=-tags 'include_gcs include_oss containers_image_openpgp no_openssl'

OUTPUT_DIR :=_output
CROSS_BUILD_BINDIR :=$(OUTPUT_DIR)/bin
RPM_VERSION :=$(shell set -o pipefail && echo '$(SOURCE_GIT_TAG)' | sed -E 's/v([0-9]+\.[0-9]+\.[0-9]+)-.*/\1/')
RPM_EXTRAFLAGS := \
	--define 'version $(RPM_VERSION)' \
	--define 'dist .el7' \
	--define 'release 1'

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - Dockerfile path
# $3 - context directory for image build
$(call build-image,ose-cli,./images/cli/Dockerfile.rhel,.)

$(call build-image,ose-deployer,./images/deployer/Dockerfile.rhel,.)
image-ose-deployer: image-ose-cli

$(call build-image,ose-recycler,./images/recycler/Dockerfile.rhel,.)
image-ose-recycler: image-ose-cli

update: update-generated-completions
.PHONY: update

verify: verify-cli-conventions verify-generated-completions
.PHONY: verify

verify-cli-conventions:
	go run ./tools/clicheck
.PHONY: verify-cli-conventions

update-generated-completions: build
	hack/update-generated-completions.sh
.PHONY: update-generated-completions

verify-generated-completions: build
	hack/verify-generated-completions.sh
.PHONY: verify-generated-completions


cross-build-darwin-amd64:
	+@GOOS=darwin GOARCH=amd64 $(MAKE) --no-print-directory build GO_BUILD_PACKAGES:=./cmd/oc GO_BUILD_FLAGS:="$(GO_BUILD_FLAGS_DARWIN)" GO_BUILD_BINDIR:=$(CROSS_BUILD_BINDIR)/darwin_amd64
.PHONY: cross-build-darwin-amd64

cross-build-windows-amd64:
	+@GOOS=windows GOARCH=amd64 $(MAKE) --no-print-directory build GO_BUILD_PACKAGES:=./cmd/oc GO_BUILD_FLAGS:="$(GO_BUILD_FLAGS_WINDOWS)" GO_BUILD_BINDIR:=$(CROSS_BUILD_BINDIR)/windows_amd64
.PHONY: cross-build-windows-amd64

cross-build: cross-build-darwin-amd64 cross-build-windows-amd64
.PHONY: cross-build

clean-cross-build:
	$(RM) -r '$(CROSS_BUILD_BINDIR)'
	if [ -d '$(OUTPUT_DIR)' ]; then rmdir --ignore-fail-on-non-empty '$(OUTPUT_DIR)'; fi
.PHONY: clean-cross-build

clean: clean-cross-build
