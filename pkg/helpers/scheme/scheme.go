package scheme

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/openshift/api/apps"
	"github.com/openshift/api/authorization"
	"github.com/openshift/api/build"
	"github.com/openshift/api/image"
	"github.com/openshift/api/oauth"
	"github.com/openshift/api/project"
	quotav1 "github.com/openshift/api/quota/v1"
	securityv1 "github.com/openshift/api/security/v1"
	"github.com/openshift/api/template"
	"github.com/openshift/api/user"
)

func InstallSchemes(scheme *apimachineryruntime.Scheme) {
	utilruntime.Must(apps.Install(scheme))
	utilruntime.Must(authorization.Install(scheme))
	utilruntime.Must(build.Install(scheme))
	utilruntime.Must(image.Install(scheme))
	utilruntime.Must(oauth.Install(scheme))
	utilruntime.Must(project.Install(scheme))
	utilruntime.Must(InstallNonCRDQuota(scheme))
	utilruntime.Must(InstallNonCRDSecurity(scheme))
	utilruntime.Must(template.Install(scheme))
	utilruntime.Must(user.Install(scheme))
}

func InstallNonCRDSecurity(scheme *apimachineryruntime.Scheme) error {
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

func InstallNonCRDQuota(scheme *apimachineryruntime.Scheme) error {
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
