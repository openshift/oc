package regeneratemco

import (
	"context"
	"fmt"
	"net/url"
	"time"

	configclient "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/certrotation"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

const (
	OneYear    = 365 * 24 * time.Hour
	caExpiry   = 10 * OneYear
	caRefresh  = 9 * OneYear
	keyExpiry  = caExpiry
	keyRefresh = caRefresh

	mcoNamespace   = "openshift-machine-config-operator"
	controllerName = "OcMachineConfigServerRotator"
	mcsName        = "machine-config-server"

	// mcsTlsSecretName is created by the installer and is not owned by default
	mcsTlsSecretName = mcsName + "-tls"
	// signerName is the name injected into the certificate as the signer
	signerName = "oc-for-machine-config-server"
	// openshiftOrg is the openshift organizational unit
	openshiftOrg = "openshift"

	// legacyRootCANamespace is the namespace of the legacy root CA
	legacyRootCANamespace = "kube-system"
	// legacyRootCA is the name of the configmap holding the MCS CA created by openshift-install
	legacyRootCA = "root-ca"

	generatedAnnotationKey   = "openshift.io/generated-by"
	generatedAnnotationValue = "oc"
)

var (
	regenerateMCOLong = templates.LongDesc(`
		Regenerate the Machine Config Operator certificates for an OCP v4 cluster.

		More information about these certificates are in the product documentation;
		visit [docs.openshift.com](https://docs.openshift.com)
		and select the version of OpenShift you are using.

		Experimental: This command is under active development and may change without notice.
	`)

	regenerateMCOExample = templates.Examples(`
		oc adm certificates regenerate-mco
	`)
)

type RegenerateMCOOptions struct {
	RESTClientGetter genericclioptions.RESTClientGetter

	mcoNamespace string

	genericclioptions.IOStreams
}

func NewCmdRegenerateTopLevel(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := &RegenerateMCOOptions{
		RESTClientGetter: restClientGetter,
		IOStreams:        streams,
	}

	cmd := &cobra.Command{
		Use:                   "regenerate-mco",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Regenerate the machine config operator certificates in an OpenShift cluster"),
		Long:                  regenerateMCOLong,
		Example:               regenerateMCOExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(run(o, args))
		},
	}

	o.AddFlags(cmd)

	return cmd
}

// AddFlags registers flags for a cli
func (o *RegenerateMCOOptions) AddFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.mcoNamespace, "mco-namespace", mcoNamespace, "Operate instead on this namespace for the MCO (useful for testing)")
}

func run(o *RegenerateMCOOptions, args []string) error {
	recorder := events.NewLoggingEventRecorder("oc")

	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	oconfig, err := configclient.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	host, err := oconfig.ConfigV1().Infrastructures().Get(context.TODO(), "cluster", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("unable to get cluster infrastructure resource: %w", err)
	}
	if host.Status.APIServerInternalURL == "" {
		return fmt.Errorf("no APIServerInternalURL")
	}
	apiserverIntURL, err := url.Parse(host.Status.APIServerInternalURL)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", apiserverIntURL, err)
	}

	inf := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		24*time.Hour,
		informers.WithNamespace(o.mcoNamespace))

	caName := mcsName + "-ca"
	cont := certrotation.NewCertRotationController(
		controllerName,
		certrotation.RotatedSigningCASecret{
			Namespace:     o.mcoNamespace,
			Name:          caName,
			Validity:      caExpiry,
			Refresh:       caRefresh,
			Informer:      inf.Core().V1().Secrets(),
			Lister:        inf.Core().V1().Secrets().Lister(),
			Client:        clientset.CoreV1(),
			EventRecorder: recorder,
		},
		certrotation.CABundleConfigMap{
			Namespace:     o.mcoNamespace,
			Name:          caName,
			Lister:        inf.Core().V1().ConfigMaps().Lister(),
			Informer:      inf.Core().V1().ConfigMaps(),
			Client:        clientset.CoreV1(),
			EventRecorder: recorder,
		},
		certrotation.RotatedSelfSignedCertKeySecret{
			Namespace: o.mcoNamespace,
			Name:      mcsTlsSecretName,
			Validity:  keyExpiry,
			Refresh:   keyRefresh,
			CertCreator: &certrotation.ServingRotation{
				Hostnames: func() []string { return []string{apiserverIntURL.Hostname()} },
			},
			Lister:        inf.Core().V1().Secrets().Lister(),
			Informer:      inf.Core().V1().Secrets(),
			Client:        clientset.CoreV1(),
			EventRecorder: recorder,
		},
		nil, // no operatorclient needed
		recorder,
	)

	ch := make(chan struct{})
	inf.Start(ch)
	inf.WaitForCacheSync(ch)

	ctx := context.WithValue(context.Background(), certrotation.RunOnceContextKey, true)
	syncCtx := factory.NewSyncContext(mcsName, recorder)
	if err := cont.Sync(ctx, syncCtx); err != nil {
		return err
	}

	return nil
}
