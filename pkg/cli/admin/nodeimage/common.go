package nodeimage

import (
	"context"
	"fmt"
	"time"

	configclient "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/openshift/library-go/pkg/operator/resource/retry"
	ocrelease "github.com/openshift/oc/pkg/cli/admin/release"
	imagemanifest "github.com/openshift/oc/pkg/cli/image/manifest"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/cmd/exec"
)

type CommonOptions struct {
	genericiooptions.IOStreams
	SecurityOptions imagemanifest.SecurityOptions

	Config                   *rest.Config
	remoteExecutor           exec.RemoteExecutor
	ConfigClient             configclient.Interface
	Client                   kubernetes.Interface
	nodeJoinerImage          string
	nodeJoinerNamespace      *corev1.Namespace
	nodeJoinerServiceAccount *corev1.ServiceAccount
	nodeJoinerRole           *rbacv1.ClusterRole
	RESTClientGetter         genericclioptions.RESTClientGetter
	nodeJoinerPod            *corev1.Pod
	command                  string
}

func (o *CommonOptions) getNodeJoinerPullSpec(ctx context.Context) error {
	// Get the current cluster release version.
	releaseImage, err := o.fetchClusterReleaseImage(ctx)
	if err != nil {
		return err
	}

	// Extract the baremetal-installer image pullspec, since it
	// provide the node-joiner tool.
	opts := ocrelease.NewInfoOptions(o.IOStreams)
	opts.SecurityOptions = o.SecurityOptions
	release, err := opts.LoadReleaseInfo(releaseImage, false)
	if err != nil {
		return err
	}

	tagName := "baremetal-installer"
	for _, tag := range release.References.Spec.Tags {
		if tag.Name == tagName {
			o.nodeJoinerImage = tag.From.Name
			return nil
		}
	}

	return fmt.Errorf("no image tag %q exists in the release image %s", tagName, releaseImage)
}

func (o *CommonOptions) fetchClusterReleaseImage(ctx context.Context) (string, error) {
	cv, err := o.ConfigClient.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		if kapierrors.IsNotFound(err) || kapierrors.ReasonForError(err) == metav1.StatusReasonUnknown {
			klog.V(2).Infof("Unable to find cluster version object from cluster: %v", err)
			return "", fmt.Errorf("command expects a connection to an OpenShift 4.x server")
		}
	}
	image := cv.Status.Desired.Image
	if len(image) == 0 && cv.Spec.DesiredUpdate != nil {
		image = cv.Spec.DesiredUpdate.Image
	}
	if len(image) == 0 {
		return "", fmt.Errorf("the server is not reporting a release image at this time")
	}

	return image, nil
}

func (o *CommonOptions) createNamespace(ctx context.Context) error {
	nsNodeJoiner := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "openshift-node-joiner-",
			Annotations: map[string]string{
				"oc.openshift.io/command":    o.command,
				"openshift.io/node-selector": "",
			},
		},
	}

	ns, err := o.Client.CoreV1().Namespaces().Create(ctx, nsNodeJoiner, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create namespace: %w", err)
	}

	o.nodeJoinerNamespace = ns
	return nil
}

func (o *CommonOptions) cleanup(ctx context.Context) {
	if o.nodeJoinerNamespace == nil {
		return
	}

	err := o.Client.CoreV1().Namespaces().Delete(ctx, o.nodeJoinerNamespace.GetName(), metav1.DeleteOptions{})
	if err != nil {
		klog.Errorf("cannot delete namespace %s: %v\n", o.nodeJoinerNamespace.GetName(), err)
	}
}

func (o *CommonOptions) createServiceAccount(ctx context.Context) error {
	nodeJoinerServiceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-joiner-",
			Annotations: map[string]string{
				"oc.openshift.io/command": o.command,
			},
			Namespace: o.nodeJoinerNamespace.GetName(),
		},
	}

	sa, err := o.Client.CoreV1().ServiceAccounts(o.nodeJoinerNamespace.GetName()).Create(ctx, nodeJoinerServiceAccount, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create service account: %w", err)
	}

	o.nodeJoinerServiceAccount = sa
	return nil
}

func (o *CommonOptions) clusterRoleBindings() *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-joiner-monitor-",
			Annotations: map[string]string{
				"oc.openshift.io/command": o.command,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Namespace",
					Name:       o.nodeJoinerNamespace.GetName(),
					UID:        o.nodeJoinerNamespace.GetUID(),
				},
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      o.nodeJoinerServiceAccount.GetName(),
				Namespace: o.nodeJoinerNamespace.GetName(),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     o.nodeJoinerRole.GetName(),
		},
	}
}

func (o *CommonOptions) waitForContainerRunning(ctx context.Context) error {
	// Wait for the node-joiner pod to come up
	return wait.PollUntilContextTimeout(
		ctx,
		time.Second*1,
		time.Minute*5,
		true,
		func(ctx context.Context) (done bool, err error) {
			pod, err := o.Client.CoreV1().Pods(o.nodeJoinerNamespace.GetName()).Get(context.TODO(), o.nodeJoinerPod.GetName(), metav1.GetOptions{})
			if err == nil {
				klog.V(2).Info("Waiting for pod")
				if len(pod.Status.ContainerStatuses) == 0 {
					return false, nil
				}
				state := pod.Status.ContainerStatuses[0].State
				if state.Waiting != nil {
					switch state.Waiting.Reason {
					case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
						return true, fmt.Errorf("unable to pull image: %v: %v", state.Waiting.Reason, state.Waiting.Message)
					}
				}
				return state.Running != nil || state.Terminated != nil, nil
			}
			if retry.IsHTTPClientError(err) {
				return false, nil
			}
			return false, err
		})
}
