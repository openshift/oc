module github.com/openshift/oc

go 1.12

require (
	github.com/AaronO/go-git-http v0.0.0-20161214145340-1d9485b3a98f
	github.com/MakeNowJust/heredoc v0.0.0-20170808103936-bb23615498cd
	github.com/Nvveen/Gotty v0.0.0-20120604004816-cd527374f1e5 // indirect
	github.com/RangelReale/osincli v0.0.0-20160924135400-fababb0555f2
	github.com/alexbrainman/sspi v0.0.0-20180613141037-e580b900e9f5
	github.com/apcera/gssapi v0.0.0-00010101000000-000000000000
	github.com/aws/aws-sdk-go v1.16.26
	github.com/blang/semver v3.5.0+incompatible
	github.com/certifi/gocertifi v0.0.0-20180905225744-ee1a9a0726d2 // indirect
	github.com/containerd/continuity v0.0.0-20190827140505-75bee3e2ccb6 // indirect
	github.com/containers/image v0.0.0-00010101000000-000000000000
	github.com/containers/storage v0.0.0-20190726081758-912de200380a // indirect
	github.com/coreos/etcd v3.3.15+incompatible
	github.com/coreos/go-systemd v0.0.0-20190719114852-fd7a80b32e1f // indirect
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f // indirect
	github.com/davecgh/go-spew v1.1.1
	github.com/docker/distribution v2.7.1+incompatible
	github.com/docker/docker v0.7.3-0.20190327010347-be7ac8be2ae0
	github.com/docker/go-metrics v0.0.1 // indirect
	github.com/docker/go-units v0.4.0
	github.com/docker/libtrust v0.0.0-20160708172513-aabc10ec26b7
	github.com/elazarl/goproxy v0.0.0-20190911111923-ecfe977594f1
	github.com/fsnotify/fsnotify v1.4.7
	github.com/fsouza/go-dockerclient v0.0.0-20171004212419-da3951ba2e9e
	github.com/getsentry/raven-go v0.0.0-20190513200303-c977f96e1095 // indirect
	github.com/ghodss/yaml v0.0.0-20180820084758-c7ce16629ff4
	github.com/gonum/blas v0.0.0-20170728112917-37e82626499e // indirect
	github.com/gonum/diff v0.0.0-20181124234638-500114f11e71 // indirect
	github.com/gonum/floats v0.0.0-20170731225635-f74b330d45c5 // indirect
	github.com/gonum/graph v0.0.0-20170401004347-50b27dea7ebb
	github.com/gonum/integrate v0.0.0-20181209220457-a422b5c0fdf2 // indirect
	github.com/gonum/internal v0.0.0-20170731230106-e57e4534cf9b // indirect
	github.com/gonum/lapack v0.0.0-20170731225844-5ed4b826becd // indirect
	github.com/gonum/mathext v0.0.0-20181121095525-8a4bf007ea55 // indirect
	github.com/gonum/matrix v0.0.0-20170731230223-dd6034299e42 // indirect
	github.com/gonum/stat v0.0.0-20181125101827-41a0da705a5b // indirect
	github.com/google/btree v0.0.0-20190910154209-be84af90a1f7 // indirect
	github.com/gorilla/websocket v1.4.1 // indirect
	github.com/gotestyourself/gotestyourself v2.2.0+incompatible // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.1.0 // indirect
	github.com/joho/godotenv v0.0.0-20171110010315-6d367c18edf6
	github.com/mitchellh/go-wordwrap v1.0.0
	github.com/moby/buildkit v0.0.0-20181107081847-c3a857e3fca0
	github.com/mtrmac/gpgme v0.0.0-20170102180018-b2432428689c // indirect
	github.com/opencontainers/go-digest v1.0.0-rc1
	github.com/openshift/api v0.0.0-20190916204813-cdbe64fb0c91
	github.com/openshift/client-go v0.0.0-20190813201236-5a5508328169
	github.com/openshift/library-go v0.0.0-20190918130704-afb7c1698137
	github.com/openshift/source-to-image v0.0.0-20190716154012-2a579ecd66df
	github.com/pkg/errors v0.8.1
	github.com/pkg/profile v1.3.0 // indirect
	github.com/prometheus/client_golang v1.1.0
	github.com/prometheus/common v0.2.0 // indirect
	github.com/russross/blackfriday v1.5.2
	github.com/soheilhy/cmux v0.1.4 // indirect
	github.com/spf13/cobra v0.0.5
	github.com/spf13/pflag v1.0.3
	github.com/tmc/grpc-websocket-proxy v0.0.0-20190109142713-0ad062ec5ee5 // indirect
	github.com/vishvananda/netlink v1.0.0 // indirect
	github.com/vishvananda/netns v0.0.0-20190625233234-7109fa855b0f // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	github.com/xeipuuv/gojsonschema v1.1.0 // indirect
	github.com/xiang90/probing v0.0.0-20190116061207-43a291ad63a2 // indirect
	golang.org/x/crypto v0.0.0-20190611184440-5c40567a22f8
	golang.org/x/net v0.0.0-20190812203447-cdfb69ac37fc
	golang.org/x/sys v0.0.0-20190626221950-04f50cda93cb
	golang.org/x/time v0.0.0-20181108054448-85acf8d2951c
	gopkg.in/asn1-ber.v1 v1.0.0-20181015200546-f715ec2f112d // indirect
	gopkg.in/ldap.v2 v2.5.1
	k8s.io/api v0.0.0
	k8s.io/apimachinery v0.0.0
	k8s.io/apiserver v0.0.0
	k8s.io/cli-runtime v0.0.0
	k8s.io/client-go v0.0.0
	k8s.io/component-base v0.0.0
	k8s.io/klog v0.4.0
	k8s.io/kubectl v0.0.0
	k8s.io/kubernetes v0.0.0-00010101000000-000000000000
	k8s.io/utils v0.0.0-20190801114015-581e00157fb1
	sigs.k8s.io/yaml v1.1.0
)

