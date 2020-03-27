FROM registry.svc.ci.openshift.org/ocp/4.5:cli
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
  netsniff-ng \
  nmap-ncat \
  parted \
  pciutils \
  psmisc \
  perf \
  screen \
  strace \
  sysstat \
  sysvinit-tools \
  tcpdump \
  util-linux \
  vim-enhanced \
  wget \
  xfsprogs \
  " && \
  yum -y install $INSTALL_PKGS && rpm -V --nosize --nofiledigest --nomtime --nomode $INSTALL_PKGS && yum clean all && rm -rf /var/cache/*
  # Disabled until they are buildable on s390x
  # numactl \
  # numactl-devel \

CMD ["/usr/bin/bash"]
LABEL io.k8s.display-name="OpenShift Tools" \
      io.k8s.description="Contains debugging and diagnostic tools for use with an OpenShift cluster." \
      io.openshift.tags="openshift,tools"
