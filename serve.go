package fcgi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/braska-io/fcgi-go/internal/cgi"
	"github.com/braska-io/fcgi-go/internal/fastcgi"
	"github.com/braska-io/fcgi-go/internal/spool"
)

// maxStderrLog caps the responder stderr that reaches the log per call.
const maxStderrLog = 8 << 10

// trace collects what the observer reports beyond the written response.
type trace struct {
	waitStart    time.Time
	slotWait     time.Duration
	requestBytes int64
	writeErr     error
}

// input is the request body of a call and how to check it.
type input struct {
	// w lets net/http close the connection on an oversized body; nil for Call.
	w    http.ResponseWriter
	body io.Reader
	// length is the declared body length, or -1 when unknown.
	length int64
	// strict makes a body of another length than length an error.
	strict bool
}

func (g *Gateway) serve(w http.ResponseWriter, r *http.Request, cfg *config) {
	ow := &observedWriter{ResponseWriter: w}
	var t trace
	if !g.begin() {
		http.Error(ow, "gateway shutting down", http.StatusServiceUnavailable)
		g.observe(r, cfg, ow.status, ow.bytes, &t, OriginGateway, newError(http.StatusServiceUnavailable, ErrShuttingDown, nil))
		return
	}
	defer g.inflight.Done()

	origin, err := g.answer(ow, r, cfg, &t)
	// After answer the slot is free, so a slow observer holds no worker.
	g.observe(r, cfg, ow.status, ow.bytes, &t, origin, err)
}

// answer runs the call and writes the library's status when it fails.
func (g *Gateway) answer(w http.ResponseWriter, r *http.Request, cfg *config, t *trace) (Origin, error) {
	in := input{
		w:      w,
		body:   r.Body,
		length: r.ContentLength,
		strict: r.Header.Get("Content-Length") != "" && r.ContentLength >= 0,
	}
	rep, err := g.run(r, cfg, in, t)
	if err != nil {
		origin, status, err := failure(r.Context(), err)
		if origin == OriginClient {
			// Nobody reads an answer.
			return origin, err
		}
		cfg.logger.WarnContext(r.Context(), "fcgi call failed", "script", cfg.script, "status", status, "error", err)
		http.Error(w, http.StatusText(status), status)
		return origin, err
	}
	defer rep.body.Close()
	t.writeErr = writeResponse(w, r, cfg, rep)
	if t.writeErr != nil {
		cfg.logger.WarnContext(r.Context(), "write response", "script", cfg.script, "error", t.writeErr)
	}
	return OriginResponder, t.writeErr
}

func (g *Gateway) observe(r *http.Request, cfg *config, status int, bytes int64, t *trace, origin Origin, err error) {
	if cfg.observer == nil {
		return
	}
	// The response is complete; a propagated panic could only abort the
	// connection or take down a server that does not recover.
	defer func() {
		if v := recover(); v != nil {
			cfg.logger.ErrorContext(r.Context(), "fcgi observer panicked",
				"script", cfg.script, "panic", v, "stack", string(debug.Stack()))
		}
	}()
	info := CallInfo{
		Script:        cfg.script,
		Status:        status,
		Origin:        origin,
		Err:           err,
		SlotWait:      t.slotWait,
		RequestBytes:  t.requestBytes,
		ResponseBytes: bytes,
	}
	if !t.waitStart.IsZero() {
		info.Duration = time.Since(t.waitStart)
	}
	cfg.observer(r, info)
}

// run is the engine behind Script and Call: it spools the request body, waits
// for a slot, runs the script and returns the spooled, parsed reply. The
// caller closes the reply's body.
func (g *Gateway) run(r *http.Request, cfg *config, in input, t *trace) (*reply, error) {
	req, n, err := readBody(in, cfg)
	t.requestBytes = n
	if err != nil {
		return nil, err
	}
	defer req.Close()

	ctx, cancel := context.WithTimeout(r.Context(), cfg.timeout)
	defer cancel()

	return g.call(ctx, r, cfg, req, t)
}

// readBody spools the request body so a slow client never holds a worker.
// It also returns the bytes read, even when reading failed.
func readBody(in input, cfg *config) (*spool.Spool, int64, error) {
	sp := spool.New(cfg.spoolDir, cfg.spoolMemory)
	if in.length > 0 && in.length <= cfg.maxBody {
		// A failure here recurs on the first write and is reported there.
		_ = sp.Expect(in.length)
	}
	n, err := io.Copy(sp, http.MaxBytesReader(in.w, io.NopCloser(in.body), cfg.maxBody))
	if err == nil && in.strict && n != in.length {
		// net/http enforces this on HTTP/1.1 and HTTP/2; quic-go's HTTP/3
		// server only catches a body longer than declared. RFC 9114 section
		// 4.1.2 makes either mismatch malformed, so it must not be forwarded.
		err = fmt.Errorf("body is %d bytes, Content-Length says %d", n, in.length)
	}
	if err == nil {
		return sp, n, nil
	}
	_ = sp.Close()
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return nil, n, newError(http.StatusRequestEntityTooLarge, ErrBodyTooLarge, fmt.Errorf("limit is %d bytes", cfg.maxBody))
	}
	return nil, n, newError(http.StatusBadRequest, ErrBadBody, err)
}

type reply struct {
	body   *spool.Spool
	parsed *cgi.Response
}

func (g *Gateway) call(ctx context.Context, r *http.Request, cfg *config, in *spool.Spool, t *trace) (*reply, error) {
	t.waitStart = time.Now()
	release, err := g.acquire(ctx)
	t.slotWait = time.Since(t.waitStart)
	if err != nil {
		return nil, err
	}
	out := spool.New(cfg.spoolDir, cfg.spoolMemory)
	stderr := &cappedBuffer{limit: maxStderrLog}
	err = g.roundTrip(ctx, r, cfg, in, out, stderr)
	release()
	if stderr.len() > 0 {
		cfg.logger.WarnContext(r.Context(), "responder stderr", slog.String("script", cfg.script), slog.String("output", stderr.String()))
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
		return nil, newError(http.StatusServiceUnavailable, ErrNoSlot, ctx.Err())
	}
}

