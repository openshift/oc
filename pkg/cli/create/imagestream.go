package create

import (
	"context"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util"
	"k8s.io/kubectl/pkg/util/templates"

	imagev1 "github.com/openshift/api/image/v1"
	imagev1client "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
)

var (
	imageStreamLong = templates.LongDesc(`
		Create a new image stream

		Image streams allow you to track, tag, and import images from other registries. They also define an
		access controlled destination that you can push images to. An image stream can reference images
		from many different registries and control how those images are referenced by pods, deployments,
		and builds.

		If --lookup-local is passed, the image stream will be used as the source when pods reference
		it by name. For example, if stream 'mysql' resolves local names, a pod that points to
		'mysql:latest' will use the image the image stream points to under the "latest" tag.
	`)

	imageStreamExample = templates.Examples(`
		# Create a new image stream
		arvan paas create imagestream mysql
	`)
)

type CreateImageStreamOptions struct {
	CreateSubcommandOptions *CreateSubcommandOptions

	LookupLocal bool

	Client imagev1client.ImageStreamsGetter
}

// NewCmdCreateImageStream is a macro command to create a new image stream
func NewCmdCreateImageStream(f genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := &CreateImageStreamOptions{
		CreateSubcommandOptions: NewCreateSubcommandOptions(streams),
	}
	cmd := &cobra.Command{
		Use:     "imagestream NAME",
		Short:   "Create a new empty image stream.",
		Long:    imageStreamLong,
		Example: imageStreamExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(cmd, f, args))
			cmdutil.CheckErr(o.Run())
		},
		Aliases: []string{"is"},
	}
	cmd.Flags().BoolVar(&o.LookupLocal, "lookup-local", o.LookupLocal, "If true, the image stream will be the source for any top-level image reference in this project.")

	o.CreateSubcommandOptions.AddFlags(cmd)
	cmdutil.AddDryRunFlag(cmd)

	return cmd
}

func (o *CreateImageStreamOptions) Complete(cmd *cobra.Command, f genericclioptions.RESTClientGetter, args []string) error {
	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.Client, err = imagev1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	return o.CreateSubcommandOptions.Complete(f, cmd, args)
}

func (o *CreateImageStreamOptions) Run() error {
	imageStream := &imagev1.ImageStream{
		// this is ok because we know exactly how we want to be serialized
		TypeMeta:   metav1.TypeMeta{APIVersion: imagev1.SchemeGroupVersion.String(), Kind: "ImageStream"},
		ObjectMeta: metav1.ObjectMeta{Name: o.CreateSubcommandOptions.Name},
		Spec: imagev1.ImageStreamSpec{
			LookupPolicy: imagev1.ImageLookupPolicy{
				Local: o.LookupLocal,
			},
		},
	}

	if err := util.CreateOrUpdateAnnotation(o.CreateSubcommandOptions.CreateAnnotation, imageStream, scheme.DefaultJSONEncoder()); err != nil {
		return err
	}

	if o.CreateSubcommandOptions.DryRunStrategy != cmdutil.DryRunClient {
		var err error
		imageStream, err = o.Client.ImageStreams(o.CreateSubcommandOptions.Namespace).Create(context.TODO(), imageStream, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}

	return o.CreateSubcommandOptions.Printer.PrintObj(imageStream, o.CreateSubcommandOptions.Out)
}
