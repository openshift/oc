package top

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestNewOvercommitRowFractions(t *testing.T) {
	allocatable := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2000m"),
		corev1.ResourceMemory: resource.MustParse("8192Mi"),
	}
	reqs := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1000m"),
		corev1.ResourceMemory: resource.MustParse("4096Mi"),
	}
	limits := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("4000m"),
		corev1.ResourceMemory: resource.MustParse("16384Mi"),
	}

	row := newOvercommitRow("worker-1", allocatable, reqs, limits)
	if row.cpuReqFraction != 50 {
		t.Fatalf("cpuReqFraction=%v, want 50", row.cpuReqFraction)
	}
	if row.cpuLimFraction != 200 {
		t.Fatalf("cpuLimFraction=%v, want 200", row.cpuLimFraction)
	}
	if row.memReqFraction != 50 {
		t.Fatalf("memReqFraction=%v, want 50", row.memReqFraction)
	}
	if row.memLimFraction != 200 {
		t.Fatalf("memLimFraction=%v, want 200", row.memLimFraction)
	}
}

func TestAggregatePodRequestsAndLimits(t *testing.T) {
	pods := []corev1.Pod{
		{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				}},
			},
		},
		{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("250m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				}},
			},
		},
	}

	reqs, limits := aggregatePodRequestsAndLimits(pods)
	if got := reqs.Cpu().MilliValue(); got != 350 {
		t.Fatalf("cpu requests=%dm, want 350m", got)
	}
	if got := limits.Cpu().MilliValue(); got != 700 {
		t.Fatalf("cpu limits=%dm, want 700m", got)
	}
	if got := reqs.Memory().Value(); got != 640*1024*1024 {
		t.Fatalf("memory requests=%d, want 640Mi", got)
	}
	if got := limits.Memory().Value(); got != (256+1024)*1024*1024 {
		t.Fatalf("memory limits=%d, want 1280Mi", got)
	}
}
