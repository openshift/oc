package nodepermissions

import (
	"context"
	"fmt"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"strings"
	"text/tabwriter"
)

var (
	nodeKind = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Node"}
)

type CheckNodePermissionsRuntime struct {
	ResourceFinder      genericclioptions.ResourceFinder
	KubeClient          kubernetes.Interface
	AnonymousKubeConfig *rest.Config

	rbacCache rbacCache

	genericiooptions.IOStreams
}

func (r *CheckNodePermissionsRuntime) Run(ctx context.Context) error {
	allClusterRoles, err := r.KubeClient.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	allClusterRoleBindings, err := r.KubeClient.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	allRoles, err := r.KubeClient.RbacV1().Roles("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	allRoleBindings, err := r.KubeClient.RbacV1().RoleBindings("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	r.rbacCache = newRBACCache(allClusterRoles, allClusterRoleBindings, allRoles, allRoleBindings)

	nodesToCheck := []*corev1.Node{}
	visitor := r.ResourceFinder.Do()
	err = visitor.Visit(func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}
		if nodeKind != info.Object.GetObjectKind().GroupVersionKind() {
			return fmt.Errorf("command must only be pointed at nodes")
		}

		uncastObj, ok := info.Object.(*unstructured.Unstructured)
		if !ok {
			return fmt.Errorf("not unstructured: %w", err)
		}
		node := &corev1.Node{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uncastObj.Object, node); err != nil {
			return fmt.Errorf("not a node: %w", err)
		}
		nodesToCheck = append(nodesToCheck, node)

		return nil
	})
	if err != nil {
		return err
	}

	for i, currNode := range nodesToCheck {
		if i > 0 {
			fmt.Fprintf(r.Out, "\n")
		}

		nodeRoles, err := r.checkNode(ctx, currNode)
		if err != nil {
			return err
		}

		fmt.Fprintf(r.Out, "node/%v Permissions\n", currNode.Name)

		fmt.Fprintf(r.Out, "\tCluster Wide\n")
		clusterRuleWriter := tabwriter.NewWriter(r.Out, 0, 4, 4, ' ', 0)
		if len(nodeRoles.clusterRoles) > 0 {
			clusterRuleWriter.Write([]byte("\tOrigin\tClusterRole\tVerbs\tGroups\tResources\tNames\n"))
		}
		for _, curr := range nodeRoles.clusterRoles {
			for _, rule := range curr.Rules {
				// TODO maybe render these
				if len(rule.NonResourceURLs) > 0 {
					continue
				}
				origins := nodeRoles.clusterRolesToOrigins[curr.Name]
				originStrings := []string{}
				for _, currOrigin := range origins {
					originStrings = append(originStrings, currOrigin.originString())
				}

				clusterRuleWriter.Write(
					[]byte(fmt.Sprintf("\t%v\t%v\t%v\t%v\t%v\t%v\n",
						strings.Join(originStrings, ","),
						curr.Name,
						strings.Join(rule.Verbs, ","),
						strings.Join(rule.APIGroups, ","),
						strings.Join(rule.Resources, ","),
						strings.Join(rule.ResourceNames, ","),
					)),
				)
			}
		}
		clusterRuleWriter.Flush()
		fmt.Fprintf(r.Out, "\n")

		if len(nodeRoles.allRoleNamespaces) > 0 {
			if len(nodeRoles.clusterRoles) > 0 {
				fmt.Fprintf(r.Out, "\t\n")
			}
			fmt.Fprintf(r.Out, "\tNamespace Scoped\n")
		}
		for i, namespace := range sets.List(nodeRoles.allRoleNamespaces) {
			if i > 0 {
				fmt.Fprintf(r.Out, "\t\t\n")
			}
			fmt.Fprintf(r.Out, "\t\tNamespace: %v\n", namespace)

			namespacedRuleWriting := tabwriter.NewWriter(r.Out, 0, 4, 4, ' ', 0)
			namespacedRuleWriting.Write([]byte("\t\tOrigin\tRole\tVerbs\tGroups\tResources\tNames\n"))
			for _, currRole := range nodeRoles.roles {
				if currRole.Namespace != namespace {
					continue
				}
				currRoleRef := newRoleRef(currRole.Namespace, currRole.Name)
				for _, rule := range currRole.Rules {
					// TODO maybe render these
					if len(rule.NonResourceURLs) > 0 {
						continue
					}
					origins := nodeRoles.rolesToOrigins[currRoleRef]
					originStrings := []string{}
					for _, currOrigin := range origins {
						originStrings = append(originStrings, currOrigin.originString())
					}

					namespacedRuleWriting.Write(
						[]byte(fmt.Sprintf("\t\t%v\t%v\t%v\t%v\t%v\t%v\n",
							strings.Join(originStrings, ","),
							currRole.Name,
							strings.Join(rule.Verbs, ","),
							strings.Join(rule.APIGroups, ","),
							strings.Join(rule.Resources, ","),
							strings.Join(rule.ResourceNames, ","),
						)),
					)
				}
			}
			namespacedRuleWriting.Flush()
		}

	}

	return nil
}

type secretRef struct {
	namespace string
	name      string
}

