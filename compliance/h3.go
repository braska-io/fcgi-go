package compliance

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/quic-go/qpack"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/quicvarint"
)

// HTTP/3 frame and stream types, RFC 9114 sections 6.2 and 7.2.
const (
	h3FrameData     = 0x00
	h3FrameHeaders  = 0x01
	h3FrameSettings = 0x04
	h3StreamControl = 0x00
)

// serveH3 starts an HTTP/3 server for h on a loopback UDP port.
func serveH3(h http.Handler) (addr string, stop func(), err error) {
	cert, err := h3Cert()
	if err != nil {
		return "", nil, err
	}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("h3 listen: %w", err)
	}
	srv := &http3.Server{
		Handler:   h,
		TLSConfig: http3.ConfigureTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}),
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(pc) // returns once Close runs
	}()
	return pc.LocalAddr().String(), func() {
		_ = srv.Close()
		_ = pc.Close()
		<-done
	}, nil
}

var h3CertOnce = sync.OnceValues(func() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("h3 key: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "example.test"},
		DNSNames:     []string{"example.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("h3 certificate: %w", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
})

func h3Cert() (tls.Certificate, error) { return h3CertOnce() }

// doH3 speaks HTTP/3 at the frame layer, so split cookie fields, a missing
// content-length and multi-frame bodies arrive exactly as the case says.
// Field names go out lowercased, as RFC 9114 section 4.2 requires.
func doH3(ctx context.Context, addr string, req *Request, body []byte) (*Observation, error) {
	conn, err := quic.DialAddr(ctx, addr, &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // test server with a throwaway certificate
		NextProtos:         []string{http3.NextProtoH3},
		ServerName:         "example.test",
	}, &quic.Config{})
	if err != nil {
		return nil, fmt.Errorf("dial h3: %w", err)
	}
	defer conn.CloseWithError(0, "")

	// RFC 9114 section 6.2.1: each side opens a control stream that starts
	// with SETTINGS.
	ctrl, err := conn.OpenUniStream()
	if err != nil {
		return nil, fmt.Errorf("h3 control stream: %w", err)
	}
	if _, err := ctrl.Write(h3Frame(quicvarint.Append(nil, h3StreamControl), h3FrameSettings, nil)); err != nil {
		return nil, fmt.Errorf("h3 settings: %w", err)
	}

	str, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("h3 request stream: %w", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = str.SetDeadline(dl) // a failed deadline surfaces as a read error
	}

	framing := req.framing(body)
	var out []byte
	out = h3Frame(out, h3FrameHeaders, h3HeaderBlock(req, body, framing))
	if framing != "none" {
		parts := [][]byte{body}
		if framing == "chunked" {
			parts = thirds(body)
		}
		for _, p := range parts {
			out = h3Frame(out, h3FrameData, p)
		}
	}
	if _, err := str.Write(out); err != nil {
		return nil, fmt.Errorf("write h3 request: %w", err)
	}
	if err := str.Close(); err != nil {
		return nil, fmt.Errorf("close h3 request: %w", err)
	}
	return readH3Response(bufio.NewReader(str))
}

func h3HeaderBlock(req *Request, body []byte, framing string) []byte {
	var block bytes.Buffer
	enc := qpack.NewEncoder(&block)
	field := func(name, value string) {
		_ = enc.WriteField(qpack.HeaderField{Name: name, Value: value}) // writes to a bytes.Buffer cannot fail
	}
	field(":method", req.Method)
	field(":scheme", "https")
	field(":authority", req.authority())
	field(":path", req.Target)
	for _, h := range req.Headers {
		field(strings.ToLower(h[0]), h[1])
	}
	if framing == "length" && !req.has("Content-Length") {
		field("content-length", strconv.Itoa(len(body)))
	}
	return block.Bytes()
}

func h3Frame(dst []byte, typ uint64, payload []byte) []byte {
	dst = quicvarint.Append(dst, typ)
	dst = quicvarint.Append(dst, uint64(len(payload)))
	return append(dst, payload...)
}

// readH3Response reads frames until the server ends or resets the stream.
// Interim 1xx header blocks (RFC 9110 section 15.2) and trailers are not
// part of the observation; unknown frame types are skipped (RFC 9114
// section 9).
func readH3Response(r *bufio.Reader) (*Observation, error) {
	o := &Observation{}
	var body bytes.Buffer
	for {
		typ, err := quicvarint.Read(r)
		if err != nil {
			return h3End(o, &body, err)
		}
		n, err := quicvarint.Read(r)
		if err != nil {
			return h3End(o, &body, err)
		}
		switch typ {
		case h3FrameHeaders:
			p := make([]byte, n)
			if _, err := io.ReadFull(r, p); err != nil {
				return h3End(o, &body, err)
			}
			if o.Status >= 200 {
				continue // trailers
			}
			if err := o.h3Headers(p); err != nil {
				return nil, err
			}
		case h3FrameData:
			if _, err := io.CopyN(&body, r, int64(n)); err != nil { //nolint:gosec // a frame length fits int64
				return h3End(o, &body, err)
			}
		default:
			if _, err := r.Discard(int(n)); err != nil { //nolint:gosec // the test server sends small frames
				return h3End(o, &body, err)
			}
		}
	}
}

func (o *Observation) h3Headers(block []byte) error {
	h := http.Header{}
	status := 0
	dec := qpack.NewDecoder().Decode(block)
	for {
		hf, err := dec()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("h3 qpack: %w", err)
		}
		if hf.Name == ":status" {
			status, _ = strconv.Atoi(hf.Value) // a malformed status stays 0 and fails the case
			continue
		}
		h.Add(http.CanonicalHeaderKey(hf.Name), hf.Value)
	}
	o.Status, o.Header = status, h
	return nil
}

// h3End turns the end of the request stream into an observation: a clean
// end, or a reset that the case can see as X-Compliance-Reset, as on h2.
func h3End(o *Observation, body *bytes.Buffer, err error) (*Observation, error) {
	if o.Header == nil {
		o.Header = http.Header{}
	}
	o.Body = body.Bytes()
	if errors.Is(err, io.EOF) {
		return o, nil
	}
	var se *quic.StreamError
	var ae *quic.ApplicationError
	switch {
	case errors.As(err, &se):
		if o.Status == 0 {
			o.Header.Set("X-Compliance-Reset", http3.ErrCode(se.ErrorCode).String())
		}
		return o, nil
	case errors.As(err, &ae):
		if o.Status == 0 {
			o.Header.Set("X-Compliance-Reset", http3.ErrCode(ae.ErrorCode).String())
		}
		return o, nil
	}
	return nil, fmt.Errorf("h3 read: %w", err)
}
