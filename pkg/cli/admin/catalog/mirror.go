package catalog

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/docker/distribution/digest"
	"github.com/openshift/library-go/pkg/image/dockerv1client"
	"github.com/operator-framework/operator-registry/pkg/sqlite"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/klog"
	kcmdutil "k8s.io/kubernetes/pkg/kubectl/cmd/util"

	imagev1 "github.com/openshift/api/image/v1"
	imageclient "github.com/openshift/client-go/image/clientset/versioned"
	imagereference "github.com/openshift/library-go/pkg/image/reference"
	"k8s.io/kubernetes/pkg/kubectl/util/templates"

	"github.com/openshift/oc/pkg/cli/image/extract"
	imagemanifest "github.com/openshift/oc/pkg/cli/image/manifest"
	"github.com/openshift/oc/pkg/cli/image/mirror"
)

// NewMirrorOptions creates the options for mirroring a release.
func NewMirrorOptions(streams genericclioptions.IOStreams) *MirrorOptions {
	return &MirrorOptions{
		IOStreams:       streams,
		ParallelOptions: imagemanifest.ParallelOptions{MaxPerRegistry: 6},
	}
}

// NewMirror creates a command to mirror an existing release.
//
// Example command to mirror a release to a local repository to work offline
//
// $ oc adm catalog mirror \
//     --from=registry.svc.ci.openshift.org/openshift/v4.0 \
//     --to=mycompany.com/myrepository/repo
//
func NewMirror(f kcmdutil.Factory, parentName string, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewMirrorOptions(streams)
	cmd := &cobra.Command{
		Use:   "mirror",
		Short: "Mirror a catalog to a different image registry location",
		Long: templates.LongDesc(`
			Mirror an Operator Lifecycle Image (OLM) Catalog image to another registry.

			Copies the catalog images, operator images, and any defined related images (for 
			operands) from one registry to another.
			By default this command will not alter the payload and will print out the configuration
			that must be applied to a cluster to use the mirror, but you may opt to rewrite the
			update to point to the new location and lose the cryptographic integrity of the update.

			The common use for this command is to mirror a specific OLM Catalog image to a private 
            registry for use in a disconnected or offline context. The command copies all
			images that are part of a catalog into the target repository and then prints the
			correct information to give to OpenShift to use that content offline. An alternate mode
			is to specify --to-image-stream, which imports the images directly into an OpenShift
			image stream.
		`),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(cmd, f, args))
			kcmdutil.CheckErr(o.Run())
		},
	}
	flags := cmd.Flags()
	o.SecurityOptions.Bind(flags)
	o.ParallelOptions.Bind(flags)

	flags.StringVar(&o.From, "from", o.From, "Image containing the release payload.")
	flags.StringVar(&o.To, "to", o.To, "An image repository to push to.")
	flags.StringVar(&o.ToImageStream, "to-image-stream", o.ToImageStream, "An image stream to tag images into.")
	flags.BoolVar(&o.DryRun, "dry-run", o.DryRun, "Display information about the mirror without actually executing it.")

	// flags.BoolVar(&o.SkipRelease, "skip-release-image", o.SkipRelease, "Do not push the release image.")
	flags.StringVar(&o.ToCatalog, "to-catalog-image", o.ToCatalog, "Specify an alternate locations for the catalog image instead as tag 'catalog' in --to")
	return cmd
}

type MirrorOptions struct {
	genericclioptions.IOStreams

	SecurityOptions imagemanifest.SecurityOptions
	ParallelOptions imagemanifest.ParallelOptions

	From string

	To            string
	ToImageStream string

	ToCatalog string
	// SkipRelease bool

	DryRun bool

	ClientFn func() (imageclient.Interface, string, error)

	ImageStream           *imagev1.ImageStream
	TargetFn              func(component string) imagereference.DockerImageReference
	ImageMetadataCallback func(m *extract.Mapping, dgst, contentDigest digest.Digest, config *dockerv1client.DockerImageConfig)
}

func (o *MirrorOptions) Complete(cmd *cobra.Command, f kcmdutil.Factory, args []string) error {
	switch {
	case len(args) == 0 && len(o.From) == 0:
		return fmt.Errorf("must specify a catalog image with --from")
	case len(args) == 1 && len(o.From) == 0:
		o.From = args[0]
	case len(args) == 1 && len(o.From) > 0:
		return fmt.Errorf("you may not specify an argument and --from")
	case len(args) > 1:
		return fmt.Errorf("only one argument is accepted")
	}
	o.ClientFn = func() (imageclient.Interface, string, error) {
		cfg, err := f.ToRESTConfig()
		if err != nil {
			return nil, "", err
		}
		client, err := imageclient.NewForConfig(cfg)
		if err != nil {
			return nil, "", err
		}
		ns, _, err := f.ToRawKubeConfigLoader().Namespace()
		if err != nil {
			return nil, "", err
		}
		return client, ns, nil
	}
	return nil
}

