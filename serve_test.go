package fcgi

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeResponder speaks just enough FastCGI to drive the handler. reply runs
// once per connection after the request was read and returns the stdout
// bytes to send; a nil result closes the connection without END_REQUEST.
type fakeResponder struct {
	addr  string
	calls chan struct{}
}

func record(typ byte, content []byte) []byte {
	pad := (8 - len(content)%8) % 8
	b := []byte{1, typ, 0, 1, byte(len(content) >> 8), byte(len(content)), byte(pad), 0}
	b = append(b, content...)
	return append(b, make([]byte, pad)...)
}

func newFake(t testing.TB, reply func(conn net.Conn) []byte) *fakeResponder {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &fakeResponder{addr: "tcp:" + ln.Addr().String(), calls: make(chan struct{}, 64)}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				drainRequest(c)
				f.calls <- struct{}{}
				out := reply(c)
				if out == nil {
					return
				}
				_, _ = c.Write(record(6, out))
				_, _ = c.Write(record(6, nil))
				end := make([]byte, 8)
				binary.BigEndian.PutUint32(end, 0)
				_, _ = c.Write(record(3, end))
			}()
		}
	}()
	return f
}

// drainRequest reads records until the empty STDIN record.
func drainRequest(c net.Conn) {
	for {
		var h [8]byte
		if _, err := io.ReadFull(c, h[:]); err != nil {
			return
		}
		n := int(binary.BigEndian.Uint16(h[4:])) + int(h[6])
		if _, err := io.CopyN(io.Discard, c, int64(n)); err != nil {
			return
		}
		if h[1] == 5 && binary.BigEndian.Uint16(h[4:]) == 0 {
			return
		}
	}
}

func serveOnce(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

func TestHandlerWritesResponderReply(t *testing.T) {
	f := newFake(t, func(net.Conn) []byte {
		return []byte("Status: 201 Created\r\nX-A: 1\r\nX-A: 2\r\nX-Powered-By: php\r\n\r\nhello")
	})
	gw := New(f.addr, WithLogger(discardLogger()))
	rec := serveOnce(gw.Script("a.php", WithDropResponseHeaders("X-Powered-By")), "GET", "/a.php", "")
	if rec.Code != 201 || rec.Body.String() != "hello" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header()["X-A"]; len(got) != 2 {
		t.Errorf("X-A = %q, want two values", got)
	}
	if rec.Header().Get("X-Powered-By") != "" {
		t.Error("dropped header reached the client")
	}
	if rec.Header().Get("Content-Length") != "5" {
		t.Errorf("Content-Length = %q", rec.Header().Get("Content-Length"))
	}
}

func TestHandlerFailureStatuses(t *testing.T) {
	tests := []struct {
		name  string
		reply func(net.Conn) []byte
		opts  []ScriptOption
		want  int
	}{
		{"no end request", func(net.Conn) []byte { return nil }, nil, 502},
		{"no header block", func(net.Conn) []byte { return []byte("just text") }, nil, 502},
		{"content-length mismatch", func(net.Conn) []byte { return []byte("Content-Length: 99\r\n\r\nshort") }, nil, 502},
		{"deadline", func(c net.Conn) []byte { time.Sleep(time.Second); return nil },
			[]ScriptOption{WithTimeout(50 * time.Millisecond)}, 504},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t, tt.reply)
			gw := New(f.addr, WithLogger(discardLogger()))
			rec := serveOnce(gw.Script("a.php", tt.opts...), "GET", "/a.php", "")
			if rec.Code != tt.want {
				t.Fatalf("status %d, want %d", rec.Code, tt.want)
			}
			if strings.Contains(rec.Body.String(), "short") || strings.Contains(rec.Body.String(), "just text") {
				t.Error("partial responder output reached the client")
			}
		})
	}
}

func TestHandlerUnreachableResponder(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	_ = ln.Close()
	gw := New("tcp:"+addr, WithLogger(discardLogger()))
	if rec := serveOnce(gw.Script("a.php"), "GET", "/a.php", ""); rec.Code != 502 {
		t.Fatalf("status %d, want 502", rec.Code)
	}
}

func TestHandlerBodyOverLimitNeverReachesResponder(t *testing.T) {
	f := newFake(t, func(net.Conn) []byte { return []byte("\r\nok") })
	gw := New(f.addr, WithLogger(discardLogger()), WithMaxBody(4))
	rec := serveOnce(gw.Script("a.php"), "POST", "/a.php", "12345")
	if rec.Code != 413 {
		t.Fatalf("status %d, want 413", rec.Code)
	}
	select {
	case <-f.calls:
		t.Fatal("responder was called")
	default:
	}
}

func TestSemaphoreBoundsConcurrentCalls(t *testing.T) {
	release := make(chan struct{})
	f := newFake(t, func(net.Conn) []byte { <-release; return []byte("\r\nok") })
	gw := New(f.addr, WithLogger(discardLogger()), WithMaxConns(1))
	// The holder's deadline is long, so it cannot free the slot at the moment
	// the waiter's deadline ends.
	holder := gw.Script("a.php", WithTimeout(5*time.Second))
	h := gw.Script("a.php", WithTimeout(200*time.Millisecond))

	var wg sync.WaitGroup
	wg.Go(func() { serveOnce(holder, "GET", "/a.php", "") })
	<-f.calls // the first call holds the only slot

	start := time.Now()
	rec := serveOnce(h, "GET", "/a.php", "")
	if rec.Code != 503 {
		t.Fatalf("status %d, want 503", rec.Code)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Errorf("answered 503 after %v, before the deadline", time.Since(start))
	}
	close(release)
	wg.Wait()
}

func TestShutdownRefusesNewAndWaitsForRunning(t *testing.T) {
	release := make(chan struct{})
	f := newFake(t, func(net.Conn) []byte { <-release; return []byte("\r\nok") })
	gw := New(f.addr, WithLogger(discardLogger()))
	h := gw.Script("a.php")

	var wg sync.WaitGroup
	var first *httptest.ResponseRecorder
	wg.Go(func() { first = serveOnce(h, "GET", "/a.php", "") })
	<-f.calls

	done := make(chan error, 1)
	go func() { done <- gw.Shutdown(context.Background()) }()
	time.Sleep(50 * time.Millisecond)

	if rec := serveOnce(h, "GET", "/a.php", ""); rec.Code != 503 {
		t.Errorf("new call status %d, want 503", rec.Code)
	}
	select {
	case <-done:
		t.Fatal("Shutdown returned while a call was running")
	default:
	}
	close(release)
	wg.Wait()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if first.Code != 200 {
		t.Errorf("running call status %d, want 200", first.Code)
	}
}

func TestPingDialsWithoutRunningAScript(t *testing.T) {
	f := newFake(t, func(net.Conn) []byte { return nil })
	if err := New(f.addr).Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := New("tcp:127.0.0.1:1").Ping(context.Background()); err == nil {
		t.Fatal("Ping of a closed port succeeded")
	}
}
