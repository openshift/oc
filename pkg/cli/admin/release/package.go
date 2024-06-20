package release

import (
	"fmt"
	"github.com/openshift/oc/pkg/cli/image/extract"
	"github.com/openshift/oc/pkg/cli/image/imagesource"
	imagemanifest "github.com/openshift/oc/pkg/cli/image/manifest"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
	"os"
	"strings"
)

func NewPackageOptions(streams genericclioptions.IOStreams) *PackageOptions {
	return &PackageOptions{
		IOStreams:              streams,
		KubeTemplatePrintFlags: *genericclioptions.NewKubeTemplatePrintFlags(),
		ParallelOptions:        imagemanifest.ParallelOptions{MaxPerRegistry: 4},
	}
}

func NewPackage(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewPackageOptions(streams)
	cmd := &cobra.Command{
		Use:   "package IMAGE",
		Short: "Display information about the list of RPM packages in the machine-os-content image",
		Long: templates.LongDesc(`
			Show information about the RPM packages inside the machine-os-content image.

			This command retrieves, verifies, and formats the information describing an OpenShift update.
			Updates are delivered as container images with metadata describing the component images and
			the configuration necessary to install the system operators. A release image is usually
			referenced via its content digest, which allows this command and the update infrastructure to
			validate that updates have not been tampered with.

			If no arguments are specified the release of the currently connected cluster is displayed.
			Specify one image to see details of each release image. You may also pass a semantic version
			(4.11.2) as an argument, and if cluster version object has seen such a version in the upgrades
			channel it will find the release info for that version.

			If the specified image supports multiple operating systems, the image that matches the
			current operating system will be chosen. Otherwise you must pass --filter-by-os to
			select the desired image.
		`),
		Example: templates.Examples(`
			# Show information about the cluster's current release
			oc adm release package

			# Show the information about a specific release
			oc adm release package 4.11.2

			# Show information about linux/s390x image
			# Note: Wildcard filter is not supported. Pass a single os/arch to extract
			oc adm release info quay.io/openshift-release-dev/ocp-release:4.11.2 --filter-by-os=linux/s390x

		`),
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
	o.KubeTemplatePrintFlags.AddFlags(cmd)

	flags.StringVar(&o.ICSPFile, "icsp-file", o.ICSPFile, "Path to an ImageContentSourcePolicy file. If set, data from this file will be used to find alternative locations for images.")
	return cmd
}

type PackageOptions struct {
	genericclioptions.IOStreams
	genericclioptions.KubeTemplatePrintFlags

	Images []string
	Image  string
	From   string

	Output   string
	ImageFor string
	Verify   bool
	ICSPFile string

	ParallelOptions imagemanifest.ParallelOptions
	SecurityOptions imagemanifest.SecurityOptions
	FilterOptions   imagemanifest.FilterOptions
}

func (o *PackageOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	args, err := findArgumentsFromCluster(f, args)
	if err != nil {
		fmt.Println(strings.Replace(err.Error(), "info", "package", 1))
	}
	if len(args) < 1 {
		return fmt.Errorf("packge expects at least one argument, a release image pull spec")
	}
	o.Images = args
	if len(o.From) == 0 && len(o.Images) == 2 && !o.Verify {
		o.From = o.Images[0]
		o.Images = o.Images[1:]
	}
	return o.FilterOptions.Complete(cmd.Flags())
}

func (o *PackageOptions) Validate() error {
	return o.FilterOptions.Validate()
}

func (o *PackageOptions) Run() error {
	var exitErr error
	image := strings.Join(o.Images, " ")
	infoopts := NewInfoOptions(o.IOStreams)
	infoopts.SecurityOptions = o.SecurityOptions
	infoopts.FilterOptions = o.FilterOptions
	infoopts.ImageFor = "machine-os-content"
	release, err := infoopts.LoadReleaseInfo(image, false)
	if err != nil {
		return err
	}
	spec, err := findImageSpec(release.References, infoopts.ImageFor, release.Image)
	if err != nil {
		return err
	}
	ref, err := imagesource.ParseReference(spec)
	if err != nil {
		return err
	}

	dir, err := os.MkdirTemp("", "machine-os-content")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	extractopts := extract.NewExtractOptions(genericclioptions.IOStreams{Out: os.Stdout})
	extractopts.Confirm = true
	extractopts.Mappings = []extract.Mapping{
		{
			ImageRef: ref,
			To:       dir,
			From:     "pkglist.txt",
		},
	}
	if err := extractopts.Run(); err != nil {
		return err
	}
	list, err := os.ReadFile(dir + "/pkglist.txt")
	if err != nil {
		return err
	}
	fmt.Print(string(list))
	return exitErr
}
