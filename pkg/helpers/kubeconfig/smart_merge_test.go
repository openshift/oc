package kubeconfig

import (
	"path/filepath"
	"testing"

	restclient "k8s.io/client-go/rest"
	clientcmd "k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// TestLoginDoesNotCopySecondaryFileContextsToDefaultFile verifies that when KUBECONFIG
// lists multiple files, oc login does not copy contexts from secondary files into the
// first (default) file.
func TestLoginDoesNotCopySecondaryFileContextsToDefaultFile(t *testing.T) {
	dir := t.TempDir()
	primaryFile := filepath.Join(dir, "config")
	secondaryFile := filepath.Join(dir, "secondary.yaml")

	// Primary file: empty config (no clusters/contexts/users)
	if err := clientcmd.WriteToFile(*clientcmdapi.NewConfig(), primaryFile); err != nil {
		t.Fatal(err)
	}

	// Secondary file: an existing cluster with context and credentials
	secondaryConfig := clientcmdapi.NewConfig()
	secondaryConfig.Clusters["sandbox"] = &clientcmdapi.Cluster{
		Server: "https://sandbox.example.com:6443",
	}
	secondaryConfig.AuthInfos["sandbox"] = &clientcmdapi.AuthInfo{
		Token: "sandbox-token",
	}
	secondaryConfig.Contexts["sandbox"] = &clientcmdapi.Context{
		Cluster:   "sandbox",
		AuthInfo:  "sandbox",
		Namespace: "sandbox-ns",
	}
	if err := clientcmd.WriteToFile(*secondaryConfig, secondaryFile); err != nil {
		t.Fatal(err)
	}

	// Load both files as a merged starting config, the way oc does when
	// KUBECONFIG=primaryFile:secondaryFile. The loader stamps LocationOfOrigin
	// on every entry so ModifyConfig can route writes back to the right file.
	loadingRules := &clientcmd.ClientConfigLoadingRules{
		Precedence: []string{primaryFile, secondaryFile},
	}
	startingConfig, err := loadingRules.Load()
	if err != nil {
		t.Fatal(err)
	}

	// Simulate logging in to a new cluster (the addition produced by CreateConfig).
	newLoginConfig, err := CreateConfig("my-project", "alice", &restclient.Config{
		Host:        "https://new-cluster.example.com:6443",
		BearerToken: "alice-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	configToWrite, err := MergeConfig(*startingConfig, *newLoginConfig)
	if err != nil {
		t.Fatal(err)
	}

	// PathOptions mirrors what SaveConfig sets up: primary file is the default
	// write target; both files are in the loading precedence list.
	pathOptions := &clientcmd.PathOptions{
		GlobalFile: primaryFile,
		EnvVar:     "", // no env-var lookup — keeps the test hermetic
		LoadingRules: &clientcmd.ClientConfigLoadingRules{
			Precedence: []string{primaryFile, secondaryFile},
		},
	}
	if err := clientcmd.ModifyConfig(pathOptions, *configToWrite, false); err != nil {
		t.Fatal(err)
	}

	// Read primary file in isolation and assert it contains the new login context
	// but not anything that originated in the secondary file.
	result, err := clientcmd.LoadFromFile(primaryFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Contexts["sandbox"]; ok {
		t.Error("primary config must not contain the sandbox context from the secondary file")
	}
	if _, ok := result.Clusters["sandbox"]; ok {
		t.Error("primary config must not contain the sandbox cluster from the secondary file")
	}
	if _, ok := result.AuthInfos["sandbox"]; ok {
		t.Error("primary config must not contain the sandbox user from the secondary file")
	}
	if _, ok := result.Contexts["my-project/new-cluster-example-com:6443/alice"]; !ok {
		t.Error("primary config must contain the new login context")
	}
}
