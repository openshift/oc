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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

func (o *RegenerateMCOOptions) Run(ctx context.Context) error {
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

	host, err := oconfig.ConfigV1().Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("unable to get cluster infrastructure resource: %w", err)
	}
	if host.Status.APIServerInternalURL == "" {
		return fmt.Errorf("no APIServerInternalURL found in cluster infrastructure resource")
	}
	apiserverIntURL, err := url.Parse(host.Status.APIServerInternalURL)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", apiserverIntURL, err)
	}

	inf := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		24*time.Hour,
		informers.WithNamespace(mcoNamespace))

	caName := mcsName + "-ca"
	cont := certrotation.NewCertRotationController(
		controllerName,
		certrotation.RotatedSigningCASecret{
			Namespace:     mcoNamespace,
			Name:          caName,
			Validity:      caExpiry,
			Refresh:       caRefresh,
			Informer:      inf.Core().V1().Secrets(),
			Lister:        inf.Core().V1().Secrets().Lister(),
			Client:        clientset.CoreV1(),
			EventRecorder: recorder,
		},
		certrotation.CABundleConfigMap{
			Namespace:     mcoNamespace,
			Name:          caName,
			Lister:        inf.Core().V1().ConfigMaps().Lister(),
			Informer:      inf.Core().V1().ConfigMaps(),
			Client:        clientset.CoreV1(),
			EventRecorder: recorder,
		},
		certrotation.RotatedSelfSignedCertKeySecret{
			Namespace: mcoNamespace,
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

	inf.Start(ctx.Done())
	inf.WaitForCacheSync(ctx.Done())

	syncCtx := factory.NewSyncContext(mcsName, recorder)
	if err := cont.Sync(ctx, syncCtx); err != nil {
		return err
	}

	fmt.Fprintf(o.IOStreams.Out, "Successfully rotated MCS CA + certs. Redeploying MCS and updating references.\n")

	// Redeploy MCS. This will eventually not be needed, see: https://github.com/openshift/machine-config-operator/pull/3744
	mcoPods := clientset.CoreV1().Pods(mcoNamespace)
	mcsPods, err := mcoPods.List(ctx, metav1.ListOptions{
		LabelSelector: mcsLabelSelector,
	})
	if err != nil {
		return fmt.Errorf("cannot get MCS pods: %w", err)
	}
	for _, pod := range mcsPods.Items {
		err := mcoPods.Delete(ctx, pod.Name, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("cannot delete MCS pod %s: %w", pod.Name, err)
		}
	}

	// TODO maybe add a watcher to make sure the MCS daemonset is ready here

	if o.ModifyUserData {
		return o.RunUserDataUpdate(ctx)
	}
	return nil
}
