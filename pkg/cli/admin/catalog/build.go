// +build linux

package catalog

import (
	"fmt"
	"regexp"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/klog"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/operator-framework/operator-registry/pkg/appregistry"
	"github.com/spf13/cobra"

	"github.com/openshift/oc/pkg/cli/admin/release"
	imgappend "github.com/openshift/oc/pkg/cli/image/append"
	imagemanifest "github.com/openshift/oc/pkg/cli/image/manifest"
)

func init() {
	subCommands = append(subCommands, NewBuildImage)
}

var (
	buildLong = templates.LongDesc(`
		Builds a catalog container image from a collection operator manifests.

		Extracts the contents of a collection of operator manifests to disk, and builds them into
		an operator registry catalog image.

		The base image used for the catalog should match the target version of ocp. This can be set manually with 
		the '--from' flag. If '--from' references a release image, the base image will be selected from the release. If
		omitted, the base image will be inferred from the current cluster.

		The base image will often be a multi-arch image. By default, the linux/amd64 variant is chosen. This can be 
		overridden with '--filter-by-os'.
		`)
	buildExample = templates.Examples(`
# Build an operator catalog from an appregistry repo and store in a file.
%[1]s --appregistry-org=redhat-operators --to=file://offline/redhat-operators:4.3

# Build an operator catalog from an appregistry repo and mirror to a registry.
%[1]s --appregistry-org=redhat-operators --to=quay.io/my/redhat-operators:4.3

# Build an operator catalog by inferring a base image from a target ocp release
%[1]s --appregistry-org=redhat-operators --to=quay.io/my/redhat-operators:4.3 --from=quay.io/openshift-release-dev/ocp-release:4.3.0

# Build an operator catalog by explicitly providing a base image
%[1]s --appregistry-org=redhat-operators --to=quay.io/my/redhat-operators:4.3 --from=quay.io/openshift/origin-operator-registry:4.4

# Build an operator catalog for a specific target architecture. Assumes you are logged in via 'oc login'.
%[1]s --appregistry-org=redhat-operators --to=file://offline/redhat-operators:4.3 --filter-by-os='linux/s390x'
`)
)

const (
	releaseImageStreamTag = "operator-registry"
)

type BuildImageOptions struct {
	*appregistry.AppregistryBuildOptions
	genericclioptions.IOStreams

	SecurityOptions imagemanifest.SecurityOptions
	FilterOptions   imagemanifest.FilterOptions
	ParallelOptions imagemanifest.ParallelOptions

	FromFileDir string
	FileDir     string
}

func NewBuildImageOptions(streams genericclioptions.IOStreams) *BuildImageOptions {
	// From must be specified, or we must infer it from a release
	o := appregistry.DefaultAppregistryBuildOptions()
	o.From = ""
	return &BuildImageOptions{
		AppregistryBuildOptions: o,
		IOStreams:               streams,
		ParallelOptions:         imagemanifest.ParallelOptions{MaxPerRegistry: 4},
	}
}

func (o *BuildImageOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	// expect to extract the base image from an active ocp connection or a release image
	var images []string
	if len(o.From) > 0 {
		klog.V(2).Info("--from specified, checking if it is a release image")
		images = append(images, o.From)
	}
	infoOpts := release.NewInfoOptions(o.IOStreams)
	infoOpts.ImageFor = releaseImageStreamTag
	if err := infoOpts.Complete(f, nil, images); err != nil {
		klog.V(2).Infof("unable to find release image: %v", err)
	}

	imageFromRealeaseTags := func(img string) {
		info, err := infoOpts.LoadReleaseInfo(img, false)
		if err != nil {
			klog.V(2).Infof("unable to load image from %s: %v", img, err)
			return
		}
		for _, tag := range info.References.Spec.Tags {
			if tag.Name == releaseImageStreamTag {
				if tag.From != nil && tag.From.Kind == "DockerImage" && len(tag.From.Name) > 0 {
					o.From = tag.From.Name
					return
				}
			}
		}
		return
	}

	// If from is specified, check if it's a release image and grab the base image from that
	if len(o.From) > 0 {
		imageFromRealeaseTags(o.From)
	}

	// If from is not specified and infoOpts.Complete found an image from a cluster, try to grab base image from that
	if len(o.From) == 0 && len(infoOpts.Images) > 0 && infoOpts.Images[0] != o.From {
		imageFromRealeaseTags(infoOpts.Images[0])
	}

	if len(o.From) == 0 {
		return fmt.Errorf("unable to resolve base image - use --from to specify a base image or a release image")
	}

	fmt.Fprintf(o.IOStreams.Out, "using %s as a base image for building", o.From)

	// default the base image os to linux/amd64 (the most common case)
	pattern := o.FilterOptions.FilterByOS
	if len(pattern) == 0 {
		o.FilterOptions.FilterByOS = regexp.QuoteMeta(fmt.Sprintf("%s/%s", "linux", "amd64"))
	}

	var appender appregistry.ImageAppendFunc = func(from, to, layer string) error {
		a := imgappend.NewAppendImageOptions(o.IOStreams)
		a.FromFileDir = o.FromFileDir
		a.FileDir = o.FileDir
		a.From = o.From
		a.To = o.To
		a.SecurityOptions = o.SecurityOptions
		a.FilterOptions = o.FilterOptions
		a.ParallelOptions = o.ParallelOptions
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

func NewBuildImage(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewBuildImageOptions(streams)
	cmd := &cobra.Command{
		Use:     "build",
		Short:   "build an operator-registry catalog",
		Long:    buildLong,
		Example: fmt.Sprintf(buildExample, "oc adm catalog build"),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}
	flags := cmd.Flags()
	o.SecurityOptions.Bind(flags)
	o.FilterOptions.Bind(flags)
	o.ParallelOptions.Bind(flags)

	flags.StringVar(&o.From, "from", o.From, "The image to use as a base, or a reference to a release image. This can be omitted if oc is already logged into the target cluster.")
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
