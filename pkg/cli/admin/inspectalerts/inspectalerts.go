// Package inspectalerts provides access to in-cluster alerts.
package inspectalerts

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/rest"
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

	routeClient, err := routev1client.NewForConfig(cfg)
	if err != nil {
		return err
	}
	o.getRoute = func(ctx context.Context, namespace string, name string, opts metav1.GetOptions) (*routev1.Route, error) {
		return routeClient.Routes(namespace).Get(ctx, name, opts)
	}

	return nil
}

func (o *options) Run(ctx context.Context) error {
	alertBytes, err := GetAlerts(ctx, o.getRoute, o.RESTConfig.BearerToken)
	if err != nil {
		return err
	}

	_, err = o.Out.Write(alertBytes)
	return err
}

// GetAlerts gets alerts (both firing and pending) from openshift-monitoring Thanos.
func GetAlerts(ctx context.Context, getRoute RouteGetter, bearerToken string) ([]byte, error) {
	uri := &url.URL{ // configure everything except Host, which will come from the Route
		Scheme: "https",
		Path:   "/api/v1/alerts",
	}

	// if we end up going this way, probably port to github.com/prometheus/client_golang/api/prometheus/v1 NewAPI
	alertBytes, err := getWithBearer(ctx, getRoute, "openshift-monitoring", "thanos-querier", uri, bearerToken)
	if err != nil {
		return alertBytes, err
	}

	// if we end up going this way, probably check and error on 'result' being an empty set (it should at least contain Watchdog)

	return alertBytes, nil
}

// getWithBearer gets a Route by namespace/name, contructs a URI using
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

	client := &http.Client{}
	uris := make([]string, 0, len(route.Status.Ingress))
	for _, ingress := range route.Status.Ingress {
		uri := *baseURI
		uri.Host = ingress.Host
		uris = append(uris, uri.String())
		req, err := http.NewRequest("GET", uri.String(), nil)
		if err != nil {
			return nil, err
		}

		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", bearerToken))

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		return body, err
	}

	return nil, fmt.Errorf("unable to get %s from any of %d URIs in the %s Route in the %s namespace: %s", baseURI.Path, len(uris), name, namespace, strings.Join(uris, ", "))
}
