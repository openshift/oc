// Package channel contains a command for setting a cluster's update channel.
package channel

import (
	"context"
	"fmt"
	"strings"

	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

func NewOptions(streams genericclioptions.IOStreams) *Options {
	return &Options{
		IOStreams: streams,
	}
}

func New(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewOptions(streams)
	cmd := &cobra.Command{
		Use:   "channel CHANNEL",
		Short: "Set or clear the update channel",
		Long: templates.LongDesc(`
			Set or clear the update channel.

			This command will set or clear the update channel, which impacts the list of updates
			recommended for the cluster.

			If there is a list of acceptable channels and the desired channel is not in that list,
			you must pass --allow-explicit-channel to allow channel change to proceed.
		`),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run())
		},
	}
	flags := cmd.Flags()
	flags.BoolVar(&o.AllowExplicitChannel, "allow-explicit-channel", o.AllowExplicitChannel, "Change the channel, even if there is a list of acceptable channels and the desired channel is not in that list.")
	return cmd
}

type Options struct {
	genericclioptions.IOStreams

	Channel string

	AllowExplicitChannel bool

	Client configv1client.Interface
}

func (o *Options) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return kcmdutil.UsageErrorf(cmd, "multiple positional arguments given")
	} else if len(args) == 1 {
		o.Channel = args[0]
	}

	cfg, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	client, err := configv1client.NewForConfig(cfg)
	if err != nil {
		return err
	}
	o.Client = client
	return nil
}

func (o *Options) Run() error {
	ctx := context.TODO()
	cv, err := o.Client.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("no cluster version information available - you must be connected to an OpenShift version 4 server to fetch the current version")
		}
		return err
	}

	if o.Channel == cv.Spec.Channel {
		if cv.Spec.Channel == "" {
			fmt.Fprint(o.Out, "info: Cluster channel is already clear\n")
		} else {
			fmt.Fprintf(o.Out, "info: Cluster is already in %s\n", cv.Spec.Channel)
		}
		return nil
	}

	if len(cv.Status.Desired.Channels) > 0 {
		found := false
		for _, channel := range cv.Status.Desired.Channels {
			if channel == o.Channel {
				found = true
				break
			}
		}
		if !found {
			if !o.AllowExplicitChannel {
				return fmt.Errorf("the requested channel %q is not one of the available channels (%s), you must pass --allow-explicit-channel to continue\n", o.Channel, strings.Join(cv.Status.Desired.Channels, ", "))
			}
			if o.Channel != "" {
				fmt.Fprintf(o.ErrOut, "warning: The requested channel %q is not one of the available channels (%s).  You have used --allow-explicit-channel to proceed anyway.\n", o.Channel, strings.Join(cv.Status.Desired.Channels, ", "))
			}
		}
	} else if o.Channel != "" {
		fmt.Fprintf(o.ErrOut, "warning: No channels known to be compatible with the current version %q; unable to validate %q.\n", cv.Status.Desired.Version, o.Channel)
	}

	if o.Channel == "" {
		fmt.Fprintf(o.ErrOut, "warning: Clearing channel %q; cluster will no longer request available update recommendations.\n", cv.Spec.Channel)
	}

	cv.Spec.Channel = o.Channel

	if _, err = o.Client.ConfigV1().ClusterVersions().Update(ctx, cv, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("unable to set channel: %w", err)
	}

	return nil
}
