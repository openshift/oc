module github.com/openshift/oc

go 1.12

require (
	github.com/Azure/go-ansiterm v0.0.0-20170929234023-d6e3b3328b78
	github.com/MakeNowJust/heredoc v0.0.0-20170808103936-bb23615498cd
	github.com/Microsoft/go-winio v0.4.11
	github.com/Nvveen/Gotty v0.0.0-20120604004816-cd527374f1e5
	github.com/PuerkitoBio/purell v1.0.0
	github.com/PuerkitoBio/urlesc v0.0.0-20160726150825-5bd2802263f2
	github.com/RangelReale/osincli v0.0.0-20160924135400-fababb0555f2
	github.com/alexbrainman/sspi v0.0.0-20180613141037-e580b900e9f5
	github.com/aws/aws-sdk-go v1.16.26
	github.com/beorn7/perks v0.0.0-20160229213445-3ac7bf7a47d1
	github.com/blang/semver v3.5.0+incompatible
	github.com/certifi/gocertifi v0.0.0-20180905225744-ee1a9a0726d2
	github.com/chai2010/gettext-go v0.0.0-20160711120539-c6fed771bfd5
	github.com/containerd/continuity v0.0.0-20190827140505-75bee3e2ccb6
	github.com/containers/storage v0.0.0-20190726081758-912de200380a
	github.com/coreos/bbolt v1.3.1-coreos.6
	github.com/coreos/etcd v3.3.10+incompatible
	github.com/davecgh/go-spew v0.0.0-20170626231645-782f4967f2dc
	github.com/daviddengcn/go-colortext v0.0.0-20160507010035-511bcaf42ccd
	github.com/docker/docker v0.0.0-20180612054059-a9fbbdc8dd87
	github.com/docker/go-connections v0.3.0
	github.com/docker/go-metrics v0.0.1
	github.com/docker/go-units v0.0.0-20170127094116-9e638d38cf69
	github.com/docker/libnetwork v0.0.0-20180830151422-a9cd636e3789
	github.com/docker/libtrust v0.0.0-20160708172513-aabc10ec26b7
	github.com/docker/spdystream v0.0.0-20160310174837-449fdfce4d96
	github.com/emicklei/go-restful v0.0.0-20170410110728-ff4f55a20633
	github.com/evanphx/json-patch v4.2.0+incompatible
	github.com/exponent-io/jsonpath v0.0.0-20151013193312-d6023ce2651d
	github.com/fatih/camelcase v0.0.0-20160318181535-f6a740d52f96
	github.com/fsnotify/fsnotify v1.4.7
	github.com/fsouza/go-dockerclient v0.0.0-20171004212419-da3951ba2e9e
	github.com/getsentry/raven-go v0.0.0-20190513200303-c977f96e1095
	github.com/ghodss/yaml v0.0.0-20150909031657-73d445a93680
	github.com/go-openapi/jsonpointer v0.19.0
	github.com/go-openapi/jsonreference v0.19.0
	github.com/go-openapi/loads v0.0.0-20170520182102-a80dea3052f0
	github.com/go-openapi/spec v0.17.2
	github.com/go-openapi/swag v0.17.2
	github.com/gogo/protobuf v0.0.0-20171007142547-342cbe0a0415
	github.com/golang/groupcache v0.0.0-20160516000752-02826c3e7903
	github.com/golang/protobuf v1.1.0
	github.com/gonum/blas v0.0.0-20170728112917-37e82626499e
	github.com/gonum/floats v0.0.0-20170731225635-f74b330d45c5
	github.com/gonum/graph v0.0.0-20170401004347-50b27dea7ebb
	github.com/gonum/internal v0.0.0-20170731230106-e57e4534cf9b
	github.com/gonum/lapack v0.0.0-20170731225844-5ed4b826becd
	github.com/gonum/matrix v0.0.0-20170731230223-dd6034299e42
	github.com/google/btree v0.0.0-20190910154209-be84af90a1f7
	github.com/google/cadvisor v0.32.0
	github.com/google/gofuzz v0.0.0-20170612174753-24818f796faf
	github.com/google/uuid v0.0.0-20171113160352-8c31c18f31ed
	github.com/googleapis/gnostic v0.0.0-20170729233727-0c5108395e2d
	github.com/gorilla/mux v0.0.0-20190830121156-884b5ffcbd3a
	github.com/gregjones/httpcache v0.0.0-20170728041850-787624de3eb7
	github.com/grpc-ecosystem/grpc-gateway v1.3.0
	github.com/hashicorp/golang-lru v0.5.0
	github.com/imdario/mergo v0.3.5
	github.com/inconshreveable/mousetrap v1.0.0
	github.com/jmespath/go-jmespath v0.0.0-20160202185014-0b12d6b521d8
	github.com/joho/godotenv v0.0.0-20171110010315-6d367c18edf6
	github.com/jonboulle/clockwork v0.0.0-20141017032234-72f9bd7c4e0c
	github.com/json-iterator/go v0.0.0-20180701071628-ab8a2e0c74be
	github.com/jteeuwen/go-bindata v0.0.0-20151023091102-a0ff2567cfb7
	github.com/liggitt/tabwriter v0.0.0-20181228230101-89fcab3d43de
	github.com/lithammer/dedent v1.1.0
	github.com/mailru/easyjson v0.0.0-20170624190925-2f5df55504eb
	github.com/matttproud/golang_protobuf_extensions v1.0.1
	github.com/miekg/dns v1.0.8
	github.com/mitchellh/go-wordwrap v0.0.0-20150314170334-ad45545899c7
	github.com/moby/buildkit v0.0.0-20181107081847-c3a857e3fca0
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd
	github.com/modern-go/reflect2 v1.0.1
	github.com/mtrmac/gpgme v0.0.0-20170102180018-b2432428689c
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f
	github.com/onsi/gomega v0.0.0-20190113212917-5533ce8a0da3
	github.com/opencontainers/go-digest v0.0.0-20170106003457-a6d0ee40d420
	github.com/opencontainers/image-spec v0.0.0-20170604055404-372ad780f634
	github.com/opencontainers/runc v0.0.0-20181113202123-f000fe11ece1
	github.com/openshift/api v0.0.0-20190916204813-cdbe64fb0c91
	github.com/openshift/client-go v0.0.0-20190813201236-5a5508328169
	github.com/openshift/library-go v0.0.0-20190918130704-afb7c1698137
	github.com/openshift/source-to-image v0.0.0-20190716154012-2a579ecd66df
	github.com/pborman/uuid v0.0.0-20150603214016-ca53cad383ca
	github.com/peterbourgon/diskv v2.0.1+incompatible
	github.com/pkg/errors v0.8.0
	github.com/pkg/profile v1.3.0
	github.com/pmezard/go-difflib v0.0.0-20181226105442-5d4384ee4fb2
	github.com/prometheus/client_golang v0.9.2
	github.com/prometheus/client_model v0.0.0-20150212101744-fa8ad6fec335
	github.com/prometheus/common v0.2.0
	github.com/prometheus/procfs v0.0.0-20170519190837-65c1f6f8f0fc
	github.com/russross/blackfriday v0.0.0-20151117072312-300106c228d5
	github.com/shurcooL/sanitized_anchor_name v0.0.0-20151028001915-10ef21a441db
	github.com/sirupsen/logrus v0.0.0-20170822132746-89742aefa4b2
	github.com/spf13/cobra v0.0.0-20180319062004-c439c4fa0937
	github.com/spf13/pflag v1.0.1
	github.com/stretchr/testify v0.0.0-20180319223459-c679ae2cc0cb
	github.com/vjeantet/ldapserver v0.0.0-20150820113053-5ac58729571e
	go4.org v0.0.0-20160314031811-03efcb870d84
	golang.org/x/crypto v0.0.0-20180808211826-de0752318171
	golang.org/x/net v0.0.0-20190206173232-65e2d4e15006
	golang.org/x/oauth2 v0.0.0-20170412232759-a6bd8cefa181
	golang.org/x/sys v0.0.0-20171031081856-95c657629925
	golang.org/x/text v0.0.0-20170810154203-b19bf474d317
	golang.org/x/time v0.0.0-20161028155119-f51c12702a4d
	golang.org/x/tools v0.0.0-20190205050122-7f7074d5bcfd
	gonum.org/v1/gonum v0.0.0-20180726124543-cebdade430cc
	google.golang.org/appengine v0.0.0-20160301025000-12d5545dc1cf
	google.golang.org/genproto v0.0.0-20170731182057-09f6ed296fc6
	google.golang.org/grpc v1.13.0
	gopkg.in/asn1-ber.v1 v1.0.0-20181015200546-f715ec2f112d
	gopkg.in/inf.v0 v0.9.0
	gopkg.in/ldap.v2 v2.5.1
	gopkg.in/square/go-jose.v2 v2.0.0-20180411045311-89060dee6a84
	gopkg.in/yaml.v2 v2.2.1
	k8s.io/api v0.0.0-20190313235455-40a48860b5ab
	k8s.io/apiextensions-apiserver v0.0.0-20190315093550-53c4693659ed
	k8s.io/apiserver v0.0.0-20190313205120-8b27c41bdbb1
	k8s.io/cloud-provider v0.0.0-20190314002645-c892ea32361a
	k8s.io/cluster-bootstrap v0.0.0-20190314002537-50662da99b70
	k8s.io/code-generator v0.0.0-20190311093542-50b561225d70
	k8s.io/component-base v0.0.0-20190314000054-4a91899592f4
	k8s.io/csi-api v0.0.0-20190314001839-693d387aa133
	k8s.io/csi-translation-lib v0.0.0-20190314002815-ce92c5cfdd61
	k8s.io/gengo v0.0.0-20181106084056-51747d6e00da
	k8s.io/klog v0.0.0-20181108234604-8139d8cb77af
	k8s.io/kube-aggregator v0.0.0-20190314000639-da8327669ac5
	k8s.io/kube-controller-manager v0.0.0-20190314002447-97ed623e3835
	k8s.io/kube-openapi v0.0.0-20190228160746-b3a7cee44a30
	k8s.io/kube-proxy v0.0.0-20190314002154-4d735c31b054
	k8s.io/kube-scheduler v0.0.0-20190314002350-b74e9e79538d
	k8s.io/kubelet v0.0.0-20190314002251-f6da02f58325
	k8s.io/metrics v0.0.0-20190314001731-1bd6a4002213
	k8s.io/sample-apiserver v0.0.0-20190314000836-236f85ce49e5
	k8s.io/sample-cli-plugin v0.0.0-20190314002056-59043b4d4f84
	k8s.io/sample-controller v0.0.0-20190314001137-324336050c97
	k8s.io/utils v0.0.0-20190221042446-c2654d5206da
	sigs.k8s.io/kustomize v2.0.3+incompatible
	sigs.k8s.io/yaml v1.1.0
	vbom.ml/util v0.0.0-20160121211510-db5cfe13f5cc
)

