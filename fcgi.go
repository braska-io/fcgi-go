// Package fcgi serves HTTP requests from a FastCGI responder, such as
// php-fpm, through one http.HandlerFunc per script.
//
// The caller routes; the library runs one script per handler:
//
//	gw := fcgi.New("unix:/run/php/fpm.sock", fcgi.WithMaxConns(30))
//	mux.Handle("/api/", gw.Script("api.php"))
//
// This version is a stub: every handler answers 501. The compliance suite in
// the compliance module defines the behaviour the implementation must reach.
package fcgi

import (
	"context"
	"errors"
	"net/http"
)

// ErrNotImplemented is returned by every operation of the stub.
var ErrNotImplemented = errors.New("fcgi: not implemented")

// Gateway is the shared connection setup to one FastCGI responder.
type Gateway struct {
	address string
	cfg     config
}

// New configures a gateway to the responder at address, either
// "unix:<path>" or "tcp:<host:port>". It does not dial.
func New(address string, opts ...GatewayOption) *Gateway {
	g := &Gateway{address: address, cfg: defaultConfig()}
	for _, o := range opts {
		o.applyGateway(&g.cfg)
	}
	return g
}

// Script returns the handler that runs the script name for every request.
// Options given here override the gateway's for this script only.
func (g *Gateway) Script(name string, opts ...ScriptOption) http.HandlerFunc {
	cfg := g.cfg.forScript(name)
	for _, o := range opts {
		o.applyScript(&cfg)
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, ErrNotImplemented.Error(), http.StatusNotImplemented)
	}
}

// Ping dials the responder and closes the connection without running a
// script.
func (g *Gateway) Ping(context.Context) error { return ErrNotImplemented }

// Shutdown answers 503 to new calls and waits for running ones until ctx
// ends.
func (g *Gateway) Shutdown(context.Context) error { return ErrNotImplemented }
