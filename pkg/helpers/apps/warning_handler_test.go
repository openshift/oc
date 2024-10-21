package apps

import (
	"net/http"
	"testing"
)

func TestHandleWarningHeader(t *testing.T) {
	testCases := []struct {
		name         string
		text         string
		wraps        bool
		expectOutput bool
	}{
		{
			name:         "deprecated old",
			text:         `apps.openshift.io/v1 DeploymentConfig is deprecated in v4.14+, unavailable in v4.10000+`,
			wraps:        true,
			expectOutput: false,
		},
		{
			name:         "deprecated new",
			text:         `apps.openshift.io/v1 DeploymentConfig is deprecated in v1.27+`,
			wraps:        true,
			expectOutput: false,
		},
		{
			name: "do not panic no match",
			text: `something else`,
		},
		{
			name:  "do not panic match",
			text:  `apps.openshift.io/v1 DeploymentConfig is deprecated in v1.27+`,
			wraps: false,
		},
		{
			name:         "not deprecation warning",
			text:         `DeploymentConfig is not deprecated`,
			wraps:        true,
			expectOutput: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var wrappedHandler mockWarningHandler
			if tc.wraps {
				wrappedHandler = mockWarningHandler{}
			}
			h := NewIgnoreDeploymentConfigWarningHandler(&wrappedHandler)
			h.HandleWarningHeader(http.StatusOK, "agent", tc.text)
			if tc.wraps {
				if tc.expectOutput != wrappedHandler.invoked {
					t.Fatalf("Wrapped hander: invoke expected: %v, invoke actual: %v", tc.expectOutput, wrappedHandler.invoked)
				}
			}
		})
	}
}

type mockWarningHandler struct {
	invoked bool
}

func (m *mockWarningHandler) HandleWarningHeader(warnStatusCode int, header string, text string) {
	m.invoked = true
}
