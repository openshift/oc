module github.com/openshift/oc

go 1.14

require (
	github.com/AaronO/go-git-http v0.0.0-20161214145340-1d9485b3a98f
	github.com/MakeNowJust/heredoc v0.0.0-20170808103936-bb23615498cd
	github.com/RangelReale/osincli v0.0.0-20160924135400-fababb0555f2
	github.com/Shopify/logrus-bugsnag v0.0.0-20171204204709-577dee27f20d // indirect
	github.com/alexbrainman/sspi v0.0.0-20180613141037-e580b900e9f5
	github.com/alicebob/sqlittle v1.4.0
	github.com/apcera/gssapi v0.0.0-00010101000000-000000000000
	github.com/aws/aws-sdk-go v1.28.2
	github.com/bitly/go-simplejson v0.5.0 // indirect
	github.com/blang/semver v3.5.1+incompatible
	github.com/bshuster-repo/logrus-logstash-hook v0.4.1 // indirect
	github.com/bugsnag/bugsnag-go v1.5.3 // indirect
	github.com/bugsnag/panicwrap v1.2.0 // indirect
	github.com/containers/image v0.0.0-00010101000000-000000000000
	github.com/containers/storage v0.0.0-20190726081758-912de200380a // indirect
	github.com/davecgh/go-spew v1.1.1
	github.com/docker/distribution v2.7.1+incompatible
	github.com/docker/docker v1.4.2-0.20200309214505-aa6a9891b09c
	github.com/docker/go-units v0.4.0
	github.com/docker/libtrust v0.0.0-20160708172513-aabc10ec26b7
	github.com/elazarl/goproxy v0.0.0-20190911111923-ecfe977594f1
	github.com/fsnotify/fsnotify v1.4.9
	github.com/fsouza/go-dockerclient v0.0.0-20171004212419-da3951ba2e9e
	github.com/garyburd/redigo v1.6.0 // indirect
	github.com/ghodss/yaml v1.0.0
	github.com/gofrs/uuid v3.2.0+incompatible // indirect
	github.com/gonum/diff v0.0.0-20181124234638-500114f11e71 // indirect
	github.com/gonum/graph v0.0.0-20170401004347-50b27dea7ebb
	github.com/gonum/integrate v0.0.0-20181209220457-a422b5c0fdf2 // indirect
	github.com/gonum/mathext v0.0.0-20181121095525-8a4bf007ea55 // indirect
	github.com/gonum/stat v0.0.0-20181125101827-41a0da705a5b // indirect
	github.com/google/go-cmp v0.4.0
	github.com/gorilla/handlers v1.4.2 // indirect
	github.com/mitchellh/go-wordwrap v1.0.0
	github.com/moby/buildkit v0.0.0-20181107081847-c3a857e3fca0
	github.com/mtrmac/gpgme v0.1.2 // indirect
	github.com/opencontainers/go-digest v1.0.0-rc1
	github.com/opencontainers/image-spec v1.0.2-0.20190823105129-775207bd45b6
	github.com/openshift/api v0.0.0-20201019163320-c6a5ec25f267
	github.com/openshift/build-machinery-go v0.0.0-20200917070002-f171684f77ab
	github.com/openshift/client-go v0.0.0-20201020074620-f8fd44879f7c
	github.com/openshift/library-go v0.0.0-20201022113156-a4ff9e1d2900
	github.com/operator-framework/operator-registry v1.8.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.7.1
	github.com/russross/blackfriday v1.5.2
	github.com/spf13/cobra v1.0.0
	github.com/spf13/pflag v1.0.5
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/xeipuuv/gojsonschema v1.1.0 // indirect
	github.com/yvasiyarov/go-metrics v0.0.0-20150112132944-c25f46c4b940 // indirect
	github.com/yvasiyarov/gorelic v0.0.7 // indirect
	github.com/yvasiyarov/newrelic_platform_go v0.0.0-20160601141957-9c099fbc30e9 // indirect
	go.etcd.io/etcd v0.5.0-alpha.5.0.20200819165624-17cef6e3e9d5
	golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9
	golang.org/x/net v0.0.0-20200707034311-ab3426394381
	golang.org/x/sys v0.0.0-20200622214017-ed371f2e16b4
	golang.org/x/time v0.0.0-20191024005414-555d28b269f0
	gopkg.in/ldap.v2 v2.5.1
	k8s.io/api v0.20.0-beta.2
	k8s.io/apimachinery v0.20.0-beta.2
	k8s.io/apiserver v0.20.0-beta.2
	k8s.io/cli-runtime v0.20.0-beta.2
	k8s.io/client-go v0.20.0-beta.2
	k8s.io/component-base v0.20.0-beta.2
	k8s.io/klog/v2 v2.3.0
	k8s.io/kubectl v0.20.0-beta.2
	k8s.io/kubernetes v0.20.0-beta.2
	k8s.io/utils v0.0.0-20200729134348-d5654de09c73
	rsc.io/letsencrypt v0.0.3 // indirect
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/Microsoft/hcsshim => github.com/Microsoft/hcsshim v0.8.7
	github.com/apcera/gssapi => github.com/openshift/gssapi v0.0.0-20161010215902-5fb4217df13b
	github.com/containers/image => github.com/openshift/containers-image v0.0.0-20190130162819-76de87591e9d
	// Taking changes from https://github.com/moby/moby/pull/40021 to accomodate new version of golang.org/x/sys.
	// Although the PR lists c3a0a3744636069f43197eb18245aaae89f568e5 as the commit with the fixes,
	// d1d5f6476656c6aad457e2a91d3436e66b6f2251 is more suitable since it does not break fsouza/go-clientdocker,
	// yet provides the same fix.
	github.com/docker/docker => github.com/docker/docker v1.4.2-0.20191121165722-d1d5f6476656

	// Temporary prebase beta.2 pins
	github.com/openshift/api => github.com/openshift/api prebase-1.20.0-beta.2
	github.com/openshift/client-go => github.com/openshift/client-go prebase-1.20.0-beta.2
	github.com/openshift/library-go => github.com/openshift/library-go prebase-1.20.0-beta.2

	k8s.io/api => k8s.io/api v0.20.0-beta.2
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.20.0-beta.2
	k8s.io/apimachinery => github.com/openshift/kubernetes-apimachinery oc-4.7-kubernetes-1.20.0-beta.2
	k8s.io/apiserver => k8s.io/apiserver v0.20.0-beta.2
	k8s.io/cli-runtime => github.com/openshift/kubernetes-cli-runtime oc-4.7-kubernetes-1.20.0-beta.2
	k8s.io/client-go => github.com/openshift/kubernetes-client-go oc-4.7-kubernetes-1.20.0-beta.2
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.20.0-beta.2
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.20.0-beta.2
	k8s.io/code-generator => k8s.io/code-generator v0.20.0-beta.2
	k8s.io/component-base => k8s.io/component-base v0.20.0-beta.2
	k8s.io/component-helpers => k8s.io/component-helpers v0.20.0-beta.2
	k8s.io/controller-manager => k8s.io/controller-manager v0.20.0-beta.2
	k8s.io/cri-api => k8s.io/cri-api v0.20.0-beta.2
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.20.0-beta.2
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.20.0-beta.2
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.20.0-beta.2
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.20.0-beta.2
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.20.0-beta.2
	k8s.io/kubectl => github.com/openshift/kubernetes-kubectl oc-4.7-kubernetes-1.20.0-beta.2
	k8s.io/kubelet => k8s.io/kubelet v0.20.0-beta.2
	k8s.io/kubernetes => github.com/openshift/kubernetes oc-4.7-kubernetes-1.20.0-beta.2
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.20.0-beta.2
	k8s.io/metrics => k8s.io/metrics v0.20.0-beta.2
	k8s.io/mount-utils => k8s.io/mount-utils v0.20.0-beta.2
	k8s.io/node-api => k8s.io/node-api v0.20.0-beta.2
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.20.0-beta.2
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.20.0-beta.2
	k8s.io/sample-controller => k8s.io/sample-controller v0.20.0-beta.2
)
