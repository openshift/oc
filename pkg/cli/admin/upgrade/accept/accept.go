package accept

import (
	"context"
	"fmt"
	"github.com/google/go-cmp/cmp"
	"sort"
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
		Accept risks exposed to conditional updates.

		Multiple risks are concatenated with comma. Append the provided accepted risks into the existing
		list. If --replace is specified, the existing accepted risks will be replaced with the provided
		ones instead of appending by default. Placing "-" as prefix to an accepted risk will lead to
		removal if it exists and no-ops otherwise. If --replace is specified, the prefix "-" on the risks
		is not allowed.

		The existing accepted risks can be removed by passing --clear.
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
	plus    sets.Set[string]
	minus   sets.Set[string]
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
		o.plus = sets.New[string]()
		o.minus = sets.New[string]()
		for _, s := range strings.Split(args[0], ",") {
			trimmed := strings.TrimSpace(s)
			if trimmed == "-" || trimmed == "" {
				return kcmdutil.UsageErrorf(cmd, "illegal risk %q", trimmed)
			}
			if strings.HasPrefix(trimmed, "-") {
				o.minus.Insert(trimmed[1:])
			} else {
				o.plus.Insert(trimmed)
			}
		}
	}

	if conflict := o.plus.Intersection(o.minus); conflict.Len() > 0 {
		return kcmdutil.UsageErrorf(cmd, "found conflicting risks: %s", strings.Join(sets.List(conflict), ","))
	}

	if o.replace && o.minus.Len() > 0 {
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
			return fmt.Errorf("no cluster version information available - you must be connected to an OpenShift version 4 server to fetch the current version")
		}
		return err
	}

	existing := map[string]configv1.AcceptRisk{}
	if cv.Spec.DesiredUpdate != nil {
		for _, risk := range cv.Spec.DesiredUpdate.AcceptRisks {
			existing[risk.Name] = risk
		}
	}
	acceptRisks := getAcceptRisks(existing, o.replace, o.clear, o.plus, o.minus)

	var update *configv1.Update
	if cv.Spec.DesiredUpdate != nil {
		update = cv.Spec.DesiredUpdate.DeepCopy()
		update.AcceptRisks = acceptRisks
	} else if len(acceptRisks) > 0 {
		update = &configv1.Update{
			Architecture: cv.Status.Desired.Architecture,
			Image:        cv.Status.Desired.Image,
			Version:      cv.Status.Desired.Version,
			AcceptRisks:  acceptRisks,
		}
	}
	if diff := cmp.Diff(update, cv.Spec.DesiredUpdate); diff != "" {
		cv.Spec.DesiredUpdate = update
		cv, err = o.Client.ConfigV1().ClusterVersions().Update(ctx, cv, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("unable to upgrade: %w", err)
		}
		var names []string
		if cv.Spec.DesiredUpdate != nil {
			for _, risk := range cv.Spec.DesiredUpdate.AcceptRisks {
				names = append(names, risk.Name)
			}
		}
		_, _ = fmt.Fprintf(o.Out, "info: Accept risks are [%s]\n", strings.Join(names, ", "))
	} else {
		_, _ = fmt.Fprintf(o.Out, "info: Accept risks are not changed\n")
	}

	return nil
}

func getAcceptRisks(existing map[string]configv1.AcceptRisk, replace, clear bool, plus sets.Set[string], minus sets.Set[string]) []configv1.AcceptRisk {
	var acceptRisks []configv1.AcceptRisk

	if clear {
		return acceptRisks
	}

	for name := range plus {
		if r, ok := existing[name]; ok {
			acceptRisks = append(acceptRisks, r)
		} else {
			acceptRisks = append(acceptRisks, configv1.AcceptRisk{
				Name: name,
			})
		}
	}

	if !replace {
		for name, r := range existing {
			if !plus.Has(name) && !minus.Has(name) {
				acceptRisks = append(acceptRisks, r)
			}
		}
	}

	sort.Slice(acceptRisks, func(i, j int) bool {
		return acceptRisks[i].Name < acceptRisks[j].Name
	})
	return acceptRisks
}
