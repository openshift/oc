package templateinstances

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	templatev1 "github.com/openshift/api/template/v1"
	templatev1typedclient "github.com/openshift/client-go/template/clientset/versioned/typed/template/v1"
	"github.com/openshift/oc/pkg/cli/admin/migrate"
)

type apiType struct {
	Kind       string
	APIVersion string
}

var (
	transforms = map[apiType]apiType{
		// legacy oapi group
		{"DeploymentConfig", "v1"}: {"DeploymentConfig", "apps.openshift.io/v1"},
		{"BuildConfig", "v1"}:      {"BuildConfig", "build.openshift.io/v1"},
		{"Build", "v1"}:            {"Build", "build.openshift.io/v1"},
		{"Route", "v1"}:            {"Route", "route.openshift.io/v1"},
		// legacy oapi group, for the lazy
		{"DeploymentConfig", ""}: {"DeploymentConfig", "apps.openshift.io/v1"},
		{"BuildConfig", ""}:      {"BuildConfig", "build.openshift.io/v1"},
		{"Build", ""}:            {"Build", "build.openshift.io/v1"},
		{"Route", ""}:            {"Route", "route.openshift.io/v1"},
	}

	internalMigrateTemplateInstancesLong = templates.LongDesc(fmt.Sprintf(`
		Migrate Template Instances to refer to new API groups

		This command locates and updates every Template Instance which refers to a particular
		group-version-kind to refer to some other, equivalent group-version-kind.

		The following transformations will occur:

%s`, prettyPrintMigrations(transforms)))

	internalMigrateTemplateInstancesExample = templates.Examples(`
		# Perform a dry-run of updating all objects
	  %[1]s

	  # To actually perform the update, the confirm flag must be appended
	  %[1]s --confirm`)
)

func prettyPrintMigrations(versionKinds map[apiType]apiType) string {
	lines := make([]string, 0, len(versionKinds))
	for initial, final := range versionKinds {
		line := fmt.Sprintf("		- %s.%s --> %s.%s", initial.APIVersion, initial.Kind, final.APIVersion, final.Kind)
		lines = append(lines, line)
	}
	sort.Strings(lines)

	return strings.Join(lines, "\n")
}

type MigrateTemplateInstancesOptions struct {
	templateClient templatev1typedclient.TemplateV1Interface

	migrate.ResourceOptions

	transforms map[apiType]apiType
}

func NewMigrateTemplateInstancesOptions(streams genericclioptions.IOStreams) *MigrateTemplateInstancesOptions {
	return &MigrateTemplateInstancesOptions{
		ResourceOptions: *migrate.NewResourceOptions(streams).WithIncludes([]string{"templateinstance"}).WithAllNamespaces(),
		transforms:      transforms,
	}
}

// NewCmdMigrateTemplateInstancesAPI implements a MigrateTemplateInstances command
func NewCmdMigrateTemplateInstances(name, fullName string, f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewMigrateTemplateInstancesOptions(streams)
	cmd := &cobra.Command{
		Use:     name,
		Short:   "Update TemplateInstances to point to the latest group-version-kinds",
		Long:    internalMigrateTemplateInstancesLong,
		Example: fmt.Sprintf(internalMigrateTemplateInstancesExample, fullName),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(name, f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}
	o.ResourceOptions.Bind(cmd)

	return cmd
}

func (o *MigrateTemplateInstancesOptions) Complete(name string, f kcmdutil.Factory, c *cobra.Command, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("%s takes no positional arguments", name)
	}

	o.ResourceOptions.SaveFn = o.save
	if err := o.ResourceOptions.Complete(f, c); err != nil {
		return err
	}

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.templateClient, err = templatev1typedclient.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	return nil
}

func (o MigrateTemplateInstancesOptions) Validate() error {
	return o.ResourceOptions.Validate()
}

func (o MigrateTemplateInstancesOptions) Run() error {
	return o.ResourceOptions.Visitor().Visit(func(info *resource.Info) (migrate.Reporter, error) {
		return o.checkAndTransform(info.Object)
	})
}

func (o *MigrateTemplateInstancesOptions) checkAndTransform(templateInstanceRaw runtime.Object) (migrate.Reporter, error) {
	templateInstance, wasTI := templateInstanceRaw.(*templatev1.TemplateInstance)
	if !wasTI {
		return nil, fmt.Errorf("unrecognized object %#v", templateInstanceRaw)
	}

	updated := false
	for i, obj := range templateInstance.Status.Objects {
		if newType, changed := o.transform(obj.Ref); changed {
			templateInstance.Status.Objects[i].Ref.Kind = newType.Kind
			templateInstance.Status.Objects[i].Ref.APIVersion = newType.APIVersion
			updated = true
		}
	}

	return migrate.ReporterBool(updated), nil
}

func (o *MigrateTemplateInstancesOptions) transform(ref corev1.ObjectReference) (apiType, bool) {
	oldType := apiType{ref.Kind, ref.APIVersion}
	if newType, ok := o.transforms[oldType]; ok {
		return newType, true
	}
	return oldType, false
}

// save invokes the API to alter an object. The reporter passed to this method is the same returned by
// the migration visitor method. It should return an error  if the input type cannot be saved
// It returns migrate.ErrRecalculate if migration should be re-run on the provided object.
func (o *MigrateTemplateInstancesOptions) save(info *resource.Info, reporter migrate.Reporter) error {
	templateInstance, wasTI := info.Object.(*templatev1.TemplateInstance)
	if !wasTI {
		return fmt.Errorf("unrecognized object %#v", info.Object)
	}

	_, err := o.templateClient.TemplateInstances(templateInstance.Namespace).UpdateStatus(templateInstance)
	return migrate.DefaultRetriable(info, err)
}
