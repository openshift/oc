#debuginfo not supported with Go
%global debug_package %{nil}
# modifying the Go binaries breaks the DWARF debugging
%global __os_install_post %{_rpmconfigdir}/brp-compress

%global gopath      %{_datadir}/gocode
%global import_path github.com/openshift/oc

%global golang_version 1.12
# %commit and %os_git_vars are intended to be set by tito custom builders provided
# in the .tito/lib directory. The values in this spec file will not be kept up to date.
%{!?commit:
%global commit 86b5e46426ba828f49195af21c56f7c6674b48f7
}
%global shortcommit %(c=%{commit}; echo ${c:0:7})
# os_git_vars needed to run hack scripts during rpm builds
%{!?os_git_vars:
%global os_git_vars OS_GIT_VERSION='' OS_GIT_COMMIT='' OS_GIT_MAJOR='' OS_GIT_MINOR='' OS_GIT_TREE_STATE=''
}

%global product_name OpenShift

%{!?version: %global version 0.0.1}
%{!?release: %global release 1}

Name:           openshift-clients
Version:        %{version}
Release:        %{release}%{dist}
Summary:        OpenShift client binaries
License:        ASL 2.0
URL:            https://%{import_path}

ExclusiveArch:  %{go_arches}

Source0:        https://%{import_path}/archive/%{commit}/%{name}-%{version}.tar.gz
#BuildRequires:  bsdtar
BuildRequires:  golang >= %{golang_version}
BuildRequires:  krb5-devel
BuildRequires:  rsync

Provides:       atomic-openshift-clients
Obsoletes:      atomic-openshift-clients
Requires:       bash-completion

# The following Bundled Provides entries are populated automatically by the
# OpenShift tito custom builder found here:
#   https://github.com/openshift/oc/blob/master/.tito/lib/oc/builder/
#
# These are defined as per:
# https://fedoraproject.org/wiki/Packaging:Guidelines#Bundling_and_Duplication_of_system_libraries
#
### AUTO-BUNDLED-GEN-ENTRY-POINT

%description
%{summary}

%package redistributable
Summary:        OpenShift Client binaries for Linux, Mac OSX, and Windows
Provides:       atomic-openshift-clients-redistributable
Obsoletes:      atomic-openshift-clients-redistributable
#BuildRequires:  goversioninfo

%description redistributable
%{summary}

%prep
%setup -q

%build
%ifarch x86_64
  # Create Binaries for all supported arches
  %{os_git_vars} OS_BUILD_RELEASE_ARCHIVES=n make build-cross
%else
  %ifarch %{ix86}
    BUILD_PLATFORM="linux/386"
  %endif
  %ifarch ppc64le
    BUILD_PLATFORM="linux/ppc64le"
  %endif
  %ifarch %{arm} aarch64
    BUILD_PLATFORM="linux/arm64"
  %endif
  %ifarch s390x
    BUILD_PLATFORM="linux/s390x"
  %endif
  %{os_git_vars} OS_BUILD_RELEASE_ARCHIVES=n make build
%endif

%install
PLATFORM="$(go env GOHOSTOS)_$(go env GOHOSTARCH)"
install -d %{buildroot}%{_bindir}

# Install for the local platform
install -p -m 755 _output/local/go/bin/oc %{buildroot}%{_bindir}/oc

%ifarch x86_64
# Install client executable for windows and mac
install -d %{buildroot}%{_datadir}/%{name}/{linux,macosx,windows}
install -p -m 755 _output/local/go/bin/oc %{buildroot}%{_datadir}/%{name}/linux/oc
install -p -m 755 _output/local/go/bin/darwin_amd64/oc %{buildroot}/%{_datadir}/%{name}/macosx/oc
install -p -m 755 _output/local/go/bin/windows_amd64/oc.exe %{buildroot}/%{_datadir}/%{name}/windows/oc.exe
%endif

ln -s oc %{buildroot}%{_bindir}/kubectl

# Install man1 man pages
install -d -m 0755 %{buildroot}%{_mandir}/man1
_output/local/go/bin/genman %{buildroot}%{_mandir}/man1 oc

# Install bash completions
install -d -m 755 %{buildroot}%{_sysconfdir}/bash_completion.d/
for bin in oc #kubectl
do
  echo "+++ INSTALLING BASH COMPLETIONS FOR ${bin} "
  %{buildroot}%{_bindir}/${bin} completion bash > %{buildroot}%{_sysconfdir}/bash_completion.d/${bin}
  chmod 644 %{buildroot}%{_sysconfdir}/bash_completion.d/${bin}
done

%files
%license LICENSE
%{_bindir}/oc
%{_bindir}/kubectl
%{_sysconfdir}/bash_completion.d/oc
#%{_sysconfdir}/bash_completion.d/kubectl
%{_mandir}/man1/oc*

%ifarch x86_64
%files redistributable
%license LICENSE
%dir %{_datadir}/%{name}/linux/
%dir %{_datadir}/%{name}/macosx/
%dir %{_datadir}/%{name}/windows/
%{_datadir}/%{name}/linux/oc
#%{_datadir}/%{name}/linux/kubectl
%{_datadir}/%{name}/macosx/oc
#%{_datadir}/%{name}/macosx/kubectl
%{_datadir}/%{name}/windows/oc.exe
#%{_datadir}/%{name}/windows/kubectl.exe
%endif

%changelog
