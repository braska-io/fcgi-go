package compliance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

const h2Stream = 1

// doH2 speaks HTTP/2 at the frame layer, so split cookie fields, a missing
// content-length and multi-frame bodies arrive exactly as the case says.
// Field names go out lowercased, as RFC 9113 section 8.2 requires.
func doH2(ctx context.Context, addr string, req *Request, body []byte) (*Observation, error) {
	c, err := dialTLS(ctx, addr, "h2")
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if c.ConnectionState().NegotiatedProtocol != "h2" {
		return nil, errors.New("h2: server did not negotiate h2")
	}
	if _, err := io.WriteString(c, http2.ClientPreface); err != nil {
		return nil, fmt.Errorf("h2 preface: %w", err)
	}
	fr := http2.NewFramer(c, c)
	fr.ReadMetaHeaders = hpack.NewDecoder(4096, nil)
	if err := fr.WriteSettings(); err != nil {
		return nil, fmt.Errorf("h2 settings: %w", err)
	}

	s := &h2Session{fr: fr, window: 65535, connWindow: 65535, maxFrame: 16384, windowCh: make(chan struct{}, 1)}
	readErr := make(chan error, 1)
	go func() { readErr <- s.read() }()

	framing := req.framing(body)
	if err := s.writeHeaders(req, body, framing); err != nil {
		return nil, err
	}
	if framing != "none" {
		if err := s.writeBody(ctx, body, framing); err != nil {
			return nil, err
		}
	}
	if err := <-readErr; err != nil {
		return nil, err
	}
	return s.observation(), nil
}

type h2Session struct {
	fr *http2.Framer

	mu         sync.Mutex // guards the write side and the windows
	window     int32
	connWindow int32
	maxFrame   uint32
	windowCh   chan struct{}

	status int
	header http.Header
	body   bytes.Buffer
	reset  http2.ErrCode
}

