package catalog

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/klog"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
	"sigs.k8s.io/yaml"

	"github.com/alicebob/sqlittle"
	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/opencontainers/go-digest"
	"github.com/spf13/cobra"

	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	imgextract "github.com/openshift/oc/pkg/cli/image/extract"
	"github.com/openshift/oc/pkg/cli/image/imagesource"
	"github.com/openshift/oc/pkg/cli/image/info"
	imagemanifest "github.com/openshift/oc/pkg/cli/image/manifest"
	imgmirror "github.com/openshift/oc/pkg/cli/image/mirror"
)

var (
	mirrorLong = templates.LongDesc(`
			Mirrors the contents of a catalog into a registry.

			This command will pull down an image containing a catalog database, extract it to disk, query it to find
			all of the images used in the manifests, and then mirror them to a target registry.

			By default, the database is extracted to a temporary directory, but can be saved locally via flags.

			An ImageContentSourcePolicy is written to a file that can be adedd to a cluster with access to the target 
			registry. This will configure the cluster to pull from the mirrors instead of the locations referenced in
			the operator manifests.

			A mapping.txt file is also created that is compatible with "oc image mirror". This may be used to further
			customize the mirroring configuration, but should not be needed in normal circumstances.
		`)
	mirrorExample = templates.Examples(`
# Mirror an operator-registry image and its contents to a registry
%[1]s quay.io/my/image:latest myregistry.com

# Mirror an operator-registry image and its contents to a particular namespace in a registry
%[1]s quay.io/my/image:latest myregistry.com/my-namespace

# Configure a cluster to use a mirrored registry
oc apply -f manifests/imageContentSourcePolicy.yaml

# Edit the mirroring mappings and mirror with "oc image mirror" manually
%[1]s --manifests-only quay.io/my/image:latest myregistry.com
oc image mirror -f manifests/mapping.txt
`)
)

const IndexLocationLabelKey = "operators.operatorframework.io.index.database.v1"

func init() {
	subCommands = append(subCommands, NewMirrorCatalog)
}

type MirrorCatalogOptions struct {
	*IndexImageMirrorerOptions
	genericclioptions.IOStreams

	DryRun       bool
	ManifestOnly bool
	DatabasePath string

	FromFileDir string
	FileDir     string

	SecurityOptions imagemanifest.SecurityOptions
	FilterOptions   imagemanifest.FilterOptions
	ParallelOptions imagemanifest.ParallelOptions

	SourceRef imagesource.TypedImageReference
	DestRef   imagesource.TypedImageReference
}

func NewMirrorCatalogOptions(streams genericclioptions.IOStreams) *MirrorCatalogOptions {
	return &MirrorCatalogOptions{
		IOStreams:                 streams,
		IndexImageMirrorerOptions: DefaultImageIndexMirrorerOptions(),
		ParallelOptions:           imagemanifest.ParallelOptions{MaxPerRegistry: 4},
	}
}

func NewMirrorCatalog(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewMirrorCatalogOptions(streams)

	cmd := &cobra.Command{
		Use:     "mirror SRC DEST",
		Short:   "mirror an operator-registry catalog",
		Long:    mirrorLong,
		Example: fmt.Sprintf(mirrorExample, "oc adm catalog mirror"),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}
	flags := cmd.Flags()

	o.SecurityOptions.Bind(flags)
	o.FilterOptions.Bind(flags)
	o.ParallelOptions.Bind(flags)

	flags.StringVar(&o.ManifestDir, "to-manifests", "", "Local path to store manifests.")
	flags.StringVar(&o.DatabasePath, "path", "", "Specify an in-container to local path mapping for the database.")
	flags.BoolVar(&o.DryRun, "dry-run", o.DryRun, "Print the actions that would be taken and exit without writing to the destinations.")
	flags.BoolVar(&o.ManifestOnly, "manifests-only", o.ManifestOnly, "Calculate the manifests required for mirroring, but do not actually mirror image content.")
	flags.StringVar(&o.FileDir, "dir", o.FileDir, "The directory on disk that file:// images will be copied under.")
	flags.StringVar(&o.FromFileDir, "from-dir", o.FromFileDir, "The directory on disk that file:// images will be read from. Overrides --dir")
	flags.IntVar(&o.MaxPathComponents, "max-components", 2, "The maximum number of path components allowed in a destination mapping. Example: `quay.io/org/repo` has two path components.")
	return cmd
}