replace (
	bitbucket.org/ww/goautoneg => github.com/munnerz/goautoneg 2ae31c8b6b30d2f4c8100c20d527b571e9c433bb
	github.com/apcera/gssapi => github.com/openshift/gssapi 5fb4217df13b8e6878046fe1e5c10e560e1b86dc
	github.com/containers/image => github.com/openshift/containers-image 4bc6d24282b115f8b61a6d08470ed42ac7c91392
	github.com/docker/distribution => github.com/openshift/docker-distribution d4c35485a70df4dce2179bc227b1393a69edb809
	github.com/docker/docker => github.com/docker/docker v0.0.0-20180612054059-a9fbbdc8dd87
	github.com/golang/glog => github.com/openshift/golang-glog 3c92600d7533018d216b534fe894ad60a1e6d5bf
	github.com/onsi/ginkgo => github.com/openshift/onsi-ginkgo 53ca7dc85f609e8aa3af7902f189ed5dca96dbb5
	github.com/prometheus/client_golang => github.com/prometheus/client_golang v0.9.2
	k8s.io/apimachinery => github.com/openshift/kubernetes-apimachinery 26f88640163ea91a407249714c7e4a7a3b2ab3bb
	k8s.io/cli-runtime => github.com/openshift/kubernetes-cli-runtime 200dde6b923a881f85d5310356ce3b3ecfecdf04
	k8s.io/client-go => github.com/openshift/kubernetes-client-go 07e29e5eae48c8279cce3dc0f544e5e7a8ee9bb7
	k8s.io/kubernetes => github.com/openshift/kubernetes 4547c3bba84481326db714c47129539d908924d6
)
