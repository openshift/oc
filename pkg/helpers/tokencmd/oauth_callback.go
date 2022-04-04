package tokencmd

import (
	"net/http"

	"github.com/RangelReale/osincli"
)

// newCallbackServer creates an HTTP server with a single endpoint /callback which receives the callback with the
// authorization code. After receiving the code server attempts to get an access token with the previously created
// osincli.AuthorizeRequest.
func newCallbackServer(request *osincli.AuthorizeRequest, client *osincli.Client) (*http.Server, chan callbackResult) {
	tokenCh := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.Handle("/callback", &callHandler{
		authorizeRequest: request,
		resultChan:       tokenCh,
		client:           client,
	})
	server := &http.Server{
		Handler: mux,
	}

	return server, tokenCh
}

type callbackResult struct {
	token string
	err   error
}

type callHandler struct {
	authorizeRequest *osincli.AuthorizeRequest
	resultChan       chan callbackResult
	client           *osincli.Client
}

func (c *callHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ad, err := c.authorizeRequest.HandleRequest(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("failed to parse callback"))
		c.resultChan <- callbackResult{
			err: err,
		}
		return
	}
	accessRequest := c.client.NewAccessRequest(osincli.AUTHORIZATION_CODE, ad)
	token, err := accessRequest.GetToken()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("failed to exchange authorization code for token"))
		c.resultChan <- callbackResult{
			err: err,
		}
		return
	}
	_, _ = w.Write([]byte("token successfully retrieved"))
	c.resultChan <- callbackResult{token: token.AccessToken}
}
