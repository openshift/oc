// Package listener provides utility for allocating a net.Listener from address candidates.
package listener

import (
	"errors"
	"fmt"
	"net"
	"net/url"
)

// Listener wraps a net.Listener and provides its URL.
type Listener struct {
	l net.Listener

	// URL to the listener.
	// This is always "http://localhost:PORT" regardless of the listening address.
	URL *url.URL
}

func (l *Listener) Accept() (net.Conn, error) {
	return l.l.Accept()
}

func (l *Listener) Close() error {
	return l.l.Close()
}

func (l *Listener) Addr() net.Addr {
	return l.l.Addr()
}

// NoAvailablePortError contains the causes of port allocation failure.
type NoAvailablePortError interface {
	Unwrap() []error
}

// New starts a Listener on one of the addresses.
// Caller should close the listener finally.
//
// If nil or an empty slice is given, it defaults to "127.0.0.1:0".
// If multiple address are given, it will try the addresses in order.
//
// If the port in the address is 0, it will allocate a free port.
//
// If no port is available, it will return an error which wraps NoAvailablePortError.
func New(addressCandidates []string) (*Listener, error) {
	if len(addressCandidates) == 0 {
		return NewOn("")
	}
	var errs []error
	for _, address := range addressCandidates {
		l, err := NewOn(address)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		return l, nil
	}
	return nil, fmt.Errorf("no available port: %w", errors.Join(errs...))
}

// NewOn starts a Listener on the address.
// Caller should close the listener finally.
//
// If an empty string is given, it defaults to "127.0.0.1:0".
//
// If the port in the address is 0, it will allocate a free port.
func NewOn(address string) (*Listener, error) {
	if address == "" {
		address = "127.0.0.1:0"
	}
	l, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("could not listen: %w", err)
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return nil, fmt.Errorf("internal error: got a unknown type of listener %T", l.Addr())
	}
	return &Listener{
		l:   l,
		URL: &url.URL{Host: fmt.Sprintf("localhost:%d", addr.Port), Scheme: "http"},
	}, nil
}
