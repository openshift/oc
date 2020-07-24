package imagestream

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	restclient "k8s.io/client-go/rest"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	imagev1 "github.com/openshift/api/image/v1"
	imagev1client "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
)

// RecommendedName is the recommended command name
const RecommendedName = "imagestream"

var (
	imagesLongDesc = templates.LongDesc(`
		Remove image stream tags by age

		This command removes historical image stream tags given a set of constraints including a
		minimum number of revisions or age of the historical tag. Unlike the prune images command
		this will not take into account whether pods or other resources on the cluster are still
		using those images, so running this command may result in pods failing to start after
		deployment. Always verify that the tags you are removing are no longer in use. This
		command assists with general maintenance when developing automated image tagging.

		By default, the prune operation performs a dry run making no changes. The --confirm flag
		is needed for changes to be effective.`)

	imagesExample = templates.Examples(`
		# Print the tags that would be removed if revisions older than 60m were removed, leaving
		# no fewer than 3 revisions per tag. Selects all image streams in the current namespace.
	  oc adm prune imagestream --all --keep-tag-revisions=3 --keep-younger-than=60m

	  # Perform the prune of older revisions as described in the above command.
	  oc adm prune imagestream --all --keep-tag-revisions=3 --keep-younger-than=60m --confirm`)
)

// Options holds all the required options for pruning images.
type Options struct {
	Confirm          bool
	KeepYoungerThan  time.Duration
	KeepTagRevisions int
	Namespace        string
	ResourceNames    []string
	All              bool
	AllNamespaces    bool
	Selector         string

	ClientConfig *restclient.Config
	ImageClient  imagev1client.ImageV1Interface
	Timeout      time.Duration
	Out          io.Writer
	ErrOut       io.Writer
}

// NewCmd implements the OpenShift cli prune imagestreams command.
func NewCmd(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := &Options{
		Confirm: false,
	}

	cmd := &cobra.Command{
		Use:     "imagestream",
		Short:   "Remove tags from image streams",
		Long:    fmt.Sprintf(imagesLongDesc),
		Example: imagesExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args, streams.Out))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().BoolVar(&o.Confirm, "confirm", o.Confirm, "If true, specify that image pruning should proceed. Defaults to false, displaying what would be deleted but not actually deleting anything.")
	cmd.Flags().BoolVarP(&o.AllNamespaces, "all-namespaces", "A", o.AllNamespaces, "If true, list the requested object(s) across all projects. Project in current context is ignored.")
	cmd.Flags().BoolVar(&o.All, "all", o.All, "If true, all image streams in the current namespace are selected.")
	cmd.Flags().StringVarP(&o.Selector, "selector", "l", o.Selector, "Selector (label query) to filter on, supports '=', '==', and '!='.(e.g. -l key1=value1,key2=value2)")
	cmd.Flags().DurationVar(&o.KeepYoungerThan, "keep-younger-than", 0, "Specify the minimum age of a tag revision to consider for removal.")
	cmd.Flags().IntVar(&o.KeepTagRevisions, "keep-tag-revisions", 0, "Specify the number of image revisions for a tag in an image stream to preserve, regardless of age.")

	return cmd
}

// Complete turns a partially defined Options into a solvent structure
// which can be validated and used for pruning images.
func (o *Options) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string, out io.Writer) error {
	o.ResourceNames = args

	var err error
	o.Namespace, _, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}
	if o.AllNamespaces {
		o.Namespace = metav1.NamespaceAll
	}

	o.Out = out
	o.ErrOut = os.Stderr

	o.ClientConfig, err = f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.ImageClient, err = imagev1client.NewForConfig(o.ClientConfig)
	if err != nil {
		return err
	}
	o.Timeout = o.ClientConfig.Timeout
	if o.Timeout == 0 {
		o.Timeout = time.Duration(10 * time.Second)
	}

	return nil
}

// Validate ensures that a Options is valid and can be used to execute pruning.
func (o Options) Validate() error {
	if len(o.ResourceNames) > 0 && o.AllNamespaces {
		return fmt.Errorf("--all-namespaces may not be specified with resource names as arguments")
	}
	if len(o.ResourceNames) == 0 && (!o.AllNamespaces || !o.All || len(o.Selector) == 0) {
		return fmt.Errorf("you must specify an image stream name, a label selector, or the --all-namespaces flag")
	}
	if o.KeepYoungerThan < 0 {
		return fmt.Errorf("--keep-younger-than must be greater than or equal to 0")
	}
	if o.KeepTagRevisions < 0 {
		return fmt.Errorf("--keep-tag-revisions must be greater than or equal to 0")
	}
	if o.KeepTagRevisions == 0 && o.KeepYoungerThan == 0 {
		return fmt.Errorf("you must specify a constraint on which tags to keep")
	}
	return nil
}

// Run contains all the necessary functionality for the OpenShift cli prune images command.
func (o Options) Run() error {
	var errs []error
	var imageStreams []*imagev1.ImageStream
	if len(o.ResourceNames) > 0 {
		for _, name := range o.ResourceNames {
			stream, err := o.ImageClient.ImageStreams(o.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
			if err != nil {
				errs = append(errs, err)
				continue
			}
			imageStreams = append(imageStreams, stream)
		}
	} else {
		allStreams, err := o.ImageClient.ImageStreams(o.Namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: o.Selector,
		})
		if err != nil {
			return err
		}
		for i := range allStreams.Items {
			imageStreams = append(imageStreams, &allStreams.Items[i])
		}
	}

	var updated []*imagev1.ImageStream
	var expires time.Time
	if o.KeepYoungerThan > 0 {
		expires = time.Now().Add(-o.KeepYoungerThan)
	}
	w := tabwriter.NewWriter(o.Out, 1, 1, 1, ' ', 0)
	for _, stream := range imageStreams {
		changed := false
		for i, tags := range stream.Status.Tags {
			deleteFrom := len(tags.Items)
			for i := range tags.Items {
				if o.KeepTagRevisions > 0 && i < o.KeepTagRevisions {
					continue
				}
				if !expires.IsZero() && tags.Items[i].Created.Time.After(expires) {
					continue
				}
				deleteFrom = i
				break
			}
			if deleteFrom < len(tags.Items) {
				for i, tag := range tags.Items[deleteFrom:] {
					fmt.Fprintf(w, "%s/%s:%s\t%d\t%s\t%s\n", stream.Namespace, stream.Name, tags.Tag, i+deleteFrom, tag.Created.Format(time.RFC3339), tag.DockerImageReference)
				}
				stream.Status.Tags[i].Items = tags.Items[:deleteFrom]
				changed = true
			}
		}
		if changed {
			updated = append(updated, stream)
		} else {
			fmt.Fprintf(o.ErrOut, "info: No update needed to %s/%s\n", stream.Namespace, stream.Name)
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}

	for _, stream := range updated {
		if o.Confirm {
			_, err := o.ImageClient.ImageStreams(stream.Namespace).UpdateStatus(context.TODO(), stream, metav1.UpdateOptions{})
			if err != nil {
				errs = append(errs, err)
				continue
			}
		} else {
			fmt.Fprintf(o.ErrOut, "dry-run: update %s/%s\n", stream.Namespace, stream.Name)
		}
	}

	return kerrors.NewAggregate(errs)
}
