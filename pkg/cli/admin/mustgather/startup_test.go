package mustgather

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestWaitForGatherContainerRunningRetriesPodLookup(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name  string
		state corev1.ContainerState
	}{
		{name: "running", state: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
		{name: "terminated", state: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			var mutex sync.Mutex
			var requests []string
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "gather-namespace", Name: "gather-pod"}}
			healthyPod := pod.DeepCopy()
			healthyPod.Status.ContainerStatuses = []corev1.ContainerStatus{{State: testCase.state}}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				mutex.Lock()
				defer mutex.Unlock()
				requests = append(requests, request.Method+" "+request.URL.Path)
				writer.Header().Set("Content-Type", "application/json")
				var response interface{} = healthyPod
				if len(requests) == 1 {
					writer.WriteHeader(http.StatusForbidden)
					response = &metav1.Status{
						TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
						Status:   metav1.StatusFailure, Reason: metav1.StatusReasonForbidden, Code: http.StatusForbidden,
					}
				}
				if err := json.NewEncoder(writer).Encode(response); err != nil {
					t.Errorf("encoding response: %v", err)
				}
			}))
			defer server.Close()
			client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			options := MustGatherOptions{Client: client, Timeout: 15 * time.Second}
			if err := options.waitForGatherContainerRunning(context.Background(), pod); err != nil {
				t.Errorf("expected retry to reach the healthy pod: %v", err)
			}
			mutex.Lock()
			defer mutex.Unlock()
			expectedRequests := []string{
				"GET /api/v1/namespaces/gather-namespace/pods/gather-pod",
				"GET /api/v1/namespaces/gather-namespace/pods/gather-pod",
			}
			if difference := cmp.Diff(expectedRequests, requests); difference != "" {
				t.Errorf("pod lookup changed across retries (-want +got):\n%s", difference)
			}
		})
	}
}

func TestWaitForGatherContainerRunningOutcomes(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name           string
		state          *corev1.ContainerState
		statusCode     int
		cancel         bool
		parentDeadline bool
		wantError      error
		wantMessage    string
	}{
		{name: "running", state: &corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
		{name: "terminated", state: &corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}},
		{name: "no container status timeout", wantError: context.DeadlineExceeded},
		{name: "waiting timeout", state: &corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}, wantError: context.DeadlineExceeded},
		{name: "parent deadline", parentDeadline: true, wantError: context.DeadlineExceeded},
		{name: "canceled during GET", cancel: true, wantError: context.Canceled},
		{name: "nonretryable server error", statusCode: http.StatusInternalServerError},
		{name: "image pull error", state: &corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ErrImagePull", Message: "pull failed"}}, wantMessage: "unable to pull image: ErrImagePull: pull failed"},
		{name: "image pull backoff", state: &corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "pull failed"}}, wantMessage: "unable to pull image: ImagePullBackOff: pull failed"},
		{name: "invalid image", state: &corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "InvalidImageName", Message: "pull failed"}}, wantMessage: "unable to pull image: InvalidImageName: pull failed"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if testCase.parentDeadline {
				var cancelDeadline context.CancelFunc
				ctx, cancelDeadline = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancelDeadline()
			}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "gather-namespace", Name: "gather-pod"}}
			responsePod := pod.DeepCopy()
			if testCase.state != nil {
				responsePod.Status.ContainerStatuses = []corev1.ContainerStatus{{State: *testCase.state}}
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodGet || request.URL.Path != "/api/v1/namespaces/gather-namespace/pods/gather-pod" {
					t.Errorf("unexpected pod request: %s %s", request.Method, request.URL.Path)
				}
				if testCase.cancel {
					cancel()
				}
				writer.Header().Set("Content-Type", "application/json")
				var response interface{} = responsePod
				if testCase.statusCode != 0 {
					writer.WriteHeader(testCase.statusCode)
					response = &metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusFailure, Reason: metav1.StatusReasonInternalError, Code: int32(testCase.statusCode)}
				}
				if err := json.NewEncoder(writer).Encode(response); err != nil {
					t.Errorf("encoding response: %v", err)
				}
			}))
			defer server.Close()
			client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			options := MustGatherOptions{Client: client, Timeout: time.Second}
			if testCase.wantError == context.DeadlineExceeded && !testCase.parentDeadline {
				options.Timeout = 100 * time.Millisecond
			}
			err = options.waitForGatherContainerRunning(ctx, pod)
			switch {
			case testCase.wantError != nil:
				if !errors.Is(err, testCase.wantError) {
					t.Errorf("expected %v, got %v", testCase.wantError, err)
				}
			case testCase.wantMessage != "":
				if err == nil || !strings.Contains(err.Error(), testCase.wantMessage) {
					t.Errorf("expected %q, got %v", testCase.wantMessage, err)
				}
			case testCase.statusCode != 0:
				if !apierrors.IsInternalError(err) {
					t.Errorf("expected original API server error, got %v", err)
				}
			case err != nil:
				t.Errorf("expected success, got %v", err)
			}
			if difference := cmp.Diff(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "gather-namespace", Name: "gather-pod"}}, pod); difference != "" {
				t.Errorf("input pod changed (-want +got):\n%s", difference)
			}
		})
	}
}
