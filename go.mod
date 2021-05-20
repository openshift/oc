module github.com/openshift/oc

go 1.14

require (
	github.com/AaronO/go-git-http v0.0.0-20161214145340-1d9485b3a98f
	github.com/MakeNowJust/heredoc v0.0.0-20170808103936-bb23615498cd
	github.com/RangelReale/osincli v0.0.0-20160924135400-fababb0555f2
	github.com/alexbrainman/sspi v0.0.0-20180613141037-e580b900e9f5
	github.com/alicebob/sqlittle v1.4.0
	github.com/aws/aws-sdk-go v1.35.24
	github.com/blang/semver v3.5.1+incompatible
	github.com/containers/image/v5 v5.10.2
	github.com/davecgh/go-spew v1.1.1
	github.com/docker/distribution v2.7.1+incompatible
	github.com/docker/docker v20.10.3+incompatible
	github.com/docker/go-units v0.4.0
	github.com/docker/libtrust v0.0.0-20160708172513-aabc10ec26b7
	github.com/elazarl/goproxy v0.0.0-20190911111923-ecfe977594f1
	github.com/fsnotify/fsnotify v1.4.9
	github.com/fsouza/go-dockerclient v1.7.1
	github.com/ghodss/yaml v1.0.0
	github.com/golangplus/bytes v1.0.0 // indirect
	github.com/gonum/diff v0.0.0-20181124234638-500114f11e71 // indirect
	github.com/gonum/graph v0.0.0-20170401004347-50b27dea7ebb
	github.com/gonum/integrate v0.0.0-20181209220457-a422b5c0fdf2 // indirect
	github.com/gonum/mathext v0.0.0-20181121095525-8a4bf007ea55 // indirect
	github.com/gonum/stat v0.0.0-20181125101827-41a0da705a5b // indirect
	github.com/google/go-cmp v0.5.4
	github.com/jcmturner/gokrb5/v8 v8.4.2
	github.com/magefile/mage v1.11.0 // indirect
	github.com/mitchellh/go-wordwrap v1.0.0
	github.com/moby/buildkit v0.0.0-20181107081847-c3a857e3fca0
	github.com/onsi/ginkgo v1.12.0 // indirect
	github.com/onsi/gomega v1.9.0 // indirect
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.0.2-0.20190823105129-775207bd45b6
	github.com/openshift/api v0.0.0-20210423140644-156ca80f8d83
	github.com/openshift/build-machinery-go v0.0.0-20210209125900-0da259a2c359
	github.com/openshift/client-go v0.0.0
	github.com/openshift/library-go v0.0.0-20210219155623-0260bfd7946b
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.9.0
	github.com/prometheus/common v0.17.0 // indirect
	github.com/prometheus/procfs v0.6.0 // indirect
	github.com/russross/blackfriday v1.5.2
	github.com/sirupsen/logrus v1.8.0 // indirect
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	golang.org/x/crypto v0.0.0-20210220033148-5ea612d1eb83
	golang.org/x/sys v0.0.0-20210225134936-a50acf3fe073
	golang.org/x/text v0.3.5 // indirect
	golang.org/x/time v0.0.0-20210220033141-f8bda1e9f3ba
	google.golang.org/genproto v0.0.0-20210219173056-d891e3cb3b5b // indirect
	google.golang.org/grpc v1.35.0 // indirect
	gopkg.in/ldap.v2 v2.5.1
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	k8s.io/api v0.21.0-rc.0
	k8s.io/apimachinery v0.21.0-rc.0
	k8s.io/apiserver v0.21.0-rc.0
	k8s.io/cli-runtime v0.21.0-beta.1
	k8s.io/client-go v0.21.0-rc.0
	k8s.io/component-base v0.21.0-rc.0
	k8s.io/klog/v2 v2.8.0
	k8s.io/kubectl v0.21.0-beta.1
	k8s.io/utils v0.0.0-20210305010621-2afb4311ab10
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/Microsoft/hcsshim => github.com/Microsoft/hcsshim v0.8.7
	// temporary pins to latest commit from soltysh/k8s-1.21 branches
	github.com/openshift/api => github.com/soltysh/api v0.0.0-20210329125043-97dfec49c179
	github.com/openshift/client-go => github.com/soltysh/client-go v0.0.0-20210503124028-ac0910aac9fa
	github.com/openshift/library-go => github.com/soltysh/library-go v0.0.0-20210430084706-e555322cb708

	// temporary pins to latest commit from oc-4.8-kubernetes-1.21.0-beta.1 branches
	k8s.io/apimachinery => github.com/openshift/kubernetes-apimachinery v0.0.0-20210318140035-c39220d4515a
	k8s.io/cli-runtime => github.com/openshift/kubernetes-cli-runtime v0.0.0-20210323194726-bd1440067d42
	k8s.io/client-go => github.com/openshift/kubernetes-client-go v0.0.0-20210318140334-0e99c560fb6e
	k8s.io/kubectl => github.com/openshift/kubernetes-kubectl v0.0.0-20210331134057-f85b080fa3ca
)
