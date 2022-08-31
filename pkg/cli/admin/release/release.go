package release

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

func NewCmd(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release",
		Short: "Tools for managing the OpenShift release process",
		Long: templates.LongDesc(`
			This tool is used by OpenShift release to build images that can update a cluster.

			The subcommands allow you to see information about releases, perform administrative
			actions inspect the content of the release, and mirror release content across image
			registries.
			`),
	}
	cmd.AddCommand(NewInfo(f, streams))
	cmd.AddCommand(NewRelease(f, streams))
	cmd.AddCommand(NewExtract(f, streams))
	cmd.AddCommand(NewMirror(f, streams))
	return cmd
}

func (o *ArchOptions) Bind(flags *pflag.FlagSet) {
	flags.StringVar(&o.Arch, "arch", o.Arch, "Specify a release's architecture (amd64|arm64|ppc64le|s390x). When used, it will bypass searching the current connected cluster for the release payload.")
}

type ArchOptions struct {
	Arch string
}

// Validating Arch, if --arch is passed
func (o *ArchOptions) Validate() error {
	if len(o.Arch) > 0 {
		switch o.Arch {
		case "", "amd64", "arm64", "ppc64le", "s390x":
			return nil
		}
		return fmt.Errorf("%s is not a valid arch", o.Arch)
	}
	return nil
}
