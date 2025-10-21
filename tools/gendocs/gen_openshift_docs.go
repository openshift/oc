package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	kubecmd "k8s.io/kubectl/pkg/cmd"

	"github.com/openshift/oc/pkg/cli"
	"github.com/openshift/oc/tools/gendocs/gendocs"
)

var admin = flag.Bool("admin", false, "Generate admin commands docs")
var microshift = flag.Bool("microshift", false, "Generate MicroShift commands docs")

func OutDir(path string) (string, error) {
	outDir, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	stat, err := os.Stat(outDir)
	if err != nil {
		return "", err
	}

	if !stat.IsDir() {
		return "", fmt.Errorf("output directory %s is not a directory\n", outDir)
	}
	outDir = outDir + "/"
	return outDir, nil
}

func main() {
	path := "docs/generated/"
	flag.Parse()
	if flag.NArg() == 1 {
		path = flag.Arg(0)
	} else if flag.NArg() > 1 {
		fmt.Fprintf(os.Stderr, "usage: %s [output directory]\n", os.Args[0])
		os.Exit(1)
	}

	outDir, err := OutDir(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get output directory: %v\n", err)
		os.Exit(1)
	}

	out := os.Stdout
	cmd := cli.NewOcCommand(kubecmd.KubectlOptions{IOStreams: genericiooptions.IOStreams{In: &bytes.Buffer{}, Out: out, ErrOut: io.Discard}})

	fileName := "oc-by-example-content.adoc"
	if *admin {
		fileName = strings.Replace(fileName, "oc", "oc-adm", 1)
	}
	if *microshift {
		fileName = "microshift-" + fileName
	}
	outFile := filepath.Join(outDir, fileName)

	if err := gendocs.GenDocs(cmd, outFile, *admin, *microshift); err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate docs: %v\n", err)
		os.Exit(1)
	}
}