func (o *MirrorOptions) Run() error {
	if len(o.From) == 0 && o.ImageStream == nil {
		return fmt.Errorf("must specify a release image with --from")
	}

	if (len(o.To) == 0) == (len(o.ToImageStream) == 0) {
		return fmt.Errorf("must specify an image repository or image stream to mirror the release to")
	}

	// if o.SkipRelease && len(o.ToRelease) > 0 {
	// 	return fmt.Errorf("--skip-release-image and --to-release-image may not both be specified")
	// }

	var recreateRequired bool
	var targetFn func(name string) mirror.MirrorReference
	var dst string
	if len(o.ToImageStream) > 0 {
		dst = imagereference.DockerImageReference{
			Registry:  "example.com",
			Namespace: "somenamespace",
			Name:      "mirror",
		}.Exact()
	} else {
		dst = o.To
	}

	ref, err := mirror.ParseMirrorReference(dst)
	if err != nil {
		return fmt.Errorf("--to must be a valid image repository: %v", err)
	}
	if len(ref.ID) > 0 || len(ref.Tag) > 0 {
		return fmt.Errorf("--to must be to an image repository and may not contain a tag or digest")
	}
	srcToDest := func(srcRef imagereference.DockerImageReference) mirror.MirrorReference {
		copied := mirror.MirrorReference{DockerImageReference: srcRef}
		copied.Registry = ref.Registry
		return copied
	}

	o.TargetFn = func(name string) imagereference.DockerImageReference {
		ref := targetFn(name)
		return ref.DockerImageReference
	}

	if recreateRequired {
		return fmt.Errorf("when mirroring to multiple repositories, use the new release command with --from-release and --mirror")
	}

	dir, err := ioutil.TempDir("", "extract")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0777); err != nil {
		return err
	}

	src := o.From
	srcref, err := imagereference.Parse(src)
	if err != nil {
		return err
	}
	opts := extract.NewOptions(genericclioptions.IOStreams{Out: o.Out, ErrOut: o.ErrOut})
	opts.SecurityOptions = o.SecurityOptions
	opts.OnlyFiles = true

	// TODO: configurable db location
	dbMapping := extract.Mapping{
		ImageRef: srcref,
		From:     "/bundles.db",
		To:       dir,
	}
	opts.Mappings = []extract.Mapping{dbMapping}
	if err := opts.Run(); err != nil {
		return err
	}

	dbPath := path.Join(dir, dbMapping.From)
	klog.V(4).Infof("Extracted catalog database to %s", dbPath)

	querier, err := sqlite.NewSQLLiteQuerier(dbPath)
	if err != nil {
		return err
	}

	images, err := querier.ListImages(context.TODO())
	if err != nil {
		return fmt.Errorf("error extracting images, check that the version of the tool matches the version of the database: %v", err)
	}
	klog.V(4).Infof("Extracted %d images", len(images))
	klog.V(4).Info(strings.Join(images, ","))

	var mappings []mirror.Mapping
	for _, i := range images {
		srcRef, err := imagereference.Parse(i)
		if err != nil {
			return err
		}
		dstMirrorRef := srcToDest(srcRef)
		mappings = append(mappings, mirror.Mapping{
			Source:      srcRef,
			Type:        dstMirrorRef.Type(),
			Destination: dstMirrorRef.Combined(),
			Name:        i,
		})
		klog.V(2).Infof("Mapping %#v", mappings[len(mappings)-1])
	}
	if len(mappings) == 0 {
		fmt.Fprintf(o.ErrOut, "warning: Catalog image contains no image references - is this a valid catalog?\n")
	}

	fmt.Fprintf(os.Stderr, "info: Mirroring %d images to %s ...\n", len(mappings), dst)
	mirrorOpts := mirror.NewMirrorImageOptions(genericclioptions.IOStreams{Out: o.Out, ErrOut: o.ErrOut})
	mirrorOpts.SecurityOptions = o.SecurityOptions
	mirrorOpts.ParallelOptions = o.ParallelOptions
	mirrorOpts.Mappings = mappings
	mirrorOpts.DryRun = o.DryRun
	if err := mirrorOpts.Run(); err != nil {
		return err
	}

	fmt.Fprintf(o.Out, "\nSuccess\nMirrored to: %s\n", o.To)
	return nil
}
