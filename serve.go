package fcgi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/braska-io/fcgi-go/internal/cgi"
	"github.com/braska-io/fcgi-go/internal/fastcgi"
	"github.com/braska-io/fcgi-go/internal/spool"
)

// maxStderrLog caps the responder stderr that reaches the log per call.
const maxStderrLog = 8 << 10

var (
	errNoSlot   = errors.New("no free responder slot")
	errBadReply = errors.New("bad responder reply")
	errTooLarge = errors.New("responder output exceeds the response limit")
)

// statusError carries the HTTP status a failed call answers with.
type statusError struct {
	status int
	err    error
}

func (e *statusError) Error() string { return e.err.Error() }
func (e *statusError) Unwrap() error { return e.err }

func fail(status int, format string, args ...any) *statusError {
	return &statusError{status: status, err: fmt.Errorf(format, args...)}
}

func (g *Gateway) serve(w http.ResponseWriter, r *http.Request, cfg *config) {
	if !g.begin() {
		http.Error(w, "gateway shutting down", http.StatusServiceUnavailable)
		return
	}
	defer g.inflight.Done()

	err := g.handle(w, r, cfg)
	if err == nil {
		return
	}
	if errors.Is(r.Context().Err(), context.Canceled) {
		return // the client left; nobody reads an answer
	}
	status := http.StatusBadGateway
	if se, ok := errors.AsType[*statusError](err); ok {
		status = se.status
	}
	cfg.logger.Warn("fcgi call failed", "script", cfg.script, "status", status, "error", err)
	http.Error(w, http.StatusText(status), status)
}

func (g *Gateway) handle(w http.ResponseWriter, r *http.Request, cfg *config) error {
	req, err := readBody(w, r, cfg)
	if err != nil {
		return err
	}
	defer req.Close()

	ctx, cancel := context.WithTimeout(r.Context(), cfg.timeout)
	defer cancel()

	resp, err := g.call(ctx, r, cfg, req)
	if err != nil {
		return err
	}
	defer resp.body.Close()
	return writeResponse(w, r, cfg, resp)
}

// readBody spools the request body so a slow client never holds a worker.
func readBody(w http.ResponseWriter, r *http.Request, cfg *config) (*spool.Spool, error) {
	sp := spool.New(cfg.spoolDir, cfg.spoolMemory)
	_, err := io.Copy(sp, http.MaxBytesReader(w, r.Body, cfg.maxBody))
	if err == nil {
		return sp, nil
	}
	_ = sp.Close()
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return nil, fail(http.StatusRequestEntityTooLarge, "request body exceeds %d bytes", cfg.maxBody)
	}
	return nil, fail(http.StatusBadRequest, "read request body: %w", err)
}

type reply struct {
	body   *spool.Spool
	parsed *cgi.Response
}

func (g *Gateway) call(ctx context.Context, r *http.Request, cfg *config, in *spool.Spool) (*reply, error) {
	release, err := g.acquire(ctx)
	if err != nil {
		return nil, err
	}
	out := spool.New(cfg.spoolDir, cfg.spoolMemory)
	stderr := &cappedBuffer{limit: maxStderrLog}
	err = g.roundTrip(ctx, r, cfg, in, out, stderr)
	release()
	if stderr.len() > 0 {
		cfg.logger.Warn("responder stderr", slog.String("script", cfg.script), slog.String("output", stderr.String()))
	}
	if err != nil {
		_ = out.Close()
		return nil, err
	}
	parsed, err := parseReply(out)
	if err != nil {
		_ = out.Close()
		return nil, err
	}
	return &reply{body: out, parsed: parsed}, nil
}

func (g *Gateway) acquire(ctx context.Context) (func(), error) {
	select {
	case g.slots <- struct{}{}:
		return func() { <-g.slots }, nil
	case <-ctx.Done():
		return nil, &statusError{status: http.StatusServiceUnavailable, err: fmt.Errorf("%w: %w", errNoSlot, ctx.Err())}
	}
}

