package nodeimage

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	testCases := []struct {
		name                 string
		IPAddressesToMonitor string
		expectedError        string
	}{
		{
			name:                 "default",
			IPAddressesToMonitor: "192.168.111.83",
		},
		{
			name:          "no IP addresses",
			expectedError: "--ip-addresses cannot be empty",
		},
		{
			name:                 "invalid IP address",
			IPAddressesToMonitor: "192.168.111.8e",
			expectedError:        "192.168.111.8e is not valid IP address",
		},
		{
			name:                 "multiple IP addresses",
			IPAddressesToMonitor: "192.168.111.83,192.168.111.84",
		},
		{
			name:                 "IPv6 addresses",
			IPAddressesToMonitor: "2001:db8::1234:5678,2001:db8:3333:4444:5555:6666:7777:8888",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			o := &MonitorOptions{
				IPAddressesToMonitor: tc.IPAddressesToMonitor,
			}

			err := o.Validate()

			if tc.expectedError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error not received: %s", tc.expectedError)
				}
				if !strings.Contains(err.Error(), tc.expectedError) {
					t.Fatalf("expected error: %s, actual: %v", tc.expectedError, err.Error())
				}
			}
		})
	}
}
