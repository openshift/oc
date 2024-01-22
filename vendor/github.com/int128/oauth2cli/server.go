package oauth2cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/int128/listener"
	"golang.org/x/sync/errgroup"
)

func receiveCodeViaLocalServer(ctx context.Context, c *Config) (string, error) {
	l, err := listener.New(c.LocalServerBindAddress)
	if err != nil {
		return "", fmt.Errorf("could not start a local server: %w", err)
	}
	defer l.Close()
	c.OAuth2Config.RedirectURL = computeRedirectURL(l, c)

	respCh := make(chan *authorizationResponse)
	server := http.Server{
		Handler: c.LocalServerMiddleware(&localServerHandler{
			config: c,
			respCh: respCh,
		}),
	}
	shutdownCh := make(chan struct{})
	var resp *authorizationResponse
	var eg errgroup.Group
	eg.Go(func() error {
		defer close(respCh)
		c.Logf("oauth2cli: starting a server at %s", l.Addr())
		defer c.Logf("oauth2cli: stopped the server")
		if c.isLocalServerHTTPS() {
			if err := server.ServeTLS(l, c.LocalServerCertFile, c.LocalServerKeyFile); err != nil && err != http.ErrServerClosed {
				return fmt.Errorf("could not start HTTPS server: %w", err)
			}
			return nil
		}
		if err := server.Serve(l); err != nil && err != http.ErrServerClosed {
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
		c.Logf("oauth2cli: shutting down the server")
		ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			c.Logf("oauth2cli: force-closing the server: shutdown failed: %s", err)
			_ = server.Close()
			return nil
		}
		return nil
	})
	eg.Go(func() error {
		if c.LocalServerReadyChan == nil {
			return nil
		}
		select {
		case c.LocalServerReadyChan <- c.OAuth2Config.RedirectURL:
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

func computeRedirectURL(l net.Listener, c *Config) string {
	hostPort := fmt.Sprintf("%s:%d", c.RedirectURLHostname, l.Addr().(*net.TCPAddr).Port)
	if c.LocalServerCertFile != "" {
		return "https://" + hostPort
	}
	return "http://" + hostPort
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
	q := r.URL.Query()
	switch {
	case r.Method == "GET" && r.URL.Path == "/" && q.Get("error") != "":
		h.onceRespCh.Do(func() {
			h.respCh <- h.handleErrorResponse(w, r)
		})
	case r.Method == "GET" && r.URL.Path == "/" && q.Get("code") != "":
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
	http.Redirect(w, r, authCodeURL, 302)
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
	} else {
		w.Header().Add("Content-Type", "text/html")
		if _, err := fmt.Fprintf(w, h.config.LocalServerSuccessHTML); err != nil {
			http.Error(w, "server error", 500)
			return &authorizationResponse{err: fmt.Errorf("write error: %w", err)}
		}
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
		http.Error(w, "authorization error", 500)
	}
}
