package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	kubecmd "k8s.io/kubectl/pkg/cmd"
	"k8s.io/kubectl/pkg/util/completion"
)

func TestKubeconfigCompletion(t *testing.T) {
	t.Setenv("KUBECTL_KUBERC", "false")
	t.Setenv("OC_ACTIVE_HELP", "0")

	writeConfig := func(names ...string) string {
		t.Helper()
		config := clientcmdapi.NewConfig()
		for _, name := range names {
			config.Clusters[name+"-cluster"] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:65535"}
			config.AuthInfos[name+"-user"] = &clientcmdapi.AuthInfo{}
			config.Contexts[name+"-context"] = &clientcmdapi.Context{
				Cluster:  name + "-cluster",
				AuthInfo: name + "-user",
			}
		}
		path := filepath.Join(t.TempDir(), "kubeconfig")
		if err := clientcmd.WriteToFile(*config, path); err != nil {
			t.Fatal(err)
		}
		return path
	}

	configPath := writeConfig("alpha", "beta")
	fallbackPath := writeConfig("fallback")
	commandTests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "use-context", args: []string{"config", "use-context", ""}, want: []string{"alpha-context", "beta-context", ":4"}},
		{name: "use-context prefix", args: []string{"config", "use-context", "alpha"}, want: []string{"alpha-context", ":4"}},
		{name: "use-context no match", args: []string{"config", "use-context", "missing"}, want: []string{":4"}},
		{name: "rename-context", args: []string{"config", "rename-context", ""}, want: []string{"alpha-context", "beta-context", ":4"}},
		{name: "delete-context", args: []string{"config", "delete-context", ""}, want: []string{"alpha-context", "beta-context", ":4"}},
		{name: "set-context", args: []string{"config", "set-context", ""}, want: []string{"alpha-context", "beta-context", ":4"}},
		{name: "delete-cluster", args: []string{"config", "delete-cluster", ""}, want: []string{"alpha-cluster", "beta-cluster", ":4"}},
		{name: "delete-user", args: []string{"config", "delete-user", ""}, want: []string{"alpha-user", "beta-user", ":4"}},
	}
	flagTests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "context flag", args: []string{"--context="}, want: []string{"alpha-context", "beta-context", ":4"}},
		{name: "context flag prefix", args: []string{"--context=alpha"}, want: []string{"alpha-context", ":4"}},
		{name: "context flag separate value", args: []string{"--context", ""}, want: []string{"alpha-context", "beta-context", ":4"}},
		{name: "inherited context flag", args: []string{"get", "--context=alpha"}, want: []string{"alpha-context", ":4"}},
		{name: "cluster flag", args: []string{"--cluster="}, want: []string{"alpha-cluster", "beta-cluster", ":4"}},
		{name: "cluster flag prefix", args: []string{"--cluster=alpha"}, want: []string{"alpha-cluster", ":4"}},
		{name: "user flag", args: []string{"--user="}, want: []string{"alpha-user", "beta-user", ":4"}},
		{name: "user flag prefix", args: []string{"--user=alpha"}, want: []string{"alpha-user", ":4"}},
	}
	sources := []struct {
		name       string
		kubeconfig string
		flags      []string
	}{
		{name: "environment", kubeconfig: configPath},
		{name: "flag with equals", kubeconfig: fallbackPath, flags: []string{"--kubeconfig=" + configPath}},
		{name: "flag with separate value", kubeconfig: fallbackPath, flags: []string{"--kubeconfig", configPath}},
	}

	for _, source := range sources {
		t.Run(source.name, func(t *testing.T) {
			t.Setenv("KUBECONFIG", source.kubeconfig)
			tests := flagTests
			if len(source.flags) == 0 {
				// Upstream config shadows the root --kubeconfig flag, while
				// its completion callbacks read the root factory's config.
				tests = append(commandTests, flagTests...)
			}
			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					// A kubectl command constructed by another test can initialize
					// this global and mask missing initialization in oc.
					completion.SetFactoryForCompletion(nil)
					t.Cleanup(func() { completion.SetFactoryForCompletion(nil) })

					args := append([]string{cobra.ShellCompRequestCmd}, source.flags...)
					args = append(args, test.args...)
					streams, _, out, errOut := genericiooptions.NewTestIOStreams()
					cmd := NewOcCommand(kubecmd.KubectlOptions{
						Arguments: append([]string{"oc"}, args...),
						IOStreams: streams,
					})
					cmd.SetOut(out)
					cmd.SetErr(errOut)
					cmd.SetArgs(args)
					if err := cmd.Execute(); err != nil {
						t.Fatalf("completion failed: %v\n%s", err, errOut.String())
					}

					got := strings.Split(strings.TrimSpace(out.String()), "\n")
					if diff := cmp.Diff(test.want, got, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
						t.Errorf("unexpected completion output (-want +got):\n%s", diff)
					}
				})
			}
		})
	}
}
