package deploymentconfigs

import (
	"bytes"
	"fmt"
	"sort"
	"text/tabwriter"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/kubectl/pkg/describe/versioned"
	"k8s.io/kubectl/pkg/polymorphichelpers"

	appsv1 "github.com/openshift/api/apps/v1"
	"github.com/openshift/library-go/pkg/apps/appsutil"
)

func NewDeploymentConfigHistoryViewer(kc kubernetes.Interface) polymorphichelpers.HistoryViewer {
	return &DeploymentConfigHistoryViewer{rn: kc.CoreV1()}
}

// DeploymentConfigHistoryViewer is an implementation of the kubectl HistoryViewer interface
// for deployment configs.
type DeploymentConfigHistoryViewer struct {
	rn corev1client.ReplicationControllersGetter
}

var _ polymorphichelpers.HistoryViewer = &DeploymentConfigHistoryViewer{}

// ViewHistory returns a description of all the history it can find for a deployment config.
func (h *DeploymentConfigHistoryViewer) ViewHistory(namespace, name string, revision int64) (string, error) {
	opts := metav1.ListOptions{LabelSelector: appsutil.ConfigSelector(name).String()}
	deploymentList, err := h.rn.ReplicationControllers(namespace).List(opts)
	if err != nil {
		return "", err
	}

	if len(deploymentList.Items) == 0 {
		return "No rollout history found.", nil
	}

	items := deploymentList.Items
	history := make([]*v1.ReplicationController, 0, len(items))
	for i := range items {
		history = append(history, &items[i])
	}

	// Print details of a specific revision
	if revision > 0 {
		var desired *v1.PodTemplateSpec
		// We could use a binary search here but brute-force is always faster to write
		for i := range history {
			rc := history[i]

			if appsutil.DeploymentVersionFor(rc) == revision {
				desired = rc.Spec.Template
				break
			}
		}

		if desired == nil {
			return "", fmt.Errorf("unable to find the specified revision")
		}

		buf := bytes.NewBuffer([]byte{})

		versioned.DescribePodTemplate(desired, versioned.NewPrefixWriter(buf))
		return buf.String(), nil
	}

	sort.Sort(appsutil.ByLatestVersionAsc(history))

	return tabbedString(func(out *tabwriter.Writer) error {
		fmt.Fprintf(out, "REVISION\tSTATUS\tCAUSE\n")
		for i := range history {
			rc := history[i]

			rev := appsutil.DeploymentVersionFor(rc)
			status := appsutil.AnnotationFor(rc, appsv1.DeploymentStatusAnnotation)
			cause := rc.Annotations[appsv1.DeploymentStatusReasonAnnotation]
			if len(cause) == 0 {
				cause = "<unknown>"
			}
			fmt.Fprintf(out, "%d\t%s\t%s\n", rev, status, cause)
		}
		return nil
	})
}

// TODO: Re-use from an utility package
func tabbedString(f func(*tabwriter.Writer) error) (string, error) {
	out := new(tabwriter.Writer)
	buf := &bytes.Buffer{}
	out.Init(buf, 0, 8, 1, '\t', 0)

	err := f(out)
	if err != nil {
		return "", err
	}

	out.Flush()
	str := string(buf.String())
	return str, nil
}
