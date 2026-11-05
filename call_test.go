package fcgi

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// seen is what the responder received for one call.
type seen struct {
	params map[string]string
	stdin  []byte
}

// answer is what a capturing responder sends back.
type answer struct {
	stdout string
	stderr string
}

// capture is a fake responder that decodes the request, unlike newFake.
type capture struct {
	addr string
	mu   sync.Mutex
	reqs []seen
}

func (c *capture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.reqs)
}

func (c *capture) last(t *testing.T) seen {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.reqs) == 0 {
		t.Fatal("responder saw no request")
	}
	return c.reqs[len(c.reqs)-1]
}

func replyWith(stdout string) func(seen) answer {
	return func(seen) answer { return answer{stdout: stdout} }
}

func newCapture(t testing.TB, handle func(seen) answer) *capture {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	c := &capture{addr: "tcp:" + ln.Addr().String()}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				s, ok := readRequest(conn)
				if !ok {
					return
				}
				c.mu.Lock()
				c.reqs = append(c.reqs, s)
				c.mu.Unlock()
				writeAnswer(conn, handle(s))
			}()
		}
	}()
	return c
}

func readRequest(conn net.Conn) (seen, bool) {
	var params, stdin []byte
	for {
		var h [8]byte
		if _, err := io.ReadFull(conn, h[:]); err != nil {
			return seen{}, false
		}
		n := int(binary.BigEndian.Uint16(h[4:]))
		content := make([]byte, n+int(h[6]))
		if _, err := io.ReadFull(conn, content); err != nil {
			return seen{}, false
		}
		content = content[:n]
		switch {
		case h[1] == 4:
			params = append(params, content...)
		case h[1] == 5 && n == 0:
			return seen{params: decodeParams(params), stdin: stdin}, true
		case h[1] == 5:
			stdin = append(stdin, content...)
		}
	}
}

func decodeParams(b []byte) map[string]string {
	next := func() int {
		if b[0]>>7 == 0 {
			n := int(b[0])
			b = b[1:]
			return n
		}
		n := int(binary.BigEndian.Uint32(b) & 0x7fffffff)
		b = b[4:]
		return n
	}
	m := map[string]string{}
	for len(b) > 0 {
		nl, vl := next(), next()
		m[string(b[:nl])] = string(b[nl : nl+vl])
		b = b[nl+vl:]
	}
	return m
}

func writeAnswer(conn net.Conn, a answer) {
	for typ, s := range map[byte]string{7: a.stderr, 6: a.stdout} {
		for b := []byte(s); len(b) > 0; {
			n := min(len(b), 32<<10)
			_, _ = conn.Write(record(typ, b[:n]))
			b = b[n:]
		}
	}
	_, _ = conn.Write(record(6, nil))
	_, _ = conn.Write(record(3, make([]byte, 8)))
}

