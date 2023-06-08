package refreshcabundle

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

type RefreshCABundleOptions struct {
	RESTClientGetter genericclioptions.RESTClientGetter

	ConfigAccess clientcmd.ConfigAccess
	ClusterName  string

	// for convenience. You can compare to an original by doing.
	// oc config view --raw -o jsonpath='{.clusters[?(@.name == "the-name")].cluster.certificate-authority-data}' | base64 -d
	DryRun bool

	genericclioptions.IOStreams
}

var (
	setClusterLong = templates.LongDesc(i18n.T(`
		Update the CA bundle by reading the content from an OpenShift cluster.`))

	setClusterExample = templates.Examples(`
		# Refresh the CA bundle for the current context's cluster
		oc config refresh-ca-bundle

		# Refresh the CA bundle for the cluster named e2e in your kubeconfig
		oc config refresh-ca-bundle e2e

		# Print the CA bundle from the current OpenShift cluster's apiserver.
		oc config refresh-ca-bundle --dry-run`)
)

func NewRefreshCABundleOptions(restClientGetter genericclioptions.RESTClientGetter, configAccess clientcmd.ConfigAccess, streams genericclioptions.IOStreams) *RefreshCABundleOptions {
	return &RefreshCABundleOptions{
		RESTClientGetter: restClientGetter,
		ConfigAccess:     configAccess,
		IOStreams:        streams,
	}
}

func NewCmdConfigRefreshCABundle(restClientGetter genericclioptions.RESTClientGetter, configAccess clientcmd.ConfigAccess, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewRefreshCABundleOptions(restClientGetter, configAccess, streams)

	cmd := &cobra.Command{
		Use:                   fmt.Sprintf("refresh-ca-bundle [NAME]"),
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Update the OpenShift CA bundle by contacting the apiserver."),
		Long:                  setClusterLong,
		Example:               setClusterExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(cmd))
			cmdutil.CheckErr(o.Run(context.TODO()))

		},
	}

	cmd.Flags().BoolVar(&o.DryRun, "dry-run", o.DryRun, "display the CA bundle, but don't make any changes to the kubeconfig")

	return cmd
}

func (o RefreshCABundleOptions) Run(ctx context.Context) error {
	err := o.Validate()
	if err != nil {
		return err
	}

	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return err
	}
	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	kubeAPIServerCABundleConfigMap, err := kubeClient.CoreV1().ConfigMaps("openshift-kube-controller-manager").Get(ctx, "serviceaccount-ca", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("unable to read the CA bundle from the cluster: %w", err)
	}
	caBundle := kubeAPIServerCABundleConfigMap.Data["ca-bundle.crt"]
	if len(caBundle) == 0 {
		return fmt.Errorf("cluster somehow missing the CA bundle: not an OCP cluster?")
	}

	if o.DryRun {
		fmt.Fprintf(o.Out, caBundle)
		return nil
	}

	config, err := o.ConfigAccess.GetStartingConfig()
	if err != nil {
		return err
	}

	// if we have no name specified, then choose the current
	if len(o.ClusterName) == 0 {
		currContext, ok := config.Contexts[config.CurrentContext]
		if !ok {
			return fmt.Errorf("cannot determine which context to update")
		}
		o.ClusterName = currContext.Cluster
	}

	startingStanza, exists := config.Clusters[o.ClusterName]
	if !exists {
		return fmt.Errorf("cannot determine which context to update")
	}

	cluster, err := o.modifyCluster(*startingStanza, caBundle)
	if err != nil {
		return fmt.Errorf("failed to update CA bundle: %w", err)
	}
	config.Clusters[o.ClusterName] = cluster

	if err := clientcmd.ModifyConfig(o.ConfigAccess, *config, true); err != nil {
		return err
	}

	fmt.Fprintf(o.Out, "CA bundle for cluster %q updated.\n", o.ClusterName)

	return nil
}

func (o *RefreshCABundleOptions) modifyCluster(existingCluster clientcmdapi.Cluster, caBundle string) (*clientcmdapi.Cluster, error) {
	modifiedCluster := existingCluster

	if len(modifiedCluster.CertificateAuthorityData) > 0 {
		modifiedCluster.CertificateAuthorityData = []byte(caBundle)
	}

	if len(modifiedCluster.CertificateAuthority) > 0 {
		fileInfo, err := os.Stat(modifiedCluster.CertificateAuthority)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(fileInfo.Name(), []byte(caBundle), fileInfo.Mode()); err != nil {
			return nil, err
		}
	}

	return &modifiedCluster, nil
}

func (o *RefreshCABundleOptions) Complete(cmd *cobra.Command) error {
	args := cmd.Flags().Args()
	if len(args) > 1 {
		return helpErrorf(cmd, "Unexpected args: %v", args)
	}

	if len(args) == 0 {
		return nil
	}

	o.ClusterName = args[0]
	return nil
}

func (o RefreshCABundleOptions) Validate() error {
	return nil
}

func helpErrorf(cmd *cobra.Command, format string, args ...interface{}) error {
	cmd.Help()
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("%s", msg)
}
