package fcgi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// observations collects every CallInfo an observer receives.
type observations struct {
	mu    sync.Mutex
	infos []CallInfo
}

func (o *observations) observe(_ *http.Request, info CallInfo) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.infos = append(o.infos, info)
}

// only fails the test unless exactly one call was observed, and returns it.
func (o *observations) only(t *testing.T) CallInfo {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.infos) != 1 {
		t.Fatalf("observer called %d times, want 1: %+v", len(o.infos), o.infos)
	}
	return o.infos[0]
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestObserverSeesResponderReply(t *testing.T) {
	f := newFake(t, func(net.Conn) []byte { return []byte("Status: 201 Created\r\n\r\nhello") })
	var obs observations
	gw := New(f.addr, WithLogger(discardLogger()), WithObserver(obs.observe))
	rec := serveOnce(gw.Script("a.php"), "POST", "/a.php", "abc")

	info := obs.only(t)
	if info.Script != "a.php" || info.Status != 201 || info.Origin != OriginResponder || info.Err != nil {
		t.Fatalf("info = %+v", info)
	}
	if info.RequestBytes != 3 || info.ResponseBytes != 5 || int64(rec.Body.Len()) != info.ResponseBytes {
		t.Errorf("RequestBytes %d, ResponseBytes %d, body %d", info.RequestBytes, info.ResponseBytes, rec.Body.Len())
	}
	if info.Duration <= 0 {
		t.Errorf("Duration = %v", info.Duration)
	}
}

// One case per row of the failure table in docs/design.md.
func TestObserverSeesGatewayStatuses(t *testing.T) {
	reply := func(b string) func(net.Conn) []byte { return func(net.Conn) []byte { return []byte(b) } }
	tests := []struct {
		name   string
		reply  func(net.Conn) []byte
		opts   []ScriptOption
		body   func() *http.Request
		closed bool
		want   int
	}{
		{name: "body over limit", reply: reply("\r\nok"), opts: []ScriptOption{WithMaxBody(4)},
			body: func() *http.Request { return httptest.NewRequest("POST", "/a.php", strings.NewReader("12345")) }, want: 413},
		{name: "unreadable body", reply: reply("\r\nok"),
			body: func() *http.Request { return httptest.NewRequest("POST", "/a.php", errReader{}) }, want: 400},
		{name: "unreachable", closed: true, want: 502},
		{name: "deadline", reply: func(net.Conn) []byte { time.Sleep(time.Second); return nil },
			opts: []ScriptOption{WithTimeout(50 * time.Millisecond)}, want: 504},
		{name: "no end request", reply: func(net.Conn) []byte { return nil }, want: 502},
		{name: "no header block", reply: reply("just text"), want: 502},
		{name: "content-length mismatch", reply: reply("Content-Length: 99\r\n\r\nshort"), want: 502},
		{name: "response over limit", reply: reply("\r\n0123456789"), opts: []ScriptOption{WithMaxResponse(4)}, want: 502},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := closedAddr(t)
			if !tt.closed {
				addr = newFake(t, tt.reply).addr
			}
			var obs observations
			gw := New(addr, WithLogger(discardLogger()), WithObserver(obs.observe))
			req := httptest.NewRequest("GET", "/a.php", nil)
			if tt.body != nil {
				req = tt.body()
			}
			rec := httptest.NewRecorder()
			gw.Script("a.php", tt.opts...).ServeHTTP(rec, req)

			info := obs.only(t)
			if info.Status != tt.want || rec.Code != tt.want || info.Origin != OriginGateway || info.Err == nil {
				t.Fatalf("got %d, info %+v, want status %d from the gateway with an error", rec.Code, info, tt.want)
			}
			if info.ResponseBytes != int64(rec.Body.Len()) {
				t.Errorf("ResponseBytes %d, body %d", info.ResponseBytes, rec.Body.Len())
			}
		})
	}
}

func TestObserverSeesBodyReadBytes(t *testing.T) {
	f := newFake(t, func(net.Conn) []byte { return []byte("\r\nok") })
	var obs observations
	gw := New(f.addr, WithLogger(discardLogger()), WithMaxBody(4), WithObserver(obs.observe))
	serveOnce(gw.Script("a.php"), "POST", "/a.php", "12345")
	if info := obs.only(t); info.RequestBytes == 0 || info.SlotWait != 0 || info.Duration != 0 {
		t.Fatalf("info = %+v, want body bytes and no slot wait", info)
	}
}

