package etcd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	internalMigrateTTLLong = templates.LongDesc(`
		This command is deprecated and will be removed in a future release
	`)

	internalMigrateTTLExample = templates.Examples(`
	`)
)

type MigrateTTLReferenceOptions struct {
	etcdAddress   string
	ttlKeysPrefix string
	leaseDuration time.Duration
	certFile      string
	keyFile       string
	caFile        string

	genericclioptions.IOStreams
}

func NewMigrateTTLReferenceOptions(streams genericclioptions.IOStreams) *MigrateTTLReferenceOptions {
	return &MigrateTTLReferenceOptions{
		IOStreams: streams,
	}
}

// NewCmdMigrateTTLs helps move etcd v2 TTL keys to etcd v3 lease keys.
func NewCmdMigrateTTLs(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewMigrateTTLReferenceOptions(streams)
	cmd := &cobra.Command{
		Use:        "etcd-ttl --etcd-address=HOST --ttl-keys-prefix=PATH",
		Short:      "Attach keys to etcd v3 leases to assist in etcd v2 migrations",
		Long:       internalMigrateTTLLong,
		Example:    internalMigrateTTLExample,
		Deprecated: "migration of content is managed automatically in OpenShift 4.x",
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().StringVar(&o.etcdAddress, "etcd-address", o.etcdAddress, "Etcd address")
	cmd.Flags().StringVar(&o.ttlKeysPrefix, "ttl-keys-prefix", o.ttlKeysPrefix, "Prefix for TTL keys")
	cmd.Flags().DurationVar(&o.leaseDuration, "lease-duration", o.leaseDuration, "Lease duration (format: '2h', '120m', etc)")
	cmd.Flags().StringVar(&o.certFile, "cert", o.certFile, "identify secure client using this TLS certificate file")
	cmd.Flags().StringVar(&o.keyFile, "key", o.keyFile, "identify secure client using this TLS key file")
	cmd.Flags().StringVar(&o.caFile, "cacert", o.caFile, "verify certificates of TLS-enabled secure servers using this CA bundle")

	return cmd
}

func (o *MigrateTTLReferenceOptions) Run() error {
	return fmt.Errorf("this command is no longer supported")
}