func mustCall(t *testing.T, c *Caller, req *Request) *Response {
	t.Helper()
	resp, err := c.Call(context.Background(), req)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCallDefaults(t *testing.T) {
	cp := newCapture(t, replyWith("\r\nok"))
	c := New(cp.addr, WithLogger(discardLogger())).Caller("a.php")
	mustCall(t, c, &Request{Target: "/a.php/x/y?q=1%2F"})

	got := cp.last(t).params
	want := map[string]string{
		"REQUEST_METHOD": "GET", "HTTP_HOST": "localhost", "SERVER_NAME": "localhost",
		"REMOTE_ADDR": "127.0.0.1", "REMOTE_PORT": "0", "REQUEST_URI": "/a.php/x/y?q=1%2F",
		"QUERY_STRING": "q=1%2F", "PATH_INFO": "/x/y", "SCRIPT_NAME": "/a.php", "CONTENT_LENGTH": "",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["HTTPS"]; ok {
		t.Error("HTTPS set without TLS")
	}
}

func TestCallRequestFieldsReachTheResponder(t *testing.T) {
	cp := newCapture(t, replyWith("\r\nok"))
	c := New(cp.addr, WithLogger(discardLogger())).Caller("a.php")
	mustCall(t, c, &Request{
		Method: "DELETE", Target: "/a.php", Host: "example.org:8443", RemoteAddr: "10.1.2.3:5555",
		TLS: true, Header: http.Header{"X-Foo": {"bar"}},
	})

	got := cp.last(t).params
	want := map[string]string{
		"REQUEST_METHOD": "DELETE", "HTTP_HOST": "example.org:8443", "SERVER_NAME": "example.org",
		"REMOTE_ADDR": "10.1.2.3", "REMOTE_PORT": "5555", "HTTPS": "on", "HTTP_X_FOO": "bar",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

type ctxKey struct{}

func TestCallHooksReceiveARequestCarryingCtx(t *testing.T) {
	cp := newCapture(t, replyWith("\r\nok"))
	var fromParams, fromQuery, fromPathInfo *http.Request
	c := New(cp.addr, WithLogger(discardLogger())).Caller("a.php",
		WithParams(func(r *http.Request) map[string]string { fromParams = r; return nil }),
		WithQuery(func(r *http.Request) string { fromQuery = r; return "k=v" }),
		WithPathInfo(func(r *http.Request) string { fromPathInfo = r; return "/derived" }),
	)
	ctx := context.WithValue(context.Background(), ctxKey{}, "trace-1")
	resp, err := c.Call(ctx, &Request{Method: "PUT", Target: "/a.php/x?y=1"})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	for name, r := range map[string]*http.Request{"WithParams": fromParams, "WithQuery": fromQuery, "WithPathInfo": fromPathInfo} {
		if r == nil || r.Context().Value(ctxKey{}) != "trace-1" || r.Method != "PUT" || r.URL.Path != "/a.php/x" {
			t.Errorf("%s got %+v", name, r)
		}
	}
	got := cp.last(t).params
	if got["QUERY_STRING"] != "k=v" || got["PATH_INFO"] != "/derived" {
		t.Errorf("QUERY_STRING %q, PATH_INFO %q", got["QUERY_STRING"], got["PATH_INFO"])
	}
}

func TestCallPathInfoOverridesDerivation(t *testing.T) {
	cp := newCapture(t, replyWith("\r\nok"))
	c := New(cp.addr, WithLogger(discardLogger())).Caller("a.php", WithPathInfo(func(*http.Request) string { return "/derived" }))
	override := "/explicit"
	mustCall(t, c, &Request{Target: "/a.php/x", PathInfo: &override})
	if got := cp.last(t).params["PATH_INFO"]; got != "/explicit" {
		t.Fatalf("PATH_INFO = %q", got)
	}
}

func TestCallParamsAreWrittenLast(t *testing.T) {
	cp := newCapture(t, replyWith("\r\nok"))
	gw := New(cp.addr, WithLogger(discardLogger()), WithParam("G", "gateway"))
	c := gw.Caller("a.php",
		WithParam("S", "script"),
		WithParams(func(*http.Request) map[string]string {
			return map[string]string{"P": "func", "Q": "func", "HTTP_X_AUTH": "func"}
		}),
	)
	mustCall(t, c, &Request{
		Target: "/a.php",
		Header: http.Header{"X-Auth": {"forged"}},
		Params: map[string]string{"P": "request", "Q": "", "G": "request", "HTTP_X_AUTH": "trusted"},
	})

	got := cp.last(t).params
	if got["P"] != "request" || got["G"] != "request" || got["HTTP_X_AUTH"] != "trusted" || got["S"] != "script" {
		t.Errorf("params = %v", got)
	}
	if _, ok := got["Q"]; ok {
		t.Error("an empty Params value must remove the parameter")
	}
}

func TestCallHeaderCannotForgeCallerParameters(t *testing.T) {
	cp := newCapture(t, replyWith("\r\nok"))
	gw := New(cp.addr, WithLogger(discardLogger()), WithProtectedHeaders("X-Auth-*"))
	mustCall(t, gw.Caller("a.php"), &Request{Target: "/a.php", Header: http.Header{
		"X-Auth-Subject": {"admin"}, "X_Evil": {"1"}, "X.Evil": {"1"}, "X-Ok": {"1"},
	}})

	got := cp.last(t).params
	for _, k := range []string{"HTTP_X_AUTH_SUBJECT", "HTTP_X_EVIL"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s reached the responder", k)
		}
	}
	if got["HTTP_X_OK"] != "1" {
		t.Error("an ordinary header was dropped")
	}
}

func TestCallRequestBody(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		length     int64
		noBody     bool
		wantLength string
	}{
		{name: "no body", noBody: true, wantLength: ""},
		{name: "unknown length", body: "hello", length: -1, wantLength: "5"},
		{name: "zero with a body means unknown", body: "hello", length: 0, wantLength: "5"},
		{name: "declared length", body: "hello", length: 5, wantLength: "5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cp := newCapture(t, replyWith("\r\nok"))
			req := &Request{Method: "POST", Target: "/a.php", ContentLength: tt.length}
			if !tt.noBody {
				req.Body = strings.NewReader(tt.body)
			}
			mustCall(t, New(cp.addr, WithLogger(discardLogger())).Caller("a.php"), req)
			got := cp.last(t)
			if got.params["CONTENT_LENGTH"] != tt.wantLength || string(got.stdin) != tt.body {
				t.Errorf("CONTENT_LENGTH %q, stdin %q", got.params["CONTENT_LENGTH"], got.stdin)
			}
		})
	}
}

func TestCallBodyOfAnotherLengthThanDeclaredIsBad(t *testing.T) {
	cp := newCapture(t, replyWith("\r\nok"))
	c := New(cp.addr, WithLogger(discardLogger())).Caller("a.php")
	_, err := c.Call(context.Background(), &Request{Method: "POST", Target: "/a.php", Body: strings.NewReader("12345"), ContentLength: 10})
	if e, ok := errors.AsType[*Error](err); !ok || e.Status != 400 || !errors.Is(err, ErrBadBody) {
		t.Fatalf("err = %v, want 400 ErrBadBody", err)
	}
	if cp.count() != 0 {
		t.Error("a malformed body reached the responder")
	}
}

func TestCallLargeBodiesStreamThroughTheSpool(t *testing.T) {
	in := bytes.Repeat([]byte("0123456789abcdef"), 16<<10) // 256 KiB, past the 1 KiB memory limit
	cp := newCapture(t, func(s seen) answer { return answer{stdout: "\r\n" + string(s.stdin)} })
	dir := t.TempDir()
	gw := New(cp.addr, WithLogger(discardLogger()), WithSpool(dir, 1024))
	resp := mustCall(t, gw.Caller("a.php"), &Request{Method: "POST", Target: "/a.php", Body: bytes.NewReader(in)})

	if !bytes.Equal(cp.last(t).stdin, in) {
		t.Error("responder received a different request body")
	}
	if resp.ContentLength != int64(len(in)) {
		t.Errorf("ContentLength = %d, want %d", resp.ContentLength, len(in))
	}
	if got := readAll(t, resp.Body); got != string(in) {
		t.Errorf("response body differs: %d bytes, want %d", len(got), len(in))
	}
}

func openFDs(t *testing.T) int {
	t.Helper()
	// A call's connections close in the background, so wait for a stable count.
	prev := -1
	for range 50 {
		entries, err := os.ReadDir("/dev/fd")
		if err != nil {
			t.Skip("no /dev/fd on this platform")
		}
		if len(entries) == prev {
			return prev
		}
		prev = len(entries)
		time.Sleep(20 * time.Millisecond)
	}
	return prev
}

func TestCallBodyCloseReleasesTheSpool(t *testing.T) {
	big := strings.Repeat("x", 1<<20)
	cp := newCapture(t, replyWith("\r\n"+big))
	dir := t.TempDir()
	gw := New(cp.addr, WithLogger(discardLogger()), WithSpool(dir, 1024))
	resp, err := gw.Caller("a.php").Call(context.Background(), &Request{Target: "/a.php"})
	if err != nil {
		t.Fatal(err)
	}
	open := openFDs(t)

	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("spool dir holds %d files after Close", len(entries))
	}
	if _, err := resp.Body.Read(make([]byte, 1)); err == nil {
		t.Error("Read after Close succeeded")
	}
	if after := openFDs(t); after != open-1 {
		t.Errorf("open files: %d before Close, %d after, want one fewer", open, after)
	}
}

