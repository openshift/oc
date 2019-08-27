#debuginfo not supported with Go
%global debug_package %{nil}
# modifying the Go binaries breaks the DWARF debugging
%global __os_install_post %{_rpmconfigdir}/brp-compress

%global gopath      %{_datadir}/gocode
%global import_path github.com/openshift/oc

%global golang_version 1.12
%global product_name OpenShift
%global package_name openshift-clients

%{!?version: %global version 0.0.1}
%{!?release: %global release 1}

# %commit and %source_git_vars are intended to be set by tito custom builders provided
# in downstream builds. The values in this spec file will not be kept up to date.
%{!?commit:
%global commit 1f14ed7363468094301bbe495bcbed5f087f9823
}
# os_git_vars needed to run hack scripts during rpm builds
%{!?os_git_vars:
%global os_git_vars OS_GIT_VERSION='' OS_GIT_COMMIT='' OS_GIT_MAJOR='' OS_GIT_MINOR='' OS_GIT_TREE_STATE=''
}
# source_git_vars needed to run hack scripts during rpm builds
%{!?source_git_vars:
%global source_git_vars SOURCE_GIT_VERSION='' SOURCE_GIT_COMMIT=''
}

Name:           openshift-clients
Version:        %{version}
Release:        %{release}%{dist}
Summary:        OpenShift client binaries
License:        ASL 2.0
URL:            https://%{import_path}

# If go_arches not defined fall through to implicit golang archs
%if 0%{?go_arches:1}
ExclusiveArch:  %{go_arches}
%else
ExclusiveArch:  x86_64 aarch64 ppc64le s390x
%endif

Source0:        https://%{import_path}/archive/%{commit}/%{name}-%{version}.tar.gz
#BuildRequires:  bsdtar
BuildRequires:  golang >= %{golang_version}
BuildRequires:  krb5-devel
BuildRequires:  rsync

Provides:       atomic-openshift-clients
Obsoletes:      atomic-openshift-clients
Requires:       bash-completion

%description
%{summary}

%package redistributable
Summary:        OpenShift Client binaries for Linux, Mac OSX, and Windows
Provides:       atomic-openshift-clients-redistributable
Obsoletes:      atomic-openshift-clients-redistributable

%description redistributable
%{summary}

%prep

%setup -q

%build
%ifarch x86_64
  # Create Binaries for all supported arches
  %{os_git_vars} make build cross-build
%else
  %ifarch %{ix86}
    GOOS=linux
    GOARCH=386
  %endif
  %ifarch ppc64le
    GOOS=linux
    GOARCH=ppc64le
  %endif
  %ifarch %{arm} aarch64
    GOOS=linux
    GOARCH=arm64
  %endif
  %ifarch s390x
    GOOS=linux
    GOARCH=s390x
  %endif
  %{source_git_vars} make build
%endif

%install
install -d %{buildroot}%{_bindir}

 # Install for the local platform
install -p -m 755 oc %{buildroot}%{_bindir}/oc

%ifarch x86_64
# Install client executable for windows and mac
install -d %{buildroot}%{_datadir}/%{name}/{linux,macosx,windows}
install -p -m 755 ./oc %{buildroot}%{_datadir}/%{name}/linux/oc
install -p -m 755 ./_output/bin/darwin_amd64/oc %{buildroot}/%{_datadir}/%{name}/macosx/oc
install -p -m 755 ./_output/bin/windows_amd64/oc.exe %{buildroot}/%{_datadir}/%{name}/windows/oc.exe
%endif

ln -s ./oc %{buildroot}%{_bindir}/kubectl

# Install man1 man pages
install -d -m 0755 %{buildroot}%{_mandir}/man1
./genman %{buildroot}%{_mandir}/man1 oc

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