func TestObserverSeesNoSlot(t *testing.T) {
	release := make(chan struct{})
	f := newFake(t, func(net.Conn) []byte { <-release; return []byte("\r\nok") })
	var obs observations
	gw := New(f.addr, WithLogger(discardLogger()), WithMaxConns(1))
	holder := gw.Script("a.php", WithTimeout(5*time.Second))
	waiter := gw.Script("a.php", WithTimeout(100*time.Millisecond), WithObserver(obs.observe))

	var wg sync.WaitGroup
	wg.Go(func() { serveOnce(holder, "GET", "/a.php", "") })
	<-f.calls
	serveOnce(waiter, "GET", "/a.php", "")
	close(release)
	wg.Wait()

	info := obs.only(t)
	if info.Status != 503 || info.Origin != OriginGateway || info.Err == nil || info.SlotWait < 50*time.Millisecond {
		t.Fatalf("info = %+v, want 503 from the gateway after a slot wait", info)
	}
}

func TestObserverSeesShutdown(t *testing.T) {
	f := newFake(t, func(net.Conn) []byte { return []byte("\r\nok") })
	var obs observations
	gw := New(f.addr, WithLogger(discardLogger()), WithObserver(obs.observe))
	if err := gw.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := serveOnce(gw.Script("a.php"), "GET", "/a.php", "")

	info := obs.only(t)
	if info.Status != 503 || info.Origin != OriginGateway || info.Err == nil || info.Duration != 0 {
		t.Fatalf("info = %+v, want 503 from the gateway", info)
	}
	if info.ResponseBytes != int64(rec.Body.Len()) {
		t.Errorf("ResponseBytes %d, body %d", info.ResponseBytes, rec.Body.Len())
	}
}

func TestObserverSeesClientCancelOnce(t *testing.T) {
	f := newFake(t, func(c net.Conn) []byte {
		_, _ = c.Read(make([]byte, 1)) // blocks until the gateway closes the connection
		return nil
	})
	var obs observations
	gw := New(f.addr, WithLogger(discardLogger()), WithObserver(obs.observe))
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-f.calls; cancel() }()
	rec := httptest.NewRecorder()
	gw.Script("a.php").ServeHTTP(rec, httptest.NewRequest("GET", "/a.php", nil).WithContext(ctx))

	info := obs.only(t)
	if info.Status != 0 || info.Origin != OriginClient || !errors.Is(info.Err, context.Canceled) {
		t.Fatalf("info = %+v, want status 0 from the client wrapping context.Canceled", info)
	}
}

func TestObserverSlotWaitWhenCallsOverlap(t *testing.T) {
	release := make(chan struct{})
	f := newFake(t, func(net.Conn) []byte { <-release; return []byte("\r\nok") })
	var obs observations
	gw := New(f.addr, WithLogger(discardLogger()), WithMaxConns(1))
	holder := gw.Script("a.php")
	waiter := gw.Script("a.php", WithObserver(obs.observe))

	var wg sync.WaitGroup
	wg.Go(func() { serveOnce(holder, "GET", "/a.php", "") })
	<-f.calls
	time.AfterFunc(50*time.Millisecond, func() { close(release) })
	serveOnce(waiter, "GET", "/a.php", "")
	wg.Wait()

	info := obs.only(t)
	if info.Status != 200 || info.SlotWait <= 0 || info.Duration < info.SlotWait {
		t.Fatalf("info = %+v, want 200 after a slot wait", info)
	}
}

func TestObserverRunsWithoutASlot(t *testing.T) {
	f := newFake(t, func(net.Conn) []byte { return []byte("\r\nok") })
	held := -1
	var gw *Gateway
	gw = New(f.addr, WithLogger(discardLogger()), WithMaxConns(1),
		WithObserver(func(*http.Request, CallInfo) { held = len(gw.slots) }))
	serveOnce(gw.Script("a.php"), "GET", "/a.php", "")
	if held != 0 {
		t.Fatalf("observer ran with %d slots held, want 0", held)
	}
}

func TestScriptObserverReplacesGateways(t *testing.T) {
	f := newFake(t, func(net.Conn) []byte { return []byte("\r\nok") })
	var gwObs, scriptObs observations
	gw := New(f.addr, WithLogger(discardLogger()), WithObserver(gwObs.observe))
	serveOnce(gw.Script("a.php", WithObserver(scriptObs.observe)), "GET", "/a.php", "")
	serveOnce(gw.Script("b.php", WithObserver(nil)), "GET", "/b.php", "")
	scriptObs.only(t)
	if len(gwObs.infos) != 0 {
		t.Fatalf("gateway observer called %d times, want 0", len(gwObs.infos))
	}
}

func TestObserverPanicIsRecoveredAndLogged(t *testing.T) {
	f := newFake(t, func(net.Conn) []byte { return []byte("\r\nok") })
	var logs bytes.Buffer
	gw := New(f.addr, WithLogger(slog.New(slog.NewTextHandler(&logs, nil))),
		WithObserver(func(*http.Request, CallInfo) { panic("observer bug") }))
	rec := serveOnce(gw.Script("a.php"), "GET", "/a.php", "")
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), "fcgi observer panicked") ||
		!strings.Contains(logs.String(), "observer bug") {
		t.Fatalf("log = %q", logs.String())
	}
}

func closedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return "tcp:" + addr
}
