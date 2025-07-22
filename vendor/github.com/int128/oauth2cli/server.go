package oauth2cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/int128/listener"
	"golang.org/x/sync/errgroup"
)

func receiveCodeViaLocalServer(ctx context.Context, cfg *Config) (string, error) {
	localServerListener, err := listener.New(cfg.LocalServerBindAddress)
	if err != nil {
		return "", fmt.Errorf("could not start a local server: %w", err)
	}
	defer localServerListener.Close()

	if cfg.OAuth2Config.RedirectURL == "" {
		var localServerURL url.URL
		localServerHostname := "localhost"
		if cfg.RedirectURLHostname != "" {
			localServerHostname = cfg.RedirectURLHostname
		}
		localServerURL.Host = fmt.Sprintf("%s:%d", localServerHostname, localServerListener.Addr().(*net.TCPAddr).Port)
		localServerURL.Scheme = "http"
		if cfg.isLocalServerHTTPS() {
			localServerURL.Scheme = "https"
		}
		localServerURL.Path = cfg.LocalServerCallbackPath
		cfg.OAuth2Config.RedirectURL = localServerURL.String()
	}

	oauth2RedirectURL, err := url.Parse(cfg.OAuth2Config.RedirectURL)
	if err != nil {
		return "", fmt.Errorf("invalid OAuth2Config.RedirectURL: %w", err)
	}
	localServerIndexURL, err := oauth2RedirectURL.Parse("/")
	if err != nil {
		return "", fmt.Errorf("construct the index URL: %w", err)
	}

	respCh := make(chan *authorizationResponse)
	server := http.Server{
		Handler: cfg.LocalServerMiddleware(&localServerHandler{
			config: cfg,
			respCh: respCh,
		}),
	}
	shutdownCh := make(chan struct{})
	var resp *authorizationResponse
	var eg errgroup.Group
	eg.Go(func() error {
		defer close(respCh)
		cfg.Logf("oauth2cli: starting a server at %s", localServerListener.Addr())
		defer cfg.Logf("oauth2cli: stopped the server")
		if cfg.isLocalServerHTTPS() {
			if err := server.ServeTLS(localServerListener, cfg.LocalServerCertFile, cfg.LocalServerKeyFile); err != nil {
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}
				return fmt.Errorf("could not start HTTPS server: %w", err)
			}
			return nil
		}
		if err := server.Serve(localServerListener); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("could not start HTTP server: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		defer close(shutdownCh)
		select {
		case gotResp, ok := <-respCh:
			if ok {
				resp = gotResp
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	eg.Go(func() error {
		<-shutdownCh
		// Gracefully shutdown the server in the timeout.
		// If the server has not started, Shutdown returns nil and this returns immediately.
		// If Shutdown has failed, force-close the server.
		cfg.Logf("oauth2cli: shutting down the server")
		ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			cfg.Logf("oauth2cli: force-closing the server: shutdown failed: %s", err)
			_ = server.Close()
			return nil
		}
		return nil
	})
	eg.Go(func() error {
		if cfg.LocalServerReadyChan == nil {
			return nil
		}
		select {
		case cfg.LocalServerReadyChan <- localServerIndexURL.String():
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err := eg.Wait(); err != nil {
		return "", fmt.Errorf("authorization error: %w", err)
	}
	if resp == nil {
		return "", errors.New("no authorization response")
	}
	return resp.code, resp.err
}

type authorizationResponse struct {
	code string // non-empty if a valid code is received
	err  error  // non-nil if an error is received or any error occurs
}

type localServerHandler struct {
	config     *Config
	respCh     chan<- *authorizationResponse // channel to send a response to
	onceRespCh sync.Once                     // ensure send once
}

func (h *localServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	callbackPath := h.config.LocalServerCallbackPath
	if callbackPath == "" {
		callbackPath = "/"
	}
	q := r.URL.Query()
	switch {
	case r.Method == "GET" && r.URL.Path == callbackPath && q.Get("error") != "":
		h.onceRespCh.Do(func() {
			h.respCh <- h.handleErrorResponse(w, r)
		})
	case r.Method == "GET" && r.URL.Path == callbackPath && q.Get("code") != "":
		h.onceRespCh.Do(func() {
			h.respCh <- h.handleCodeResponse(w, r)
		})
	case r.Method == "GET" && r.URL.Path == "/":
		h.handleIndex(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *localServerHandler) handleIndex(w http.ResponseWriter, r *http.Request) {
	authCodeURL := h.config.OAuth2Config.AuthCodeURL(h.config.State, h.config.AuthCodeOptions...)
	h.config.Logf("oauth2cli: sending redirect to %s", authCodeURL)
	http.Redirect(w, r, authCodeURL, http.StatusFound)
}

func (h *localServerHandler) handleCodeResponse(w http.ResponseWriter, r *http.Request) *authorizationResponse {
	q := r.URL.Query()
	code, state := q.Get("code"), q.Get("state")

	if state != h.config.State {
		h.authorizationError(w, r)
		return &authorizationResponse{err: fmt.Errorf("state does not match (wants %s but got %s)", h.config.State, state)}
	}

	if h.config.SuccessRedirectURL != "" {
		http.Redirect(w, r, h.config.SuccessRedirectURL, http.StatusFound)
		return &authorizationResponse{code: code}
	}

	w.Header().Add("Content-Type", "text/html")
	if _, err := fmt.Fprint(w, h.config.LocalServerSuccessHTML); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return &authorizationResponse{err: fmt.Errorf("write error: %w", err)}
	}
	return &authorizationResponse{code: code}
}

func (h *localServerHandler) handleErrorResponse(w http.ResponseWriter, r *http.Request) *authorizationResponse {
	q := r.URL.Query()
	errorCode, errorDescription := q.Get("error"), q.Get("error_description")
	h.authorizationError(w, r)
	return &authorizationResponse{err: fmt.Errorf("authorization error from server: %s %s", errorCode, errorDescription)}
}

func (h *localServerHandler) authorizationError(w http.ResponseWriter, r *http.Request) {
	if h.config.FailureRedirectURL != "" {
		http.Redirect(w, r, h.config.FailureRedirectURL, http.StatusFound)
	} else {
		http.Error(w, "authorization error", http.StatusInternalServerError)
	}
}
