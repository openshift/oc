module github.com/openshift/oc

go 1.12

require (
	github.com/AaronO/go-git-http v0.0.0-20161214145340-1d9485b3a98f
	github.com/MakeNowJust/heredoc v0.0.0-20170808103936-bb23615498cd
	github.com/RangelReale/osincli v0.0.0-20160924135400-fababb0555f2
	github.com/alexbrainman/sspi v0.0.0-20180613141037-e580b900e9f5
	github.com/apcera/gssapi v0.0.0-00010101000000-000000000000
	github.com/aws/aws-sdk-go v1.17.7
	github.com/blang/semver v3.5.0+incompatible
	github.com/containerd/continuity v0.0.0-20191127005431-f65d91d395eb // indirect
	github.com/containers/image v0.0.0-00010101000000-000000000000
	github.com/containers/storage v0.0.0-20190726081758-912de200380a // indirect
	github.com/coreos/etcd v3.3.15+incompatible
	github.com/davecgh/go-spew v1.1.1
	github.com/docker/distribution v2.7.1+incompatible
	github.com/docker/docker v0.7.3-0.20190817195342-4760db040282
	github.com/docker/go-units v0.4.0
	github.com/docker/libtrust v0.0.0-20160708172513-aabc10ec26b7
	github.com/elazarl/goproxy v0.0.0-20190911111923-ecfe977594f1
	github.com/fsnotify/fsnotify v1.4.7
	github.com/fsouza/go-dockerclient v0.0.0-20171004212419-da3951ba2e9e
	github.com/ghodss/yaml v1.0.0
	github.com/gonum/diff v0.0.0-20181124234638-500114f11e71 // indirect
	github.com/gonum/graph v0.0.0-20170401004347-50b27dea7ebb
	github.com/gonum/integrate v0.0.0-20181209220457-a422b5c0fdf2 // indirect
	github.com/gonum/mathext v0.0.0-20181121095525-8a4bf007ea55 // indirect
	github.com/gonum/stat v0.0.0-20181125101827-41a0da705a5b // indirect
	github.com/gotestyourself/gotestyourself v2.2.0+incompatible // indirect
	github.com/mitchellh/go-wordwrap v1.0.0
	github.com/moby/buildkit v0.0.0-20181107081847-c3a857e3fca0
	github.com/mtrmac/gpgme v0.0.0-20170102180018-b2432428689c // indirect
	github.com/opencontainers/go-digest v1.0.0-rc1
	github.com/openshift/api v0.0.0-20191217141120-791af96035a5
	github.com/openshift/client-go v0.0.0-20191216194936-57f413491e9e
	github.com/openshift/library-go v0.0.0-20191003152030-97c62d8a2901
	github.com/operator-framework/operator-registry v1.5.4
	github.com/pkg/errors v0.8.1
	github.com/prometheus/client_golang v1.1.0
	github.com/russross/blackfriday v1.5.2
	github.com/spf13/cobra v0.0.5
	github.com/spf13/pflag v1.0.5
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	github.com/xeipuuv/gojsonschema v1.1.0 // indirect
	golang.org/x/crypto v0.0.0-20190820162420-60c769a6c586
	golang.org/x/net v0.0.0-20191004110552-13f9640d40b9
	golang.org/x/sys v0.0.0-20190826190057-c7b8b68b1456
	golang.org/x/time v0.0.0-20190308202827-9d24e82272b4
	gopkg.in/ldap.v2 v2.5.1
	k8s.io/api v0.17.0
	k8s.io/apimachinery v0.17.0
	k8s.io/apiserver v0.17.0
	k8s.io/cli-runtime v0.17.0
	k8s.io/client-go v0.17.0
	k8s.io/component-base v0.17.0
	k8s.io/klog v1.0.0
	k8s.io/kubectl v0.0.0
	k8s.io/kubernetes v0.0.0-00010101000000-000000000000
	k8s.io/utils v0.0.0-20191114184206-e782cd3c129f
	sigs.k8s.io/yaml v1.1.0
)

