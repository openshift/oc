package kubeconfig

import (
	"github.com/spf13/cobra"

	kclientcmd "k8s.io/client-go/tools/clientcmd"
)

func NewPathOptions(cmd *cobra.Command) *kclientcmd.PathOptions {
	return &kclientcmd.PathOptions{
		GlobalFile: kclientcmd.RecommendedHomeFile,

		EnvVar:           kclientcmd.RecommendedConfigPathEnvVar,
		ExplicitFileFlag: kclientcmd.RecommendedConfigPathFlag,

		LoadingRules: kclientcmd.NewDefaultClientConfigLoadingRules(),
	}
}
