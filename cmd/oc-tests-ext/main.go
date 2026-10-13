/*
This command is used to run the oc tests extension for OpenShift.
It registers the oc tests with the OpenShift Tests Extension framework
and provides a command-line interface to execute them.
For further information, please refer to the documentation at:
https://github.com/openshift-eng/openshift-tests-extension/blob/main/cmd/example-tests/main.go
*/
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/component-base/cli"

	otecmd "github.com/openshift-eng/openshift-tests-extension/pkg/cmd"
	oteextension "github.com/openshift-eng/openshift-tests-extension/pkg/extension"
	oteginkgo "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"
	"github.com/openshift/oc/pkg/version"

	"k8s.io/klog/v2"
)

func main() {
	cmd, err := newOperatorTestCommand()
	if err != nil {
		klog.Fatal(err)
	}

	code := cli.Run(cmd)
	os.Exit(code)
}

func newOperatorTestCommand() (*cobra.Command, error) {
	registry, err := prepareOperatorTestsRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to prepare test registry: %w", err)
	}

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

	return cmd, nil
}

func prepareOperatorTestsRegistry() (*oteextension.Registry, error) {
	registry := oteextension.NewRegistry()
	extension := oteextension.NewExtension("openshift", "payload", "oc")

	// The following suite runs tests that verify the oc's behaviour.
	// This suite is executed only on pull requests targeting this repository.
	// Tests tagged with [Serial] are included in this suite.
	extension.AddSuite(oteextension.Suite{
		Name:        "openshift/oc/conformance/serial",
		Parents:     []string{"openshift/conformance/serial"},
		Parallelism: 1,
		Qualifiers: []string{
			`name.contains("[Serial]")`,
		},
	})

	// The following suite runs tests that verify the oc's behaviour.
	// This suite is executed only on pull requests targeting this repository.
	// Parallel tests are included in this suite.
	extension.AddSuite(oteextension.Suite{
		Name:        "openshift/oc/conformance/parallel",
		Parents:     []string{"openshift/conformance/parallel"},
		Parallelism: 1,
		Qualifiers: []string{
			`!name.contains("[Serial]")`,
		},
	})

	// Build OTE specs from Ginkgo tests
	specs, err := oteginkgo.BuildExtensionTestSpecsFromOpenShiftGinkgoSuite()
	if err != nil {
		return nil, fmt.Errorf("couldn't build extension test specs from ginkgo: %w", err)
	}

	extension.AddSpecs(specs)
	registry.Register(extension)
	return registry, nil
}
