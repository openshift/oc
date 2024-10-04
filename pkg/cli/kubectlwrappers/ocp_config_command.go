package kubectlwrappers

import (
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	kclientcmd "k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/cmd/config"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/cli/config/adminkubeconfig"
	"github.com/openshift/oc/pkg/cli/config/kubeletbootstrapkubeconfig"
	"github.com/openshift/oc/pkg/cli/config/refreshcabundle"
	cmdutil "github.com/openshift/oc/pkg/helpers/cmd"
)

// NewCmdConfig is a wrapper for the Kubernetes cli config command
func NewCmdConfig(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	pathOptions := kclientcmd.NewDefaultPathOptions()

	configCommand := config.NewCmdConfig(f, pathOptions, streams)
	configCommand.AddCommand(refreshcabundle.NewCmdConfigRefreshCABundle(f, pathOptions, streams))
	configCommand.AddCommand(adminkubeconfig.NewCmdNewAdminKubeconfigOptions(f, streams))
	configCommand.AddCommand(kubeletbootstrapkubeconfig.NewCmdNewKubeletBootstrapKubeconfig(f, streams))

	return cmdutil.ReplaceCommandName("kubectl", "oc", templates.Normalize(configCommand))
}
