package compliance

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"
)

// ErrPending marks a run the suite cannot execute yet.
var ErrPending = errors.New("compliance: pending")

// Responder is one reference FastCGI responder, read from
// fixtures/<name>/responder.yaml.
type Responder struct {
	Name         string `yaml:"name"`
	Address      string `yaml:"address"`
	DocumentRoot string `yaml:"document_root"`
	// ScriptExtension is appended to a route script without extension, for
	// responders that run files, such as php-fpm: "crash" runs "crash.php".
	ScriptExtension string      `yaml:"script_extension"`
	Fixtures        []string    `yaml:"fixtures"`
	Deviations      []Deviation `yaml:"deviations"`
}

// Deviation is a documented difference of one responder's fixture.
type Deviation struct {
	Fixture string `yaml:"fixture"`
	Note    string `yaml:"note"`
}

// Script maps a route script to the file this responder runs.
func (r *Responder) Script(script string) string {
	if r.ScriptExtension == "" || path.Ext(script) != "" {
		return script
	}
	return script + r.ScriptExtension
}

// Implements reports whether the responder serves the fixture. A trailing
// "*" in the responder's list matches a prefix, such as "app/*".
func (r *Responder) Implements(fixture string) bool {
	for _, f := range r.Fixtures {
		if f == fixture || (strings.HasSuffix(f, "*") && strings.HasPrefix(fixture, strings.TrimSuffix(f, "*"))) {
			return true
		}
	}
	return false
}

// LoadResponders reads dir/*/responder.yaml.
func LoadResponders(dir string) ([]Responder, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*", "responder.yaml"))
	if err != nil {
		return nil, fmt.Errorf("glob responders: %w", err)
	}
	out := make([]Responder, 0, len(files))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		var r Responder
		if err := yaml.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("parse %s: %w", f, err)
		}
		out = append(out, r)
	}
	return out, nil
}

// Target builds the gateway under test for one responder. The returned
// function gives the handler for one route; it is called once per request,
// so a gateway shared across calls keeps its connection limit.
type Target func(r *Responder) func(rt Route) http.Handler

// Observation is what the client saw, plus what the dump fixture reported.
type Observation struct {
	Status      int
	Header      http.Header
	Body        []byte
	Duration    time.Duration
	Port        string
	Dumped      bool
	Env         map[string]string
	StdinLength int64
	StdinSHA256 string
}

// Do sends one request over tr to the handler and observes the result.
func Do(ctx context.Context, tr Transport, req *Request, h http.Handler) (*Observation, error) {
	if tr == H3 {
		return doH3Server(ctx, req, h)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.EnableHTTP2 = tr == H2
	srv.StartTLS()
	defer srv.Close()

	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		return nil, fmt.Errorf("server address: %w", err)
	}
	body := req.body()
	start := time.Now()
	var o *Observation
	switch tr {
	case H1:
		o, err = doH1(ctx, srv.Listener.Addr().String(), req, body)
	case H2:
		o, err = doH2(ctx, srv.Listener.Addr().String(), req, body)
	default:
		return nil, fmt.Errorf("unknown transport %q", tr)
	}
	if err != nil {
		return nil, err
	}
	o.Duration = time.Since(start)
	o.Port = port
	o.parseDump()
	return o, nil
}

func doH3Server(ctx context.Context, req *Request, h http.Handler) (*Observation, error) {
	addr, stop, err := serveH3(h)
	if err != nil {
		return nil, err
	}
	defer stop()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("server address: %w", err)
	}
	start := time.Now()
	o, err := doH3(ctx, addr, req, req.body())
	if err != nil {
		return nil, err
	}
	o.Duration = time.Since(start)
	o.Port = port
	o.parseDump()
	return o, nil
}

func (r *Request) body() []byte {
	switch {
	case r.Body != nil:
		return []byte(*r.Body)
	case r.BodyBase64 != "":
		b, _ := base64.StdEncoding.DecodeString(r.BodyBase64) // validated when the catalog loads
		return b
	case r.BodySize > 0:
		b := make([]byte, r.BodySize)
		for i := range b {
			b[i] = byte(i)
		}
		return b
	}
	return nil
}

func (r *Request) authority() string {
	if r.Authority != "" {
		return r.Authority
	}
	return "example.test"
}