func (s *h2Session) writeHeaders(req *Request, body []byte, framing string) error {
	var block bytes.Buffer
	enc := hpack.NewEncoder(&block)
	field := func(name, value string) {
		_ = enc.WriteField(hpack.HeaderField{Name: name, Value: value}) // writes to a bytes.Buffer cannot fail
	}
	authority := req.authority()
	field(":method", req.Method)
	field(":scheme", "https")
	field(":authority", authority)
	field(":path", req.Target)
	for _, h := range req.Headers {
		if strings.EqualFold(h[0], "Host") {
			continue // :authority carries it
		}
		field(strings.ToLower(h[0]), h[1])
	}
	if framing == "length" && !req.has("Content-Length") {
		field("content-length", strconv.Itoa(len(body)))
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	frag := block.Bytes()
	first := true
	for first || len(frag) > 0 {
		n := min(len(frag), int(s.maxFrame))
		chunk, rest := frag[:n], frag[n:]
		end := len(rest) == 0
		var err error
		if first {
			err = s.fr.WriteHeaders(http2.HeadersFrameParam{
				StreamID: h2Stream, BlockFragment: chunk, EndHeaders: end, EndStream: framing == "none",
			})
		} else {
			err = s.fr.WriteContinuation(h2Stream, end, chunk)
		}
		if err != nil {
			return fmt.Errorf("h2 headers: %w", err)
		}
		frag, first = rest, false
	}
	return nil
}

func (s *h2Session) writeBody(ctx context.Context, body []byte, framing string) error {
	parts := [][]byte{body}
	if framing == "chunked" {
		parts = thirds(body)
	}
	if len(parts) == 0 {
		return s.writeData(nil, true)
	}
	for i, p := range parts {
		for len(p) > 0 {
			n, err := s.reserve(ctx, len(p))
			if err != nil {
				return err
			}
			last := i == len(parts)-1 && n == len(p)
			if err := s.writeData(p[:n], last); err != nil {
				return err
			}
			p = p[n:]
		}
	}
	return nil
}

// reserve waits until the peer's flow-control windows allow at least one
// byte and takes up to want bytes from them.
func (s *h2Session) reserve(ctx context.Context, want int) (int, error) {
	for {
		s.mu.Lock()
		n := min(want, int(s.window), int(s.connWindow), int(s.maxFrame))
		if n > 0 {
			s.window -= int32(n)     //nolint:gosec // n is bounded by the int32 windows
			s.connWindow -= int32(n) //nolint:gosec // n is bounded by the int32 windows
			s.mu.Unlock()
			return n, nil
		}
		s.mu.Unlock()
		select {
		case <-s.windowCh:
		case <-ctx.Done():
			return 0, fmt.Errorf("h2 flow control: %w", ctx.Err())
		}
	}
}

func (s *h2Session) writeData(p []byte, end bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fr.WriteData(h2Stream, end, p); err != nil {
		return fmt.Errorf("h2 data: %w", err)
	}
	return nil
}

// read handles every frame until the response stream ends or is reset.
func (s *h2Session) read() error {
	for {
		f, err := s.fr.ReadFrame()
		if err != nil {
			return fmt.Errorf("h2 read: %w", err)
		}
		done, err := s.handle(f)
		if err != nil || done {
			return err
		}
	}
}

func (s *h2Session) handle(f http2.Frame) (bool, error) {
	switch f := f.(type) {
	case *http2.SettingsFrame:
		return false, s.settings(f)
	case *http2.WindowUpdateFrame:
		s.windowUpdate(f)
	case *http2.PingFrame:
		if !f.IsAck() {
			s.mu.Lock()
			defer s.mu.Unlock()
			return false, s.fr.WritePing(true, f.Data)
		}
	case *http2.MetaHeadersFrame:
		if f.StreamID != h2Stream {
			return false, nil
		}
		s.headers(f)
		return f.StreamEnded(), nil
	case *http2.DataFrame:
		if f.StreamID != h2Stream {
			return false, nil
		}
		s.body.Write(f.Data())
		if err := s.refund(len(f.Data())); err != nil {
			return false, err
		}
		return f.StreamEnded(), nil
	case *http2.RSTStreamFrame:
		if f.StreamID == h2Stream {
			s.reset = f.ErrCode
			return true, nil
		}
	case *http2.GoAwayFrame:
		s.reset = f.ErrCode
		return true, nil
	}
	return false, nil
}

func (s *h2Session) settings(f *http2.SettingsFrame) error {
	if f.IsAck() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := f.Value(http2.SettingInitialWindowSize); ok {
		s.window += int32(v) - 65535 //nolint:gosec // RFC 9113 caps the value at 2^31-1
	}
	if v, ok := f.Value(http2.SettingMaxFrameSize); ok {
		s.maxFrame = v
	}
	s.notify()
	return s.fr.WriteSettingsAck()
}

func (s *h2Session) windowUpdate(f *http2.WindowUpdateFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inc := int32(f.Increment) //nolint:gosec // RFC 9113 caps the increment at 2^31-1
	if f.StreamID == 0 {
		s.connWindow += inc
	} else if f.StreamID == h2Stream {
		s.window += inc
	}
	s.notify()
}

func (s *h2Session) notify() {
	select {
	case s.windowCh <- struct{}{}:
	default:
	}
}

// refund returns received bytes to the server's windows so large responses
// keep flowing.
func (s *h2Session) refund(n int) error {
	if n == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fr.WriteWindowUpdate(0, uint32(n)); err != nil { //nolint:gosec // n is one frame's length
		return fmt.Errorf("h2 window update: %w", err)
	}
	if err := s.fr.WriteWindowUpdate(h2Stream, uint32(n)); err != nil { //nolint:gosec // n is one frame's length
		return fmt.Errorf("h2 window update: %w", err)
	}
	return nil
}

func (s *h2Session) headers(f *http2.MetaHeadersFrame) {
	if s.header != nil {
		return // trailers: not part of the observation
	}
	s.header = http.Header{}
	for _, hf := range f.Fields {
		if hf.Name == ":status" {
			s.status, _ = strconv.Atoi(hf.Value) // a malformed status stays 0 and fails the case
			continue
		}
		s.header.Add(http.CanonicalHeaderKey(hf.Name), hf.Value)
	}
}

func (s *h2Session) observation() *Observation {
	h := s.header
	if h == nil {
		h = http.Header{}
	}
	if s.reset != http2.ErrCodeNo && s.status == 0 {
		h.Set("X-Compliance-Reset", s.reset.String())
	}
	return &Observation{Status: s.status, Header: h, Body: s.body.Bytes()}
}

func mustMatch(pattern, s string) bool {
	re, err := regexp.Compile(pattern)
	return err == nil && re.MatchString(s)
}
