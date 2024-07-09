package nodeimage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strconv"
	"time"

	ocrelease "github.com/openshift/oc/pkg/cli/admin/release"
	"github.com/openshift/oc/pkg/cli/rsync"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	kutils "k8s.io/client-go/util/exec"
	"k8s.io/kubectl/pkg/cmd/exec"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	configclient "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/openshift/library-go/pkg/operator/resource/retry"
	imagemanifest "github.com/openshift/oc/pkg/cli/image/manifest"
)

const (
	nodeJoinerConfigurationFile = "nodes-config.yaml"
	nodeJoinerContainer         = "node-joiner"
)

func NewCreate(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewCreateOptions(streams)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an ISO image for booting the nodes to be added to the target cluster",
		Long: templates.LongDesc(`
			<TODO>
		`),
		Example: templates.Examples(`
			<TODO>
		`),
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}

	flags := cmd.Flags()
	o.SecurityOptions.Bind(flags)

	flags.StringVar(&o.AssetsDir, "dir", o.AssetsDir, "The path containing the configuration file, used also to store the generated artifacts.")
	flags.StringVarP(&o.OutputName, "output-name", "o", "node.iso", "The name of the output image.")
	return cmd
}

func NewCreateOptions(streams genericiooptions.IOStreams) *CreateOptions {
	return &CreateOptions{
		IOStreams: streams,
	}
}

type CreateOptions struct {
	genericiooptions.IOStreams
	SecurityOptions imagemanifest.SecurityOptions

	Config         *rest.Config
	Client         kubernetes.Interface
	ConfigClient   configclient.Interface
	FSys           fs.FS
	RemoteExecutor exec.RemoteExecutor
	CopyStrategy   func(*rsync.RsyncOptions) rsync.CopyStrategy

	AssetsDir  string
	OutputName string

	factory                  kcmdutil.Factory
	nodeJoinerImage          string
	nodeJoinerNamespace      *corev1.Namespace
	nodeJoinerServiceAccount *corev1.ServiceAccount
	nodeJoinerRole           *rbacv1.ClusterRole
	nodeJoinerPod            *corev1.Pod
	nodeJoinerExitCode       int
	rsyncRshCmd              string
}

func (o *CreateOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	o.factory = f

	var err error
	if o.Config, err = f.ToRESTConfig(); err != nil {
		return err
	}
	if o.Client, err = kubernetes.NewForConfig(o.Config); err != nil {
		return err
	}
	if o.ConfigClient, err = configclient.NewForConfig(o.Config); err != nil {
		return err
	}

	if o.AssetsDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		o.AssetsDir = cwd
	}
	o.FSys = os.DirFS(o.AssetsDir)
	o.RemoteExecutor = &exec.DefaultRemoteExecutor{}
	o.rsyncRshCmd = rsync.DefaultRsyncRemoteShellToUse(cmd)
	o.CopyStrategy = func(o *rsync.RsyncOptions) rsync.CopyStrategy {
		return rsync.NewDefaultCopyStrategy(o)
	}
	return nil
}

func (o *CreateOptions) Validate() error {
	return nil
}

func (o *CreateOptions) Run() error {
	ctx := context.Background()
	defer o.cleanup(ctx)

	err := o.runNodeJoinerPod(ctx)
	if err != nil {
		return err
	}

	err = o.waitForCompletion(ctx)
	if err != nil {
		return err
	}
	// Something went wrong during the node-joiner tool execution,
	// let's show the logs and return an error
	if o.nodeJoinerExitCode != 0 {
		err = o.printLogs(ctx)
		if err != nil {
			return err
		}
		return fmt.Errorf("image generation error (exit code: %d)", o.nodeJoinerExitCode)
	}

	err = o.copyArtifactsFromNodeJoinerPod()
	if err != nil {
		return err
	}
	klog.V(1).Info("Command successfully completed")
	return nil
}

