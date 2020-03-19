package deploymentconfigs

import (
	"bytes"
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/describe/versioned"
	"k8s.io/kubectl/pkg/polymorphichelpers"

	appsv1 "github.com/openshift/api/apps/v1"
	appsclient "github.com/openshift/client-go/apps/clientset/versioned"
	appstypedclient "github.com/openshift/client-go/apps/clientset/versioned/typed/apps/v1"
)

func NewDeploymentConfigRollbacker(appsClient appsclient.Interface) polymorphichelpers.Rollbacker {
	return &DeploymentConfigRollbacker{dn: appsClient.AppsV1()}
}

// DeploymentConfigRollbacker is an implementation of the kubectl Rollbacker interface
// for deployment configs.
type DeploymentConfigRollbacker struct {
	dn appstypedclient.DeploymentConfigsGetter
}

var _ polymorphichelpers.Rollbacker = &DeploymentConfigRollbacker{}

// Rollback the provided deployment config to a specific revision. If revision is zero, we will
// rollback to the previous deployment.
func (r *DeploymentConfigRollbacker) Rollback(obj runtime.Object, updatedAnnotations map[string]string, toRevision int64, dryRunStrategy cmdutil.DryRunStrategy) (string, error) {
	config, ok := obj.(*appsv1.DeploymentConfig)
	if !ok {
		return "", fmt.Errorf("passed object is not a deployment config: %#v", obj)
	}
	if config.Spec.Paused {
		return "", fmt.Errorf("cannot rollback a paused config; resume it first with 'rollout resume dc/%s' and try again", config.Name)
	}

	rollback := &appsv1.DeploymentConfigRollback{
		Name:               config.Name,
		UpdatedAnnotations: updatedAnnotations,
		Spec: appsv1.DeploymentConfigRollbackSpec{
			Revision:        toRevision,
			IncludeTemplate: true,
		},
	}

	rolledback, err := r.dn.DeploymentConfigs(config.Namespace).Rollback(context.TODO(), config.Name, rollback, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}

	if dryRunStrategy == cmdutil.DryRunClient {
		out := bytes.NewBuffer([]byte("\n"))
		versioned.DescribePodTemplate(rolledback.Spec.Template, describe.NewPrefixWriter(out))
		return out.String(), nil
	}

	_, err = r.dn.DeploymentConfigs(config.Namespace).Update(context.TODO(), rolledback, metav1.UpdateOptions{})
	if err != nil {
		return "", err
	}

	return "rolled back", nil
}
