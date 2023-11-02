// Package status displays the status of current cluster version updates.
package status

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
)

const (
	// clusterStatusFailing is set on the ClusterVersion status when a cluster
	// cannot reach the desired state.
	clusterStatusFailing = configv1.ClusterStatusConditionType("Failing")
)

func newOptions(streams genericiooptions.IOStreams) *options {
	return &options{
		IOStreams: streams,
	}
}

func New(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := newOptions(streams)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Display the status of current cluster version updates.",
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run(cmd.Context()))
		},
	}

	flags := cmd.Flags()
	// TODO: We can remove these flags once the idea about `oc adm upgrade status` stabilizes and the command
	//       is promoted out of the OC_ENABLE_CMD_UPGRADE_STATUS feature gate
	flags.StringVar(&o.mockCvPath, "mock-clusterversion", "", "Path to a YAML ClusterVersion object to use for testing (will be removed later).")
	flags.StringVar(&o.mockOperatorsPath, "mock-clusteroperators", "", "Path to a YAML ClusterOperatorList to use for testing (will be removed later).")

	return cmd
}

type options struct {
	genericiooptions.IOStreams

	mockCvPath           string
	mockOperatorsPath    string
	mockClusterVersion   *configv1.ClusterVersion
	mockClusterOperators *configv1.ClusterOperatorList

	Client configv1client.Interface
}

func (o *options) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return kcmdutil.UsageErrorf(cmd, "positional arguments given")
	}

	if (o.mockCvPath == "") != (o.mockOperatorsPath == "") {
		return fmt.Errorf("--mock-clusterversion and --mock-clusteroperators must be used together")
	}

	if o.mockCvPath == "" {
		cfg, err := f.ToRESTConfig()
		if err != nil {
			return err
		}
		client, err := configv1client.NewForConfig(cfg)
		if err != nil {
			return err
		}
		o.Client = client
	} else {
		// Process the mock data passed in --mock-clusterversion and --mockclusteroperators
		scheme := runtime.NewScheme()
		codecs := serializer.NewCodecFactory(scheme)

		if err := configv1.Install(scheme); err != nil {
			return err
		}
		if err := corev1.AddToScheme(scheme); err != nil {
			return err
		}
		decoder := codecs.UniversalDecoder(configv1.GroupVersion, corev1.SchemeGroupVersion)

		cvBytes, err := os.ReadFile(o.mockCvPath)
		if err != nil {
			return err
		}
		cvObj, err := runtime.Decode(decoder, cvBytes)
		if err != nil {
			return err
		}
		switch cvObj.(type) {
		case *configv1.ClusterVersion:
			o.mockClusterVersion = cvObj.(*configv1.ClusterVersion)
		case *configv1.ClusterVersionList:
			o.mockClusterVersion = &cvObj.(*configv1.ClusterVersionList).Items[0]
		case *corev1.List:
			cvObj, err := runtime.Decode(decoder, cvObj.(*corev1.List).Items[0].Raw)
			if err != nil {
				return err
			}
			cv, ok := cvObj.(*configv1.ClusterVersion)
			if !ok {
				return fmt.Errorf("unexpected object type %T in --mock-clusterversion=%s List content", cvObj, o.mockCvPath)
			}
			o.mockClusterVersion = cv
		default:
			return fmt.Errorf("unexpected object type %T in --mock-clusterversion=%s content", cvObj, o.mockCvPath)
		}

		coListBytes, err := os.ReadFile(o.mockOperatorsPath)
		if err != nil {
			return err
		}
		coListObj, err := runtime.Decode(decoder, coListBytes)
		if err != nil {
			return err
		}
		switch coListObj.(type) {
		case *configv1.ClusterOperatorList:
			o.mockClusterOperators = coListObj.(*configv1.ClusterOperatorList)
		case *corev1.List:
			o.mockClusterOperators = &configv1.ClusterOperatorList{}
			coListItems := coListObj.(*corev1.List).Items
			for i := range coListItems {
				coObj, err := runtime.Decode(decoder, coListItems[i].Raw)
				if err != nil {
					return err
				}
				co, ok := coObj.(*configv1.ClusterOperator)
				if !ok {
					return fmt.Errorf("unexpected object type %T in --mock-clusteroperators=%s List content at index %d", coObj, o.mockOperatorsPath, i)
				}
				o.mockClusterOperators.Items = append(o.mockClusterOperators.Items, *co)

			}
		default:
			return fmt.Errorf("unexpected object type %T in --mock-clusteroperators=%s content", cvObj, o.mockOperatorsPath)
		}
	}

	return nil
}

func (o *options) Run(ctx context.Context) error {
	var cv *configv1.ClusterVersion
	if cv = o.mockClusterVersion; cv == nil {
		var err error
		cv, err = o.Client.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("no cluster version information available - you must be connected to an OpenShift version 4 server to fetch the current version")
			}
			return err
		}
	}

	var operators *configv1.ClusterOperatorList
	if operators = o.mockClusterOperators; operators == nil {
		var err error
		operators, err = o.Client.ConfigV1().ClusterOperators().List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
	}
	if len(operators.Items) == 0 {
		return fmt.Errorf("no cluster operator information available - you must be connected to an OpenShift version 4 server")
	}

	progressing := findClusterOperatorStatusCondition(cv.Status.Conditions, configv1.OperatorProgressing)
	if progressing == nil {
		return fmt.Errorf("no current %s info, see `oc describe clusterversion` for more details.\n", configv1.OperatorProgressing)
	}

	if progressing.Status != configv1.ConditionTrue {
		fmt.Fprintf(o.Out, "The cluster version is not updating (%s=%s).\n\n  Reason: %s\n  Message: %s\n", progressing.Type, progressing.Status, progressing.Reason, strings.ReplaceAll(progressing.Message, "\n", "\n  "))
		return nil
	}

	fmt.Fprintf(o.Out, "An update is in progress for %s: %s\n", time.Since(progressing.LastTransitionTime.Time).Round(time.Second), progressing.Message)

	if c := findClusterOperatorStatusCondition(cv.Status.Conditions, clusterStatusFailing); c != nil {
		if c.Status != configv1.ConditionFalse {
			fmt.Fprintf(o.Out, "\n%s=%s:\n\n  Reason: %s\n  Message: %s\n\n", c.Type, c.Status, c.Reason, strings.ReplaceAll(c.Message, "\n", "\n  "))
		}
	} else {
		fmt.Fprintf(o.ErrOut, "warning: No current %s info, see `oc describe clusterversion` for more details.\n", clusterStatusFailing)
	}

	return nil
}

func findClusterOperatorStatusCondition(conditions []configv1.ClusterOperatorStatusCondition, name configv1.ClusterStatusConditionType) *configv1.ClusterOperatorStatusCondition {
	for i := range conditions {
		if conditions[i].Type == name {
			return &conditions[i]
		}
	}
	return nil
}
