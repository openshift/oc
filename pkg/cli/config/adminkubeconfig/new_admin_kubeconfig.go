package adminkubeconfig

import (
	"bytes"
	"context"
	"crypto/x509"
	"fmt"
	"reflect"
	"time"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/util/cert"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/library-go/pkg/crypto"
	"github.com/openshift/library-go/pkg/operator/certrotation"

	"github.com/openshift/oc/pkg/cli/config/refreshcabundle"
)

type NewAdminKubeconfigOptions struct {
	RESTClientGetter genericclioptions.RESTClientGetter

	genericiooptions.IOStreams
}

var (
	newAdminKubeconfigLong = templates.LongDesc(i18n.T(`
		Generate, make the server trust, and display a new admin.kubeconfig.

		The key is produced locally and is not persisted to disk.  The public half is pushed to the cluster
		for the kube-apiserver to trust the locally created admin.kubeconfig.`))

	newAdminKubeconfigExample = templates.Examples(`
		# Generate a new admin kubeconfig
		oc config new-admin-kubeconfig`)
)

const (
	// adminKubeconfigClientCAConfigMap is described in https://github.com/openshift/api/blob/master/tls/docs/kube-apiserver%20Client%20Certificates/README.md#kube-apiserver-admin-kubeconfig-client-ca
	adminKubeconfigClientCAConfigMap = "admin-kubeconfig-client-ca"

	tenYears = 24 * time.Hour * 365 * 10
)

func NewNewAdminKubeconfigOptions(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *NewAdminKubeconfigOptions {
	return &NewAdminKubeconfigOptions{
		RESTClientGetter: restClientGetter,
		IOStreams:        streams,
	}
}

func NewCmdNewAdminKubeconfigOptions(restClientGetter genericclioptions.RESTClientGetter, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewNewAdminKubeconfigOptions(restClientGetter, streams)

	cmd := &cobra.Command{
		Use:                   "new-admin-kubeconfig",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Generate, make the server trust, and display a new admin.kubeconfig"),
		Long:                  newAdminKubeconfigLong,
		Example:               newAdminKubeconfigExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete())
			cmdutil.CheckErr(o.Validate(args))
			r, err := o.ToRuntime()
			cmdutil.CheckErr(err)
			cmdutil.CheckErr(r.Run(context.TODO()))

		},
	}

	o.AddFlags(cmd)

	return cmd
}

func (o *NewAdminKubeconfigOptions) AddFlags(cmd *cobra.Command) {
}

func (o *NewAdminKubeconfigOptions) Complete() error {

	return nil
}

func (o NewAdminKubeconfigOptions) Validate(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("no arguments allowed")
	}

	return nil
}

func (o *NewAdminKubeconfigOptions) ToRuntime() (*NewAdminKubeconfigRuntime, error) {
	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	ret := &NewAdminKubeconfigRuntime{
		KubeClient: kubeClient,
		Host:       clientConfig.Host,
		IOStreams:  o.IOStreams,
	}

	return ret, nil
}

type NewAdminKubeconfigRuntime struct {
	KubeClient kubernetes.Interface
	Host       string

	genericiooptions.IOStreams
}

