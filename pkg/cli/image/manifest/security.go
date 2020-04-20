package manifest

import (
	"fmt"
	"net/http"

	"github.com/spf13/pflag"

	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"

	operatorv1alpha1client "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1alpha1"
	"github.com/openshift/library-go/pkg/image/registryclient"
	"github.com/openshift/library-go/pkg/image/strategy"
	"github.com/openshift/oc/pkg/cli/image/manifest/dockercredentials"
)

type SecurityOptions struct {
	RegistryConfig    string
	Insecure          bool
	SkipVerification  bool
	ICSPFile          string
	ICSPClientFn      func() (operatorv1alpha1client.ImageContentSourcePolicyInterface, error)
	LookupClusterICSP bool
	FileDir           string

	CachedContext *registryclient.Context
}

func (o *SecurityOptions) Bind(flags *pflag.FlagSet) {
	flags.StringVarP(&o.RegistryConfig, "registry-config", "a", o.RegistryConfig, "Path to your registry credentials (defaults to ~/.docker/config.json)")
	flags.BoolVar(&o.Insecure, "insecure", o.Insecure, "Allow push and pull operations to registries to be made over HTTP")
	flags.BoolVar(&o.SkipVerification, "skip-verification", o.SkipVerification, "Skip verifying the integrity of the retrieved content. This is not recommended, but may be necessary when importing images from older image registries. Only bypass verification if the registry is known to be trustworthy.")
	flags.BoolVar(&o.LookupClusterICSP, "lookup-cluster-icsp", o.LookupClusterICSP, "default=true with 'oc adm release', default=false with 'oc image'. When explicitly set to true, look for alternative image sources from ImageContentSourcePolicy objects in cluster, honor the ordering of those sources, and fail if an ImageContentSourcePolicy is not found in cluster. Cannot be set to true with --icsp-file. Note: with implicit lookup, command will not error if unable to find any cluster ICSPs.")
	flags.StringVar(&o.ICSPFile, "icsp-file", o.ICSPFile, "Path to an ImageContentSourcePolicy file.  If set, data from this file will be used to set alternative image sources. Cannot be set together with --lookup-cluster-icsp=true.")
}

// ReferentialHTTPClient returns an http.Client that is appropriate for accessing
// blobs referenced outside of the registry (due to the present of the URLs attribute
// in the manifest reference for a layer).
func (o *SecurityOptions) ReferentialHTTPClient() (*http.Client, error) {
	regContext, err := o.Context()
	if err != nil {
		return nil, err
	}
	client := &http.Client{}
	if o.Insecure {
		client.Transport = regContext.InsecureTransport
	} else {
		client.Transport = regContext.Transport
	}
	return client, nil
}

func (o *SecurityOptions) Complete(f kcmdutil.Factory) error {
	if o.LookupClusterICSP && len(o.ICSPFile) > 0 {
		return fmt.Errorf("cannot set both --lookup-cluster-icsp=true and --icsp-file")
	}
	if o.LookupClusterICSP {
		o.ICSPClientFn = func() (operatorv1alpha1client.ImageContentSourcePolicyInterface, error) {
			// If ImageContentSourceFile is given, only add ImageContentSource from file, don't search cluster ICSP
			if len(o.ICSPFile) != 0 {
				return nil, nil
			}
			restConfig, err := f.ToRESTConfig()
			if err != nil {
				// may or may not be connected to a cluster
				// don't error if can't connect
				klog.V(4).Infof("did not connect to an OpenShift 4.x server, will not lookup ImageContentSourcePolicies: %v", err)
				return nil, nil
			}
			icspClient, err := operatorv1alpha1client.NewForConfig(restConfig)
			if err != nil {
				// may or may not be connected to a cluster
				// don't error if can't connect
				klog.V(4).Infof("did not connect to an OpenShift 4.x server, will not lookup ImageContentSourcePolicies: %v", err)
				return nil, nil
			}
			return icspClient.ImageContentSourcePolicies(), nil
		}
	}
	return nil
}

func (o *SecurityOptions) Context() (*registryclient.Context, error) {
	if o.CachedContext != nil {
		return o.CachedContext, nil
	}
	context, err := o.NewContext()
	if err == nil {
		o.CachedContext = context
		o.CachedContext.Retries = 3
	}
	return context, err
}

func (o *SecurityOptions) NewContext() (*registryclient.Context, error) {
	rt, err := rest.TransportFor(&rest.Config{})
	if err != nil {
		return nil, err
	}
	insecureRT, err := rest.TransportFor(&rest.Config{TLSClientConfig: rest.TLSClientConfig{Insecure: true}})
	if err != nil {
		return nil, err
	}
	creds := dockercredentials.NewLocal()
	if len(o.RegistryConfig) > 0 {
		creds, err = dockercredentials.NewFromFile(o.RegistryConfig)
		if err != nil {
			return nil, fmt.Errorf("unable to load --registry-config: %v", err)
		}
	}

	var regContext *registryclient.Context
	var icspClient operatorv1alpha1client.ImageContentSourcePolicyInterface
	if o.LookupClusterICSP {
		icspClient, err = o.ICSPClientFn()
		if err != nil {
			return nil, err
		}
	}
	regContext = registryclient.NewContext(rt, insecureRT).WithCredentials(creds)
	if len(o.ICSPFile) > 0 || o.LookupClusterICSP {
		regContext = regContext.WithAlternateRepositoryStrategy(strategy.NewSimpleLookupICSPStrategy(o.ICSPFile, icspClient))
	}
	regContext.DisableDigestVerification = o.SkipVerification
	return regContext, nil
}
