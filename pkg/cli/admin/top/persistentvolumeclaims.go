package top

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	"k8s.io/klog/v2"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	routev1 "github.com/openshift/api/route/v1"
	routev1client "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
)

const (
	localhostRecoveryTokenSecret = "localhost-recovery-client-token"
	kubeApiserverNamespace       = "openshift-kube-apiserver"
)

var (
	topPersistentVolumeClaimsLong = templates.LongDesc(`
		Experimental: Show usage statistics for bound persistentvolumeclaims.

		This command analyzes all the bound persistentvolumeclaims managed by the platform and presents current usage statistics.
	`)

	topPersistentVolumeClaimsExample = templates.Examples(`
		# Show usage statistics for all the bound persistentvolumeclaims across the cluster
		oc adm top persistentvolumeclaims -A

		# Show usage statistics for all the bound persistentvolumeclaims in a specific namespace
		oc adm top persistentvolumeclaims -n default

		# Show usage statistics for specific bound persistentvolumeclaims 
		oc adm top persistentvolumeclaims database-pvc app-pvc -n default

	`)
)

// RouteGetter is a function that gets a Route.
type RouteGetter func(ctx context.Context, namespace string, name string, opts metav1.GetOptions) (*routev1.Route, error)

type options struct {
	genericiooptions.IOStreams
	getRoute      RouteGetter
	Namespace     string
	InsecureTLS   bool
	allNamespaces bool
	ClientConfig  *rest.Config
	ClientSet     kubernetes.Interface
	BearerToken   string
}

func newOptions(streams genericiooptions.IOStreams) *options {
	return &options{
		IOStreams: streams,
	}
}

func NewCmdTopPersistentVolumeClaims(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := newOptions(streams)
	cmd := &cobra.Command{
		Use:     "persistentvolumeclaims",
		Aliases: []string{"persistentvolumeclaim", "pvc"},
		Short:   "Experimental: Show usage statistics for bound persistentvolumeclaims",
		Long:    topPersistentVolumeClaimsLong,
		Example: topPersistentVolumeClaimsExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Run(cmd.Context(), args))
		},
	}

	cmd.Flags().BoolVarP(&o.allNamespaces, "all-namespaces", "A", o.allNamespaces, "If present, list the pvc usage across all namespaces. Namespace in current context is ignored even if specified with --namespace")
	cmd.Flags().BoolVar(&o.InsecureTLS, "insecure-skip-tls-verify", false, "If true, the server's certificate will not be checked for validity. This will make your HTTPS connections insecure")
	cmd.Flags().MarkHidden("insecure-skip-tls-verify")
	return cmd
}

func (o *options) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	var err error
	o.Namespace, _, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}
	cfg, err := f.ToRESTConfig()
	if err != nil {
		return err
	}

	o.ClientConfig = cfg

	routeClient, err := routev1client.NewForConfig(cfg)
	if err != nil {
		return err
	}

	o.ClientSet, err = kubernetes.NewForConfig(o.ClientConfig)
	if err != nil {
		return err
	}
	o.getRoute = func(ctx context.Context, namespace string, name string, opts metav1.GetOptions) (*routev1.Route, error) {
		return routeClient.Routes(namespace).Get(ctx, name, opts)
	}

	if o.allNamespaces {
		o.Namespace = metav1.NamespaceAll
	} else {
		namespace := cmd.Flag("namespace").Value.String()
		if len(namespace) != 0 {
			o.Namespace = namespace
		}
	}

	if o.allNamespaces && len(args) != 0 {
		return fmt.Errorf("a persistentvolumeclaim resource cannot be retrieved by name across all namespaces.")
	}

	return nil
}

type persistentVolumeClaimInfo struct {
	Namespace       string
	Name            string
	UsagePercentage string
}

func (v persistentVolumeClaimInfo) PrintLine(out io.Writer) {
	printValue(out, v.Namespace)
	printValue(out, v.Name)
	printValue(out, v.UsagePercentage)
}

