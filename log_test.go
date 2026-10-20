package fcgi

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type requestIDKey struct{}

// contextHandler records, per message, the request ID found in the context
// of the log call.
type contextHandler struct {
	mu  *sync.Mutex
	ids map[string]any
}

func (h contextHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h contextHandler) WithAttrs([]slog.Attr) slog.Handler     { return h }
func (h contextHandler) WithGroup(string) slog.Handler          { return h }

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ids[r.Message] = ctx.Value(requestIDKey{})
	return nil
}

// failingWriter accepts the status and fails every body write.
type failingWriter struct{ *httptest.ResponseRecorder }

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestLogLinesCarryTheRequestContext(t *testing.T) {
	tests := []struct {
		msg   string
		reply func(net.Conn) []byte
		w     func() http.ResponseWriter
	}{
		{msg: "fcgi call failed", reply: func(net.Conn) []byte { return []byte("just text") }},
		{msg: "responder stderr", reply: func(c net.Conn) []byte {
			_, _ = c.Write(record(7, []byte("warning")))
			return []byte("\r\nok")
		}},
		{msg: "responder exited with non-zero status", reply: func(c net.Conn) []byte {
			end := make([]byte, 8)
			binary.BigEndian.PutUint32(end, 1)
			_, _ = c.Write(record(6, []byte("\r\nok")))
			_, _ = c.Write(record(6, nil))
			_, _ = c.Write(record(3, end))
			return nil
		}},
		{msg: "write response", reply: func(net.Conn) []byte { return []byte("\r\nok") },
			w: func() http.ResponseWriter { return failingWriter{httptest.NewRecorder()} }},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			h := contextHandler{mu: &sync.Mutex{}, ids: map[string]any{}}
			gw := New(newFake(t, tt.reply).addr, WithLogger(slog.New(h)))
			var w http.ResponseWriter = httptest.NewRecorder()
			if tt.w != nil {
				w = tt.w()
			}
			ctx := context.WithValue(context.Background(), requestIDKey{}, "req-1")
			gw.Script("a.php").ServeHTTP(w, httptest.NewRequest("GET", "/a.php", nil).WithContext(ctx))

			h.mu.Lock()
			defer h.mu.Unlock()
			id, logged := h.ids[tt.msg]
			if !logged || id != "req-1" {
				t.Fatalf("%q logged %v with request ID %v, want req-1; lines: %v", tt.msg, logged, id, h.ids)
			}
		})
	}
}
