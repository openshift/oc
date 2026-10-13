package credwriter

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	v1 "k8s.io/client-go/pkg/apis/clientauthentication/v1"
)

// Writer writes ExecCredentials and basically
// populates the status with the retrieved
// token and expiration time.
type Writer struct {
	out io.Writer
}

func NewWriter(iostreams genericiooptions.IOStreams) *Writer {
	return &Writer{
		out: iostreams.Out,
	}
}

// Write writes the ExecCredential to standard output for oc.
func (w *Writer) Write(token string, expiry time.Time) error {
	ec := &v1.ExecCredential{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "client.authentication.k8s.io/v1",
			Kind:       "ExecCredential",
		},
		Status: &v1.ExecCredentialStatus{
			Token:               token,
			ExpirationTimestamp: &metav1.Time{Time: expiry},
		},
	}
	e := json.NewEncoder(w.out)
	if err := e.Encode(ec); err != nil {
		return fmt.Errorf("could not write the ExecCredential: %w", err)
	}
	return nil
}