func (r *Request) framing(body []byte) string {
	if r.Framing != "" {
		return r.Framing
	}
	if len(body) > 0 {
		return "length"
	}
	return "none"
}

func (r *Request) has(name string) bool {
	for _, h := range r.Headers {
		if strings.EqualFold(h[0], name) {
			return true
		}
	}
	return false
}

func dialTLS(ctx context.Context, addr, proto string) (*tls.Conn, error) {
	d := tls.Dialer{Config: &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // test server with a throwaway certificate
		NextProtos:         []string{proto},
		ServerName:         "example.test",
	}}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", proto, err)
	}
	tc, ok := c.(*tls.Conn)
	if !ok {
		return nil, errors.New("dial: not a TLS connection")
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = tc.SetDeadline(dl) // a failed deadline surfaces as a read error
	}
	return tc, nil
}

// doH1 writes the request as raw bytes so target, header order, header case
// and framing arrive exactly as the case wrote them.
func doH1(ctx context.Context, addr string, req *Request, body []byte) (*Observation, error) {
	c, err := dialTLS(ctx, addr, "http/1.1")
	if err != nil {
		return nil, err
	}
	defer c.Close()

	var b bytes.Buffer
	fmt.Fprintf(&b, "%s %s HTTP/1.1\r\n", req.Method, req.Target)
	if !req.has("Host") {
		fmt.Fprintf(&b, "Host: %s\r\n", req.authority())
	}
	for _, h := range req.Headers {
		fmt.Fprintf(&b, "%s: %s\r\n", h[0], h[1])
	}
	framing := req.framing(body)
	switch {
	case framing == "length" && !req.has("Content-Length"):
		fmt.Fprintf(&b, "Content-Length: %d\r\n", len(body))
	case framing == "chunked" && !req.has("Transfer-Encoding"):
		b.WriteString("Transfer-Encoding: chunked\r\n")
	}
	b.WriteString("\r\n")
	if framing == "chunked" {
		for _, part := range thirds(body) {
			fmt.Fprintf(&b, "%x\r\n%s\r\n", len(part), part)
		}
		b.WriteString("0\r\n\r\n")
	} else {
		b.Write(body)
	}
	if _, err := c.Write(b.Bytes()); err != nil {
		return nil, fmt.Errorf("write h1 request: %w", err)
	}

	br := bufio.NewReader(c)
	resp, err := readFinalResponse(br, req.Method)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read h1 body: %w", err)
	}
	return &Observation{Status: resp.StatusCode, Header: resp.Header, Body: rb}, nil
}

// readFinalResponse skips interim 1xx responses (RFC 9110 section 15.2),
// such as 100 Continue, and returns the final one.
func readFinalResponse(br *bufio.Reader, method string) (*http.Response, error) {
	for {
		resp, err := http.ReadResponse(br, &http.Request{Method: method})
		if err != nil {
			return nil, fmt.Errorf("read h1 response: %w", err)
		}
		if resp.StatusCode >= 200 || resp.StatusCode == http.StatusSwitchingProtocols {
			return resp, nil
		}
	}
}

// thirds splits a body into up to three non-empty parts, so chunked and
// multi-frame bodies really arrive in pieces.
func thirds(b []byte) [][]byte {
	if len(b) < 3 {
		if len(b) == 0 {
			return nil
		}
		return [][]byte{b}
	}
	n := len(b) / 3
	return [][]byte{b[:n], b[n : 2*n], b[2*n:]}
}

func (o *Observation) parseDump() {
	if o.Header.Get("X-Fixture") != "dump" || o.Status != http.StatusOK {
		return
	}
	var d struct {
		Env         map[string]string `json:"env"`
		StdinLength int64             `json:"stdin_length"`
		StdinSHA256 string            `json:"stdin_sha256"`
	}
	if json.Unmarshal(o.Body, &d) != nil {
		return
	}
	o.Dumped, o.Env, o.StdinLength, o.StdinSHA256 = true, d.Env, d.StdinLength, d.StdinSHA256
}