func (o *MirrorCatalogOptions) Complete(cmd *cobra.Command, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("must specify source and dest")
	}
	src := args[0]
	dest := args[1]

	srcRef, err := imagesource.ParseReference(src)
	if err != nil {
		return err
	}
	o.SourceRef = srcRef
	destRef, err := imagesource.ParseReference(dest)
	if err != nil {
		return err
	}
	o.DestRef = destRef

	if o.ManifestDir == "" {
		o.ManifestDir = o.SourceRef.Ref.Name + "-manifests"
	}

	if err := os.MkdirAll(o.ManifestDir, os.ModePerm); err != nil {
		return err
	}

	// try to get the index db location label from src, from pkg/image/info
	var image *info.Image
	retriever := &info.ImageRetriever{
		FileDir: o.FileDir,
		Image: map[string]imagesource.TypedImageReference{
			src: srcRef,
		},
		SecurityOptions: o.SecurityOptions,
		ManifestListCallback: func(from string, list *manifestlist.DeserializedManifestList, all map[digest.Digest]distribution.Manifest) (map[digest.Digest]distribution.Manifest, error) {
			filtered := make(map[digest.Digest]distribution.Manifest)
			for _, manifest := range list.Manifests {
				if !o.FilterOptions.Include(&manifest, len(list.Manifests) > 1) {
					klog.V(5).Infof("Skipping image for %#v from %s", manifest.Platform, from)
					continue
				}
				filtered[manifest.Digest] = all[manifest.Digest]
			}
			if len(filtered) == 1 {
				return filtered, nil
			}

			buf := &bytes.Buffer{}
			w := tabwriter.NewWriter(buf, 0, 0, 1, ' ', 0)
			fmt.Fprintf(w, "  OS\tDIGEST\n")
			for _, manifest := range list.Manifests {
				fmt.Fprintf(w, "  %s\t%s\n", imagemanifest.PlatformSpecString(manifest.Platform), manifest.Digest)
			}
			w.Flush()
			return nil, fmt.Errorf("the image is a manifest list and contains multiple images - use --filter-by-os to select from:\n\n%s\n", buf.String())
		},

		ImageMetadataCallback: func(from string, i *info.Image, err error) error {
			if err != nil {
				return err
			}
			image = i
			return nil
		},
	}
	if err := retriever.Run(); err != nil {
		return err
	}
	indexLocation, ok := image.Config.Config.Labels[IndexLocationLabelKey]
	if ok {
		fmt.Fprintf(o.IOStreams.Out, "src image has index label for database path: %s\n", indexLocation)
	} else {
		indexLocation = "/"
	}

	if o.DatabasePath == "" {
		tmpdir, err := ioutil.TempDir("", "")
		if err != nil {
			return err
		}
		o.DatabasePath = indexLocation + ":" + tmpdir
	} else {
		dir := strings.Split(o.DatabasePath, ":")
		if len(dir) < 2 {
			return fmt.Errorf("invalid path")
		}
		if err := os.MkdirAll(filepath.Dir(dir[1]), os.ModePerm); err != nil {
			return err
		}
	}
	fmt.Fprintf(o.IOStreams.Out, "using database path mapping: %s\n", o.DatabasePath)

	var mirrorer ImageMirrorerFunc
	mirrorer = func(mapping map[string]Target) error {
		for from, to := range mapping {
			fromRef, err := imagesource.ParseReference(from)
			if err != nil {
				fmt.Fprintf(o.IOStreams.ErrOut, "couldn't parse %s, skipping mirror: %v\n", from, err)
				continue
			}

			// Mirroring happens with a tag so that the images are not GCd from the target registry
			toRef, err := imagesource.ParseDestinationReference(to.WithTag)
			if err != nil {
				fmt.Fprintf(o.IOStreams.ErrOut, "couldn't parse %s, skipping mirror: %v\n", to, err)
				continue
			}

			a := imgmirror.NewMirrorImageOptions(o.IOStreams)
			a.SkipMissing = true
			a.DryRun = o.DryRun
			a.SecurityOptions = o.SecurityOptions
			a.FilterOptions = o.FilterOptions
			a.ParallelOptions = o.ParallelOptions
			a.KeepManifestList = true
			a.Mappings = []imgmirror.Mapping{{
				Source:      fromRef,
				Destination: toRef,
			}}
			if err := a.Validate(); err != nil {
				fmt.Fprintf(o.IOStreams.ErrOut, "error configuring image mirroring: %v\n", err)
			}
			if err := a.Run(); err != nil {
				fmt.Fprintf(o.IOStreams.ErrOut, "error mirroring image: %v\n", err)
			}
		}
		return nil
	}

	if o.ManifestOnly {
		mirrorer = func(mapping map[string]Target) error {
			return nil
		}
	}
	o.ImageMirrorer = mirrorer

	var extractor DatabaseExtractorFunc = func(from imagesource.TypedImageReference) (string, error) {
		e := imgextract.NewOptions(o.IOStreams)
		e.SecurityOptions = o.SecurityOptions
		e.FilterOptions = o.FilterOptions
		e.ParallelOptions = o.ParallelOptions
		e.FileDir = o.FileDir
		if len(o.FromFileDir) > 0 {
			e.FileDir = o.FromFileDir
		}
		e.Paths = []string{o.DatabasePath}
		e.Confirm = true
		if err := e.Complete(cmd, []string{o.SourceRef.String()}); err != nil {
			return "", err
		}
		if err := e.Validate(); err != nil {
			return "", err
		}
		if err := e.Run(); err != nil {
			return "", err
		}
		if len(e.Mappings) < 1 {
			return "", fmt.Errorf("couldn't extract database")
		}

		fmt.Fprintf(o.IOStreams.Out, "wrote database to %s\n", e.Mappings[0].To)
		var dbPath string
		errFound := fmt.Errorf("found valid db file")
		err := filepath.Walk(e.Mappings[0].To, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}
			if _, err := sqlittle.Open(path); err == nil {
				dbPath = path
				return errFound
			}
			return nil
		})
		if err == errFound {
			fmt.Fprintf(o.IOStreams.Out, "using database at: %s\n", dbPath)
			return dbPath, nil
		}
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("no database file found in %s", e.Mappings[0].To)
	}
	o.DatabaseExtractor = extractor
	return nil
}

