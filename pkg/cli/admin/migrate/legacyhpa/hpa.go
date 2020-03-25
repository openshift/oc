package legacyhpa

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	autoscalingv1typedclient "k8s.io/client-go/kubernetes/typed/autoscaling/v1"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/cli/admin/migrate"
)

var (
	defaultMigrations = map[metav1.TypeMeta]metav1.TypeMeta{
		// legacy oapi group
		{Kind: "DeploymentConfig", APIVersion: "v1"}: {Kind: "DeploymentConfig", APIVersion: "apps.openshift.io/v1"},
		// legacy oapi group, for the lazy
		{Kind: "DeploymentConfig"}: {Kind: "DeploymentConfig", APIVersion: "apps.openshift.io/v1"},

		// webconsole shenaniganry
		{Kind: "DeploymentConfig", APIVersion: "extensions/v1beta1"}:      {Kind: "DeploymentConfig", APIVersion: "apps.openshift.io/v1"},
		{Kind: "Deployment", APIVersion: "extensions/v1beta1"}:            {Kind: "Deployment", APIVersion: "apps/v1"},
		{Kind: "ReplicaSet", APIVersion: "extensions/v1beta1"}:            {Kind: "ReplicaSet", APIVersion: "apps/v1"},
		{Kind: "ReplicationController", APIVersion: "extensions/v1beta1"}: {Kind: "ReplicationController", APIVersion: "v1"},
	}

	internalMigrateLegacyHPALong = templates.LongDesc(fmt.Sprintf(`
		Migrate Horizontal Pod Autoscalers to refer to new API groups

		This command locates and updates every Horizontal Pod Autoscaler which refers to a particular
		group-version-kind to refer to some other, equivalent group-version-kind.

		The following transformations will occur:

%s`, prettyPrintMigrations(defaultMigrations)))

	internalMigrateLegacyHPAExample = templates.Examples(`
		# Perform a dry-run of updating all objects
	  %[1]s

	  # To actually perform the update, the confirm flag must be appended
	  %[1]s --confirm`)
)

func prettyPrintMigrations(versionKinds map[metav1.TypeMeta]metav1.TypeMeta) string {
	lines := make([]string, 0, len(versionKinds))
	for initial, final := range versionKinds {
		line := fmt.Sprintf("		- %s.%s --> %s.%s", initial.APIVersion, initial.Kind, final.APIVersion, final.Kind)
		lines = append(lines, line)
	}
	sort.Strings(lines)

	return strings.Join(lines, "\n")
}

type MigrateLegacyHPAOptions struct {
	// maps initial gvks to final gvks in the same format
	// as HPAs use (CrossVersionObjectReferences) for ease of access.
	finalVersionKinds map[metav1.TypeMeta]metav1.TypeMeta

	hpaClient autoscalingv1typedclient.AutoscalingV1Interface

	migrate.ResourceOptions
}

func NewMigrateLegacyHPAOptions(streams genericclioptions.IOStreams) *MigrateLegacyHPAOptions {
	return &MigrateLegacyHPAOptions{
		ResourceOptions: *migrate.NewResourceOptions(streams).WithIncludes([]string{"horizontalpodautoscalers.autoscaling"}).WithAllNamespaces(),
	}
}

// NewCmdMigrateLegacyAPI implements a MigrateLegacyHPA command
func NewCmdMigrateLegacyHPA(name, fullName string, f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewMigrateLegacyHPAOptions(streams)
	cmd := &cobra.Command{
		Use:        name,
		Short:      "Update HPAs to point to the latest group-version-kinds",
		Long:       internalMigrateLegacyHPALong,
		Example:    fmt.Sprintf(internalMigrateLegacyHPAExample, fullName),
		Deprecated: "migration of content is managed automatically in OpenShift 4.x",
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(name, f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}
	o.ResourceOptions.Bind(cmd)

	return cmd
}

func (o *MigrateLegacyHPAOptions) Complete(name string, f kcmdutil.Factory, c *cobra.Command, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("%s takes no positional arguments", name)
	}

	o.ResourceOptions.SaveFn = o.save
	if err := o.ResourceOptions.Complete(f, c); err != nil {
		return err
	}

	o.finalVersionKinds = make(map[metav1.TypeMeta]metav1.TypeMeta)

	// copy all manual transformations in
	for initial, final := range defaultMigrations {
		o.finalVersionKinds[initial] = final
	}

	config, err := f.ToRESTConfig()
	if err != nil {
		return err
	}

	o.hpaClient, err = autoscalingv1typedclient.NewForConfig(config)
	if err != nil {
		return err
	}

	return nil
}

func (o MigrateLegacyHPAOptions) Validate() error {
	if len(o.ResourceOptions.Include) != 1 || o.ResourceOptions.Include[0] != "horizontalpodautoscalers.autoscaling" {
		return fmt.Errorf("the only supported resources are horizontalpodautoscalers")
	}
	return o.ResourceOptions.Validate()
}

func (o MigrateLegacyHPAOptions) Run() error {
	return o.ResourceOptions.Visitor().Visit(func(info *resource.Info) (migrate.Reporter, error) {
		return o.checkAndTransform(info.Object)
	})
}

func (o *MigrateLegacyHPAOptions) checkAndTransform(hpaRaw runtime.Object) (migrate.Reporter, error) {
	hpa, wasHPA := hpaRaw.(*autoscalingv1.HorizontalPodAutoscaler)
	if !wasHPA {
		return nil, fmt.Errorf("unrecognized object %#v", hpaRaw)
	}

	currentVersionKind := metav1.TypeMeta{
		APIVersion: hpa.Spec.ScaleTargetRef.APIVersion,
		Kind:       hpa.Spec.ScaleTargetRef.Kind,
	}

	newVersionKind := o.latestVersionKind(currentVersionKind)

	if currentVersionKind != newVersionKind {
		hpa.Spec.ScaleTargetRef.APIVersion = newVersionKind.APIVersion
		hpa.Spec.ScaleTargetRef.Kind = newVersionKind.Kind
		return migrate.ReporterBool(true), nil
	}

	return migrate.ReporterBool(false), nil
}

func (o *MigrateLegacyHPAOptions) latestVersionKind(current metav1.TypeMeta) metav1.TypeMeta {
	if newVersionKind, isKnown := o.finalVersionKinds[current]; isKnown {
		return newVersionKind
	}

	return current
}

// save invokes the API to alter an object. The reporter passed to this method is the same returned by
// the migration visitor method. It should return an error  if the input type cannot be saved
// It returns migrate.ErrRecalculate if migration should be re-run on the provided object.
func (o *MigrateLegacyHPAOptions) save(info *resource.Info, reporter migrate.Reporter) error {
	hpa, wasHPA := info.Object.(*autoscalingv1.HorizontalPodAutoscaler)
	if !wasHPA {
		return fmt.Errorf("unrecognized object %#v", info.Object)
	}

	_, err := o.hpaClient.HorizontalPodAutoscalers(hpa.Namespace).Update(context.TODO(), hpa, metav1.UpdateOptions{})
	return migrate.DefaultRetriable(info, err)
}
