package cert

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"unicode"
	"unicode/utf8"

	"github.com/openshift/library-go/pkg/certs"

	"github.com/openshift/oc/pkg/helpers/term"
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

const EncryptCommandName = "encrypt"

type EncryptOptions struct {
	// CleartextFile contains cleartext data to encrypt.
	CleartextFile string
	// CleartextData is cleartext data to encrypt.
	CleartextData []byte
	// CleartextReader reads cleartext data to encrypt if CleartextReader and CleartextFile are unspecified.
	CleartextReader io.Reader

	// EncryptedFile has encrypted data written to it.
	EncryptedFile string
	// EncryptedWriter has encrypted data written to it if EncryptedFile is unspecified.
	EncryptedWriter io.Writer

	// KeyFile contains the password in PEM format (as previously written by GenKeyFile)
	KeyFile string
	// GenKeyFile indicates a key should be generated and written
	GenKeyFile string

	// PromptWriter is used to write status and prompt messages
	PromptWriter io.Writer
}

var encryptExample = templates.Examples(`
	# Encrypt the content of secret.txt with a generated key:
	%[1]s --genkey=secret.key --in=secret.txt --out=secret.encrypted

	# Encrypt the content of secret2.txt with an existing key:
	%[1]s --key=secret.key < secret2.txt > secret2.encrypted`)

func NewEncryptOptions(streams genericclioptions.IOStreams) *EncryptOptions {
	return &EncryptOptions{
		CleartextReader: streams.In,
		EncryptedWriter: streams.Out,
		PromptWriter:    streams.ErrOut,
	}
}

func NewCommandEncrypt(commandName string, fullName string, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewEncryptOptions(streams)
	cmd := &cobra.Command{
		Use:     commandName,
		Short:   "Encrypt data with AES-256-CBC encryption",
		Example: fmt.Sprintf(encryptExample, fullName),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Validate(args))
			kcmdutil.CheckErr(o.Encrypt())
		},
	}

	cmd.Flags().StringVar(&o.CleartextFile, "in", o.CleartextFile, "File containing the data to encrypt. Read from stdin if omitted.")
	cmd.Flags().StringVar(&o.EncryptedFile, "out", o.EncryptedFile, "File to write the encrypted data to. Written to stdout if omitted.")

	cmd.Flags().StringVar(&o.KeyFile, "key", o.KeyFile, "File containing the encrypting key from in the format written by --genkey.")
	cmd.Flags().StringVar(&o.GenKeyFile, "genkey", o.GenKeyFile, "File to write a randomly generated key to.")

	// autocompletion hints
	cmd.MarkFlagFilename("in")
	cmd.MarkFlagFilename("out")
	cmd.MarkFlagFilename("key")
	cmd.MarkFlagFilename("genkey")

	return cmd
}

func (o *EncryptOptions) Validate(args []string) error {
	if len(args) != 0 {
		return errors.New("no arguments are supported")
	}

	if len(o.CleartextFile) == 0 && len(o.CleartextData) == 0 && o.CleartextReader == nil {
		return errors.New("an input file, data, or reader is required")
	}
	if len(o.CleartextFile) > 0 && len(o.CleartextData) > 0 {
		return errors.New("cannot specify both an input file and data")
	}

	if len(o.EncryptedFile) == 0 && o.EncryptedWriter == nil {
		return errors.New("an output file or writer is required")
	}

	if len(o.GenKeyFile) > 0 && len(o.KeyFile) > 0 {
		return errors.New("either --genkey or --key may be specified, not both")
	}
	if len(o.GenKeyFile) == 0 && len(o.KeyFile) == 0 {
		return errors.New("--genkey or --key is required")
	}

	return nil
}

func (o *EncryptOptions) Encrypt() error {
	// Get data
	var data []byte
	var warnWhitespace = true
	switch {
	case len(o.CleartextFile) > 0:
		if d, err := ioutil.ReadFile(o.CleartextFile); err != nil {
			return err
		} else {
			data = d
		}
	case len(o.CleartextData) > 0:
		// Don't warn in cases where we're explicitly being given the data to use
		warnWhitespace = false
		data = o.CleartextData
	case o.CleartextReader != nil && term.IsTerminalReader(o.CleartextReader) && o.PromptWriter != nil:
		// Read a single line from stdin with prompting
		data = []byte(term.PromptForString(o.CleartextReader, o.PromptWriter, "Data to encrypt: "))
	case o.CleartextReader != nil:
		// Read data from stdin without prompting (allows binary data and piping)
		if d, err := ioutil.ReadAll(o.CleartextReader); err != nil {
			return err
		} else {
			data = d
		}
	}
	if warnWhitespace && (o.PromptWriter != nil) && (len(data) > 0) {
		r1, _ := utf8.DecodeRune(data)
		r2, _ := utf8.DecodeLastRune(data)
		if unicode.IsSpace(r1) || unicode.IsSpace(r2) {
			fmt.Fprintln(o.PromptWriter, "Warning: Data includes leading or trailing whitespace, which will be included in the encrypted value")
		}
	}

	// Get key
	var key []byte
	switch {
	case len(o.KeyFile) > 0:
		if block, ok, err := certs.BlockFromFile(o.KeyFile, certs.StringSourceKeyBlockType); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("%s does not contain a valid PEM block of type %q", o.KeyFile, certs.StringSourceKeyBlockType)
		} else if len(block.Bytes) == 0 {
			return fmt.Errorf("%s does not contain a key", o.KeyFile)
		} else {
			key = block.Bytes
		}
	case len(o.GenKeyFile) > 0:
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return err
		}
	}
	if len(key) == 0 {
		return errors.New("--genkey or --key is required")
	}

	// Encrypt
	dataBlock, err := x509.EncryptPEMBlock(rand.Reader, certs.StringSourceEncryptedBlockType, data, key, x509.PEMCipherAES256)
	if err != nil {
		return err
	}

	// Write data
	if len(o.EncryptedFile) > 0 {
		if err := certs.BlockToFile(o.EncryptedFile, dataBlock, os.FileMode(0644)); err != nil {
			return err
		}
	} else if o.EncryptedWriter != nil {
		encryptedBytes, err := certs.BlockToBytes(dataBlock)
		if err != nil {
			return err
		}
		n, err := o.EncryptedWriter.Write(encryptedBytes)
		if err != nil {
			return err
		}
		if n != len(encryptedBytes) {
			return fmt.Errorf("could not completely write encrypted data")
		}
	}

	// Write key
	if len(o.GenKeyFile) > 0 {
		keyBlock := &pem.Block{Bytes: key, Type: certs.StringSourceKeyBlockType}
		if err := certs.BlockToFile(o.GenKeyFile, keyBlock, os.FileMode(0600)); err != nil {
			return err
		}
	}

	return nil
}
