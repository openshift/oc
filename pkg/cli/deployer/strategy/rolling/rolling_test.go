package rolling

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"reflect"

	appsv1 "github.com/openshift/api/apps/v1"
	"github.com/openshift/library-go/pkg/apps/appsutil"
	strat "github.com/openshift/oc/pkg/cli/deployer/strategy"
	"github.com/openshift/oc/pkg/cli/deployer/strategy/util/appstest"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/diff"
	"k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
)

func TestRolling_deployInitial(t *testing.T) {
	initialStrategyInvoked := false

	strategy := &RollingDeploymentStrategy{
		rcClient:    fake.NewSimpleClientset().CoreV1(),
		eventClient: fake.NewSimpleClientset().CoreV1(),
		initialStrategy: &testStrategy{
			deployFn: func(from *corev1.ReplicationController, to *corev1.ReplicationController, desiredReplicas int, updateAcceptor strat.UpdateAcceptor) error {
				initialStrategyInvoked = true
				return nil
			},
		},
		rollingUpdate: func(config *RollingUpdaterConfig) error {
			t.Fatalf("unexpected call to rollingUpdate")
			return nil
		},
		getUpdateAcceptor: getUpdateAcceptor,
		apiRetryPeriod:    1 * time.Millisecond,
		apiRetryTimeout:   10 * time.Millisecond,
	}

	config := appstest.OkDeploymentConfig(1)
	config.Spec.Strategy = appstest.OkRollingStrategy()
	deployment, _ := appsutil.MakeDeployment(config)
	strategy.out, strategy.errOut = &bytes.Buffer{}, &bytes.Buffer{}
	err := strategy.Deploy(nil, deployment, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !initialStrategyInvoked {
		t.Fatalf("expected initial strategy to be invoked")
	}
}

func TestRolling_deployRolling(t *testing.T) {
	latestConfig := appstest.OkDeploymentConfig(1)
	latestConfig.Spec.Strategy = appstest.OkRollingStrategy()
	latest, _ := appsutil.MakeDeployment(latestConfig)
	config := appstest.OkDeploymentConfig(2)
	config.Spec.Strategy = appstest.OkRollingStrategy()
	deployment, _ := appsutil.MakeDeployment(config)

	deployments := map[string]*corev1.ReplicationController{
		latest.Name:     latest,
		deployment.Name: deployment,
	}
	deploymentUpdated := false

	client := &fake.Clientset{}
	client.AddReactor("get", "replicationcontrollers", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
		name := action.(clientgotesting.GetAction).GetName()
		return true, deployments[name], nil
	})
	client.AddReactor("patch", "replicationcontrollers", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
		patchAction := action.(clientgotesting.PatchAction)
		if patchAction.GetPatchType() != types.ApplyPatchType {
			return true, nil, fmt.Errorf("unhandled")
		}
		appliedRC := &unstructured.Unstructured{}
		if _, _, err := unstructured.UnstructuredJSONScheme.Decode(patchAction.GetPatch(), nil, appliedRC); err != nil {
			return true, nil, err
		}
		deploymentUpdated = true

		if deployment.Annotations == nil {
			deployment.Annotations = map[string]string{}
		}
		for k, v := range appliedRC.GetAnnotations() {
			deployment.Annotations[k] = v
		}

		return true, deployment, nil
	})

	var rollingConfig *RollingUpdaterConfig
	strategy := &RollingDeploymentStrategy{
		rcClient:    client.CoreV1(),
		eventClient: fake.NewSimpleClientset().CoreV1(),
		initialStrategy: &testStrategy{
			deployFn: func(from *corev1.ReplicationController, to *corev1.ReplicationController, desiredReplicas int, updateAcceptor strat.UpdateAcceptor) error {
				t.Fatalf("unexpected call to initial strategy")
				return nil
			},
		},
		rollingUpdate: func(config *RollingUpdaterConfig) error {
			rollingConfig = config
			return nil
		},
		getUpdateAcceptor: getUpdateAcceptor,
		apiRetryPeriod:    1 * time.Millisecond,
		apiRetryTimeout:   10 * time.Millisecond,
	}

	strategy.out, strategy.errOut = &bytes.Buffer{}, &bytes.Buffer{}
	err := strategy.Deploy(latest, deployment, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rollingConfig == nil {
		t.Fatalf("expected rolling update to be invoked")
	}

	if !reflect.DeepEqual(latest, rollingConfig.OldRc) {
		t.Errorf("unexpected rollingConfig.OldRc:%s\n", diff.ObjectGoPrintDiff(latest, rollingConfig.OldRc))
	}

	if !reflect.DeepEqual(deployment, rollingConfig.NewRc) {
		t.Errorf("unexpected rollingConfig.NewRc:%s\n", diff.ObjectGoPrintDiff(latest, rollingConfig.OldRc))
	}

	if e, a := 1*time.Second, rollingConfig.Interval; e != a {
		t.Errorf("expected Interval %d, got %d", e, a)
	}

	if e, a := 1*time.Second, rollingConfig.UpdatePeriod; e != a {
		t.Errorf("expected UpdatePeriod %d, got %d", e, a)
	}

	if e, a := 20*time.Second, rollingConfig.Timeout; e != a {
		t.Errorf("expected Timeout %d, got %d", e, a)
	}

	// verify hack
	if e, a := int32(1), rollingConfig.NewRc.Spec.Replicas; e != *a {
		t.Errorf("expected rollingConfig.NewRc.Spec.Replicas %d, got %d", e, a)
	}

	// verify hack
	if !deploymentUpdated {
		t.Errorf("expected deployment to be updated for source annotation")
	}
	sid := fmt.Sprintf("%s:%s", latest.Name, latest.ObjectMeta.UID)
	if e, a := sid, rollingConfig.NewRc.Annotations[sourceIdAnnotation]; e != a {
		t.Errorf("expected sourceIdAnnotation %s, got %s", e, a)
	}
}

