package fcgi

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The benchmarks drive the handler against an in-process responder that
// answers from precomputed bytes, so the numbers are the gateway's own cost
// plus a Unix socket.

// fakeReply frames stdout as STDOUT records followed by END_REQUEST.
func fakeReply(stdout []byte) []byte {
	var b bytes.Buffer
	for len(stdout) > 0 {
		n := min(len(stdout), 65535)
		b.Write(record(6, stdout[:n]))
		stdout = stdout[n:]
	}
	b.Write(record(6, nil))
	b.Write(record(3, make([]byte, 8))) // END_REQUEST: app status 0, request complete
	return b.Bytes()
}

func newBenchFake(b *testing.B, stdout []byte) string {
	b.Helper()
	reply := fakeReply(stdout)
	// A Unix socket: a TCP connection per call would run out of ephemeral
	// ports within seconds. MkdirTemp keeps the path short enough for a
	// socket name.
	dir, err := os.MkdirTemp("", "fcgi")
	if err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(dir, "fpm.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		_ = ln.Close()
		_ = os.RemoveAll(dir)
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				drainRequest(c)
				_, _ = c.Write(reply)
			}()
		}
	}()
	return "unix:" + path
}

// discardWriter is a ResponseWriter that keeps nothing but the header map.
type discardWriter struct {
	h    http.Header
	code int
	n    int
}

func (w *discardWriter) Header() http.Header         { return w.h }
func (w *discardWriter) WriteHeader(code int)        { w.code = code }
func (w *discardWriter) Write(p []byte) (int, error) { w.n += len(p); return len(p), nil }

func benchHandler(b *testing.B, stdout []byte, method string, body []byte) {
	addr := newBenchFake(b, stdout)
	gw := New(addr, WithLogger(discardLogger()), WithMaxConns(64), WithMaxBody(64<<20), WithMaxResponse(1<<30))
	h := gw.Script("bench.php")
	b.SetBytes(int64(len(stdout) + len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		w := &discardWriter{h: http.Header{}}
		for pb.Next() {
			clear(w.h)
			r := httptest.NewRequest(method, "/bench.php?a=1", bytes.NewReader(body))
			r.Header.Set("User-Agent", "bench")
			r.Header.Set("Accept", "text/html")
			r.Header.Set("Cookie", "session=abc")
			h.ServeHTTP(w, r)
			if w.code != http.StatusOK {
				b.Fatalf("status %d", w.code)
			}
		}
	})
}

func BenchmarkHello(b *testing.B) {
	benchHandler(b, []byte("Content-Type: text/plain\r\n\r\nok\n"), http.MethodGet, nil)
}

func BenchmarkPage60K(b *testing.B) {
	benchHandler(b, []byte("Content-Type: text/html\r\n\r\n"+strings.Repeat("<tr><td>x</td></tr>\n", 3000)), http.MethodGet, nil)
}

func BenchmarkUpload1M(b *testing.B) {
	benchHandler(b, []byte("Content-Type: text/plain\r\n\r\nok\n"), http.MethodPost, make([]byte, 1<<20))
}

func BenchmarkDownload1M(b *testing.B) {
	benchHandler(b, append([]byte("Content-Type: application/octet-stream\r\n\r\n"), make([]byte, 1<<20)...), http.MethodGet, nil)
}
