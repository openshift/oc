package etcd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"time"

	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	"go.etcd.io/etcd/clientv3"
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	bootstrapIPAnnotationKey = "alpha.installer.openshift.io/etcd-bootstrap"
	operatorNamespace        = "openshift-etcd"
)

func NewEtcdClient(config *rest.Config) (*clientv3.Client, error) {
	configClient, err := configv1client.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	endpoints, err := etcdEndpoints(ctx, kubeClient, configClient)
	if err != nil {
		return nil, err
	}

	// Read etcd client certs from the api
	certSecretName := "etcd-client"
	secret, err := kubeClient.CoreV1().Secrets(operatorNamespace).Get(ctx, certSecretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	certKey := "tls.crt"
	certData, ok := secret.Data[certKey]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s is missing key %q ", operatorNamespace, certSecretName, certKey)
	}
	keyKey := "tls.key"
	keyData, ok := secret.Data[keyKey]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s is missing key %q ", operatorNamespace, certSecretName, keyKey)
	}
	cert, err := tls.X509KeyPair(certData, keyData)
	if err != nil {
		return nil, err
	}

	// Read etcd ca bundle from the api
	bundleConfigMapName := "etcd-ca-bundle"
	configMap, err := kubeClient.CoreV1().ConfigMaps(operatorNamespace).Get(ctx, bundleConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	bundleKey := "ca-bundle.crt"
	bundleData, ok := configMap.Data[bundleKey]
	if !ok {
		return nil, fmt.Errorf("configmap %s/%s is missing key %q ", operatorNamespace, bundleConfigMapName, bundleKey)
	}
	pemByte := []byte(bundleData)
	certPool := x509.NewCertPool()
	for {
		var block *pem.Block
		block, pemByte = pem.Decode(pemByte)
		if block == nil {
			break
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}

		certPool.AddCert(cert)
	}

	cfg := &clientv3.Config{
		DialOptions: []grpc.DialOption{
			grpc.WithBlock(), // block until the underlying connection is up
		},
		Endpoints:   endpoints,
		DialTimeout: 15 * time.Second,
		TLS: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
			RootCAs:      certPool,
		},
	}

	cli, err := clientv3.New(*cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to make etcd client for endpoints %v: %w", endpoints, err)
	}
	return cli, err
}

func etcdEndpoints(ctx context.Context, kubeClient *kubernetes.Clientset, configClient *configv1client.Clientset) ([]string, error) {
	network, err := configClient.ConfigV1().Networks().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list cluster network: %w", err)
	}

	endpoints := []string{}

	// Add ip addresses of master nodes
	labelSelector := labels.Set(map[string]string{"node-role.kubernetes.io/master": ""}).AsSelector()
	nodes, err := kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
	if err != nil {
		return nil, err
	}
	for _, node := range nodes.Items {
		internalIP, err := GetEscapedPreferredInternalIPAddressForNodeName(network, &node)
		if err != nil {
			return nil, fmt.Errorf("failed to get internal IP for node: %w", err)
		}
		endpoints = append(endpoints, fmt.Sprintf("https://%s:2379", internalIP))
	}

	// Add ip address of bootstrap node if still present
	configmap, err := kubeClient.CoreV1().ConfigMaps(operatorNamespace).Get(ctx, "etcd-endpoints", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list endpoints: %w", err)
	}
	if bootstrapIP, ok := configmap.Annotations[bootstrapIPAnnotationKey]; ok && bootstrapIP != "" {
		// escape if IPv6
		if net.ParseIP(bootstrapIP).To4() == nil {
			bootstrapIP = "[" + bootstrapIP + "]"
		}
		endpoints = append(endpoints, fmt.Sprintf("https://%s:2379", bootstrapIP))
	}

	return endpoints, nil
}
