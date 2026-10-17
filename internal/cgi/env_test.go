package cgi

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/braska-io/fcgi-go/internal/fastcgi"
)

func asMap(ps []fastcgi.Param) map[string]string {
	m := map[string]string{}
	for _, p := range ps {
		m[p.Name] = p.Value
	}
	return m
}

func newRequest(method, target string, headers ...string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.Host = "example.test:8443"
	r.RemoteAddr = "192.0.2.7:5555"
	r.Header = http.Header{}
	for i := 0; i < len(headers); i += 2 {
		r.Header.Add(headers[i], headers[i+1])
	}
	local := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8443}
	return r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, net.Addr(local)))
}

func TestBuildMetaVariables(t *testing.T) {
	r := newRequest("GET", "/api.php/users/7?a=1&b=%20")
	r.TLS = &tls.ConnectionState{}
	env := asMap(Build(r, Config{Script: "api.php", DocumentRoot: "/srv"}))
	want := map[string]string{
		"GATEWAY_INTERFACE": "CGI/1.1",
		"SERVER_PROTOCOL":   "HTTP/1.1",
		"SERVER_NAME":       "example.test",
		"SERVER_PORT":       "8443",
		"REMOTE_ADDR":       "192.0.2.7",
		"REMOTE_PORT":       "5555",
		"REQUEST_METHOD":    "GET",
		"QUERY_STRING":      "a=1&b=%20",
		"REQUEST_URI":       "/api.php/users/7?a=1&b=%20",
		"SCRIPT_NAME":       "/api.php",
		"PATH_INFO":         "/users/7",
		"PATH_TRANSLATED":   "/srv/users/7",
		"SCRIPT_FILENAME":   "/srv/api.php",
		"DOCUMENT_ROOT":     "/srv",
		"HTTPS":             "on",
		"HTTP_HOST":         "example.test:8443",
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
	if _, ok := env["CONTENT_LENGTH"]; ok {
		t.Error("CONTENT_LENGTH set without a body")
	}
}

func TestBuildContentLengthOnlyWithBody(t *testing.T) {
	r := newRequest("POST", "/a.php")
	env := asMap(Build(r, Config{Script: "a.php", HasBody: true, BodyLength: 0}))
	if env["CONTENT_LENGTH"] != "0" {
		t.Errorf("CONTENT_LENGTH = %q, want 0", env["CONTENT_LENGTH"])
	}
}

func TestBuildHeaders(t *testing.T) {
	r := newRequest("GET", "/a.php",
		"Accept", "a",
		"Accept", "b",
		"Cookie", "x=1",
		"Cookie", "y=2",
		"Connection", "close, X-Hop",
		"X-Hop", "1",
		"Keep-Alive", "5",
		"Transfer-Encoding", "chunked",
		"X_Under", "1",
		"X.Dot", "1",
		"X-Auth-Subject", "evil",
		"X-Auth-Other", "evil",
		"Authorization", "Basic abc",
		"Content-Type", "text/plain",
	)
	env := asMap(Build(r, Config{Script: "a.php", Protected: []string{"X-Auth-Subject", "X-Auth-*"}}))
	for k, v := range map[string]string{
		"HTTP_ACCEPT":        "a, b",
		"HTTP_COOKIE":        "x=1; y=2",
		"HTTP_AUTHORIZATION": "Basic abc",
		"HTTP_CONTENT_TYPE":  "text/plain",
		"CONTENT_TYPE":       "text/plain",
	} {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
	for _, k := range []string{"HTTP_CONNECTION", "HTTP_X_HOP", "HTTP_KEEP_ALIVE", "HTTP_TRANSFER_ENCODING",
		"HTTP_X_UNDER", "HTTP_X_DOT", "HTTP_X_AUTH_SUBJECT", "HTTP_X_AUTH_OTHER"} {
		if v, ok := env[k]; ok {
			t.Errorf("%s = %q, want absent", k, v)
		}
	}
}

func TestBuildParameterPrecedence(t *testing.T) {
	r := newRequest("GET", "/a.php", "X-Level", "client", "X-Gone", "client")
	ps := Build(r, Config{
		Script:     "a.php",
		Gateway:    map[string]string{"HTTP_X_LEVEL": "gateway", "G": "1", "HTTP_X_GONE": "gateway"},
		ScriptVars: map[string]string{"HTTP_X_LEVEL": "script", "HTTP_X_GONE": ""},
		ParamsFunc: func(*http.Request) map[string]string { return map[string]string{"REQUEST_METHOD": "FUNC"} },
	})
	env := asMap(ps)
	if env["HTTP_X_LEVEL"] != "script" || env["G"] != "1" || env["REQUEST_METHOD"] != "FUNC" {
		t.Errorf("unexpected precedence: %v", env)
	}
	if _, ok := env["HTTP_X_GONE"]; ok {
		t.Error("empty script value did not remove the parameter")
	}
	seen := map[string]bool{}
	for _, p := range ps {
		if seen[p.Name] {
			t.Errorf("%s written twice", p.Name)
		}
		seen[p.Name] = true
	}
}

func TestBuildPathInfoOverride(t *testing.T) {
	r := newRequest("GET", "/other/path")
	env := asMap(Build(r, Config{
		Script:   "a.php",
		PathInfo: func(*http.Request) string { return "/custom" },
		Query:    func(*http.Request) string { return "q=1" },
	}))
	if env["PATH_INFO"] != "/custom" || env["QUERY_STRING"] != "q=1" {
		t.Errorf("got PATH_INFO %q QUERY_STRING %q", env["PATH_INFO"], env["QUERY_STRING"])
	}
}
