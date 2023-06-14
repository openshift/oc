package regeneratemco

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/operator/certrotation"
	"github.com/spf13/cobra"

	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

const (
	oneYear    = 365 * 24 * time.Hour
	caExpiry   = 10 * oneYear
	caRefresh  = 9 * oneYear
	keyExpiry  = caExpiry
	keyRefresh = caRefresh

	mcoNamespace   = "openshift-machine-config-operator"
	mapiNamespace  = "openshift-machine-api"
	controllerName = "OCMachineConfigServerRotator"
	mcsName        = "machine-config-server"

	// mcsTlsSecretName is created by the installer and is not owned by default
	mcsTlsSecretName = mcsName + "-tls"

	// newMCSCASecret is the location of the CA after rotation
	newMCSCASecret = "machine-config-server-ca"
	userDataKey    = "userData"

	// mcoManagedWorkerSecret is the unused, MCO-managed stub ignition for workers
	mcoManagedWorkerSecret = "worker-user-data-managed"
	// mcoManagedMasterSecret is the unused, MCO-managed stub ignition for masters
	mcoManagedMasterSecret = "master-user-data-managed"
	// mcsLabelSelector is used to select the MCS pods
	mcsLabelSelector = "k8s-app=machine-config-server"

	// ign* are for the user-data ignition fields
	ignFieldIgnition = "ignition"
	ignFieldSource   = "source"
)

var (
	regenerateMCOLong = templates.LongDesc(`
		Regenerate the Machine Config Operator certificates for an OCP v4 cluster.
		This is the certificate used to verify the MCS contents when a new nodes attempts to join the cluster.

		Experimental: This command is under active development and may change without notice.
	`)

	regenerateMCOExample = templates.Examples(`
	    # Regenerate the MCO certs without modifying user-data secrets
		oc adm certificates regenerate-machine-config-server-serving-cert --update-ignition=false

		# Update the user-data secrets to use new MCS certs
		oc adm certificates update-ignition-ca-bundle-for-machine-config-server
	`)
)

type RegenerateMCOOptions struct {
	RESTClientGetter genericclioptions.RESTClientGetter

	ModifyUserData bool

	genericclioptions.IOStreams
}

func NewCmdRegenerateTopLevel(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := &RegenerateMCOOptions{
		RESTClientGetter: restClientGetter,
		IOStreams:        streams,
		ModifyUserData:   true,
	}

	cmd := &cobra.Command{
		Use:                   "regenerate-machine-config-server-serving-cert",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Regenerate the machine config operator certificates in an OpenShift cluster"),
		Long:                  regenerateMCOLong,
		Example:               regenerateMCOExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Run(context.WithValue(context.Background(), certrotation.RunOnceContextKey, true)))
		},
	}

	o.AddFlags(cmd)

	return cmd
}

func NewCmdUpdateUserData(restClientGetter genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := &RegenerateMCOOptions{
		RESTClientGetter: restClientGetter,
		IOStreams:        streams,
		ModifyUserData:   true,
	}

	cmd := &cobra.Command{
		Use:                   "update-ignition-ca-bundle-for-machine-config-server",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Update user-data secrets in an OpenShift cluster to use updated MCO certfs"),
		Long:                  regenerateMCOLong,
		Example:               regenerateMCOExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.RunUserDataUpdate(context.Background()))
		},
	}

	return cmd
}

// AddFlags registers flags for a cli
func (o *RegenerateMCOOptions) AddFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&o.ModifyUserData, "update-ignition", o.ModifyUserData, "If true, automatically update user-data secrets (ignition) in machine-api namespace. Not useful if node scaling not backed by MachineSet.")
}
