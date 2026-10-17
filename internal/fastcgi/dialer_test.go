package fastcgi

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// countingLookup resolves every host to the listener's IP and counts calls.
func countingLookup(ip string, n *atomic.Int32) func(context.Context, string) ([]string, error) {
	return func(context.Context, string) ([]string, error) {
		n.Add(1)
		return []string{ip}, nil
	}
}

func listen(t *testing.T) (ip, port string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	ip, port, _ = net.SplitHostPort(ln.Addr().String())
	return ip, port
}

// nginx resolves an upstream name once; a gateway that resolves per call
// pays a DNS round trip on every request.
func TestDialerResolvesOncePerTTL(t *testing.T) {
	ip, port := listen(t)
	var lookups atomic.Int32
	d := NewDialer("tcp:responder.test:" + port)
	d.lookup = countingLookup(ip, &lookups)
	for range 5 {
		c, err := d.Dial(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		_ = c.Close()
	}
	if got := lookups.Load(); got != 1 {
		t.Fatalf("lookups = %d, want 1", got)
	}

	d.now = func() time.Time { return time.Now().Add(resolveTTL + time.Second) }
	c, err := d.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	// The refresh runs in the background.
	for deadline := time.Now().Add(time.Second); lookups.Load() < 2 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if got := lookups.Load(); got != 2 {
		t.Fatalf("lookups after the TTL = %d, want 2", got)
	}
}

// A failed dial may mean the name points elsewhere now; the next call
// resolves again instead of waiting out the TTL.
func TestDialerResolvesAgainAfterFailedDial(t *testing.T) {
	ip, port := listen(t)
	var lookups atomic.Int32
	d := NewDialer("tcp:responder.test:" + port)
	stale := true
	d.lookup = func(ctx context.Context, host string) ([]string, error) {
		lookups.Add(1)
		if stale {
			stale = false
			return []string{"::1"}, nil // the listener is on 127.0.0.1 only, so this is refused
		}
		return []string{ip}, nil
	}
	if c, err := d.Dial(context.Background()); err == nil {
		_ = c.Close()
		t.Skip("::1 accepted the connection on this host")
	}
	c, err := d.Dial(context.Background())
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	_ = c.Close()
	if got := lookups.Load(); got != 2 {
		t.Fatalf("lookups = %d, want 2", got)
	}
}

func TestDialerSkipsLookupForIPAndUnix(t *testing.T) {
	ip, port := listen(t)
	var lookups atomic.Int32
	d := NewDialer("tcp:" + net.JoinHostPort(ip, port))
	d.lookup = countingLookup(ip, &lookups)
	c, err := d.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if lookups.Load() != 0 {
		t.Fatal("an IP literal was looked up")
	}
}

// A slow lookup must not stall calls while cached addresses exist: the
// refresh runs in the background and calls keep the old addresses.
func TestDialerRefreshDoesNotBlockCalls(t *testing.T) {
	ip, port := listen(t)
	release := make(chan struct{})
	defer close(release)
	var lookups atomic.Int32
	d := NewDialer("tcp:responder.test:" + port)
	d.lookup = func(ctx context.Context, host string) ([]string, error) {
		if lookups.Add(1) > 1 {
			<-release // the refresh hangs, as a lookup that times out would
		}
		return []string{ip}, nil
	}
	c, err := d.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()

	d.now = func() time.Time { return time.Now().Add(resolveTTL + time.Second) }
	done := make(chan error, 1)
	go func() {
		c, err := d.Dial(context.Background())
		if err == nil {
			_ = c.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("a call waited for the background refresh")
	}
}