replace (
	bitbucket.org/ww/goautoneg => github.com/munnerz/goautoneg v0.0.0-20190414153302-2ae31c8b6b30
	github.com/apcera/gssapi => github.com/openshift/gssapi v0.0.0-20161010215902-5fb4217df13b
	github.com/aws/aws-sdk-go => github.com/aws/aws-sdk-go v0.0.0-20190125224522-81f3829f5a9d
	github.com/containers/image => github.com/openshift/containers-image v0.0.0-20190130162827-4bc6d24282b1
	github.com/docker/distribution => github.com/openshift/docker-distribution v0.0.0-20180925154709-d4c35485a70d
	github.com/docker/docker => github.com/docker/docker v0.0.0-20180612054059-a9fbbdc8dd87
	github.com/ghodss/yaml => github.com/ghodss/yaml v0.0.0-20170327235444-0ca9ea5df545
	github.com/golang/glog => github.com/openshift/golang-glog v0.0.0-20190322123450-3c92600d7533
	github.com/onsi/ginkgo => github.com/openshift/onsi-ginkgo v0.0.0-20190125161613-53ca7dc85f60
	github.com/openshift/api => github.com/openshift/api v0.0.0-20191217141120-791af96035a5
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20191216194936-57f413491e9e
	github.com/openshift/library-go => github.com/openshift/library-go v0.0.0-20191218095328-1c12909e5923
	github.com/prometheus/client_golang => github.com/prometheus/client_golang v0.0.0-20181207105117-505eaef01726
	golang.org/x/time => github.com/golang/time v0.0.0-20181108054448-85acf8d2951c
	k8s.io/api => github.com/kubernetes/api v0.0.0-20191121175643-4ed536977f46
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20191204090830-8d4ebf9010bd
	k8s.io/apimachinery => github.com/openshift/kubernetes-apimachinery v0.0.0-20191211181342-5a804e65bdc1
	k8s.io/apiserver => k8s.io/apiserver v0.0.0-20191204085032-7fb3a25c3bc4
	k8s.io/cli-runtime => github.com/openshift/kubernetes-cli-runtime v0.0.0-20191211181810-5b89652d688e
	k8s.io/client-go => github.com/openshift/kubernetes-client-go v0.0.0-20191211181558-5dcabadb2b45
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.0.0-20191121182543-b8af8c87a0d2
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.0.0-20191121182434-3459e7278621
	k8s.io/code-generator => k8s.io/code-generator v0.17.1-beta.0
	k8s.io/component-base => k8s.io/component-base v0.0.0-20191128032904-4bcd454928ff
	k8s.io/cri-api => k8s.io/cri-api v0.17.1-beta.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.0.0-20191121182650-d032a1f882e1
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.0.0-20191121180901-7ce2d4f093e4
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.0.0-20191121182328-3e5a379d6404
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.0.0-20191121182004-c1057c1a0821
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.0.0-20191121182219-f40ec664a26f
	k8s.io/kubectl => github.com/openshift/kubernetes-kubectl v0.0.0-20191216144544-eca8e8ef564d
	k8s.io/kubelet => k8s.io/kubelet v0.0.0-20191121182112-95f295975fc9
	k8s.io/kubernetes => github.com/openshift/kubernetes v1.17.0-alpha.0.0.20191216151305-079984b0a154
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.0.0-20191215115203-1896ee2ad49b
	k8s.io/metrics => k8s.io/metrics v0.0.0-20191121181631-c7d4ee0ffc0e
	k8s.io/node-api => k8s.io/node-api v0.0.0-20191121182916-ad54f283563d
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.0.0-20191121181040-36c9528858d2
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.0.0-20191121181855-541d7bb23c26
	k8s.io/sample-controller => k8s.io/sample-controller v0.0.0-20191121181305-e6c211291103
)