replace (
	bitbucket.org/ww/goautoneg => github.com/munnerz/goautoneg v0.0.0-20190414153302-2ae31c8b6b30
	github.com/apcera/gssapi => github.com/openshift/gssapi v0.0.0-20161010215902-5fb4217df13b
	github.com/containers/image => github.com/openshift/containers-image v0.0.0-20190130162827-4bc6d24282b1
	github.com/docker/distribution => github.com/openshift/docker-distribution v0.0.0-20180925154709-d4c35485a70d
	github.com/docker/docker => github.com/docker/docker v0.0.0-20180612054059-a9fbbdc8dd87
	github.com/golang/glog => github.com/openshift/golang-glog v0.0.0-20190322123450-3c92600d7533
	github.com/onsi/ginkgo => github.com/openshift/onsi-ginkgo v1.2.1-0.20190125161613-53ca7dc85f60
	github.com/openshift/api => github.com/openshift/api v3.9.1-0.20190905133006-15918a3c2c89+incompatible
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20190905095913-1ede333d7ba5
	github.com/prometheus/client_golang => github.com/prometheus/client_golang v0.9.2
	k8s.io/api => k8s.io/api v0.0.0-20190913200010-d2ab659560cb
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20190913202221-52e3edd98126
	k8s.io/apimachinery => github.com/soltysh/apimachinery v0.0.0-20190916191719-6d4d531d3894
	k8s.io/apiserver => k8s.io/apiserver v0.0.0-20190913201147-5669a5603d96
	k8s.io/cli-runtime => github.com/soltysh/cli-runtime v0.0.0-20190916192338-ab99223dd497
	k8s.io/client-go => github.com/soltysh/k8s-client-go v0.0.0-20190916191915-2ebb04399ecd
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.0.0-20190913203700-32e6ab0de2a2
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.0.0-20190913203524-c351f77e9a62
	k8s.io/code-generator => k8s.io/code-generator v0.0.0-20190912054826-cd179ad6a269
	k8s.io/component-base => k8s.io/component-base v0.0.0-20190913200627-c1db07a83ddd
	k8s.io/cri-api => k8s.io/cri-api v0.0.0-20190828162817-608eb1dad4ac
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.0.0-20190913203836-0443d78e4a59
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.0.0-20190913201430-06145edbcf86
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.0.0-20190913203351-7116f89e0eb2
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.0.0-20190913202909-61cd20ae3d92
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.0.0-20190913203215-88ce8cca4f54
	k8s.io/kubectl => github.com/soltysh/kubectl v0.0.0-20190916192604-b1edf147d402
	k8s.io/kubelet => k8s.io/kubelet v0.0.0-20190913203039-8a485d6b870b
	k8s.io/kubernetes => github.com/soltysh/kubernetes v1.16.0-rc.2
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.0.0-20190913204022-a09231045882
	k8s.io/metrics => k8s.io/metrics v0.0.0-20190913202417-f6bee5ba35c7
	k8s.io/node-api => k8s.io/node-api v0.0.0-20190913204202-bf5c096842a8
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.0.0-20190913201707-470afe49ddf4
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.0.0-20190913202733-73f3d345fb5f
	k8s.io/sample-controller => k8s.io/sample-controller v0.0.0-20190913201905-cdcbc4548915
)