func (g *Gateway) roundTrip(ctx context.Context, r *http.Request, cfg *config, in, out *spool.Spool, stderr io.Writer) error {
	conn, err := g.dialer.Dial(ctx)
	if err != nil {
		return classify(ctx, err, ErrUnreachable)
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
	res, err := fastcgi.Do(ctx, conn, params, in.Reader(0), in.Len(), &limitWriter{w: out, left: cfg.maxResponse}, stderr)
	if errors.Is(err, ErrResponseTooLarge) {
		return newError(http.StatusBadGateway, ErrResponseTooLarge, fmt.Errorf("limit is %d bytes", cfg.maxResponse))
	}
	if err != nil {
		return classify(ctx, err, ErrBadReply)
	}
	if res.ProtocolStatus != 0 {
		return newError(http.StatusBadGateway, ErrBadReply, fmt.Errorf("responder refused the request: protocol status %d", res.ProtocolStatus))
	}
	if res.AppStatus != 0 {
		cfg.logger.WarnContext(r.Context(), "responder exited with non-zero status", "script", cfg.script, "app_status", res.AppStatus)
	}
	return nil
}

// hasBody tells whether the client sent a message-body, even an empty one.
func hasBody(r *http.Request, in *spool.Spool) bool {
	// net/http reports -1 only when the client framed a body. quic-go's
	// HTTP/3 server reports -1 whenever Content-Length is absent, so there it
	// says nothing.
	framed := r.ContentLength > 0 || (r.ContentLength < 0 && r.ProtoMajor < 3)
	return in.Len() > 0 || framed || len(r.TransferEncoding) > 0 || r.Header.Get("Content-Length") != ""
}

func parseReply(out *spool.Spool) (*cgi.Response, error) {
	parsed, err := cgi.ParseResponse(out.Reader(0))
	if err != nil {
		return nil, newError(http.StatusBadGateway, ErrBadReply, err)
	}
	bodyLen := out.Len() - parsed.BodyOffset
	for _, v := range parsed.Header.Values("Content-Length") {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n != bodyLen {
			return nil, newError(http.StatusBadGateway, ErrBadReply, fmt.Errorf("content-length %q does not match %d body bytes", v, bodyLen))
		}
	}
	return parsed, nil
}

// applyHeader writes the reply's headers into h as the client sees them:
// hop-by-hop and dropped headers removed, Content-Length computed from the
// spooled body unless the responder or HEAD says otherwise.
func applyHeader(h http.Header, cfg *config, rep *reply, method string) {
	cgi.RemoveHopByHop(rep.parsed.Header)
	for name, values := range rep.parsed.Header {
		h[name] = values
	}
	for _, name := range cfg.dropHeaders {
		h.Del(name)
	}
	// HEAD: php-fpm sends no body, so a length computed from the spool would
	// be 0 and contradict the GET response.
	if h.Get("Content-Length") == "" && bodyAllowed(rep.parsed.Status) && method != http.MethodHead {
		h.Set("Content-Length", strconv.FormatInt(rep.bodyLen(), 10))
	}
}

func (rep *reply) bodyLen() int64 { return rep.body.Len() - rep.parsed.BodyOffset }

// writeResponse returns only the failure to write the body: the status is
// sent by then, so the caller cannot answer anything else.
func writeResponse(w http.ResponseWriter, r *http.Request, cfg *config, rep *reply) error {
	h := w.Header()
	applyHeader(h, cfg, rep, r.Method)
	if _, ok := h["Content-Type"]; !ok {
		// A nil entry stops net/http from sniffing a type the responder
		// deliberately left out.
		h["Content-Type"] = nil
	}
	w.WriteHeader(rep.parsed.Status)
	if r.Method == http.MethodHead || !bodyAllowed(rep.parsed.Status) {
		// quic-go's HTTP/3 server adds Content-Length: 0 to a response whose
		// header is still unsent when the handler returns; flushing sends it
		// as the responder wrote it.
		_ = http.NewResponseController(w).Flush() // a writer without Flush has nothing to add
		return nil
	}
	if _, err := io.Copy(w, rep.body.Reader(rep.parsed.BodyOffset)); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}

func bodyAllowed(status int) bool {
	return status >= 200 && status != http.StatusNoContent && status != http.StatusNotModified
}

// observedWriter records the status and body bytes the client received.
// Unwrap keeps http.ResponseController working on the writer underneath.
type observedWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (o *observedWriter) Unwrap() http.ResponseWriter { return o.ResponseWriter }

func (o *observedWriter) WriteHeader(code int) {
	if o.status == 0 {
		o.status = code
	}
	o.ResponseWriter.WriteHeader(code)
}

func (o *observedWriter) Write(p []byte) (int, error) {
	if o.status == 0 {
		o.status = http.StatusOK
	}
	n, err := o.ResponseWriter.Write(p)
	o.bytes += int64(n)
	return n, err
}

// ReadFrom keeps the underlying writer's own io.ReaderFrom path, such as
// net/http's, for spooled bodies that io.Copy would otherwise copy through a
// fresh buffer.
func (o *observedWriter) ReadFrom(src io.Reader) (int64, error) {
	if o.status == 0 {
		o.status = http.StatusOK
	}
	n, err := io.Copy(o.ResponseWriter, src)
	o.bytes += n
	return n, err
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
		return 0, ErrResponseTooLarge
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