func newSecretRef(namespace, name string) secretRef {
	return secretRef{
		namespace: namespace,
		name:      name,
	}
}

type podRef struct {
	namespace string
	name      string
}

func newPodRef(namespace, name string) podRef {
	return podRef{
		namespace: namespace,
		name:      name,
	}
}

type podIdentityToCheck struct {
	// when we handle the transitive permissions of permissions, this is needed.
	parentPodRef *podIdentityToCheck

	podRef  podRef
	users   []user.Info
	secrets []secretRef
}

func (p podIdentityToCheck) originString() string {
	parentString := ""
	if p.parentPodRef != nil {
		parentString = p.parentPodRef.originString()
	}

	if len(parentString) == 0 {
		return fmt.Sprintf("pod/%s[%s]", p.podRef.name, p.podRef.namespace)
	}

	return parentString + fmt.Sprintf("->pod/%s[%s]", p.podRef.name, p.podRef.namespace)
}

func (r *CheckNodePermissionsRuntime) checkNode(ctx context.Context, node *corev1.Node) (*nodeRoles, error) {
	podsOnNode, err := r.KubeClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%v", node.Name),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to check permissions on nodes/%v: %w", node.Name, err)
	}

	//errs := []error{}

	podIdentities := []podIdentityToCheck{}
	for _, pod := range podsOnNode.Items {
		currPodIdentity := podIdentityToCheck{
			podRef: newPodRef(pod.Namespace, pod.Name),
		}
		// check service account permissions
		if len(pod.Spec.ServiceAccountName) > 0 {
			currPodIdentity.users = append(currPodIdentity.users, serviceaccount.UserInfo(pod.Namespace, pod.Spec.ServiceAccountName, ""))
		}
		// check all mounted secrets for kubeconfigs
		for _, currVolume := range pod.Spec.Volumes {
			if currVolume.Secret != nil {
				currPodIdentity.secrets = append(currPodIdentity.secrets, newSecretRef(pod.Namespace, currVolume.Secret.SecretName))
			}
			if currVolume.Projected != nil {
				for _, currSource := range currVolume.Projected.Sources {
					if currSource.Secret != nil {
						currPodIdentity.secrets = append(currPodIdentity.secrets, newSecretRef(pod.Namespace, currSource.Secret.Name))
					}
				}
			}
		}
		podIdentities = append(podIdentities, currPodIdentity)
	}

	//for _, currSecretRef := range firstOrderSecretsToCheck.UnsortedList() {
	//	currSecretUser, err := r.userInfoFromSecret(ctx, currSecretRef)
	//	if err != nil {
	//		errs = append(errs, fmt.Errorf("unable to check permissions on nodes/%v: %w", node.Name, err))
	//		continue
	//	}
	//	if currSecretUser != nil {
	//		usersOnNode = append(usersOnNode, currSecretUser)
	//	}
	//}

	nodeRules := newNodeRules()
	newRolesToCheck := newNodeRules()
	for _, podIdentity := range podIdentities {
		for _, user := range podIdentity.users {
			userClusterRoles, userRoles := r.rbacCache.logicalRolesForUser(user)
			newClusterRoles, newRoles := nodeRules.addRoles(podIdentity, userClusterRoles, userRoles)
			newRolesToCheck.addRoles(podIdentity, newClusterRoles, newRoles)
		}
	}

	for len(newRolesToCheck.roles) == 0 && len(newRolesToCheck.clusterRoles) == 0 {
		// TODO check here for access to additional secrets and projected volumes
		newRolesToCheck = newNodeRules()
	}

	// TODO sort
	return nodeRules, nil
}

func (r *CheckNodePermissionsRuntime) userInfoFromSecret(ctx context.Context, currSecretRef secretRef) (user.Info, error) {
	secret, err := r.KubeClient.CoreV1().Secrets(currSecretRef.namespace).Get(ctx, currSecretRef.name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to check permissions for secrets/%v -n %v: %w", currSecretRef.name, currSecretRef.namespace, err)
	}

	if secret.Type == "kubernetes.io/service-account-token" {
		localKubeConfig := rest.CopyConfig(r.AnonymousKubeConfig)
		localKubeConfig.BearerToken = string(secret.Data["token"])
		secretKubeClient, err := kubernetes.NewForConfig(localKubeConfig)
		if err != nil {
			return nil, fmt.Errorf("unable to make kubeconfig for secrets/%v -n %v: %w", currSecretRef.name, currSecretRef.namespace, err)
		}
		currUserInfo, err := secretKubeClient.AuthenticationV1().SelfSubjectReviews().Create(ctx, &authenticationv1.SelfSubjectReview{}, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("unable to request user for secrets/%v -n %v: %w", currSecretRef.name, currSecretRef.namespace, err)
		}

		ret := &user.DefaultInfo{
			Name:   currUserInfo.Status.UserInfo.Username,
			UID:    currUserInfo.Status.UserInfo.UID,
			Groups: currUserInfo.Status.UserInfo.Groups,
			Extra:  map[string][]string{},
		}
		for k, v := range currUserInfo.Status.UserInfo.Extra {
			ret.Extra[k] = v
		}
		return ret, nil
	}

	return nil, nil
}
