package accept

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/go-cmp/cmp"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

func newOptions(streams genericiooptions.IOStreams) *options {
	return &options{
		IOStreams: streams,
	}
}

var (
	acceptExample = templates.Examples(`
		# Accept RiskA and RiskB and stop accepting RiskC if accepted
		oc adm upgrade accept RiskA,RiskB,-RiskC

		# Accept RiskA and RiskB and nothing else
		oc adm upgrade accept --replace RiskA,RiskB

		# Accept no risks
		oc adm upgrade accept --clear
	`)

	acceptLong = templates.LongDesc(`
		Manage update risk acceptance.

		Multiple risks are concatenated with comma. By default, the command appends the provided accepted risks into the existing
		list. If --replace is specified, the existing accepted risks will be replaced with the provided
		ones instead of appending. Placing "-" as prefix to an accepted risk will lead to
		removal if it exists and no-ops otherwise. If --replace is specified, the prefix "-" on the risks
		is not allowed.

		Passing --clear removes all existing excepted risks.
		`)
)

func New(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := newOptions(streams)
	cmd := &cobra.Command{
		Use:     "accept",
		Hidden:  true,
		Short:   "Accept risks exposed to conditional updates.",
		Long:    acceptLong,
		Example: acceptExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run(cmd.Context()))
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&o.replace, "replace", false, "Replace existing accepted risks with new ones")
	flags.BoolVar(&o.clear, "clear", false, "Remove all existing accepted risks")
	return cmd
}

// clusterVersionInterface is the subset of configv1client.ClusterVersionInterface
// that we need, for easier mocking in unit tests.
type clusterVersionInterface interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*configv1.ClusterVersion, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *configv1.ClusterVersion, err error)
}

type options struct {
	genericiooptions.IOStreams

	Client  configv1client.Interface
	replace bool
	clear   bool
	add     sets.Set[string]
	remove  sets.Set[string]
}

func (o *options) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if o.clear && o.replace {
		return kcmdutil.UsageErrorf(cmd, "--clear and --replace are mutually exclusive")
	}

	if o.clear {
		kcmdutil.RequireNoArguments(cmd, args)
	} else if len(args) == 0 {
		return kcmdutil.UsageErrorf(cmd, "no positional arguments given")
	}

	if len(args) > 1 {
		return kcmdutil.UsageErrorf(cmd, "multiple positional arguments given")
	} else if len(args) == 1 {
		o.add = sets.New[string]()
		o.remove = sets.New[string]()
		for _, s := range strings.Split(args[0], ",") {
			trimmed := strings.TrimSpace(s)
			if trimmed == "-" || trimmed == "" {
				return kcmdutil.UsageErrorf(cmd, "illegal risk %q", trimmed)
			}
			if strings.HasPrefix(trimmed, "-") {
				o.remove.Insert(trimmed[1:])
			} else {
				o.add.Insert(trimmed)
			}
		}
	}

	if conflict := o.add.Intersection(o.remove); conflict.Len() > 0 {
		return kcmdutil.UsageErrorf(cmd, "requested risks with both Risk and -Risk: %s", strings.Join(sets.List(conflict), ","))
	}

	if o.replace && o.remove.Len() > 0 {
		return kcmdutil.UsageErrorf(cmd, "The prefix '-' on risks is not allowed if --replace is specified")
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

func (o *options) Run(ctx context.Context) error {
	cv, err := o.Client.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("no cluster version information available - you must be connected to an OpenShift server to fetch the current version")
		}
		return err
	}

	var existing []configv1.AcceptRisk
	if cv.Spec.DesiredUpdate != nil {
		existing = cv.Spec.DesiredUpdate.AcceptRisks
	}
	acceptRisks := getAcceptRisks(existing, o.replace, o.clear, o.add, o.remove)

	if diff := cmp.Diff(acceptRisks, existing); diff != "" {
		if err := patchDesiredUpdate(context.TODO(), acceptRisks, o.Client.ConfigV1().ClusterVersions(), "version"); err != nil {
			return err
		}
		var names []string
		for _, risk := range acceptRisks {
			names = append(names, risk.Name)
		}
		_, _ = fmt.Fprintf(o.Out, "info: Accept risks are [%s]\n", strings.Join(names, ", "))
	} else {
		_, _ = fmt.Fprintf(o.Out, "info: Accept risks are not changed\n")
	}

	return nil
}

func getAcceptRisks(existing []configv1.AcceptRisk, replace, clear bool, add sets.Set[string], remove sets.Set[string]) []configv1.AcceptRisk {
	var acceptRisks []configv1.AcceptRisk

	if clear {
		return acceptRisks
	}

	if !replace {
		for _, risk := range existing {
			if !remove.Has(risk.Name) {
				acceptRisks = append(acceptRisks, *risk.DeepCopy())
			}
		}
	}

	riskNames := sets.New[string]()
	for _, risk := range acceptRisks {
		riskNames.Insert(risk.Name)
	}

	for _, name := range sets.List[string](add) {
		if !riskNames.Has(name) && !remove.Has(name) {
			acceptRisks = append(acceptRisks, configv1.AcceptRisk{
				Name: name,
			})
		}
	}

	return acceptRisks
}

func patchDesiredUpdate(ctx context.Context, acceptRisks []configv1.AcceptRisk, client clusterVersionInterface,
	clusterVersionName string) error {
	acceptRisksJSON, err := json.Marshal(acceptRisks)
	if err != nil {
		return fmt.Errorf("marshal ClusterVersion patch: %v", err)
	}
	patch := []byte(fmt.Sprintf(`{"spec": {"desiredUpdate": {"acceptRisks": %s}}}`, acceptRisksJSON))
	if _, err := client.Patch(ctx, clusterVersionName, types.MergePatchType, patch,
		metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("unable to accept risks: %v", err)
	}
	return nil
}
