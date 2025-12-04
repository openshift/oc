package main

import (
	"context"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/component-base/cli"

	otecmd "github.com/openshift-eng/openshift-tests-extension/pkg/cmd"
	oteextension "github.com/openshift-eng/openshift-tests-extension/pkg/extension"
	"github.com/openshift/oc/pkg/version"

	"k8s.io/klog/v2"
)

func main() {
	command := newOperatorTestCommand(context.Background())
	code := cli.Run(command)
	os.Exit(code)
}

func newOperatorTestCommand(ctx context.Context) *cobra.Command {
	registry := prepareOperatorTestsRegistry()

	cmd := &cobra.Command{
		Use:   "oc-tests-ext",
		Short: "A binary used to run oc tests as part of OTE.",
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.Help(); err != nil {
				klog.Fatal(err)
			}
		},
	}

	if v := version.Get().String(); len(v) == 0 {
		cmd.Version = "<unknown>"
	} else {
		cmd.Version = v
	}

	cmd.AddCommand(otecmd.DefaultExtensionCommands(registry)...)

	return cmd
}

func prepareOperatorTestsRegistry() *oteextension.Registry {
	registry := oteextension.NewRegistry()
	extension := oteextension.NewExtension("openshift", "payload", "oc")

	registry.Register(extension)
	return registry
}