func (o *CreateOptions) printLogs(ctx context.Context) error {
	logOptions := &corev1.PodLogOptions{
		Container:  nodeJoinerContainer,
		Timestamps: true,
	}
	readCloser, err := o.Client.CoreV1().Pods(o.nodeJoinerNamespace.GetName()).GetLogs(o.nodeJoinerPod.GetName(), logOptions).Stream(ctx)
	if err != nil {
		return err
	}
	defer readCloser.Close()

	_, err = io.Copy(o.IOStreams.ErrOut, readCloser)
	return err
}

func (o *CreateOptions) copyArtifactsFromNodeJoinerPod() error {
	klog.V(2).Info("\nCopying artifacts")
	rsyncOptions := &rsync.RsyncOptions{
		Namespace:     o.nodeJoinerNamespace.GetName(),
		Source:        &rsync.PathSpec{PodName: o.nodeJoinerPod.GetName(), Path: path.Join("/assets", "node.x86_64.iso")},
		ContainerName: nodeJoinerContainer,
		Destination:   &rsync.PathSpec{PodName: "", Path: path.Join(o.AssetsDir, o.OutputName)},
		Client:        o.Client,
		Config:        o.Config,
		Compress:      true,
		RshCmd:        fmt.Sprintf("%s --namespace=%s -c %s", o.rsyncRshCmd, o.nodeJoinerNamespace.GetName(), nodeJoinerContainer),
		IOStreams:     o.IOStreams,
		Quiet:         true,
	}
	rsyncOptions.Strategy = o.CopyStrategy(rsyncOptions)
	return rsyncOptions.RunRsync()
}

