package fcgi

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"net/url"

	"github.com/braska-io/fcgi-go/internal/spool"
)

// Caller runs one script for callers that have no HTTP request. It shares the
// gateway's slots, spool and Shutdown with every Script handler.
type Caller struct {
	g   *Gateway
	cfg config
}

// Caller returns a Caller for the script name. It takes the same options as
// Script and applies the same defaults and overrides.
func (g *Gateway) Caller(name string, opts ...ScriptOption) *Caller {
	return &Caller{g: g, cfg: g.scriptConfig(name, opts)}
}

// Request describes one call without an HTTP server in between.
type Request struct {
	Method     string // default GET
	Target     string // origin-form request-target, e.g. "/api/v1/customer/7?x=1"
	Host       string // HTTP_HOST and SERVER_NAME; default "localhost"
	RemoteAddr string // "ip:port"; default "127.0.0.1:0"
	TLS        bool   // sets HTTPS=on
	// Header becomes HTTP_* under the rules for client headers: protected
	// names and names with "_" or "." are dropped. Pass trusted values
	// through Params.
	Header http.Header
	// Body is nil for no body.
	Body io.Reader
	// ContentLength is the length of Body; the library spools and measures it
	// anyway. As with net/http, 0 or -1 with a Body means unknown. A positive
	// value that Body does not match fails with ErrBadBody.
	ContentLength int64
	// PathInfo replaces PATH_INFO, also over WithPathInfo; nil derives it as
	// Script does.
	PathInfo *string
	// Params are written last, after the result of WithParams.
	Params map[string]string
}

// Response is the responder's reply, fully spooled.
type Response struct {
	Status int
	Header http.Header
	// Body reads the spooled output. The caller must close it: Close
	// releases the spool, and an unclosed body keeps its file. Like Script,
	// a HEAD request or a status without a body gets an empty Body.
	Body          io.ReadCloser
	ContentLength int64
}

// Call runs the script and returns after the whole reply is spooled, so the
// responder worker is free before the caller reads Body. The responder's own
// status, such as a PHP 500, is a Response and never an error.
//
// A failure of the library is an *Error. A cancelled ctx returns an error
// that wraps context.Canceled and is not an *Error.
func (c *Caller) Call(ctx context.Context, req *Request) (*Response, error) {
	r, cfg, in, err := c.newRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	var t trace
	resp, err := c.g.runCall(r, cfg, in, &t)

	origin, status, n := OriginResponder, 0, int64(0)
	if err == nil {
		status, n = resp.Status, resp.ContentLength
	} else {
		origin, status, err = failure(ctx, err)
	}
	c.g.observe(r, cfg, status, n, &t, origin, err)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (g *Gateway) runCall(r *http.Request, cfg *config, in input, t *trace) (*Response, error) {
	if !g.begin() {
		return nil, newError(http.StatusServiceUnavailable, ErrShuttingDown, nil)
	}
	defer g.inflight.Done()

	rep, err := g.run(r, cfg, in, t)
	if err != nil {
		return nil, err
	}
	resp := &Response{Status: rep.parsed.Status, Header: http.Header{}}
	applyHeader(resp.Header, cfg, rep, r.Method)
	if r.Method == http.MethodHead || !bodyAllowed(rep.parsed.Status) {
		_ = rep.body.Close()
		resp.Body = http.NoBody
		return resp, nil
	}
	resp.ContentLength = rep.bodyLen()
	resp.Body = &spoolBody{sp: rep.body, r: rep.body.Reader(rep.parsed.BodyOffset)}
	return resp, nil
}

// newRequest builds the *http.Request that WithParams, WithQuery,
// WithPathInfo and the observer receive, and the per-call config.
func (c *Caller) newRequest(ctx context.Context, req *Request) (*http.Request, *config, input, error) {
	u, err := url.ParseRequestURI(req.Target)
	if err != nil {
		return nil, nil, input{}, fmt.Errorf("fcgi: invalid request target: %w", err)
	}
	r := &http.Request{
		Method:     orDefault(req.Method, http.MethodGet),
		URL:        u,
		RequestURI: req.Target,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     req.Header.Clone(),
		Body:       http.NoBody,
		Host:       orDefault(req.Host, "localhost"),
		RemoteAddr: orDefault(req.RemoteAddr, "127.0.0.1:0"),
	}
	if r.Header == nil {
		r.Header = http.Header{}
	}
	if req.TLS {
		r.TLS = &tls.ConnectionState{}
	}
	r = r.WithContext(ctx)

	in := input{body: http.NoBody}
	if req.Body != nil {
		in = input{body: req.Body, length: -1}
		if req.ContentLength > 0 {
			in.length, in.strict = req.ContentLength, true
		}
	}
	r.ContentLength = in.length

	cfg := c.cfg
	if req.PathInfo != nil {
		pi := *req.PathInfo
		cfg.pathInfo = func(*http.Request) string { return pi }
	}
	if len(req.Params) > 0 {
		cfg.paramsFunc = withParams(c.cfg.paramsFunc, req.Params)
	}
	return r, &cfg, in, nil
}

// withParams writes extra after what f returns, so Request.Params rank last.
func withParams(f func(*http.Request) map[string]string, extra map[string]string) func(*http.Request) map[string]string {
	return func(r *http.Request) map[string]string {
		m := map[string]string{}
		if f != nil {
			maps.Copy(m, f(r))
		}
		maps.Copy(m, extra)
		return m
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// spoolBody reads a spooled reply and releases the spool on Close.
type spoolBody struct {
	sp     *spool.Spool
	r      io.Reader
	closed bool
}

func (b *spoolBody) Read(p []byte) (int, error) {
	if b.closed {
		return 0, fs.ErrClosed
	}
	return b.r.Read(p)
}

func (b *spoolBody) Close() error {
	if b.closed {
		return nil
	}
	b.closed = true
	return b.sp.Close()
}
