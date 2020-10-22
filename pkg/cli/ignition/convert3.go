package ignition

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/coreos/ign-converter/translate/v24tov31"
	ignv2 "github.com/coreos/ignition/config/v2_4"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

const convert3CommandName = "convert3"

type convert3Options struct {
	// source file
	source string
	// destination file
	destination string

	sourceReader io.Reader
	destWriter   io.Writer
}

var convert3Example = templates.Examples(`
	# Convert an Ignition Spec 2 pointer configuration to Spec 3
	oc ignition convert3 < worker.ign > worker.ign3
`)

// NewCommandConvert3 creates the command
func NewCommandConvert3(streams genericclioptions.IOStreams) *cobra.Command {
	o := convert3Options{
		sourceReader: streams.In,
		destWriter:   streams.Out,
	}
	cmd := &cobra.Command{
		Use:     convert3CommandName,
		Short:   "Convert Ignition Spec 2 to Spec 3",
		Example: convert3Example,
		Args:    cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.validate(args))
			kcmdutil.CheckErr(o.run())
		},
	}

	cmd.Flags().StringVar(&o.source, "in", o.source, "File containing input Ignition to convert. Read from stdin if omitted.")
	cmd.Flags().StringVar(&o.destination, "out", o.destination, "Path to file for converted Ignition. Written to stdout if omitted.")
	// autocompletion hints
	cmd.MarkFlagFilename("in")
	cmd.MarkFlagFilename("out")

	return cmd
}

func (o *convert3Options) validate(args []string) error {
	if len(o.source) == 0 && o.sourceReader == nil {
		return errors.New("an input file, or reader is required")
	}

	if len(o.destination) == 0 && o.destWriter == nil {
		return errors.New("an output file or writer is required")
	}
	return nil
}

func (o *convert3Options) run() error {
	var data []byte
	switch {
	case len(o.source) > 0:
		d, err := ioutil.ReadFile(o.source)
		if err != nil {
			return err
		}
		data = d
	case o.sourceReader != nil:
		d, err := ioutil.ReadAll(o.sourceReader)
		if err != nil {
			return err
		}
		data = d
	}

	cfg, rpt, err := ignv2.Parse(data)
	fmt.Fprintf(os.Stderr, "%s", rpt.String())
	if err != nil || rpt.IsFatal() {
		return errors.Errorf("Error parsing spec v2 config: %v\n%v", err, rpt)
	}

	newCfg, err := v24tov31.Translate(cfg, nil)
	if err != nil {
		return errors.Wrap(err, "translation failed")
	}
	dataOut, err := json.Marshal(newCfg)
	if err != nil {
		return errors.Wrap(err, "failed to marshal JSON")
	}

	// Write data
	if len(o.destination) > 0 {
		return ioutil.WriteFile(o.destination, dataOut, 0644)
	} else if o.destWriter != nil {
		_, err := o.destWriter.Write(dataOut)
		if err != nil {
			return err
		}
	}
	return nil
}
