// Package fcgi serves HTTP requests from a FastCGI responder, such as
// php-fpm, through one http.HandlerFunc per script.
//
// The caller routes; the library runs one script per handler:
//
//	gw := fcgi.New("unix:/run/php/fpm.sock", fcgi.WithMaxConns(30))
//	mux.Handle("/api/", gw.Script("api.php"))
//
// docs/design.md defines the behaviour; the compliance module proves it.
package fcgi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/braska-io/fcgi-go/internal/fastcgi"
)

// ErrNotImplemented was returned by the stub. Nothing returns it any more.
//
// Deprecated: kept so the public API stays source compatible.
var ErrNotImplemented = errors.New("fcgi: not implemented")

// Gateway is the shared connection setup to one FastCGI responder.
type Gateway struct {
	address string
	cfg     config

	// slots is the semaphore behind WithMaxConns, shared by all scripts.
	slots chan struct{}

	mu       sync.Mutex
	closing  bool
	inflight sync.WaitGroup
}

// New configures a gateway to the responder at address, either
// "unix:<path>" or "tcp:<host:port>". It does not dial.
func New(address string, opts ...GatewayOption) *Gateway {
	g := &Gateway{address: address, cfg: defaultConfig()}
	for _, o := range opts {
		o.applyGateway(&g.cfg)
	}
	g.slots = make(chan struct{}, max(g.cfg.maxConns, 1))
	return g
}

// Script returns the handler that runs the script name for every request.
// Options given here override the gateway's for this script only.
func (g *Gateway) Script(name string, opts ...ScriptOption) http.HandlerFunc {
	cfg := g.cfg.forScript(name)
	for _, o := range opts {
		o.applyScript(&cfg)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		g.serve(w, r, &cfg)
	}
}

// Ping dials the responder and closes the connection without running a
// script. A FastCGI request would occupy a worker and make a saturated pool
// look unready.
func (g *Gateway) Ping(ctx context.Context) error {
	conn, err := fastcgi.Dial(ctx, g.address)
	if err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return conn.Close()
}

// Shutdown answers 503 to new calls and waits for running ones until ctx
// ends.
func (g *Gateway) Shutdown(ctx context.Context) error {
	g.mu.Lock()
	g.closing = true
	g.mu.Unlock()

	done := make(chan struct{})
	go func() {
		g.inflight.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("shutdown: waiting for running calls: %w", ctx.Err())
	}
}

// begin registers a running call, or reports that the gateway is closing.
func (g *Gateway) begin() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return false
	}
	g.inflight.Add(1)
	return true
}