type hookExecutorImpl struct {
	executeFunc func(hook *appsv1.LifecycleHook, deployment *corev1.ReplicationController, suffix, label string) error
}

func (h *hookExecutorImpl) Execute(hook *appsv1.LifecycleHook, rc *corev1.ReplicationController, suffix, label string) error {
	return h.executeFunc(hook, rc, suffix, label)
}

func TestRolling_deployRollingHooks(t *testing.T) {
	config := appstest.OkDeploymentConfig(1)
	config.Spec.Strategy = appstest.OkRollingStrategy()
	latest, _ := appsutil.MakeDeployment(config)

	var hookError error

	deployments := map[string]*corev1.ReplicationController{latest.Name: latest}

	client := &fake.Clientset{}
	client.AddReactor("get", "replicationcontrollers", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
		name := action.(clientgotesting.GetAction).GetName()
		return true, deployments[name], nil
	})
	client.AddReactor("update", "replicationcontrollers", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
		updated := action.(clientgotesting.UpdateAction).GetObject().(*corev1.ReplicationController)
		return true, updated, nil
	})

	strategy := &RollingDeploymentStrategy{
		rcClient:    client.CoreV1(),
		eventClient: fake.NewSimpleClientset().CoreV1(),
		initialStrategy: &testStrategy{
			deployFn: func(from *corev1.ReplicationController, to *corev1.ReplicationController, desiredReplicas int, updateAcceptor strat.UpdateAcceptor) error {
				t.Fatalf("unexpected call to initial strategy")
				return nil
			},
		},
		rollingUpdate: func(config *RollingUpdaterConfig) error {
			return nil
		},
		hookExecutor: &hookExecutorImpl{
			executeFunc: func(hook *appsv1.LifecycleHook, deployment *corev1.ReplicationController, suffix, label string) error {
				return hookError
			},
		},
		getUpdateAcceptor: getUpdateAcceptor,
		apiRetryPeriod:    1 * time.Millisecond,
		apiRetryTimeout:   10 * time.Millisecond,
	}

	cases := []struct {
		params               *appsv1.RollingDeploymentStrategyParams
		hookShouldFail       bool
		deploymentShouldFail bool
	}{
		{rollingParams(appsv1.LifecycleHookFailurePolicyAbort, ""), true, true},
		{rollingParams(appsv1.LifecycleHookFailurePolicyAbort, ""), false, false},
		{rollingParams("", appsv1.LifecycleHookFailurePolicyAbort), true, true},
		{rollingParams("", appsv1.LifecycleHookFailurePolicyAbort), false, false},
	}

	for _, tc := range cases {
		config := appstest.OkDeploymentConfig(2)
		config.Spec.Strategy.RollingParams = tc.params
		deployment, _ := appsutil.MakeDeployment(config)
		deployments[deployment.Name] = deployment
		hookError = nil
		if tc.hookShouldFail {
			hookError = fmt.Errorf("hook failure")
		}
		strategy.out, strategy.errOut = &bytes.Buffer{}, &bytes.Buffer{}
		err := strategy.Deploy(latest, deployment, 2)
		if err != nil && tc.deploymentShouldFail {
			t.Logf("got expected error: %v", err)
		}
		if err == nil && tc.deploymentShouldFail {
			t.Errorf("expected an error for case: %#v", tc)
		}
		if err != nil && !tc.deploymentShouldFail {
			t.Errorf("unexpected error for case: %#v: %v", tc, err)
		}
	}
}

