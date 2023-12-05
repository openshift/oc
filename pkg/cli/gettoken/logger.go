package gettoken

import (
	"log"

	"github.com/spf13/pflag"

	"github.com/int128/kubelogin/pkg/infrastructure/logger"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/klog/v2"
)

type goLogger interface {
	Printf(format string, v ...interface{})
}

func NewLogger(iostreams genericiooptions.IOStreams) *Logger {
	return &Logger{
		goLogger: log.New(iostreams.ErrOut, "", 0),
	}
}

// Logger provides logging facility using log.Logger and klog.
type Logger struct {
	goLogger
}

// AddFlags adds the flags such as -v.
func (*Logger) AddFlags(f *pflag.FlagSet) {
	return
}

// V returns a logger enabled only if the level is enabled.
func (*Logger) V(level int) logger.Verbose {
	return klog.V(klog.Level(level))
}

// IsEnabled returns true if the level is enabled.
func (*Logger) IsEnabled(level int) bool {
	return klog.V(klog.Level(level)).Enabled()
}
