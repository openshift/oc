package cmd

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
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

	if errors.IsForbidden(err) && strings.Contains(err.Error(), "violates PodSecurity") {
		err = fmt.Errorf("PodSecurity violation error:\n"+
			"Ensure the target namespace has the appropriate security level set "+
			"or consider creating a dedicated privileged namespace using:\n"+
			"\t\"oc create ns <namespace> -o yaml | oc label -f - security.openshift.io/scc.podSecurityLabelSync=false pod-security.kubernetes.io/enforce=privileged pod-security.kubernetes.io/audit=privileged pod-security.kubernetes.io/warn=privileged --overwrite\".\n\nOriginal error:\n%w", err)
	}

	kcmdutil.CheckErr(err)
}