func (o *MirrorCatalogOptions) Validate() error {
	if o.DatabasePath == "" {
		return fmt.Errorf("must specify path for database")
	}
	if o.ManifestDir == "" {
		return fmt.Errorf("must specify path for manifests")
	}
	return nil
}

func (o *MirrorCatalogOptions) Run() error {
	indexMirrorer, err := NewIndexImageMirror(o.IndexImageMirrorerOptions.ToOption(),
		WithSource(o.SourceRef),
		WithDest(o.DestRef),
	)
	if err != nil {
		return err
	}
	mapping, err := indexMirrorer.Mirror()
	if err != nil {
		fmt.Fprintf(o.IOStreams.ErrOut, "errors during mirroring. the full contents of the catalog may not have been mirrored: %s\n", err.Error())
	}

	return WriteManifests(o.IOStreams.Out, o.SourceRef.Ref.Name, o.ManifestDir, mapping)
}

func WriteManifests(out io.Writer, name, dir string, mapping map[string]Target) error {
	f, err := os.Create(filepath.Join(dir, "mapping.txt"))
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Fprintf(out, "error closing file\n")
		}
	}()

	if err := writeToMapping(f, mapping); err != nil {
		return err
	}

	icsp, err := generateICSP(out, name, mapping)
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(filepath.Join(dir, "imageContentSourcePolicy.yaml"), icsp, os.ModePerm); err != nil {
		return fmt.Errorf("error writing ImageContentSourcePolicy")
	}
	fmt.Fprintf(out, "wrote mirroring manifests to %s\n", dir)
	return nil
}

func writeToMapping(w io.StringWriter, mapping map[string]Target) error {
	for k, v := range mapping {
		if _, err := w.WriteString(fmt.Sprintf("%s=%s\n", k, v.WithTag)); err != nil {
			return err
		}
	}

	return nil
}

func generateICSP(out io.Writer, name string, mapping map[string]Target) ([]byte, error) {
	icsp := operatorv1alpha1.ImageContentSourcePolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: operatorv1alpha1.GroupVersion.String(),
			Kind:       "ImageContentSourcePolicy"},
		ObjectMeta: metav1.ObjectMeta{
			Name: strings.Join(strings.Split(name, "/"), "-"),
		},
		Spec: operatorv1alpha1.ImageContentSourcePolicySpec{
			RepositoryDigestMirrors: []operatorv1alpha1.RepositoryDigestMirrors{},
		},
	}

	for k, v := range mapping {
		if len(v.WithDigest) == 0 {
			fmt.Fprintf(out, "no digest mapping available for %s, skip writing to ImageContentSourcePolicy\n", k)
			continue
		}
		toRef, err := imagesource.ParseReference(v.WithDigest)
		if err != nil {
			fmt.Fprintf(out, "error parsing target reference for %s, skip writing to ImageContentSourcePolicy\n", v)
			continue
		}
		icsp.Spec.RepositoryDigestMirrors = append(icsp.Spec.RepositoryDigestMirrors, operatorv1alpha1.RepositoryDigestMirrors{
			Source:  k,
			Mirrors: []string{toRef.Ref.AsRepository().String()},
		})
	}

	// Create an unstructured object for removing creationTimestamp
	unstructuredObj := unstructured.Unstructured{}
	var err error
	unstructuredObj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(&icsp)
	if err != nil {
		return nil, fmt.Errorf("error converting to unstructured: %v", err)
	}
	delete(unstructuredObj.Object["metadata"].(map[string]interface{}), "creationTimestamp")

	icspExample, err := yaml.Marshal(unstructuredObj.Object)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal ImageContentSourcePolicy yaml: %v", err)
	}

	return icspExample, nil
}
