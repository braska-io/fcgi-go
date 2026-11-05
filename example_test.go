package fcgi_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

// A caller without an HTTP request runs a script through Call. A failure of
// the library is an *Error with the status a Script handler would answer;
// the responder's own status would be a Response instead.
func Example_call() {
	// No responder listens here, so the call fails as unreachable.
	gw := fcgi.New("unix:/nonexistent/fpm.sock", fcgi.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	api := gw.Caller("api.php")

	resp, err := api.Call(context.Background(), &fcgi.Request{
		Method: "POST",
		Target: "/api/v1/customer/7?x=1",
		Body:   strings.NewReader(`{"name":"x"}`),
		Params: map[string]string{"HTTP_X_AUTH_SUBJECT": "job-42"},
	})
	if err != nil {
		var e *fcgi.Error
		fmt.Println(errors.As(err, &e), e.Status, errors.Is(err, fcgi.ErrUnreachable))
		return
	}
	defer resp.Body.Close()
	fmt.Println(resp.Status)
	// Output: true 502 true
}