func (o *options) Run(ctx context.Context, args []string) error {
	o.BearerToken = o.ClientConfig.BearerToken
	if len(o.ClientConfig.BearerToken) == 0 {
		klog.V(4).Info(fmt.Sprintf(`no token is currently in use for this session, attempting to retrieve token from secret "%s" in namespace "%s"`, localhostRecoveryTokenSecret, kubeApiserverNamespace))
		secret, err := o.ClientSet.CoreV1().Secrets(kubeApiserverNamespace).Get(context.TODO(), localhostRecoveryTokenSecret, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				klog.V(4).Info(fmt.Errorf("error retrieving secret: %s", err.Error()))
				return fmt.Errorf("no token is currently in use for this session")
			}
			return fmt.Errorf("%s", err.Error())
		}
		localhostRecoveryToken, exist := secret.Data["token"]
		if !exist {
			return fmt.Errorf(`"token" key not found in secret "%s" in namespace "%s"`, localhostRecoveryTokenSecret, kubeApiserverNamespace)
		}
		o.BearerToken = string(localhostRecoveryToken)
	}
	persistentVolumeClaimsBytes, err := GetPersistentVolumeClaims(ctx, o.getRoute, o.BearerToken, o.Namespace, o.InsecureTLS, args)
	if err != nil {
		return err
	}
	promOutput := &PromOutput{}
	err = json.Unmarshal([]byte(persistentVolumeClaimsBytes), &promOutput)
	if err != nil {
		return err
	}

	if len(promOutput.Data.Result) == 0 {
		if o.Namespace == "" {
			return fmt.Errorf("no persistentvolumeclaims found.")
		}
		if len(args) == 0 {
			return fmt.Errorf("no persistentvolumeclaims found in %s namespace.", o.Namespace)
		}
		return fmt.Errorf("persistentvolumeclaim %q not found in %s namespace.", args[0], o.Namespace)

	}

	// if more pvc are requested as args but one of them does not not exist
	if len(args) != 0 && len(promOutput.Data.Result) != len(args) {
		resultingPvc := make(map[string]bool)
		for _, promOutputDataResult := range promOutput.Data.Result {
			pvcName := promOutputDataResult.Metric["persistentvolumeclaim"]
			resultingPvc[pvcName] = true
		}
		for _, arg := range args {
			if _, ok := resultingPvc[arg]; !ok {
				return fmt.Errorf("persistentvolumeclaim %q not found in %s namespace.", arg, o.Namespace)
			}
		}

	}
	headers := []string{"NAMESPACE", "NAME", "USAGE(%)"}
	pvcInfos := []persistentVolumeClaimInfo{}
	infos := []Info{}
	for _, promOutputDataResult := range promOutput.Data.Result {
		namespaceName := promOutputDataResult.Metric["namespace"]
		pvcName := promOutputDataResult.Metric["persistentvolumeclaim"]
		usagePercentage := promOutputDataResult.Value[1]
		valueFloatLong, _ := strconv.ParseFloat(usagePercentage.(string), 64)
		valueFloat := fmt.Sprintf("%.2f", valueFloatLong)
		if len(pvcInfos) > 0 {
			if !(namespaceName == pvcInfos[len(pvcInfos)-1].Namespace && pvcName == pvcInfos[len(pvcInfos)-1].Name) {
				pvcInfos = append(pvcInfos, persistentVolumeClaimInfo{Namespace: namespaceName, Name: pvcName, UsagePercentage: valueFloat})
				infos = append(infos, persistentVolumeClaimInfo{Namespace: namespaceName, Name: pvcName, UsagePercentage: valueFloat})
			}
		} else {
			pvcInfos = append(pvcInfos, persistentVolumeClaimInfo{Namespace: namespaceName, Name: pvcName, UsagePercentage: valueFloat})
			infos = append(infos, persistentVolumeClaimInfo{Namespace: namespaceName, Name: pvcName, UsagePercentage: valueFloat})
		}
	}

	Print(o.Out, headers, infos)
	return nil
}

