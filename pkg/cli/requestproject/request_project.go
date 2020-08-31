package requestproject

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	projectv1 "github.com/openshift/api/project/v1"
	projectv1client "github.com/openshift/client-go/project/clientset/versioned/typed/project/v1"
	ocproject "github.com/openshift/oc/pkg/cli/project"
	cliconfig "github.com/openshift/oc/pkg/helpers/kubeconfig"
)

// RequestProjectOptions contains all the options for running the RequestProject cli command.
type RequestProjectOptions struct {
	ProjectName string
	DisplayName string
	Description string

	Server string

	SkipConfigWrite bool

	Client         projectv1client.ProjectV1Interface
	ProjectOptions *ocproject.ProjectOptions

	genericclioptions.IOStreams
}

// RequestProject command description.
var (
	requestProjectLong = templates.LongDesc(`
		Create a new project for yourself

		If your administrator allows self-service, this command will create a new project for you and assign you
		as the project admin.

		After your project is created it will become the default project in your config.`)

	requestProjectExample = templates.Examples(`
		# Create a new project with minimal information
		oc new-project web-team-dev

		# Create a new project with a display name and description
		oc new-project web-team-dev --display-name="Web Team Development" --description="Development project for the web team."`)
)

// RequestProject next steps.
const (
	requestProjectNewAppOutput = `
You can add applications to this project with the 'new-app' command. For example, try:

    oc new-app rails-postgresql-example

to build a new example application in Ruby. Or use kubectl to deploy a simple Kubernetes application:

    kubectl create deployment hello-node --image=k8s.gcr.io/echoserver:1.4

`
	requestProjectSwitchProjectOutput = `Project %[1]q created on server %[2]q.

To switch to this project and start adding applications, use:

    oc project %[2]s
`
)

func NewRequestProjectOptions(streams genericclioptions.IOStreams) *RequestProjectOptions {
	return &RequestProjectOptions{
		IOStreams: streams,
	}
}

// NewCmdRequestProject implement the OpenShift cli RequestProject command.
func NewCmdRequestProject(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewRequestProjectOptions(streams)
	cmd := &cobra.Command{
		Use:     "new-project NAME [--display-name=DISPLAYNAME] [--description=DESCRIPTION]",
		Short:   "Request a new project",
		Long:    requestProjectLong,
		Example: requestProjectExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run())
		},
	}
	cmd.Flags().StringVar(&o.DisplayName, "display-name", o.DisplayName, "Project display name")
	cmd.Flags().StringVar(&o.Description, "description", o.Description, "Project description")
	cmd.Flags().BoolVar(&o.SkipConfigWrite, "skip-config-write", o.SkipConfigWrite, "If true, the project will not be set as a cluster entry in kubeconfig after being created")

	return cmd
}

// Complete completes all the required options.
func (o *RequestProjectOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("must have exactly one argument")
	}

	o.ProjectName = args[0]

	if !o.SkipConfigWrite {
		o.ProjectOptions = ocproject.NewProjectOptions(o.IOStreams)
		o.ProjectOptions.PathOptions = cliconfig.NewPathOptions(cmd)
		if err := o.ProjectOptions.Complete(f, cmd, []string{""}); err != nil {
			return err
		}
	} else {
		clientConfig, err := f.ToRESTConfig()
		if err != nil {
			return err
		}
		o.Server = clientConfig.Host
	}

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.Client, err = projectv1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	return nil
}

// Run implements all the necessary functionality for RequestProject.
func (o *RequestProjectOptions) Run() error {
	if err := o.Client.RESTClient().Get().Resource("projectrequests").Do(context.TODO()).Into(&metav1.Status{}); err != nil {
		return err
	}

	projectRequest := &projectv1.ProjectRequest{}
	projectRequest.Name = o.ProjectName
	projectRequest.DisplayName = o.DisplayName
	projectRequest.Description = o.Description
	projectRequest.Annotations = make(map[string]string)

	project, err := o.Client.ProjectRequests().Create(context.TODO(), projectRequest, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	if o.ProjectOptions != nil {
		o.ProjectOptions.ProjectName = project.Name
		o.ProjectOptions.ProjectOnly = true
		o.ProjectOptions.SkipAccessValidation = true
		o.ProjectOptions.IOStreams = o.IOStreams
		if err := o.ProjectOptions.Run(); err != nil {
			return err
		}

		fmt.Fprintf(o.Out, requestProjectNewAppOutput)
	} else {
		fmt.Fprintf(o.Out, requestProjectSwitchProjectOutput, o.ProjectName, o.Server)
	}

	return nil
}
