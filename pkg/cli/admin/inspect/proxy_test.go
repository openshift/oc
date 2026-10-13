package inspect

import (
	"reflect"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestElideProxy(t *testing.T) {
	tests := []struct {
		input    configv1.Proxy
		expected configv1.Proxy
	}{
		{
			input: configv1.Proxy{
				Spec: configv1.ProxySpec{
					HTTPProxy:  "",
					HTTPSProxy: "",
				},
				Status: configv1.ProxyStatus{
					HTTPProxy:  "",
					HTTPSProxy: "",
				},
			}, expected: configv1.Proxy{
				Spec: configv1.ProxySpec{
					HTTPProxy:  "",
					HTTPSProxy: "",
				},
				Status: configv1.ProxyStatus{
					HTTPProxy:  "",
					HTTPSProxy: "",
				},
			},
		},
		{
			input: configv1.Proxy{
				Spec: configv1.ProxySpec{
					HTTPProxy:  "http://user:pass@endpoint.com",
					HTTPSProxy: "https://user:pass@endpoint.com",
				},
				Status: configv1.ProxyStatus{
					HTTPProxy:  "http://user:pass@endpoint.com",
					HTTPSProxy: "https://user:pass@endpoint.com",
				},
			}, expected: configv1.Proxy{
				Spec: configv1.ProxySpec{
					HTTPProxy:  "http://user:****@endpoint.com",
					HTTPSProxy: "https://user:****@endpoint.com",
				},
				Status: configv1.ProxyStatus{
					HTTPProxy:  "http://user:****@endpoint.com",
					HTTPSProxy: "https://user:****@endpoint.com",
				},
			},
		},
		{
			input: configv1.Proxy{
				Spec: configv1.ProxySpec{
					HTTPProxy:  "http://user@endpoint.com",
					HTTPSProxy: "https://user@endpoint.com",
				},
				Status: configv1.ProxyStatus{
					HTTPProxy:  "http://user@endpoint.com",
					HTTPSProxy: "https://user@endpoint.com",
				},
			}, expected: configv1.Proxy{
				Spec: configv1.ProxySpec{
					HTTPProxy:  "http://user@endpoint.com",
					HTTPSProxy: "https://user@endpoint.com",
				},
				Status: configv1.ProxyStatus{
					HTTPProxy:  "http://user@endpoint.com",
					HTTPSProxy: "https://user@endpoint.com",
				},
			},
		},
		{
			input: configv1.Proxy{
				Spec: configv1.ProxySpec{
					HTTPProxy:  "http://user:@endpoint.com",
					HTTPSProxy: "https://user:@endpoint.com",
				},
				Status: configv1.ProxyStatus{
					HTTPProxy:  "http://user:@endpoint.com",
					HTTPSProxy: "https://user:@endpoint.com",
				},
			}, expected: configv1.Proxy{
				Spec: configv1.ProxySpec{
					HTTPProxy:  "http://user:@endpoint.com",
					HTTPSProxy: "https://user:@endpoint.com",
				},
				Status: configv1.ProxyStatus{
					HTTPProxy:  "http://user:@endpoint.com",
					HTTPSProxy: "https://user:@endpoint.com",
				},
			},
		},
		{
			input: configv1.Proxy{
				Spec: configv1.ProxySpec{
					HTTPProxy:  "http://openshift.com",
					HTTPSProxy: "https://openshift.com",
				},
				Status: configv1.ProxyStatus{
					HTTPProxy:  "http://openshift.com",
					HTTPSProxy: "https://openshift.com",
				},
			}, expected: configv1.Proxy{
				Spec: configv1.ProxySpec{
					HTTPProxy:  "http://openshift.com",
					HTTPSProxy: "https://openshift.com",
				},
				Status: configv1.ProxyStatus{
					HTTPProxy:  "http://openshift.com",
					HTTPSProxy: "https://openshift.com",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			elideProxy(&test.input)
			if !reflect.DeepEqual(test.input.Spec, test.expected.Spec) {
				t.Errorf("expected %v, got %v", test.expected.Spec, test.input.Spec)
			}
			if !reflect.DeepEqual(test.input.Status, test.expected.Status) {
				t.Errorf("expected %v, got %v", test.expected.Status, test.input.Status)
			}
		})
	}
}
