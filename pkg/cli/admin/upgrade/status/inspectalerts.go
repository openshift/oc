package status

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	"k8s.io/klog/v2"

	routev1 "github.com/openshift/api/route/v1"
)

// RouteGetter is a function that gets a Route.
type RouteGetter func(ctx context.Context, namespace string, name string, opts metav1.GetOptions) (*routev1.Route, error)

// GetAlerts gets alerts (both firing and pending) from openshift-monitoring Thanos.
func GetAlerts(ctx context.Context, getRoute RouteGetter, bearerToken string) ([]byte, error) {
	uri := &url.URL{ // configure everything except Host, which will come from the Route
		Scheme: "https",
		Path:   "/api/v1/alerts",
	}

	// if we end up going this way, probably port to github.com/prometheus/client_golang/api/prometheus/v1 NewAPI
	alertBytes, err := getWithBearer(ctx, getRoute, "openshift-monitoring", "thanos-querier", uri, bearerToken)
	if err != nil {
		return alertBytes, fmt.Errorf("failed to get alerts from Thanos: %w", err)
	}

	// if we end up going this way, probably check and error on 'result' being an empty set (it should at least contain Watchdog)

	return alertBytes, nil
}

// getWithBearer gets a Route by namespace/name, constructs a URI using
// status.ingress[].host and the path argument, and performs GETs on that
// URI using Bearer authentication with the token argument.
func getWithBearer(ctx context.Context, getRoute RouteGetter, namespace, name string, baseURI *url.URL, bearerToken string) ([]byte, error) {
	if len(bearerToken) == 0 {
		return nil, fmt.Errorf("no token is currently in use for this session")
	}

	route, err := getRoute(ctx, namespace, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	withDebugWrappers, err := transport.HTTPWrappersForConfig(
		&transport.Config{
			UserAgent:   rest.DefaultKubernetesUserAgent() + "(inspect-alerts)",
			BearerToken: bearerToken,
		},
		http.DefaultTransport,
	)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Transport: withDebugWrappers}
	errs := make([]error, 0, len(route.Status.Ingress))
	for _, ingress := range route.Status.Ingress {
		uri := *baseURI
		uri.Host = ingress.Host
		content, err := checkedGet(uri, client)
		if err == nil {
			return content, nil
		} else {
			errs = append(errs, fmt.Errorf("%s->%w", ingress.Host, err))
		}
	}

	if len(errs) == 1 {
		return nil, fmt.Errorf("unable to get %s from URI in the %s/%s Route: %s", baseURI.Path, namespace, name, errors.NewAggregate(errs))
	} else {
		return nil, fmt.Errorf("unable to get %s from any of %d URIs in the %s/%s Route: %s", baseURI.Path, len(errs), namespace, name, errors.NewAggregate(errs))
	}

}

func checkedGet(uri url.URL, client *http.Client) ([]byte, error) {
	req, err := http.NewRequest("GET", uri.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	glogBody("Response Body", body)

	if resp.StatusCode != http.StatusOK {
		return body, fmt.Errorf("GET status code=%d", resp.StatusCode)
	}

	return body, nil
}

// glogBody and truncateBody taken from client-go Request
// https://github.com/openshift/oc/blob/4be3c8609f101a8c5867abc47bda33caae629113/vendor/k8s.io/client-go/rest/request.go#L1183-L1215

// truncateBody decides if the body should be truncated, based on the glog Verbosity.
func truncateBody(body string) string {
	max := 0
	switch {
	case bool(klog.V(10).Enabled()):
		return body
	case bool(klog.V(9).Enabled()):
		max = 10240
	case bool(klog.V(8).Enabled()):
		max = 1024
	}

	if len(body) <= max {
		return body
	}

	return body[:max] + fmt.Sprintf(" [truncated %d chars]", len(body)-max)
}

// glogBody logs a body output that could be either JSON or protobuf. It explicitly guards against
// allocating a new string for the body output unless necessary. Uses a simple heuristic to determine
// whether the body is printable.
func glogBody(prefix string, body []byte) {
	if klogV := klog.V(8); klogV.Enabled() {
		if bytes.IndexFunc(body, func(r rune) bool {
			return r < 0x0a
		}) != -1 {
			klogV.Infof("%s:\n%s", prefix, truncateBody(hex.Dump(body)))
		} else {
			klogV.Infof("%s: %s", prefix, truncateBody(string(body)))
		}
	}
}
