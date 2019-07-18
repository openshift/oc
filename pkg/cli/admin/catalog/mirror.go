package catalog

import (
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	kcmdutil "k8s.io/kubernetes/pkg/kubectl/cmd/util"

	imagev1 "github.com/openshift/api/image/v1"
	imageclient "github.com/openshift/client-go/image/clientset/versioned"
	imagereference "github.com/openshift/library-go/pkg/image/reference"
	"k8s.io/kubernetes/pkg/kubectl/util/templates"

	imagemanifest "github.com/openshift/oc/pkg/cli/image/manifest"
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
// $ oc adm release mirror \
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

	ToCatalog   string
	// SkipRelease bool

	DryRun bool

	ClientFn func() (imageclient.Interface, string, error)

	ImageStream *imagev1.ImageStream
	TargetFn    func(component string) imagereference.DockerImageReference
}

func (o *MirrorOptions) Complete(cmd *cobra.Command, f kcmdutil.Factory, args []string) error {
	// switch {
	// case len(args) == 0 && len(o.From) == 0:
	// 	return fmt.Errorf("must specify a release image with --from")
	// case len(args) == 1 && len(o.From) == 0:
	// 	o.From = args[0]
	// case len(args) == 1 && len(o.From) > 0:
	// 	return fmt.Errorf("you may not specify an argument and --from")
	// case len(args) > 1:
	// 	return fmt.Errorf("only one argument is accepted")
	// }
	// o.ClientFn = func() (imageclient.Interface, string, error) {
	// 	cfg, err := f.ToRESTConfig()
	// 	if err != nil {
	// 		return nil, "", err
	// 	}
	// 	client, err := imageclient.NewForConfig(cfg)
	// 	if err != nil {
	// 		return nil, "", err
	// 	}
	// 	ns, _, err := f.ToRawKubeConfigLoader().Namespace()
	// 	if err != nil {
	// 		return nil, "", err
	// 	}
	// 	return client, ns, nil
	// }
	return nil
}

func (o *MirrorOptions) Run() error {
	// if len(o.From) == 0 && o.ImageStream == nil {
	// 	return fmt.Errorf("must specify a release image with --from")
	// }
	//
	// if (len(o.To) == 0) == (len(o.ToImageStream) == 0) {
	// 	return fmt.Errorf("must specify an image repository or image stream to mirror the release to")
	// }
	//
	// // if o.SkipRelease && len(o.ToRelease) > 0 {
	// // 	return fmt.Errorf("--skip-release-image and --to-release-image may not both be specified")
	// // }
	//
	// var recreateRequired bool
	// var hasPrefix bool
	// var targetFn func(name string) mirror.MirrorReference
	// var dst string
	// if len(o.ToImageStream) > 0 {
	// 	dst = imagereference.DockerImageReference{
	// 		Registry:  "example.com",
	// 		Namespace: "somenamespace",
	// 		Name:      "mirror",
	// 	}.Exact()
	// } else {
	// 	dst = o.To
	// }
	return nil
}
