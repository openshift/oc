FROM registry.ci.openshift.org/ocp/builder:rhel-8-golang-1.21-openshift-4.16 AS builder
WORKDIR /go/src/github.com/openshift/oc
COPY . .
RUN make build --warn-undefined-variables

FROM registry.ci.openshift.org/ocp/4.16:base
COPY --from=builder /go/src/github.com/openshift/oc/oc /usr/bin/
RUN for i in kubectl openshift-deploy openshift-docker-build openshift-sti-build openshift-git-clone openshift-manage-dockerfile openshift-extract-image-content openshift-recycle; do ln -sf /usr/bin/oc /usr/bin/$i; done

RUN INSTALL_PKGS="\
  bash-completion \
  bc \
  bind-utils \
  blktrace \
  crash \
  e2fsprogs \
  ethtool \
  file \
  git \
  glibc-utils \
  gzip \
  hwloc \
  iotop \
  iproute \
  iputils \
  jq \
  less \
  ltrace \
  mailx \
  net-tools \
  nmap-ncat \
  parted \
  pciutils \
  procps-ng \
  psmisc \
  perf \
  sos \
  strace \
  sysstat \
  tcpdump \
  tmux \
  util-linux \
  vim-enhanced \
  wget \
  xfsprogs \
  fio \
  stress-ng \
  python36 \
  " && \
  yum -y install $INSTALL_PKGS && rpm -V --nosize --nofiledigest --nomtime --nomode $INSTALL_PKGS && yum clean all && rm -rf /var/cache/*
  # Disabled until they are buildable on s390x
  # numactl \
  # numactl-devel \

CMD ["/usr/bin/bash"]
LABEL io.k8s.display-name="OpenShift Tools" \
      io.k8s.description="Contains debugging and diagnostic tools for use with an OpenShift cluster." \
      io.openshift.tags="openshift,tools"
