package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/openshift/oc/pkg/cli"
	"github.com/openshift/oc/tools/gendocs/gendocs"
)

var admin = flag.Bool("admin", false, "Generate admin commands docs")

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
	cmd := cli.NewOcCommand(&bytes.Buffer{}, out, ioutil.Discard)

	outFile := filepath.Join(outDir, "oc-by-example-content.adoc")
	if *admin {
		outFile = filepath.Join(outDir, "oc-adm-by-example-content.adoc")
	}

	if err := gendocs.GenDocs(cmd, outFile, *admin); err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate docs: %v\n", err)
		os.Exit(1)
	}
}
