package cgi

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// MaxHeaderBlock bounds the CGI header block so a broken responder cannot
// make us buffer an endless "header".
const MaxHeaderBlock = 1 << 20

// ErrResponse marks a responder output that is not a valid CGI response.
var ErrResponse = errors.New("invalid cgi response")

// Response is the parsed header block of a CGI response.
type Response struct {
	Status int
	Reason string
	Header http.Header
	// BodyOffset is the position of the first body byte in the parsed stream.
	BodyOffset int64
}

var readers = sync.Pool{New: func() any { return bufio.NewReaderSize(nil, 4096) }}

// ParseResponse reads the header block of a CGI response (RFC 3875 section 6).
func ParseResponse(r io.Reader) (*Response, error) {
	br := readers.Get().(*bufio.Reader)
	br.Reset(r)
	defer func() {
		br.Reset(nil)
		readers.Put(br)
	}()
	resp := &Response{Header: http.Header{}}
	var consumed int64
	for {
		line, err := br.ReadString('\n')
		consumed += int64(len(line))
		if consumed > MaxHeaderBlock {
			return nil, fmt.Errorf("%w: header block exceeds %d bytes", ErrResponse, MaxHeaderBlock)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: no header terminator", ErrResponse)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if err := resp.addLine(line); err != nil {
			return nil, err
		}
	}
	resp.BodyOffset = consumed
	resp.applyDefaults()
	return resp, nil
}

func (r *Response) addLine(line string) error {
	name, value, ok := strings.Cut(line, ":")
	if !ok || name == "" || strings.ContainsAny(name, " \t") {
		return fmt.Errorf("%w: malformed header line %q", ErrResponse, truncate(line))
	}
	value = strings.TrimSpace(value)
	if strings.EqualFold(name, "Status") {
		return r.setStatus(value)
	}
	r.Header.Add(textproto.CanonicalMIMEHeaderKey(name), value)
	return nil
}

func (r *Response) setStatus(value string) error {
	code, reason, _ := strings.Cut(value, " ")
	n, err := strconv.Atoi(code)
	if err != nil || n < 100 || n > 999 {
		return fmt.Errorf("%w: invalid status %q", ErrResponse, truncate(value))
	}
	r.Status = n
	r.Reason = strings.TrimSpace(reason)
	return nil
}

// finish applies the defaults for a missing Status (RFC 3875 section 6.2).
func (r *Response) applyDefaults() {
	if r.Status != 0 {
		return
	}
	r.Status = http.StatusOK
	if loc := r.Header.Get("Location"); loc != "" {
		if u, err := url.Parse(loc); err == nil && u.IsAbs() {
			r.Status = http.StatusFound
		}
	}
}

func truncate(s string) string {
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

// RemoveHopByHop deletes the fields that describe the responder's
// connection. The gateway owns the client connection, and HTTP/2 forbids
// these fields outright (RFC 9113 section 8.2.2).
func RemoveHopByHop(h http.Header) {
	for name := range connectionTokens(h) {
		h.Del(name)
	}
	for name := range hopByHop {
		h.Del(name)
	}
}