// RunCase runs one case over one transport against one responder.
func RunCase(ctx context.Context, c *Case, tr Transport, target func(Route) http.Handler) ([]string, error) {
	o, err := Do(ctx, tr, &c.Request, target(c.Route.withScript(c.Request.Target)))
	if err != nil {
		return nil, err
	}
	return c.Expect.Check(o), nil
}

// RunScenario runs the steps of a scenario in order over one transport.
func RunScenario(ctx context.Context, s *Scenario, tr Transport, target func(Route) http.Handler) ([]string, error) {
	var jar *cookieJar
	if s.CookieJar {
		jar = &cookieJar{values: map[string]string{}}
	}
	return runSteps(ctx, s.Steps, tr, target, "", jar)
}

// cookieJar carries Set-Cookie values from one step to the Cookie field of
// the next, the way a browser would within one site.
type cookieJar struct {
	mu     sync.Mutex
	values map[string]string
	order  []string
}

func (j *cookieJar) header() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	parts := make([]string, 0, len(j.order))
	for _, k := range j.order {
		if v, ok := j.values[k]; ok {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, "; ")
}

func (j *cookieJar) store(h http.Header) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, c := range (&http.Response{Header: h}).Cookies() {
		if _, seen := j.values[c.Name]; !seen {
			j.order = append(j.order, c.Name)
		}
		if c.MaxAge < 0 || c.Value == "deleted" {
			delete(j.values, c.Name)
			continue
		}
		j.values[c.Name] = c.Value
	}
}

func (j *cookieJar) apply(r *Request) *Request {
	if j == nil {
		return r
	}
	c := j.header()
	if c == "" {
		return r
	}
	cp := *r
	cp.Headers = append(append([]Header(nil), r.Headers...), Header{"cookie", c})
	return &cp
}

func runSteps(ctx context.Context, steps []Step, tr Transport, target func(Route) http.Handler, prefix string, jar *cookieJar) ([]string, error) {
	var failures []string
	for i := range steps {
		st := &steps[i]
		label := fmt.Sprintf("%sstep %d", prefix, i+1)
		switch {
		case st.WaitMS > 0:
			select {
			case <-time.After(time.Duration(st.WaitMS) * time.Millisecond):
			case <-ctx.Done():
				return failures, ctx.Err()
			}
		case len(st.Parallel) > 0:
			f, err := runParallel(ctx, st.Parallel, tr, target, label+".", jar)
			failures = append(failures, f...)
			if err != nil {
				return failures, err
			}
		default:
			if st.DelayMS > 0 {
				select {
				case <-time.After(time.Duration(st.DelayMS) * time.Millisecond):
				case <-ctx.Done():
					return failures, ctx.Err()
				}
			}
			o, err := Do(ctx, tr, jar.apply(st.Request), target(st.Route.withScript(st.Request.Target)))
			if err != nil {
				return failures, fmt.Errorf("%s: %w", label, err)
			}
			if jar != nil {
				jar.store(o.Header)
			}
			for _, f := range st.Expect.Check(o) {
				failures = append(failures, label+": "+f)
			}
		}
	}
	return failures, nil
}

func runParallel(ctx context.Context, steps []Step, tr Transport, target func(Route) http.Handler, prefix string, jar *cookieJar) ([]string, error) {
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		failures []string
		firstErr error
	)
	for i := range steps {
		wg.Go(func() {
			f, err := runSteps(ctx, steps[i:i+1], tr, target, prefix+strconv.Itoa(i+1)+" ", jar)
			mu.Lock()
			defer mu.Unlock()
			failures = append(failures, f...)
			if err != nil && firstErr == nil {
				firstErr = err
			}
		})
	}
	wg.Wait()
	return failures, firstErr
}

func (r Route) withScript(target string) Route {
	r.Script = r.script(target)
	return r
}

