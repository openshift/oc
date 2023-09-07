package scheme

import (
	"github.com/openshift/api/apps"
	"github.com/openshift/api/authorization"
	"github.com/openshift/api/build"
	"github.com/openshift/api/image"
	"github.com/openshift/api/oauth"
	"github.com/openshift/api/project"
	"github.com/openshift/api/template"
	"github.com/openshift/api/user"
	"github.com/openshift/oc/pkg/helpers/legacy"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	quotav1 "github.com/openshift/api/quota/v1"
	securityv1 "github.com/openshift/api/security/v1"
)

func InstallSchemes(scheme *apimachineryruntime.Scheme) {
	utilruntime.Must(apps.Install(scheme))
	utilruntime.Must(authorization.Install(scheme))
	utilruntime.Must(build.Install(scheme))
	utilruntime.Must(image.Install(scheme))
	utilruntime.Must(oauth.Install(scheme))
	utilruntime.Must(project.Install(scheme))
	utilruntime.Must(installNonCRDQuota(scheme))
	utilruntime.Must(installNonCRDSecurity(scheme))
	utilruntime.Must(template.Install(scheme))
	utilruntime.Must(user.Install(scheme))
	legacy.InstallExternalLegacyAll(scheme)
}

func installNonCRDSecurity(scheme *apimachineryruntime.Scheme) error {
	scheme.AddKnownTypes(securityv1.GroupVersion,
		&securityv1.PodSecurityPolicySubjectReview{},
		&securityv1.PodSecurityPolicySelfSubjectReview{},
		&securityv1.PodSecurityPolicyReview{},
		&securityv1.RangeAllocation{},
		&securityv1.RangeAllocationList{},
	)
	if err := corev1.AddToScheme(scheme); err != nil {
		return err
	}
	metav1.AddToGroupVersion(scheme, securityv1.GroupVersion)
	return nil
}

func installNonCRDQuota(scheme *apimachineryruntime.Scheme) error {
	scheme.AddKnownTypes(securityv1.GroupVersion,
		&quotav1.AppliedClusterResourceQuota{},
		&quotav1.AppliedClusterResourceQuotaList{},
	)
	if err := corev1.AddToScheme(scheme); err != nil {
		return err
	}
	metav1.AddToGroupVersion(scheme, quotav1.GroupVersion)
	return nil
}
