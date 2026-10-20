package fcgi_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"

	fcgi "github.com/braska-io/fcgi-go"
)

// tokenStore holds tokens that are valid only while their call runs.
type tokenStore struct {
	mu     sync.Mutex
	active map[string]bool
}

func (s *tokenStore) issue() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok := rand.Text()
	s.active[tok] = true
	return tok
}

func (s *tokenStore) revoke(tok string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.active, tok)
}

func (s *tokenStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.active)
}

type tokenKey struct{}

// A caller that binds state to one call wraps the handler Script returns and
// releases the state in a defer. WithParams has no end-of-call hook, and the
// observer runs after the response, so neither replaces the wrapper.
func Example_perCallState() {
	tokens := &tokenStore{active: map[string]bool{}}
	// No responder listens here, so the call answers 502. The defer releases
	// the token on that path as on every other.
	gw := fcgi.New("unix:/nonexistent/fpm.sock", fcgi.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

	api := gw.Script("api.php", fcgi.WithParams(func(r *http.Request) map[string]string {
		tok, _ := r.Context().Value(tokenKey{}).(string)
		return map[string]string{"HTTP_X_CALL_TOKEN": tok}
	}))
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := tokens.issue()
		defer tokens.revoke(tok)
		api(w, r.WithContext(context.WithValue(r.Context(), tokenKey{}, tok)))
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api.php", nil))
	fmt.Println(rec.Code, tokens.count())
	// Output: 502 0
}