// Check compares an observation with the expectation and returns one line
// per failed assertion.
func (e *Expect) Check(o *Observation) []string {
	var f []string
	fail := func(format string, a ...any) { f = append(f, fmt.Sprintf(format, a...)) }

	reached := o.Header.Get("X-Fixture") != ""
	want := e.ReachesResponder == nil || *e.ReachesResponder
	if reached != want {
		fail("reaches_responder: want %t, got %t (status %d, body %q)", want, reached, o.Status, clip(o.Body))
	}
	reset := e.OrReset && o.Status == 0 && o.Header.Get("X-Compliance-Reset") != ""
	if e.Status != 0 && o.Status != e.Status && !reset {
		fail("status: want %d, got %d (body %q)", e.Status, o.Status, clip(o.Body))
	}
	if len(e.StatusIn) > 0 && !containsInt(e.StatusIn, o.Status) && !reset {
		fail("status: want one of %v, got %d", e.StatusIn, o.Status)
	}
	f = append(f, e.checkEnv(o)...)
	for k, v := range e.Headers {
		if got := o.Header.Get(k); got != v {
			fail("header %s: want %q, got %q", k, v, got)
		}
	}
	for k, want := range e.HeadersAll {
		got := o.Header.Values(k)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			fail("header %s: want all of %q, got %q", k, want, got)
		}
	}
	for _, k := range e.HeadersAbsent {
		if _, ok := o.Header[http.CanonicalHeaderKey(k)]; ok {
			fail("header %s: want absent, got %q", k, o.Header.Get(k))
		}
	}
	if e.Body != nil && string(o.Body) != *e.Body {
		fail("body: want %q, got %q", *e.Body, clip(o.Body))
	}
	if e.BodyContains != "" && !bytes.Contains(o.Body, []byte(e.BodyContains)) {
		fail("body: want it to contain %q, got %q", e.BodyContains, clip(o.Body))
	}
	if e.BodyLength != nil && int64(len(o.Body)) != *e.BodyLength {
		fail("body_length: want %d, got %d", *e.BodyLength, len(o.Body))
	}
	if e.BodySHA256 != "" && !strings.EqualFold(SHA256(o.Body), e.BodySHA256) {
		fail("body_sha256: want %s, got %s", e.BodySHA256, SHA256(o.Body))
	}
	ms := int(o.Duration / time.Millisecond)
	if e.DurationMinMS > 0 && ms < e.DurationMinMS {
		fail("duration: want at least %d ms, got %d ms", e.DurationMinMS, ms)
	}
	if e.DurationMaxMS > 0 && ms > e.DurationMaxMS {
		fail("duration: want at most %d ms, got %d ms", e.DurationMaxMS, ms)
	}
	return f
}

func (e *Expect) checkEnv(o *Observation) []string {
	needsDump := len(e.Env)+len(e.EnvMatch)+len(e.EnvAbsent) > 0 || e.StdinLength != nil || e.StdinSHA256 != ""
	if !needsDump {
		return nil
	}
	if !o.Dumped {
		return []string{fmt.Sprintf("env: no dump fixture output (status %d, body %q)", o.Status, clip(o.Body))}
	}
	var f []string
	for k, v := range e.Env {
		v = strings.ReplaceAll(v, "${SERVER_PORT}", o.Port)
		got, ok := o.Env[k]
		switch {
		case !ok:
			f = append(f, fmt.Sprintf("env %s: want %q, got absent", k, v))
		case got != v:
			f = append(f, fmt.Sprintf("env %s: want %q, got %q", k, v, clip([]byte(got))))
		}
	}
	for k, pattern := range e.EnvMatch {
		got, ok := o.Env[k]
		if !ok {
			f = append(f, fmt.Sprintf("env %s: want match %q, got absent", k, pattern))
			continue
		}
		if !mustMatch(pattern, got) {
			f = append(f, fmt.Sprintf("env %s: want match %q, got %q", k, pattern, clip([]byte(got))))
		}
	}
	for _, k := range e.EnvAbsent {
		if got, ok := o.Env[k]; ok {
			f = append(f, fmt.Sprintf("env %s: want absent, got %q", k, clip([]byte(got))))
		}
	}
	if e.StdinLength != nil && o.StdinLength != *e.StdinLength {
		f = append(f, fmt.Sprintf("stdin_length: want %d, got %d", *e.StdinLength, o.StdinLength))
	}
	if e.StdinSHA256 != "" && !strings.EqualFold(o.StdinSHA256, e.StdinSHA256) {
		f = append(f, fmt.Sprintf("stdin_sha256: want %s, got %s", e.StdinSHA256, o.StdinSHA256))
	}
	return f
}

// SHA256 is the hex digest the dump fixture reports, for writing cases.
func SHA256(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func clip(b []byte) string {
	const n = 200
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
