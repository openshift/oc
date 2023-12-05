package gettoken

import (
	"encoding/json"
	"fmt"

	credentialplugin "github.com/int128/kubelogin/pkg/credentialplugin"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	v1 "k8s.io/client-go/pkg/apis/clientauthentication/v1"
)

func NewWriter(iostreams genericiooptions.IOStreams) *Writer {
	return &Writer{
		out: iostreams.Out,
	}
}

// Write writes the ExecCredential to standard output for kubectl.
func (w *Writer) Write(out credentialplugin.Output) error {
	ec := &v1.ExecCredential{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "client.authentication.k8s.io/v1",
			Kind:       "ExecCredential",
		},
		Status: &v1.ExecCredentialStatus{
			Token:               out.Token,
			ExpirationTimestamp: &metav1.Time{Time: out.Expiry},
		},
	}
	e := json.NewEncoder(w.out)
	if err := e.Encode(ec); err != nil {
		return fmt.Errorf("could not write the ExecCredential: %w", err)
	}
	return nil
}
