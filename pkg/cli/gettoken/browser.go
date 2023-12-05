package gettoken

import (
	"context"
	"os/exec"

	"github.com/pkg/browser"

	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type Browser struct {
	genericiooptions.IOStreams
}

func NewBrowser(streams genericiooptions.IOStreams) *Browser {
	return &Browser{
		streams,
	}
}

// Open opens the default browser.
func (b *Browser) Open(url string) error {
	return browser.OpenURL(url)
}

// OpenCommand opens the browser using the command.
func (b *Browser) OpenCommand(ctx context.Context, url, command string) error {
	c := exec.CommandContext(ctx, command, url)
	c.Stdout = b.Out
	c.Stderr = b.ErrOut
	return c.Run()
}
