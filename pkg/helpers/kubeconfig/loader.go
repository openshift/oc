package kubeconfig

import (
	"github.com/spf13/cobra"

	kclientcmd "k8s.io/client-go/tools/clientcmd"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
)

func NewPathOptions(cmd *cobra.Command) *kclientcmd.PathOptions {
	return NewPathOptionsWithConfig(kcmdutil.GetFlagString(cmd, kclientcmd.RecommendedConfigPathFlag))
}

func NewPathOptionsWithConfig(configPath string) *kclientcmd.PathOptions {
	return &kclientcmd.PathOptions{
		GlobalFile: kclientcmd.RecommendedHomeFile,

		EnvVar:           kclientcmd.RecommendedConfigPathEnvVar,
		ExplicitFileFlag: kclientcmd.RecommendedConfigPathFlag,

		LoadingRules: &kclientcmd.ClientConfigLoadingRules{
			ExplicitPath: configPath,
		},
	}
}
