package etcd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
)

type ListMembersOptions struct {
	config *rest.Config

	genericclioptions.IOStreams
}

func NewCommandListMembers(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := ListMembersOptions{
		IOStreams: streams,
	}
	cmd := &cobra.Command{
		Use:   "list-members",
		Short: "List etcd cluster members",
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run())
		},
	}

	return cmd
}

func (o *ListMembersOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	config, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.config = config
	return nil
}

func (o *ListMembersOptions) Run() error {
	client, err := NewEtcdClient(o.config)
	if err != nil {
		return err
	}
	members, err := client.MemberList(context.Background())
	if err != nil {
		return err
	}
	for _, member := range members.Members {
		_, err = io.WriteString(o.Out, fmt.Sprintf("%s\n", member.Name))
		if err != nil {
			return err
		}
	}
	return nil
}
