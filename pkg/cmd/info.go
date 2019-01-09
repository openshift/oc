package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericclioptions/resource"

	configv1 "github.com/openshift/api/config/v1"
)

var (
	infoExample = `
	# Collect debugging data for the "openshift-apiserver-operator"
	%[1]s info clusteroperator/openshift-apiserver-operator

	# Collect debugging data for all clusteroperators
	%[1]s info clusteroperator
`
)

type InfoOptions struct {
	configFlags *genericclioptions.ConfigFlags

	builder *resource.Builder
	args    []string

	genericclioptions.IOStreams
}

func NewInfoOptions(streams genericclioptions.IOStreams) *InfoOptions {
	return &InfoOptions{
		configFlags: genericclioptions.NewConfigFlags(),
		IOStreams:   streams,
	}
}

func NewCmdInfo(parentName string, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewInfoOptions(streams)

	cmd := &cobra.Command{
		Use:          "info <operator> [flags]",
		Short:        "Gather debugging data for a given cluster operator",
		Example:      fmt.Sprintf(infoExample, parentName),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(c, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			if err := o.Run(); err != nil {
				return err
			}

			return nil
		},
	}

	return cmd
}

func (o *InfoOptions) Complete(cmd *cobra.Command, args []string) error {
	o.args = args

	o.configFlags.ToRESTConfig()
	o.builder = resource.NewBuilder(o.configFlags)

	return nil
}

func (o *InfoOptions) Validate() error {
	if len(o.args) != 1 {
		return fmt.Errorf("exactly 1 argument (operator name) is supported")
	}
	return nil
}

func (o *InfoOptions) Run() error {
	r := o.builder.
		Unstructured().
		ResourceTypeOrNameArgs(true, o.args...).
		Flatten().
		Latest().Do()

	infos, err := r.Infos()
	if err != nil {
		return err
	}

	for _, info := range infos {
		if configv1.GroupName != info.Mapping.GroupVersionKind.Group {
			return fmt.Errorf("unexpected resource API group %q. Expected %q", info.Mapping.GroupVersionKind.Group, configv1.GroupName)
		}
		if info.Mapping.Resource.Resource != "clusteroperators" {
			return fmt.Errorf("unsupported resource type, must be %q", "clusteroperators")
		}

	}

	return nil
}