func (g *Gateway) roundTrip(ctx context.Context, r *http.Request, cfg *config, in, out *spool.Spool, stderr io.Writer) error {
	conn, err := fastcgi.Dial(ctx, g.address)
	if err != nil {
		return classify(ctx, err)
	}
	params := cgi.Build(r, cgi.Config{
		Script:       cfg.script,
		DocumentRoot: cfg.documentRoot,
		Protected:    cfg.protected,
		PathInfo:     cfg.pathInfo,
		Query:        cfg.query,
		Gateway:      g.cfg.params,
		ScriptVars:   cfg.params,
		ParamsFunc:   cfg.paramsFunc,
		BodyLength:   in.Len(),
		HasBody:      hasBody(r, in),
	})
	res, err := fastcgi.Do(ctx, conn, params, in.Reader(0), &limitWriter{w: out, left: cfg.maxResponse}, stderr)
	if errors.Is(err, errTooLarge) {
		return fail(http.StatusBadGateway, "%w: %d bytes", errTooLarge, cfg.maxResponse)
	}
	if err != nil {
		return classify(ctx, err)
	}
	if res.ProtocolStatus != 0 {
		return fail(http.StatusBadGateway, "responder refused the request: protocol status %d", res.ProtocolStatus)
	}
	if res.AppStatus != 0 {
		cfg.logger.Warn("responder exited with non-zero status", "script", cfg.script, "app_status", res.AppStatus)
	}
	return nil
}

// hasBody tells whether the client sent a message-body, even an empty one.
func hasBody(r *http.Request, in *spool.Spool) bool {
	return in.Len() > 0 || r.ContentLength != 0 || len(r.TransferEncoding) > 0 || r.Header.Get("Content-Length") != ""
}

func classify(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &statusError{status: http.StatusGatewayTimeout, err: err}
	}
	return &statusError{status: http.StatusBadGateway, err: err}
}

func parseReply(out *spool.Spool) (*cgi.Response, error) {
	parsed, err := cgi.ParseResponse(out.Reader(0))
	if err != nil {
		return nil, &statusError{status: http.StatusBadGateway, err: err}
	}
	bodyLen := out.Len() - parsed.BodyOffset
	for _, v := range parsed.Header.Values("Content-Length") {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n != bodyLen {
			return nil, fail(http.StatusBadGateway, "%w: content-length %q does not match %d body bytes", errBadReply, v, bodyLen)
		}
	}
	return parsed, nil
}

func writeResponse(w http.ResponseWriter, r *http.Request, cfg *config, rep *reply) error {
	cgi.RemoveHopByHop(rep.parsed.Header)
	h := w.Header()
	for name, values := range rep.parsed.Header {
		h[name] = values
	}
	for _, name := range cfg.dropHeaders {
		h.Del(name)
	}
	if _, ok := h["Content-Type"]; !ok {
		// A nil entry stops net/http from sniffing a type the responder
		// deliberately left out.
		h["Content-Type"] = nil
	}
	bodyLen := rep.body.Len() - rep.parsed.BodyOffset
	// HEAD: php-fpm sends no body, so a length computed from the spool would
	// be 0 and contradict the GET response.
	if h.Get("Content-Length") == "" && bodyAllowed(rep.parsed.Status) && r.Method != http.MethodHead {
		h.Set("Content-Length", strconv.FormatInt(bodyLen, 10))
	}
	w.WriteHeader(rep.parsed.Status)
	if r.Method == http.MethodHead || !bodyAllowed(rep.parsed.Status) {
		return nil
	}
	if _, err := io.Copy(w, rep.body.Reader(rep.parsed.BodyOffset)); err != nil {
		cfg.logger.Warn("write response", "script", cfg.script, "error", err)
	}
	return nil
}

func bodyAllowed(status int) bool {
	return status >= 200 && status != http.StatusNoContent && status != http.StatusNotModified
}

// cappedBuffer keeps the first limit bytes and drops the rest, so a noisy
// responder cannot grow the log line.
// limitWriter fails the write that crosses the limit. Do then closes the
// connection, so the responder stops instead of filling the spool.
type limitWriter struct {
	w    io.Writer
	left int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.left {
		return 0, errTooLarge
	}
	l.left -= int64(len(p))
	return l.w.Write(p)
}

type cappedBuffer struct {
	buf   []byte
	limit int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.limit - len(c.buf); room > 0 {
		c.buf = append(c.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

func (c *cappedBuffer) len() int       { return len(c.buf) }
func (c *cappedBuffer) String() string { return string(c.buf) }
