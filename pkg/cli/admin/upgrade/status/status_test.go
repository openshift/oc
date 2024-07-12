package status

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestRemoveCustomWorkerNodes(t *testing.T) {
	testCases := []struct {
		name     string
		pools    []poolDisplayData
		expected []poolDisplayData
	}{
		{
			name: "basic case",
			pools: []poolDisplayData{
				{
					Name: "worker",
					Nodes: []nodeDisplayData{
						{
							Name: "node1",
						},
						{
							Name: "node2",
						},
					},
				},
				{
					Name: "custom",
					Nodes: []nodeDisplayData{
						{
							Name: "node2",
						},
					},
				},
			},
			expected: []poolDisplayData{
				{
					Name: "worker",
					Nodes: []nodeDisplayData{
						{
							Name: "node1",
						},
					},
				},
				{
					Name: "custom",
					Nodes: []nodeDisplayData{
						{
							Name: "node2",
						},
					},
				},
			},
		},
		{
			name: "no custom pool",
			pools: []poolDisplayData{
				{
					Name: "worker",
					Nodes: []nodeDisplayData{
						{
							Name: "node1",
						},
						{
							Name: "node2",
						},
					},
				},
			},
			expected: []poolDisplayData{
				{
					Name: "worker",
					Nodes: []nodeDisplayData{
						{
							Name: "node1",
						},
						{
							Name: "node2",
						},
					},
				},
			},
		},
		{
			name: "no custom nodes",
			pools: []poolDisplayData{
				{
					Name: "worker",
					Nodes: []nodeDisplayData{
						{
							Name: "node1",
						},
						{
							Name: "node2",
						},
					},
				},
				{
					Name: "custom",
				},
			},
			expected: []poolDisplayData{
				{
					Name: "worker",
					Nodes: []nodeDisplayData{
						{
							Name: "node1",
						},
						{
							Name: "node2",
						},
					},
				},
				{
					Name: "custom",
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := removeCustomWorkerNodes(tc.pools)
			if diff := cmp.Diff(tc.expected, actual, cmp.Options{
				cmpopts.IgnoreUnexported(nodeDisplayData{}),
			}); diff != "" {
				t.Errorf("pools differ from expected:\n%s", diff)
			}
		})
	}
}
