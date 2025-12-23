package accept

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/client-go/config/clientset/versioned/fake"
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

	Client  clusterVersionInterface
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
			if trimmed == "-" {
				return kcmdutil.UsageErrorf(cmd, "illegal risk \"-\"")
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
	o.Client = client.ClusterVersions()

	// TODO remove this testing code
	o.Client = fake.NewClientset(&configv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: "version",
		},
	}).ConfigV1().ClusterVersions()
	return nil
}

func (o *options) Run(ctx context.Context) error {
	_, err := o.Client.Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("no cluster version information available - you must be connected to an OpenShift version 4 server to fetch the current version")
		}
		return err
	}

	// TODO: get it from the existing CV above
	// We need to bump o/api first
	risks := sets.New[string]("fakeRiskA", "fakeRiskB")
	newRisks := risks.Union(o.plus).Difference(o.minus)
	if o.replace {
		newRisks = o.plus
	}

	added := newRisks.Difference(risks)
	deleted := risks.Difference(newRisks)

	acceptedRisks := sets.List(newRisks)
	if err := patchDesiredUpdate(context.TODO(), acceptedRisks, o.Client, "version"); err != nil {
		return err
	}

	if o.replace {
		fmt.Fprintf(o.Out, "info: Accept risks are replaced with %s\n", strings.Join(acceptedRisks, ","))
	} else {
		fmt.Fprintf(o.Out, "info: Accept risks are %s with %s added and %s deleted\n", strings.Join(acceptedRisks, ","), nothingOrJoined(added), nothingOrJoined(deleted))
	}

	return nil
}

func nothingOrJoined(s sets.Set[string]) string {
	if s.Len() == 0 {
		return "nothing"
	}
	return strings.Join(sets.List(s), ",")
}

func patchDesiredUpdate(ctx context.Context, acceptRisks []string, client clusterVersionInterface,
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