func TestCallReturnsBeforeTheCallerReadsTheBody(t *testing.T) {
	cp := newCapture(t, replyWith("\r\nok"))
	gw := New(cp.addr, WithLogger(discardLogger()), WithMaxConns(1))
	c := gw.Caller("a.php", WithTimeout(2*time.Second))
	// The first body stays unread and open; a held slot would starve the second call.
	mustCall(t, c, &Request{Target: "/a.php"})
	mustCall(t, c, &Request{Target: "/a.php"})
}

func TestCallResponderStatusIsAResponseNotAnError(t *testing.T) {
	cp := newCapture(t, replyWith("Status: 500 Internal Server Error\r\nX-A: 1\r\nX-A: 2\r\nX-Powered-By: php\r\n\r\nboom"))
	c := New(cp.addr, WithLogger(discardLogger())).Caller("a.php", WithDropResponseHeaders("X-Powered-By"))
	resp := mustCall(t, c, &Request{Target: "/a.php"})

	if resp.Status != 500 || resp.ContentLength != 4 || readAll(t, resp.Body) != "boom" {
		t.Fatalf("got %d, length %d", resp.Status, resp.ContentLength)
	}
	if len(resp.Header["X-A"]) != 2 || resp.Header.Get("X-Powered-By") != "" || resp.Header.Get("Content-Length") != "4" {
		t.Errorf("header = %v", resp.Header)
	}
}