func (r *NewAdminKubeconfigRuntime) Run(ctx context.Context) error {
	systemMasterClientCert, signerCertBytes, err := createNewAdminClientCert()
	if err != nil {
		return fmt.Errorf("unable to create new client certificate: %w", err)
	}

	serverCABundle, err := refreshcabundle.GetCABundleToTrustKubeAPIServer(ctx, r.KubeClient)
	if err != nil {
		return fmt.Errorf("unable to get the CA bundle from the cluster: %w", err)
	}

	// update the in-cluster configmap
	existingConfigMap, err := r.KubeClient.CoreV1().ConfigMaps("openshift-config").Get(ctx, adminKubeconfigClientCAConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		existingConfigMap, err = r.KubeClient.CoreV1().ConfigMaps("openshift-config").Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "openshift-config",
				Name:      adminKubeconfigClientCAConfigMap,
			},
		}, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("unable to create starting --namespace=openshift-config configmap/%s : %w", adminKubeconfigClientCAConfigMap, err)
		}
	} else if err != nil {
		return fmt.Errorf("unable to read configmap %w", err)
	}

	// this ensures that we honor both old and new trust.  Revocation of existing trust would be a separate step and/or command.
	caBundle, err := combineCABundles(existingConfigMap.Data["ca-bundle.crt"], string(signerCertBytes))
	if err != nil {
		return fmt.Errorf("unable to combine CA bundles %w", err)
	}

	toWrite := existingConfigMap.DeepCopy()
	toWrite.Data["ca-bundle.crt"] = string(caBundle)
	if _, err := r.KubeClient.CoreV1().ConfigMaps("openshift-config").Update(ctx, toWrite, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("unable to combine update configmap %w", err)
	}

	clientCertBytes := &bytes.Buffer{}
	clientKeyBytes := &bytes.Buffer{}
	if err := systemMasterClientCert.WriteCertConfig(clientCertBytes, clientKeyBytes); err != nil {
		return fmt.Errorf("unable to serialize client certificate: %w", err)
	}

	newConfig := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"cluster": {
				Server:                   r.Host,
				CertificateAuthorityData: []byte(serverCABundle),
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"admin": {
				ClientCertificateData: clientCertBytes.Bytes(),
				ClientKeyData:         clientKeyBytes.Bytes(),
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"admin": {
				Cluster:  "cluster",
				AuthInfo: "admin",
			},
		},
		CurrentContext: "admin",
	}
	newAdminKubeconfig, err := clientcmd.Write(newConfig)
	if err != nil {
		return fmt.Errorf("unable to serialize new kubeconfig: %w", err)
	}

	fmt.Fprintln(r.Out, string(newAdminKubeconfig))

	return nil
}

func createNewAdminClientCert() (*crypto.TLSCertificateConfig, []byte, error) {
	signerName := fmt.Sprintf("%s_%s@%d", "openshift-config", "admin.kubeconfig-signer", time.Now().Unix())
	signer, err := crypto.MakeSelfSignedCAConfigForDuration(signerName, tenYears)
	if err != nil {
		return nil, nil, err
	}
	certBytes := &bytes.Buffer{}
	keyBytes := &bytes.Buffer{}
	if err := signer.WriteCertConfig(certBytes, keyBytes); err != nil {
		return nil, nil, err
	}
	signingCertKeyPair, err := crypto.GetCAFromBytes(certBytes.Bytes(), keyBytes.Bytes())
	if err != nil {
		return nil, nil, err
	}

	certSpecification := &certrotation.ClientRotation{
		UserInfo: &user.DefaultInfo{
			Name:   "system:admin",
			Groups: []string{"system:masters"},
		},
	}
	clientCertKeyPair, err := certSpecification.NewCertificate(signingCertKeyPair, tenYears)
	if err != nil {
		return nil, nil, err
	}

	return clientCertKeyPair, certBytes.Bytes(), nil
}

func combineCABundles(startingCABundle, additionalCABundle string) ([]byte, error) {
	certificates := []*x509.Certificate{}

	if len(startingCABundle) > 0 {
		startingCerts, err := cert.ParseCertsPEM([]byte(startingCABundle))
		if err != nil {
			return nil, fmt.Errorf("starting CA bundle is malformed: %w", err)
		}
		certificates = append(certificates, startingCerts...)
	}

	additionalCerts, err := cert.ParseCertsPEM([]byte(additionalCABundle))
	if err != nil {
		return nil, fmt.Errorf("additional CA bundle is malformed: %w", err)
	}
	certificates = append(certificates, additionalCerts...)

	certificates = crypto.FilterExpiredCerts(certificates...)
	finalCertificates := []*x509.Certificate{}
	// now check for duplicates. n^2, but super simple
	for i := range certificates {
		found := false
		for j := range finalCertificates {
			if reflect.DeepEqual(certificates[i].Raw, finalCertificates[j].Raw) {
				found = true
				break
			}
		}
		if !found {
			finalCertificates = append(finalCertificates, certificates[i])
		}
	}

	caBytes, err := crypto.EncodeCertificates(finalCertificates...)
	if err != nil {
		return nil, err
	}

	return caBytes, nil
}
