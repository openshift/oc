package gettoken

import (
	"github.com/int128/kubelogin/pkg/cmd"
	"github.com/int128/kubelogin/pkg/di"
	"github.com/spf13/cobra"
)

// NewCmdGetTokenWrapper wraps oidc-login get-token command
// because other way would result in the installation of plugin
// mandatory. Users can still install their plugins and directly using
// these plugins but this import make easier to external OIDC login.
func NewCmdGetTokenWrapper() *cobra.Command {
	cmd := di.NewCmd().(*cmd.Cmd).GetToken.New()
	cmd.Short = "Wrapper command for oidc-login get-token"

	return cmd
}
