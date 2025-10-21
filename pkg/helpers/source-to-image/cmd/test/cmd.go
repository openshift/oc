package test

import (
	"bytes"
	"io"

	"github.com/openshift/oc/pkg/helpers/source-to-image/cmd"
)

// FakeCmdRunner provider the fake command runner
type FakeCmdRunner struct {
	Name string
	Args []string
	Opts cmd.CommandOpts
	Err  error
}

// RunWithOptions runs the command runner with extra options
func (f *FakeCmdRunner) RunWithOptions(opts cmd.CommandOpts, name string, args ...string) error {
	f.Name = name
	f.Args = args
	f.Opts = opts
	return f.Err
}

// Run runs the fake command runner
func (f *FakeCmdRunner) Run(name string, args ...string) error {
	return f.RunWithOptions(cmd.CommandOpts{}, name, args...)
}

// StartWithStdoutPipe executes a command returning a ReadCloser connected to
// the command's stdout.
func (f *FakeCmdRunner) StartWithStdoutPipe(opts cmd.CommandOpts, name string, arg ...string) (io.ReadCloser, error) {
	return io.NopCloser(&bytes.Buffer{}), f.Err
}

// Wait waits for the command to exit.
func (f *FakeCmdRunner) Wait() error {
	return f.Err
}