func TestCallHeadAndBodylessStatusesHaveAnEmptyBody(t *testing.T) {
	for _, tt := range []struct{ method, reply string }{
		{"HEAD", "\r\nignored"},
		{"GET", "Status: 204 No Content\r\n\r\n"},
	} {
		cp := newCapture(t, replyWith(tt.reply))
		resp := mustCall(t, New(cp.addr, WithLogger(discardLogger())).Caller("a.php"), &Request{Method: tt.method, Target: "/a.php"})
		if got := readAll(t, resp.Body); got != "" || resp.ContentLength != 0 {
			t.Errorf("%s: body %q, length %d", tt.method, got, resp.ContentLength)
		}
	}
}

func TestCallLogsResponderStderrCapped(t *testing.T) {
	cp := newCapture(t, func(seen) answer {
		return answer{stdout: "\r\nok", stderr: strings.Repeat("e", 20<<10)}
	})
	var logs bytes.Buffer
	gw := New(cp.addr, WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	mustCall(t, gw.Caller("a.php"), &Request{Target: "/a.php"})
	if !strings.Contains(logs.String(), "responder stderr") {
		t.Fatalf("log = %q", logs.String())
	}
	if logs.Len() > 9<<10 {
		t.Errorf("log line is %d bytes, want at most 8 KiB of stderr", logs.Len())
	}
}

func TestCallInvalidTargetIsNotALibraryFailure(t *testing.T) {
	c := New("tcp:127.0.0.1:1", WithLogger(discardLogger())).Caller("a.php")
	_, err := c.Call(context.Background(), &Request{Target: "no-slash"})
	if err == nil {
		t.Fatal("want an error")
	}
	if _, ok := errors.AsType[*Error](err); ok {
		t.Error("a malformed target is the caller's bug, not an *Error")
	}
}

func TestCallCancelledContextIsNotAnError(t *testing.T) {
	cp := newCapture(t, func(seen) answer { time.Sleep(time.Second); return answer{} })
	var obs observations
	c := New(cp.addr, WithLogger(discardLogger()), WithObserver(obs.observe)).Caller("a.php")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for cp.count() == 0 {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	_, err := c.Call(ctx, &Request{Target: "/a.php"})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if _, ok := errors.AsType[*Error](err); ok {
		t.Error("a cancelled call decided no status, so it is no *Error")
	}
	if info := obs.only(t); info.Origin != OriginClient || info.Status != 0 || !errors.Is(info.Err, context.Canceled) {
		t.Errorf("info = %+v", info)
	}
}

// One row per row of the failure table. Each runs through Call and through
// the Script handler, so the typed error and the observer's CallInfo.Err
// carry the same sentinel and status.
func TestFailureTableMapsToSentinelAndStatus(t *testing.T) {
	reply := func(b string) func(net.Conn) []byte { return func(net.Conn) []byte { return []byte(b) } }
	type setup struct {
		addr   string
		opts   []ScriptOption
		gwOpts []GatewayOption
		pre    func(*Gateway)
		body   io.Reader
	}
	fake := func(t *testing.T, f func(net.Conn) []byte, opts ...ScriptOption) setup {
		return setup{addr: newFake(t, f).addr, opts: opts}
	}
	tests := []struct {
		name   string
		want   error
		status int
		setup  func(t *testing.T) setup
	}{
		{"body over limit", ErrBodyTooLarge, 413, func(t *testing.T) setup {
			s := fake(t, reply("\r\nok"), WithMaxBody(4))
			s.body = strings.NewReader("12345")
			return s
		}},
		{"unreadable body", ErrBadBody, 400, func(t *testing.T) setup {
			s := fake(t, reply("\r\nok"))
			s.body = errReader{}
			return s
		}},
		{"unreachable", ErrUnreachable, 502, func(t *testing.T) setup { return setup{addr: closedAddr(t)} }},
		{"no slot", ErrNoSlot, 503, func(t *testing.T) setup {
			release := make(chan struct{})
			f := newFake(t, func(net.Conn) []byte { <-release; return []byte("\r\nok") })
			var wg sync.WaitGroup
			t.Cleanup(func() { close(release); wg.Wait() })
			return setup{addr: f.addr, gwOpts: []GatewayOption{WithMaxConns(1)}, opts: []ScriptOption{WithTimeout(100 * time.Millisecond)}, pre: func(gw *Gateway) {
				holder := gw.Script("a.php", WithTimeout(5*time.Second))
				wg.Go(func() { serveOnce(holder, "GET", "/a.php", "") })
				<-f.calls
			}}
		}},
		{"deadline", ErrTimeout, 504, func(t *testing.T) setup {
			return fake(t, func(net.Conn) []byte { time.Sleep(time.Second); return nil }, WithTimeout(50*time.Millisecond))
		}},
		{"no end request", ErrBadReply, 502, func(t *testing.T) setup { return fake(t, func(net.Conn) []byte { return nil }) }},
		{"no header block", ErrBadReply, 502, func(t *testing.T) setup { return fake(t, reply("just text")) }},
		{"content-length mismatch", ErrBadReply, 502, func(t *testing.T) setup {
			return fake(t, reply("Content-Length: 99\r\n\r\nshort"))
		}},
		{"response over limit", ErrResponseTooLarge, 502, func(t *testing.T) setup {
			return fake(t, reply("\r\n0123456789"), WithMaxResponse(4))
		}},
		{"shutting down", ErrShuttingDown, 503, func(t *testing.T) setup {
			s := fake(t, reply("\r\nok"))
			s.pre = func(gw *Gateway) {
				if err := gw.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			return s
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/call", func(t *testing.T) {
			s := tt.setup(t)
			var obs observations
			gw := New(s.addr, append(s.gwOpts, WithLogger(discardLogger()), WithObserver(obs.observe))...)
			if s.pre != nil {
				s.pre(gw)
			}
			resp, err := gw.Caller("a.php", s.opts...).Call(context.Background(), &Request{Method: "POST", Target: "/a.php", Body: s.body})
			if resp != nil {
				t.Fatal("a failure returned a response")
			}
			e, ok := errors.AsType[*Error](err)
			if !ok || e.Status != tt.status || !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want *Error %d wrapping %v", err, tt.status, tt.want)
			}
			if tt.want == ErrTimeout && !errors.Is(err, context.DeadlineExceeded) {
				t.Error("ErrTimeout must also satisfy context.DeadlineExceeded")
			}
			info := obs.only(t)
			if info.Origin != OriginGateway || info.Status != tt.status || !errors.Is(info.Err, tt.want) {
				t.Errorf("info = %+v", info)
			}
		})
		t.Run(tt.name+"/script", func(t *testing.T) {
			s := tt.setup(t)
			var obs observations
			gw := New(s.addr, append(s.gwOpts, WithLogger(discardLogger()), WithObserver(obs.observe))...)
			if s.pre != nil {
				s.pre(gw)
			}
			req := httptest.NewRequest("POST", "/a.php", s.body)
			rec := httptest.NewRecorder()
			gw.Script("a.php", s.opts...).ServeHTTP(rec, req)

			info := obs.only(t)
			if rec.Code != tt.status || info.Status != tt.status || info.Origin != OriginGateway || !errors.Is(info.Err, tt.want) {
				t.Fatalf("code %d, info %+v, want %d wrapping %v", rec.Code, info, tt.status, tt.want)
			}
			if tt.want == ErrTimeout && !errors.Is(info.Err, context.DeadlineExceeded) {
				t.Error("ErrTimeout must also satisfy context.DeadlineExceeded")
			}
		})
	}
}

func TestCallAndScriptShareSlots(t *testing.T) {
	release := make(chan struct{})
	f := newFake(t, func(net.Conn) []byte { <-release; return []byte("\r\nok") })
	gw := New(f.addr, WithLogger(discardLogger()), WithMaxConns(1))
	c := gw.Caller("a.php", WithTimeout(100*time.Millisecond))

	var wg sync.WaitGroup
	wg.Go(func() { serveOnce(gw.Script("a.php", WithTimeout(5*time.Second)), "GET", "/a.php", "") })
	<-f.calls
	_, err := c.Call(context.Background(), &Request{Target: "/a.php"})
	close(release)
	wg.Wait()
	if !errors.Is(err, ErrNoSlot) {
		t.Fatalf("err = %v, want ErrNoSlot while a Script call holds the only slot", err)
	}
}

func TestShutdownWaitsForARunningCall(t *testing.T) {
	release := make(chan struct{})
	f := newFake(t, func(net.Conn) []byte { <-release; return []byte("\r\nok") })
	gw := New(f.addr, WithLogger(discardLogger()))

	var wg sync.WaitGroup
	var resp *Response
	var callErr error
	wg.Go(func() { resp, callErr = gw.Caller("a.php").Call(context.Background(), &Request{Target: "/a.php"}) })
	<-f.calls

	done := make(chan error, 1)
	go func() { done <- gw.Shutdown(context.Background()) }()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("Shutdown returned while a Call was running")
	default:
	}
	close(release)
	wg.Wait()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if callErr != nil || resp.Status != 200 {
		t.Fatalf("running call: %v", callErr)
	}
	_ = resp.Body.Close()
}

func TestCallObserverRunsOnceBeforeCallReturns(t *testing.T) {
	cp := newCapture(t, replyWith("Status: 201 Created\r\n\r\nhello"))
	var obs observations
	var got *http.Request
	gw := New(cp.addr, WithLogger(discardLogger()), WithObserver(func(r *http.Request, info CallInfo) {
		got = r
		obs.observe(r, info)
	}))
	ctx := context.WithValue(context.Background(), ctxKey{}, "trace-2")
	resp, err := gw.Caller("a.php").Call(ctx, &Request{Method: "POST", Target: "/a.php?x=1", Body: strings.NewReader("abc")})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	info := obs.only(t) // already recorded when Call returned
	if info.Script != "a.php" || info.Status != 201 || info.Origin != OriginResponder || info.Err != nil {
		t.Fatalf("info = %+v", info)
	}
	if info.RequestBytes != 3 || info.ResponseBytes != 5 || info.Duration <= 0 {
		t.Errorf("RequestBytes %d, ResponseBytes %d, Duration %v", info.RequestBytes, info.ResponseBytes, info.Duration)
	}
	if got.Context().Value(ctxKey{}) != "trace-2" || got.Method != "POST" || got.RequestURI != "/a.php?x=1" {
		t.Errorf("observer request = %+v", got)
	}
}

func TestCallObserverPanicIsRecovered(t *testing.T) {
	cp := newCapture(t, replyWith("\r\nok"))
	var logs bytes.Buffer
	gw := New(cp.addr, WithLogger(slog.New(slog.NewTextHandler(&logs, nil))),
		WithObserver(func(*http.Request, CallInfo) { panic("observer bug") }))
	resp := mustCall(t, gw.Caller("a.php"), &Request{Target: "/a.php"})
	if resp.Status != 200 || !strings.Contains(logs.String(), "fcgi observer panicked") {
		t.Fatalf("status %d, log %q", resp.Status, logs.String())
	}
}
