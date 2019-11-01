package catalog

import (
	"github.com/operator-framework/operator-registry/pkg/appregistry"
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	imgappend "github.com/openshift/oc/pkg/cli/image/append"
)

var (
	buildLong = templates.LongDesc(`
		Builds a catalog container image from a collection operator manifests.

		Extracts the contents of a collection of operator manifests to disk, and builds them into
		an operator registry catalog image.
		`)
	buildExample = templates.Examples(`
# Build an operator catalog from an appregistry repo and store in a file 
%[1]s --appregistry-org=redhat-operators --to=file://offline/redhat-operators:4.3

# Build an operator catalog from an appregistry repo and mirror to a registry 
%[1]s --appregistry-org=redhat-operators --to=quay.io/my/redhat-operators:4.3
`)
)

type BuildImageOptions struct {
	*appregistry.AppregistryBuildOptions
	genericclioptions.IOStreams

	FromFileDir string
	FileDir     string
}

func NewBuildImageOptions(streams genericclioptions.IOStreams) *BuildImageOptions {
	return &BuildImageOptions{
		AppregistryBuildOptions: appregistry.DefaultAppregistryBuildOptions(),
		IOStreams:               streams,
	}
}

func (o *BuildImageOptions) Complete(cmd *cobra.Command, args []string) error {
	var appender appregistry.ImageAppendFunc = func(from, to, layer string) error {
		a := imgappend.NewAppendImageOptions(o.IOStreams)
		a.FromFileDir = o.FromFileDir
		a.FileDir = o.FileDir
		a.From = o.From
		a.To = o.To
		a.LayerFiles = []string{layer}
		if err := a.Validate(); err != nil {
			return err
		}
		return a.Run()
	}
	o.AppregistryBuildOptions.Appender = appender

	return o.AppregistryBuildOptions.Complete()
}

func (o *BuildImageOptions) Validate() error {
	return o.AppregistryBuildOptions.Validate()
}

func (o *BuildImageOptions) Run() error {
	builder, err := appregistry.NewAppregistryImageBuilder(o.AppregistryBuildOptions.ToOption())
	if err != nil {
		return err
	}
	return builder.Build()
}

func NewBuildImage(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewBuildImageOptions(streams)
	cmd := &cobra.Command{
		Use:     "build",
		Short:   "build an operator-registry catalog",
		Long:    buildLong,
		Example: buildExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}
	flags := cmd.Flags()

	flags.StringVar(&o.From, "from", o.From, "The image to use as a base.")
	flags.StringVar(&o.To, "to", "", "The image repository tag to apply to the built catalog image.")
	flags.StringVar(&o.AuthToken, "auth-token", "", "Auth token for communicating with an application registry.")
	flags.StringVar(&o.AppRegistryEndpoint, "appregistry-endpoint", o.AppRegistryEndpoint, "Endpoint for pulling from an application registry instance.")
	flags.StringVar(&o.AppRegistryOrg, "appregistry-org", "", "Organization (Namespace) to pull from an application registry instance")
	flags.StringVar(&o.DatabasePath, "to-db", "", "Local path to save the database to.")
	flags.StringVar(&o.CacheDir, "manifest-dir", "", "Local path to cache manifests when downloading.")

	flags.StringVar(&o.FileDir, "dir", o.FileDir, "The directory on disk that file:// images will be copied under.")
	flags.StringVar(&o.FromFileDir, "from-dir", o.FromFileDir, "The directory on disk that file:// images will be read from. Overrides --dir")

	return cmd
}
