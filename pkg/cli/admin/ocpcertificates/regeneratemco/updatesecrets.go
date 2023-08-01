package regeneratemco

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/vincent-petithory/dataurl"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"k8s.io/client-go/kubernetes"
)

func (o *RegenerateMCOOptions) RunUserDataUpdate(ctx context.Context) error {
	return o.updateSecrets(ctx)
}

func (o *RegenerateMCOOptions) updateSecrets(ctx context.Context) error {
	clientConfig, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	// Get the updated CA cert
	mcoSecrets := clientset.CoreV1().Secrets(mcoNamespace)
	mcsSecret, err := mcoSecrets.Get(ctx, newMCSCASecret, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("cannot read MCS secret: %w", err)
	}

	caCert, exists := mcsSecret.Data[corev1.TLSCertKey]
	if !exists {
		return fmt.Errorf("could not find MCS CA cert at %s", newMCSCASecret)
	}

	// Get user-data-secret
	mapiSecrets := clientset.CoreV1().Secrets(mapiNamespace)
	secretList, err := mapiSecrets.List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("cannot list MAO secrets: %w", err)
	}
	for _, secret := range secretList.Items {
		// These two are managed by and only used for BareMetal IPI. Skip them since the MCO will write them back.
		if secret.Name == mcoManagedWorkerSecret || secret.Name == mcoManagedMasterSecret {
			continue
		}

		// These secrets don't really have a label or not, so the determining factor is if they:
		// 1. have a userData field
		// 2. is an ignition config
		userData, exists := secret.Data[userDataKey]
		if !exists {
			continue
		}
		// userData is an ignition config. To save the effort of multiple-version parsing, just parse it as a json
		var userDataIgn interface{}
		if err = json.Unmarshal(userData, &userDataIgn); err != nil {
			return fmt.Errorf("failed to unmarshal decoded user-data to json (secret %s): %w", secret.Name, err)
		}

		_, isIgn, err := unstructured.NestedMap(userDataIgn.(map[string]interface{}), ignFieldIgnition)
		if !isIgn {
			// Didn't find ignition in user-data, warn but continue
			fmt.Fprintf(o.IOStreams.Out, "Unable to find ignition in user-data, skipping secret %s\n", secret.Name)
			continue
		}

		ignCAPath := []string{ignFieldIgnition, "security", "tls", "certificateAuthorities"}
		caSlice, isSlice, err := unstructured.NestedFieldNoCopy(userDataIgn.(map[string]interface{}), ignCAPath...)
		if !isSlice || err != nil {
			return fmt.Errorf("failed to find certificateauthorities field in ignition (secret %s): %w", secret.Name, err)
		}
		if len(caSlice.([]interface{})) > 1 {
			return fmt.Errorf("additional CAs detected, cannot modify automatically. Aborting")
		}
		caSlice.([]interface{})[0].(map[string]interface{})[ignFieldSource] = dataurl.EncodeBytes(caCert)

		updatedIgnition, err := json.Marshal(userDataIgn)
		if err != nil {
			return fmt.Errorf("failed to marshal updated ignition back to json (secret %s): %w", secret.Name, err)
		}

		if bytes.Equal(userData, updatedIgnition) {
			fmt.Fprintf(o.IOStreams.Out, "Secret %s already updated to use the latest CA, nothing to do\n", secret.Name)
			continue
		}

		secret.Data[userDataKey] = updatedIgnition
		_, err = mapiSecrets.Update(ctx, &secret, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("could not update secret %s: %w", secret.Name, err)
		}

		fmt.Fprintf(o.IOStreams.Out, "Successfully modified user-data secret %s\n", secret.Name)
	}

	// For Baremetal IPI, the worker-user-data-managed machineset is being used for scaling,
	// so we need to do update the source (root-ca configmap) which will also cause all nodes to reboot.
	oconfig, err := configclient.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	cfg, err := oconfig.ConfigV1().Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("unable to get cluster infrastructure resource: %w", err)
	}
	if cfg.Status.Platform == configv1.BareMetalPlatformType {
		kubeSystemRootCA, err := clientset.CoreV1().ConfigMaps(kubeSystemNamespace).Get(ctx, rootCAConfigmap, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				fmt.Fprintf(o.IOStreams.Out, "Could not find configmap %s/%s on platform %s, skipping. This may cause issues when scaling nodes.", kubeSystemNamespace, rootCAConfigmap, configv1.BareMetalPlatformType)
				return nil
			}
			return fmt.Errorf("unable to get configmap %s/%s: %w", kubeSystemNamespace, rootCAConfigmap, err)
		}

		kubeSystemRootCA.Data[rootCACertKey] = string(caCert)
		_, err = clientset.CoreV1().ConfigMaps(kubeSystemNamespace).Update(ctx, kubeSystemRootCA, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("could not update configmap %s: %w", rootCAConfigmap, err)
		}
		fmt.Fprintf(o.IOStreams.Out, "Successfully updated configmap %s/%s, nodes will now reboot.\n", kubeSystemNamespace, rootCAConfigmap)
	}

	return nil
}
