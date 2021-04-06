package create

import (
	"context"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util"
	"k8s.io/kubectl/pkg/util/templates"

	appsv1 "github.com/openshift/api/apps/v1"
	appsv1client "github.com/openshift/client-go/apps/clientset/versioned/typed/apps/v1"
)

var (
	deploymentConfigLong = templates.LongDesc(`
		Create a deployment config that uses a given image.

		Deployment configs define the template for a pod and manage deploying new images or configuration changes.
	`)

	deploymentConfigExample = templates.Examples(`
		# Create an nginx deployment config named my-nginx
		oc create deploymentconfig my-nginx --image=nginx
	`)
)

type CreateDeploymentConfigOptions struct {
	CreateSubcommandOptions *CreateSubcommandOptions

	Image string
	Args  []string

	Client appsv1client.DeploymentConfigsGetter
}

// NewCmdCreateDeploymentConfig is a macro command to create a new deployment config.
func NewCmdCreateDeploymentConfig(f genericclioptions.RESTClientGetter, streams genericclioptions.IOStreams) *cobra.Command {
	o := &CreateDeploymentConfigOptions{
		CreateSubcommandOptions: NewCreateSubcommandOptions(streams),
	}
	cmd := &cobra.Command{
		Use:     "deploymentconfig NAME --image=IMAGE -- [COMMAND] [args...]",
		Short:   "Create a deployment config with default options that uses a given image",
		Long:    deploymentConfigLong,
		Example: deploymentConfigExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(cmd, f, args))
			cmdutil.CheckErr(o.Run())
		},
		Aliases: []string{"dc"},
	}
	cmd.Flags().StringVar(&o.Image, "image", o.Image, "The image for the container to run.")
	cmd.MarkFlagRequired("image")

	o.CreateSubcommandOptions.AddFlags(cmd)
	cmdutil.AddDryRunFlag(cmd)

	return cmd
}

func (o *CreateDeploymentConfigOptions) Complete(cmd *cobra.Command, f genericclioptions.RESTClientGetter, args []string) error {
	if len(args) > 1 {
		o.Args = args[1:]
	}

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.Client, err = appsv1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	return o.CreateSubcommandOptions.Complete(f, cmd, args)
}

func (o *CreateDeploymentConfigOptions) Run() error {
	labels := map[string]string{"deployment-config.name": o.CreateSubcommandOptions.Name}
	deploymentConfig := &appsv1.DeploymentConfig{
		// this is ok because we know exactly how we want to be serialized
		TypeMeta:   metav1.TypeMeta{APIVersion: appsv1.SchemeGroupVersion.String(), Kind: "DeploymentConfig"},
		ObjectMeta: metav1.ObjectMeta{Name: o.CreateSubcommandOptions.Name},
		Spec: appsv1.DeploymentConfigSpec{
			Selector: labels,
			Replicas: 1,
			Template: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "default-container",
							Image: o.Image,
							Args:  o.Args,
						},
					},
				},
			},
		},
	}

	if err := util.CreateOrUpdateAnnotation(o.CreateSubcommandOptions.CreateAnnotation, deploymentConfig, scheme.DefaultJSONEncoder()); err != nil {
		return err
	}

	if o.CreateSubcommandOptions.DryRunStrategy != cmdutil.DryRunClient {
		var err error
		deploymentConfig, err = o.Client.DeploymentConfigs(o.CreateSubcommandOptions.Namespace).Create(context.TODO(), deploymentConfig, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}

	return o.CreateSubcommandOptions.Printer.PrintObj(deploymentConfig, o.CreateSubcommandOptions.Out)
}
