package icsp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	yaml "sigs.k8s.io/yaml/goyaml.v3"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/errors"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	apicfgv1 "github.com/openshift/api/config/v1"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	operatorv1alpha1scheme "github.com/openshift/client-go/operator/clientset/versioned/scheme"
)

var (
	internalMigrateICSPLong = templates.LongDesc(`
	Update imagecontentsourcepolicy file(s) to imagedigestmirrorset file(s). If --dest-dir is unset, the imagedigestmirrorset file(s) that can be added to a cluster will be written to file(s) under the current directory.
	`)

	internalMigrateICSPExample = templates.Examples(`
	# Update the imagecontentsourcepolicy.yaml file to a new imagedigestmirrorset file under the mydir directory
	oc adm migrate icsp imagecontentsourcepolicy.yaml --dest-dir mydir
`)
)

type MigrateICSPOptions struct {
	genericiooptions.IOStreams

	ICSPFiles []string
	DestDir   string
}

func NewCmdMigrateICSP(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewMigrateICSPOptions(streams)
	cmd := &cobra.Command{
		Use:     "icsp",
		Short:   "Update imagecontentsourcepolicy file(s) to imagedigestmirrorset file(s)",
		Long:    internalMigrateICSPLong,
		Example: internalMigrateICSPExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run())
		},
	}
	cmd.Flags().StringVar(&o.DestDir, "dest-dir", o.DestDir, "Set a specific directory on the local machine to write imagedigestmirrorset file(s) to.")

	return cmd
}

func NewMigrateICSPOptions(streams genericiooptions.IOStreams) *MigrateICSPOptions {
	return &MigrateICSPOptions{
		IOStreams: streams,
	}
}

func (o *MigrateICSPOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("convert expects at least one argument, path to an imagecontentsourcepolicy file")
	}
	o.ICSPFiles = args
	return nil
}

func (o *MigrateICSPOptions) Run() error {
	if o.DestDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		o.DestDir = cwd
	}
	if err := o.ensureDirectoryViable(); err != nil {
		return err
	}
	// ensure destination path exists
	if err := os.MkdirAll(o.DestDir, os.ModePerm); err != nil {
		return err
	}

	var multiErr []error
	for _, file := range o.ICSPFiles {
		if err := func() error {
			icsps, name, err := readICSPsFromFile(file)
			if err != nil {
				return err
			}
			idmsYml, err := generateIDMS(icsps)
			if err != nil {
				return err
			}

			fname := filepath.Join(o.DestDir, fmt.Sprintf("imagedigestmirrorset_%s.%06d.yaml", name, rand.Int63()))
			if err = os.WriteFile(fname, idmsYml, os.ModePerm); err != nil {
				defer func() {
					os.Remove(fname)
				}()
				return fmt.Errorf("error writing ImageDigestMirrorSet: %v", err)
			}
			fmt.Fprintf(o.Out, "wrote ImageDigestMirrorSet to %s\n", fname)
			return nil
		}(); err != nil {
			multiErr = append(multiErr, err)
			continue
		}
	}
	return errors.NewAggregate(multiErr)
}

// ensureDirectoryViable returns an error if DestDir:
// 1. already exists AND is a file (not a directory)
// 2. an IO error occurs
func (o *MigrateICSPOptions) ensureDirectoryViable() error {
	baseDirInfo, err := os.Stat(o.DestDir)
	if err != nil && os.IsNotExist(err) {
		// no error, directory simply does not exist yet
		return nil
	}
	if err != nil {
		return err
	}

	if !baseDirInfo.IsDir() {
		return fmt.Errorf("%q exists and is a file", o.DestDir)
	}
	if _, err = os.ReadDir(o.DestDir); err != nil {
		return err
	}
	return nil
}

func generateIDMS(icsps []operatorv1alpha1.ImageContentSourcePolicy) ([]byte, error) {
	var idmsYml []byte
	const yamlSeparator = "---\n"

	for i, icsp := range icsps {
		imgDigestMirrors := []apicfgv1.ImageDigestMirrors{}
		for _, rdm := range icsp.Spec.RepositoryDigestMirrors {
			idm := apicfgv1.ImageDigestMirrors{}
			idm.Source = rdm.Source
			mirrors := []apicfgv1.ImageMirror{}
			for _, m := range rdm.Mirrors {
				mirrors = append(mirrors, apicfgv1.ImageMirror(m))
			}
			idm.Mirrors = mirrors
			imgDigestMirrors = append(imgDigestMirrors, idm)
		}
		idms := apicfgv1.ImageDigestMirrorSet{
			TypeMeta: metav1.TypeMeta{
				APIVersion: apicfgv1.GroupVersion.String(),
				Kind:       "ImageDigestMirrorSet",
			},
			ObjectMeta: icsp.ObjectMeta,
			Spec:       apicfgv1.ImageDigestMirrorSetSpec{ImageDigestMirrors: imgDigestMirrors},
		}

		// Create an unstructured object for removing creationTimestamp, status
		unstructuredObj := unstructured.Unstructured{}
		var err error
		unstructuredObj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(&idms)
		if err != nil {
			return nil, fmt.Errorf("error converting to unstructured: %v", err)
		}
		delete(unstructuredObj.Object["metadata"].(map[string]interface{}), "creationTimestamp")
		delete(unstructuredObj.Object, "status")
		idmsBytes, err := yaml.Marshal(unstructuredObj.Object)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal ImageDigestMirrorSet yaml: %v", err)
		}
		if len(icsps) > 1 && i != len(icsps)-1 {
			idmsBytes = append(idmsBytes, []byte(yamlSeparator)...)
		}
		idmsYml = append(idmsYml, idmsBytes...)
	}
	return idmsYml, nil
}

// readICSPsFromFile appends to list of alternative image sources from ICSP file
// returns error if no icsp object decoded from file data
func readICSPsFromFile(icspFile string) ([]operatorv1alpha1.ImageContentSourcePolicy, string, error) {
	icspData, err := os.ReadFile(icspFile)
	if err != nil {
		return []operatorv1alpha1.ImageContentSourcePolicy{}, "", fmt.Errorf("unable to read ImageContentSourceFile %s: %v", icspFile, err)
	}
	if len(icspData) == 0 {
		return []operatorv1alpha1.ImageContentSourcePolicy{}, "", fmt.Errorf("no data found in ImageContentSourceFile %s", icspFile)
	}

	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(icspData)))
	var icsps []operatorv1alpha1.ImageContentSourcePolicy
	for {
		icspBytes, err := reader.Read()
		if err != nil && err != io.EOF {
			return []operatorv1alpha1.ImageContentSourcePolicy{}, "", fmt.Errorf("error reading ImageContentSourcePolicy from %s: %v", icspFile, err)
		}
		if icspBytes == nil {
			break
		}
		icspObj, err := runtime.Decode(operatorv1alpha1scheme.Codecs.UniversalDeserializer(), icspBytes)
		if err != nil {
			return []operatorv1alpha1.ImageContentSourcePolicy{}, "", fmt.Errorf("error decoding ImageContentSourcePolicy from %s: %v", icspFile, err)
		}
		icsp, ok := icspObj.(*operatorv1alpha1.ImageContentSourcePolicy)
		if !ok {
			return []operatorv1alpha1.ImageContentSourcePolicy{}, "", fmt.Errorf("could not decode ImageContentSourcePolicy from %s", icspFile)
		}
		icsps = append(icsps, *icsp)
	}
	return icsps, icsps[0].Name, nil
}
