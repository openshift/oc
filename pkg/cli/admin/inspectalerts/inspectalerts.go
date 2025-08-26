// Package inspectalerts provides access to in-cluster alerts.
package inspectalerts

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"

	routev1 "github.com/openshift/api/route/v1"
	routev1client "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
)

// RouteGetter is a function that gets a Route.
type RouteGetter func(ctx context.Context, namespace string, name string, opts metav1.GetOptions) (*routev1.Route, error)

type Alert struct {
}

type options struct {
	genericiooptions.IOStreams
	RESTConfig *rest.Config
	getRoute   RouteGetter
}

func newOptions(streams genericiooptions.IOStreams) *options {
	return &options{
		IOStreams: streams,
	}
}

func New(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := newOptions(streams)
	cmd := &cobra.Command{
		Use:   "inspect-alerts",
		Short: "Collect information about running alerts.",
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run(cmd.Context()))
		},
	}

	return cmd
}

func (o *options) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	kcmdutil.RequireNoArguments(cmd, args)

	cfg, err := f.ToRESTConfig()
	if err != nil {
		return err
	}

	o.RESTConfig = cfg
	o.RESTConfig.UserAgent = rest.DefaultKubernetesUserAgent() + "(inspect-alerts)"
	if err = ValidateRESTConfig(o.RESTConfig); err != nil {
		return err
	}

	routeClient, err := routev1client.NewForConfig(o.RESTConfig)
	if err != nil {
		return err
	}
	o.getRoute = func(ctx context.Context, namespace string, name string, opts metav1.GetOptions) (*routev1.Route, error) {
		return routeClient.Routes(namespace).Get(ctx, name, opts)
	}

	return nil
}

func (o *options) Run(ctx context.Context) error {
	roundTripper, err := rest.TransportFor(o.RESTConfig)
	if err != nil {
		return err
	}

	alertBytes, err := GetAlerts(ctx, roundTripper, o.getRoute)
	if err != nil {
		return err
	}

	_, err = o.Out.Write(alertBytes)
	return err
}

// ValidateRESTConfig validates a rest.Config for alert retrieval,
// requiring the use of BearerToken, because the platform Thanos rejects
// other forms of authentication.
func ValidateRESTConfig(restConfig *rest.Config) error {
	if restConfig.BearerToken == "" && restConfig.BearerTokenFile == "" {
		return fmt.Errorf("no token is currently in use for this session")
	}
	return nil
}

// GetAlerts gets alerts (both firing and pending) from openshift-monitoring Thanos.
func GetAlerts(ctx context.Context, roundTripper http.RoundTripper, getRoute RouteGetter) ([]byte, error) {
	uri := &url.URL{ // configure everything except Host, which will come from the Route
		Scheme: "https",
		Path:   "/api/v1/alerts",
	}

	// if we end up going this way, probably port to github.com/prometheus/client_golang/api/prometheus/v1 NewAPI
	alertBytes, err := getWithRoundTripper(ctx, roundTripper, getRoute, "openshift-monitoring", "thanos-querier", uri)
	if err != nil {
		return alertBytes, fmt.Errorf("failed to get alerts from Thanos: %w", err)
	}

	// if we end up going this way, probably check and error on 'result' being an empty set (it should at least contain Watchdog)

	return alertBytes, nil
}

// getWithRoundTripper gets a Route by namespace/name, constructs a URI using
// status.ingress[].host and the path argument, and performs GETs on that
// URI.
func getWithRoundTripper(ctx context.Context, roundTripper http.RoundTripper, getRoute RouteGetter, namespace, name string, baseURI *url.URL) ([]byte, error) {
	route, err := getRoute(ctx, namespace, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	client := &http.Client{Transport: roundTripper}
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
