package inspect

import (
	"reflect"
	"testing"

	v1 "github.com/openshift/api/route/v1"
)

func TestElideRoute(t *testing.T) {
	tests := []struct {
		input    v1.Route
		expected v1.Route
	}{
		{
			input: v1.Route{
				Spec: v1.RouteSpec{
					TLS: &v1.TLSConfig{
						Key: "testkey",
					},
				},
			},
			expected: v1.Route{
				Spec: v1.RouteSpec{
					TLS: &v1.TLSConfig{
						Key: "",
					},
				},
			},
		},
		{
			input: v1.Route{
				Spec: v1.RouteSpec{
					TLS: nil,
				},
			},
			expected: v1.Route{
				Spec: v1.RouteSpec{
					TLS: nil,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			elideRoute(&test.input)
			if !reflect.DeepEqual(test.input.Spec, test.expected.Spec) {
				t.Errorf("expected %v, got %v", test.expected.Spec, test.input.Spec)
			}
		})
	}
}
