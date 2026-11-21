package top

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubectl/pkg/describe"
	metricsapi "k8s.io/metrics/pkg/apis/metrics"
)

func TestFindNodeRoles(t *testing.T) {
	testCases := []struct {
		name     string
		labels   map[string]string
		expected []string
	}{
		{
			name:     "no roles",
			labels:   map[string]string{"kubernetes.io/hostname": "node1"},
			expected: []string{},
		},
		{
			name: "worker role",
			labels: map[string]string{
				describe.LabelNodeRolePrefix + "worker": "",
			},
			expected: []string{"worker"},
		},
		{
			name: "infra role",
			labels: map[string]string{
				describe.LabelNodeRolePrefix + "infra": "",
			},
			expected: []string{"infra"},
		},
		{
			name: "multiple roles sorted",
			labels: map[string]string{
				describe.LabelNodeRolePrefix + "master":        "",
				describe.LabelNodeRolePrefix + "control-plane": "",
			},
			expected: []string{"control-plane", "master"},
		},
		{
			name: "legacy role label",
			labels: map[string]string{
				describe.NodeLabelRole: "worker",
			},
			expected: []string{"worker"},
		},
		{
			name: "prefix and legacy combined",
			labels: map[string]string{
				describe.LabelNodeRolePrefix + "infra": "",
				describe.NodeLabelRole:                 "worker",
			},
			expected: []string{"infra", "worker"},
		},
		{
			name: "empty role suffix ignored",
			labels: map[string]string{
				describe.LabelNodeRolePrefix: "",
			},
			expected: []string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Labels: tc.labels},
			}
			got := findNodeRoles(node)
			if diff := cmp.Diff(tc.expected, got); diff != "" {
				t.Errorf("findNodeRoles() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFormatNodeRoles(t *testing.T) {
	testCases := []struct {
		name     string
		roles    []string
		expected string
	}{
		{name: "none", roles: nil, expected: "<none>"},
		{name: "empty", roles: []string{}, expected: "<none>"},
		{name: "single", roles: []string{"worker"}, expected: "worker"},
		{name: "multiple", roles: []string{"control-plane", "master"}, expected: "control-plane,master"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatNodeRoles(tc.roles); got != tc.expected {
				t.Errorf("formatNodeRoles(%v) = %q, want %q", tc.roles, got, tc.expected)
			}
		})
	}
}

func TestPrintNodeMetricsIncludesRolesColumn(t *testing.T) {
	cpu := resource.MustParse("250m")
	mem := resource.MustParse("4096Mi")
	allocatableCPU := resource.MustParse("2000m")
	allocatableMem := resource.MustParse("8192Mi")

	metrics := []metricsapi.NodeMetrics{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "master-0"},
			Usage: corev1.ResourceList{
				corev1.ResourceCPU:    cpu,
				corev1.ResourceMemory: mem,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
			Usage: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi"),
			},
		},
	}
	available := map[string]corev1.ResourceList{
		"master-0": {
			corev1.ResourceCPU:    allocatableCPU,
			corev1.ResourceMemory: allocatableMem,
		},
		"worker-1": {
			corev1.ResourceCPU:    allocatableCPU,
			corev1.ResourceMemory: allocatableMem,
		},
	}
	roles := map[string]string{
		"master-0": "control-plane,master",
		"worker-1": "worker",
	}

	var buf bytes.Buffer
	if err := printNodeMetrics(&buf, metrics, available, roles, nil, false, "", false); err != nil {
		t.Fatalf("printNodeMetrics() error: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least header + 2 rows, got %d lines:\n%s", len(lines), out)
	}

	header := lines[0]
	if !strings.Contains(header, "NAME") || !strings.Contains(header, "ROLES") {
		t.Errorf("header missing NAME/ROLES: %q", header)
	}
	// ROLES should appear before CPU
	nameIdx := strings.Index(header, "NAME")
	rolesIdx := strings.Index(header, "ROLES")
	cpuIdx := strings.Index(header, "CPU(cores)")
	if !(nameIdx < rolesIdx && rolesIdx < cpuIdx) {
		t.Errorf("expected column order NAME ROLES CPU..., got %q", header)
	}

	if !strings.Contains(lines[1], "control-plane,master") {
		t.Errorf("master row missing roles: %q", lines[1])
	}
	if !strings.Contains(lines[2], "worker") {
		t.Errorf("worker row missing roles: %q", lines[2])
	}
}

func TestPrintNodeMetricsMissingRole(t *testing.T) {
	metrics := []metricsapi.NodeMetrics{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "custom-0"},
			Usage: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
	}
	available := map[string]corev1.ResourceList{
		"custom-0": {
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("2048Mi"),
		},
	}
	// empty roles map -> <none>
	var buf bytes.Buffer
	if err := printNodeMetrics(&buf, metrics, available, map[string]string{}, nil, false, "", false); err != nil {
		t.Fatalf("printNodeMetrics() error: %v", err)
	}
	if !strings.Contains(buf.String(), "<none>") {
		t.Errorf("expected <none> for missing roles, got:\n%s", buf.String())
	}
}

func TestPrintNodeMetricsNoHeaders(t *testing.T) {
	metrics := []metricsapi.NodeMetrics{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
			Usage: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("1024Mi"),
			},
		},
	}
	available := map[string]corev1.ResourceList{
		"worker-1": {
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("2048Mi"),
		},
	}
	roles := map[string]string{"worker-1": "infra"}

	var buf bytes.Buffer
	if err := printNodeMetrics(&buf, metrics, available, roles, nil, true, "", false); err != nil {
		t.Fatalf("printNodeMetrics() error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "NAME") || strings.Contains(out, "ROLES") {
		t.Errorf("--no-headers should omit header, got:\n%s", out)
	}
	if !strings.Contains(out, "infra") {
		t.Errorf("expected infra role in output:\n%s", out)
	}
}

func TestPrintNodeMetricsShowSwap(t *testing.T) {
	metrics := []metricsapi.NodeMetrics{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
			Usage: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("1024Mi"),
			},
		},
	}
	available := map[string]corev1.ResourceList{
		"worker-1": {
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("2048Mi"),
		},
	}
	roles := map[string]string{"worker-1": "worker"}

	var buf bytes.Buffer
	if err := printNodeMetrics(&buf, metrics, available, roles, nil, false, "", true); err != nil {
		t.Fatalf("printNodeMetrics() error: %v", err)
	}
	header := strings.Split(buf.String(), "\n")[0]
	if !strings.Contains(header, "SWAP(bytes)") || !strings.Contains(header, "ROLES") {
		t.Errorf("expected ROLES and SWAP columns in header: %q", header)
	}
}