func (o *CreateOptions) waitForCompletion(ctx context.Context) error {
	klog.V(2).Info("Starting command")
	// Wait for the node-joiner pod to come up
	err := wait.PollUntilContextTimeout(
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
	if err != nil {
		return err
	}

	// Wait for the node-joiner cli tool to complete
	return wait.PollUntilContextTimeout(
		ctx,
		time.Second*5,
		time.Minute*5,
		true,
		func(ctx context.Context) (done bool, err error) {
			w := &bytes.Buffer{}
			wErr := &bytes.Buffer{}

			execOptions := &exec.ExecOptions{
				StreamOptions: exec.StreamOptions{
					Namespace:     o.nodeJoinerNamespace.GetName(),
					PodName:       o.nodeJoinerPod.GetName(),
					ContainerName: nodeJoinerContainer,
					IOStreams: genericiooptions.IOStreams{
						In:     nil,
						Out:    w,
						ErrOut: wErr,
					},
					Stdin: false,
					Quiet: false,
				},
				Executor:  o.RemoteExecutor,
				PodClient: o.Client.CoreV1(),
				Config:    o.Config,
				Command: []string{
					"cat", "/assets/exit_code",
				},
			}

			err = execOptions.Validate()
			if err != nil {
				return false, err
			}

			klog.V(1).Info("Image generation in progress, please wait")
			err = execOptions.Run()
			if err != nil {
				var codeExitErr kutils.CodeExitError
				if !errors.As(err, &codeExitErr) {
					return false, err
				}
				if codeExitErr.Code != 1 {
					return false, fmt.Errorf("unexpected error code: %w", codeExitErr)
				}
				return false, nil
			}

			// Extract node-joiner tool exit code on completion
			o.nodeJoinerExitCode, err = strconv.Atoi(w.String())
			if err != nil {
				return false, err
			}
			return true, nil
		})
}

func (o *CreateOptions) runNodeJoinerPod(ctx context.Context) error {
	tasks := []func(context.Context) error{
		o.getNodeJoinerPullSpec,
		o.createNamespace,
		o.createServiceAccount,
		o.createRolesAndBindings,
		o.createInputConfigMap,
		o.createPod,
	}
	for _, task := range tasks {
		if err := task(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (o *CreateOptions) getNodeJoinerPullSpec(ctx context.Context) error {
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

func (o *CreateOptions) fetchClusterReleaseImage(ctx context.Context) (string, error) {
	cv, err := o.ConfigClient.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		if kapierrors.IsNotFound(err) || kapierrors.ReasonForError(err) == metav1.StatusReasonUnknown {
			klog.Errorf("Unable to find cluster version object from cluster: %v", err)
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

func (o *CreateOptions) createNamespace(ctx context.Context) error {
	nsNodeJoiner := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "openshift-node-joiner-",
			Annotations: map[string]string{
				"oc.openshift.io/command":    "oc adm node-image create",
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

func (o *CreateOptions) cleanup(ctx context.Context) {
	if o.nodeJoinerNamespace == nil {
		return
	}

	err := o.Client.CoreV1().Namespaces().Delete(ctx, o.nodeJoinerNamespace.GetName(), metav1.DeleteOptions{})
	if err != nil {
		klog.Errorf("cannot delete namespace %s: %v\n", o.nodeJoinerNamespace.GetName(), err)
	}
}

func (o *CreateOptions) createServiceAccount(ctx context.Context) error {
	nodeJoinerServiceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-joiner-",
			Annotations: map[string]string{
				"oc.openshift.io/command": "oc adm node-image create",
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

func (o *CreateOptions) createRolesAndBindings(ctx context.Context) error {
	nodeJoinerRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-joiner-",
			Annotations: map[string]string{
				"oc.openshift.io/command": "oc adm node-image create",
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
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"config.openshift.io",
				},
				Resources: []string{
					"clusterversions",
					"proxies",
				},
				Verbs: []string{
					"get",
				},
			},
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"configmaps",
					"nodes",
					"secrets",
				},
				Verbs: []string{
					"get",
					"list",
				},
			},
		},
	}
	cr, err := o.Client.RbacV1().ClusterRoles().Create(ctx, nodeJoinerRole, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create role: %w", err)
	}
	o.nodeJoinerRole = cr

	nodeJoinerRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-joiner-",
			Annotations: map[string]string{
				"oc.openshift.io/command": "oc adm node-image create",
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
	_, err = o.Client.RbacV1().ClusterRoleBindings().Create(ctx, nodeJoinerRoleBinding, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create role binding: %w", err)
	}

	return nil
}

func (o *CreateOptions) createInputConfigMap(ctx context.Context) error {
	data, err := fs.ReadFile(o.FSys, nodeJoinerConfigurationFile)
	if err != nil {
		return err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodes-config",
			Namespace: o.nodeJoinerNamespace.GetName(),
		},
		Data: map[string]string{
			nodeJoinerConfigurationFile: string(data),
		},
	}

	_, err = o.Client.CoreV1().ConfigMaps(o.nodeJoinerNamespace.GetName()).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create configmap: %w", err)
	}

	return nil
}

func (o *CreateOptions) createPod(ctx context.Context) error {
	nodeJoinerPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-joiner-",
			Labels: map[string]string{
				"app": "node-joiner",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:      corev1.RestartPolicyNever,
			ServiceAccountName: o.nodeJoinerServiceAccount.GetName(),
			SecurityContext: &corev1.PodSecurityContext{
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "nodes-config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "nodes-config",
							},
						},
					},
				},
				{
					Name: "assets",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            nodeJoinerContainer,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Image:           o.nodeJoinerImage,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "nodes-config",
							MountPath: "/config",
						},
						{
							Name:      "assets",
							MountPath: "/assets",
						},
					},
					Command: []string{
						"/bin/bash", "-c",
						fmt.Sprintf("cp /config/%s /assets; HOME=/assets node-joiner add-nodes --dir=/assets --log-level=debug; sleep 600", nodeJoinerConfigurationFile),
					},
				},
			},
		},
	}
	pod, err := o.Client.CoreV1().Pods(o.nodeJoinerNamespace.GetName()).Create(ctx, nodeJoinerPod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create pod: %w", err)
	}
	o.nodeJoinerPod = pod

	return nil
}
