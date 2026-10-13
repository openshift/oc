package main

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/pflag"

	"github.com/openshift/library-go/pkg/serviceability"
	kcli "k8s.io/component-base/cli"
	"k8s.io/component-base/logs"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"

	"github.com/openshift/oc/pkg/cli"
	schemehelper "github.com/openshift/oc/pkg/helpers/scheme"
	"github.com/openshift/oc/pkg/version"

	// Import to initialize client auth plugins.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func injectLoglevelFlag(flags *pflag.FlagSet) {
	if vFlag := flags.Lookup("v"); vFlag != nil {
		flags.Var(&flagValueWrapper{vFlag.Value}, "loglevel", "Set the level of log output (0-10)")
	}
}

func main() {
	defer serviceability.BehaviorOnPanic(os.Getenv("OPENSHIFT_ON_PANIC"), version.Get())()
	defer serviceability.Profile(os.Getenv("OPENSHIFT_PROFILE")).Stop()

	if len(os.Getenv("GOMAXPROCS")) == 0 {
		runtime.GOMAXPROCS(runtime.NumCPU())
	}

	// Prevents race condition present in vendored version of Docker.
	// See: https://github.com/moby/moby/issues/39859
	os.Setenv("MOBY_DISABLE_PIGZ", "true")

	// the kubectl scheme expects to have all the recognizable external types it needs to consume.  Install those here.
	// We can't use the "normal" scheme because apply will use that to build stategic merge patches on CustomResources
	// new-app and set commands don't use this global scheme. Instead they use their own scheme.
	schemehelper.InstallSchemes(scheme.Scheme)

	basename := filepath.Base(os.Args[0])
	command := cli.CommandFor(basename)
	logs.AddFlags(command.PersistentFlags())
	injectLoglevelFlag(command.PersistentFlags())

	if err := kcli.RunNoErrOutput(command); err != nil {
		// Pretty-print the error and exit with an error.
		kcmdutil.CheckErr(err)
	}
}
