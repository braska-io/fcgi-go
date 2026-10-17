package fastcgi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// resolveTTL bounds how long a resolved host name is reused. nginx resolves
// an upstream name once at start; resolving on every call instead costs a
// DNS round trip per request. A few seconds keep a changed name, such as a
// recreated Kubernetes service, visible quickly.
const resolveTTL = 5 * time.Second

// Dialer opens connections to one responder address, either "unix:<path>"
// or "tcp:<host:port>". It caches the addresses a host name resolves to.
type Dialer struct {
	network, addr string
	host, port    string
	err           error

	lookup func(ctx context.Context, host string) ([]string, error)
	now    func() time.Time

	mu         sync.Mutex
	ips        []string
	resolved   time.Time
	refreshing bool
}

// refreshTimeout bounds a background lookup, which no call's context does.
const refreshTimeout = 10 * time.Second

// NewDialer parses address. A malformed address fails on every Dial.
func NewDialer(address string) *Dialer {
	d := &Dialer{lookup: net.DefaultResolver.LookupHost, now: time.Now}
	network, addr, ok := strings.Cut(address, ":")
	if !ok || (network != "unix" && network != "tcp") || addr == "" {
		d.err = fmt.Errorf("invalid responder address %q", address)
		return d
	}
	d.network, d.addr = network, addr
	if network == "tcp" {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			d.err = fmt.Errorf("invalid responder address %q: %w", address, err)
			return d
		}
		if net.ParseIP(host) == nil {
			d.host, d.port = host, port
		}
	}
	return d
}

// Dial opens one connection.
func (d *Dialer) Dial(ctx context.Context) (net.Conn, error) {
	if d.err != nil {
		return nil, d.err
	}
	var nd net.Dialer
	if d.host == "" {
		c, err := nd.DialContext(ctx, d.network, d.addr)
		if err != nil {
			return nil, fmt.Errorf("dial responder: %w", err)
		}
		return c, nil
	}
	ips, err := d.resolve(ctx)
	if err != nil {
		return nil, fmt.Errorf("dial responder: %w", err)
	}
	var errs []error
	for _, ip := range ips {
		c, err := nd.DialContext(ctx, "tcp", net.JoinHostPort(ip, d.port))
		if err == nil {
			return c, nil
		}
		errs = append(errs, err)
		if ctx.Err() != nil {
			break
		}
	}
	// The name may point elsewhere now; the next call looks it up again.
	d.forget()
	return nil, fmt.Errorf("dial responder: %w", errors.Join(errs...))
}

// resolve returns the cached addresses. Past the TTL it starts one
// background refresh and keeps answering with the old addresses: a slow
// lookup then delays nobody, where a lookup under the lock would stall
// every call behind it. Only a dialer without addresses looks up inline.
func (d *Dialer) resolve(ctx context.Context) ([]string, error) {
	d.mu.Lock()
	if d.ips != nil {
		if d.now().Sub(d.resolved) >= resolveTTL && !d.refreshing {
			d.refreshing = true
			go d.refresh()
		}
		ips := d.ips
		d.mu.Unlock()
		return ips, nil
	}
	d.mu.Unlock()

	ips, err := d.lookup(ctx, d.host)
	if err == nil && len(ips) == 0 {
		err = fmt.Errorf("lookup %s: no addresses", d.host)
	}
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.ips, d.resolved = ips, d.now()
	d.mu.Unlock()
	return ips, nil
}

func (d *Dialer) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()
	ips, err := d.lookup(ctx, d.host)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshing = false
	// A failed refresh keeps the old addresses until the next TTL; a dial
	// that fails on them clears them.
	d.resolved = d.now()
	if err == nil && len(ips) > 0 && d.ips != nil {
		d.ips = ips
	}
}

func (d *Dialer) forget() {
	d.mu.Lock()
	d.ips = nil
	d.mu.Unlock()
}