func constructPrometheusQuery(namespace string, args []string) string {
	query := ""
	claimNames := ".*"
	if namespace != "" {
		if len(args) > 0 {
			claimNames = strings.Join(args, "|")
		}
		query = fmt.Sprintf(`100*kubelet_volume_stats_used_bytes{persistentvolumeclaim=~"%s", namespace="%s"}/kubelet_volume_stats_capacity_bytes{persistentvolumeclaim=~"%s", namespace="%s"}`, claimNames, namespace, claimNames, namespace)
	} else {
		query = `100*kubelet_volume_stats_used_bytes{persistentvolumeclaim=~".*"}/kubelet_volume_stats_capacity_bytes{persistentvolumeclaim=~".*"}`
	}
	return query
}

func GetPersistentVolumeClaims(ctx context.Context, getRoute RouteGetter, bearerToken string, namespace string, insecureTLS bool, args []string) ([]byte, error) {
	uri := &url.URL{
		Scheme: "https",
		Path:   "/api/v1/query",
	}
	query := constructPrometheusQuery(namespace, args)
	urlParams := url.Values{}
	urlParams.Set("query", query)
	uri.RawQuery = urlParams.Encode()

	persistentVolumeClaimsBytes, err := getWithBearer(ctx, getRoute, "openshift-monitoring", "prometheus-k8s", uri, bearerToken, insecureTLS)
	if err != nil {
		return persistentVolumeClaimsBytes, fmt.Errorf("failed to get persistentvolumeclaims from Prometheus: %w", err)
	}

	return persistentVolumeClaimsBytes, nil
}

// getWithBearer gets a Route by namespace/name, constructs a URI using
// status.ingress[].host and the path argument, and performs GETs on that
// URI using Bearer authentication with the token argument.
func getWithBearer(ctx context.Context, getRoute RouteGetter, namespace, name string, baseURI *url.URL, bearerToken string, InsecureTLS bool) ([]byte, error) {
	if len(bearerToken) == 0 {
		return nil, fmt.Errorf("no token is currently in use for this session")
	}

	route, err := getRoute(ctx, namespace, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	httpTransport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: InsecureTLS},
	}

	withDebugWrappers, err := transport.HTTPWrappersForConfig(
		&transport.Config{
			UserAgent:   rest.DefaultKubernetesUserAgent() + "(top persistentvolumeclaims)",
			BearerToken: bearerToken,
		},
		httpTransport,
	)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Transport: withDebugWrappers}
	errs := make([]error, 0, len(route.Status.Ingress))
	for _, ingress := range route.Status.Ingress {
		baseURI.Host = ingress.Host
		content, err := getMetrics(*baseURI, client)
		if err == nil {
			return content, nil
		} else {
			errs = append(errs, fmt.Errorf("%s, %w", ingress.Host, err))
		}
	}

	if len(errs) == 1 {
		return nil, fmt.Errorf("unable to get %s from URI in the %s/%s Route: %s", baseURI.Path, namespace, name, errorsutil.NewAggregate(errs))
	}
	return nil, fmt.Errorf("unable to get %s from any of %d URIs in the %s/%s Route: %s", baseURI.Path, len(errs), namespace, name, errorsutil.NewAggregate(errs))

}

func getMetrics(uri url.URL, client *http.Client) ([]byte, error) {
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

type PromOutput struct {
	Status string         `json:"status"`
	Data   PromOutputData `json:"data"`
}

type PromOutputData struct {
	ResultType string                 `json:"resultType"`
	Result     []PromOutputDataResult `json:"result"`
}

type PromOutputDataResult struct {
	Metric map[string]string `json:"metric"`
	Value  FloatStringPair   `json:"value"`
}

type FloatStringPair [2]interface{}

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
