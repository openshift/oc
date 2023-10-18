package cmd

import (
	"errors"
	"fmt"
	"strings"

	oauthv1 "github.com/openshift/api/oauth/v1"
	userv1 "github.com/openshift/api/user/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/discovery"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
)

// CheckPodSecurityErr introspects the given error for pod security violations.
// Upon successful detection it wraps it with suggestions on pod security labels
// and delegates to k8s.io/kubectl/pkg/cmd/util#CheckErr.
func CheckPodSecurityErr(err error) {
	if agg, ok := err.(utilerrors.Aggregate); ok && len(agg.Errors()) == 1 {
		err = agg.Errors()[0]
	}

	if err == nil {
		return
	}

	if apierrors.IsForbidden(err) && strings.Contains(err.Error(), "violates PodSecurity") {
		err = fmt.Errorf("PodSecurity violation error:\n"+
			"Ensure the target namespace has the appropriate security level set "+
			"or consider creating a dedicated privileged namespace using:\n"+
			"\t\"oc create ns <namespace> -o yaml | oc label -f - security.openshift.io/scc.podSecurityLabelSync=false pod-security.kubernetes.io/enforce=privileged pod-security.kubernetes.io/audit=privileged pod-security.kubernetes.io/warn=privileged --overwrite\".\n\nOriginal error:\n%w", err)
	}

	kcmdutil.CheckErr(err)
}

// CheckOAuthDisabledErr checks to see if originalError stems from the fact that
// the OAuth server is disabled.
func CheckOAuthDisabledErr(originalError error, discoveryClient discovery.DiscoveryInterface, groups ...string) {
	if agg, ok := originalError.(utilerrors.Aggregate); ok && len(agg.Errors()) == 1 {
		originalError = agg.Errors()[0]
	}

	if originalError == nil {
		return
	}

	if discoveryClient == nil {
		kcmdutil.CheckErr(originalError)
		return
	}

	// only NotFound errors can stem from missing GRs, so short-circuit if we're
	// dealing with any other error
	if !apierrors.IsNotFound(originalError) {
		kcmdutil.CheckErr(originalError)
		return
	}

	var status apierrors.APIStatus
	if !errors.As(originalError, &status) {
		// this should never happen, since `apierrors.IsNotFound()` succeeded above
		kcmdutil.CheckErr(originalError)
		return
	}
	details := status.Status().Details
	if details == nil {
		// we're not going to be able to detect whether the GR is registered
		kcmdutil.CheckErr(originalError)
		return
	}

	if !sets.New[string](oauthv1.GroupVersion.Group, userv1.GroupVersion.Group).Has(details.Group) {
		// not a group we care about
		kcmdutil.CheckErr(originalError)
		return
	}

	// in the case of a NotFound error, either the GR is not found or the object iself
	// is not found (when the action was a mutation of existing content); make sure it's
	// a missing GR by cross-checking discovery
	_, resourceLists, err := discoveryClient.ServerGroupsAndResources()
	// ServerGroupsAndResources() can return partial data alongside an error, so first check
	// for the GR - if we find it, we know that the culprit is not a missing resource
	for _, resourceList := range resourceLists {
		for _, resource := range resourceList.APIResources {
			// n.b. it seems like metav1.Status.Details.Kind is a resource, not a kind
			// n.b. metav1.Status does not specify a version, so the best we can do is detect that
			// *some* related GroupResource (at some version) exists on the server
			if resource.Name == details.Kind && resource.Group == details.Group {
				// the GVK does exist on the server, so the NotFound must have been for a missing resource
				kcmdutil.CheckErr(originalError)
				return
			}
		}
	}
	if err != nil {
		kcmdutil.CheckErr(originalError)
		return
	}

	kcmdutil.CheckErr(fmt.Errorf(`Error: %s.%s are not enabled on this cluster.
Is the embeedded OAuth server disabled?. 

Original error:
%w`, details.Kind, details.Group, err))
}