// TestRolling_deployInitialHooks can go away once the rolling strategy
// supports initial deployments.
func TestRolling_deployInitialHooks(t *testing.T) {
	var hookError error

	strategy := &RollingDeploymentStrategy{
		rcClient:    fake.NewSimpleClientset().CoreV1(),
		eventClient: fake.NewSimpleClientset().CoreV1(),
		initialStrategy: &testStrategy{
			deployFn: func(from *corev1.ReplicationController, to *corev1.ReplicationController, desiredReplicas int,
				updateAcceptor strat.UpdateAcceptor) error {
				return nil
			},
		},
		rollingUpdate: func(config *RollingUpdaterConfig) error {
			return nil
		},
		hookExecutor: &hookExecutorImpl{
			executeFunc: func(hook *appsv1.LifecycleHook, deployment *corev1.ReplicationController, suffix, label string) error {
				return hookError
			},
		},
		getUpdateAcceptor: getUpdateAcceptor,
		apiRetryPeriod:    1 * time.Millisecond,
		apiRetryTimeout:   10 * time.Millisecond,
	}

	cases := []struct {
		params               *appsv1.RollingDeploymentStrategyParams
		hookShouldFail       bool
		deploymentShouldFail bool
	}{
		{rollingParams(appsv1.LifecycleHookFailurePolicyAbort, ""), true, true},
		{rollingParams(appsv1.LifecycleHookFailurePolicyAbort, ""), false, false},
		{rollingParams("", appsv1.LifecycleHookFailurePolicyAbort), true, true},
		{rollingParams("", appsv1.LifecycleHookFailurePolicyAbort), false, false},
	}

	for i, tc := range cases {
		config := appstest.OkDeploymentConfig(2)
		config.Spec.Strategy.RollingParams = tc.params
		deployment, _ := appsutil.MakeDeployment(config)
		hookError = nil
		if tc.hookShouldFail {
			hookError = fmt.Errorf("hook failure")
		}
		strategy.out, strategy.errOut = &bytes.Buffer{}, &bytes.Buffer{}
		err := strategy.Deploy(nil, deployment, 2)
		if err != nil && tc.deploymentShouldFail {
			t.Logf("got expected error: %v", err)
		}
		if err == nil && tc.deploymentShouldFail {
			t.Errorf("%d: expected an error for case: %v", i, tc)
		}
		if err != nil && !tc.deploymentShouldFail {
			t.Errorf("%d: unexpected error for case: %v: %v", i, tc, err)
		}
	}
}

type testStrategy struct {
	deployFn func(from *corev1.ReplicationController, to *corev1.ReplicationController, desiredReplicas int, updateAcceptor strat.UpdateAcceptor) error
}

func (s *testStrategy) DeployWithAcceptor(from *corev1.ReplicationController, to *corev1.ReplicationController, desiredReplicas int, updateAcceptor strat.UpdateAcceptor) error {
	return s.deployFn(from, to, desiredReplicas, updateAcceptor)
}

func mkintp(i int) *int64 {
	v := int64(i)
	return &v
}

func rollingParams(preFailurePolicy, postFailurePolicy appsv1.LifecycleHookFailurePolicy) *appsv1.RollingDeploymentStrategyParams {
	var pre *appsv1.LifecycleHook
	var post *appsv1.LifecycleHook

	if len(preFailurePolicy) > 0 {
		pre = &appsv1.LifecycleHook{
			FailurePolicy: preFailurePolicy,
			ExecNewPod:    &appsv1.ExecNewPodHook{},
		}
	}
	if len(postFailurePolicy) > 0 {
		post = &appsv1.LifecycleHook{
			FailurePolicy: postFailurePolicy,
			ExecNewPod:    &appsv1.ExecNewPodHook{},
		}
	}
	return &appsv1.RollingDeploymentStrategyParams{
		UpdatePeriodSeconds: mkintp(1),
		IntervalSeconds:     mkintp(1),
		TimeoutSeconds:      mkintp(20),
		Pre:                 pre,
		Post:                post,
	}
}

func getUpdateAcceptor(timeout time.Duration, minReadySeconds int32) strat.UpdateAcceptor {
	return &testAcceptor{
		acceptFn: func(deployment *corev1.ReplicationController) error {
			return nil
		},
	}
}

type testAcceptor struct {
	acceptFn func(*corev1.ReplicationController) error
}

func (t *testAcceptor) Accept(deployment *corev1.ReplicationController) error {
	return t.acceptFn(deployment)
}
