package main

import (
	"fmt"
	"os"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	kubecmd "k8s.io/kubectl/pkg/cmd"

	"github.com/openshift/oc/pkg/cli"
	"github.com/openshift/oc/tools/clicheck/sanity"
)

func main() {
	oc := cli.NewOcCommand(kubecmd.KubectlOptions{IOStreams: genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}})
	errors := sanity.CheckCmdTree(oc, sanity.AllCmdChecks, nil)
	if len(errors) > 0 {
		for i, err := range errors {
			fmt.Fprintf(os.Stderr, "%d. %s\n\n", i+1, err)
		}
		os.Exit(1)
	}

	fmt.Fprintln(os.Stdout, "Congrats, CLI looks good!")
}
