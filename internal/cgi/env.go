// Package cgi maps HTTP requests to CGI/1.1 variables and CGI responses back
// to HTTP, following RFC 3875.
package cgi

import (
	"maps"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/braska-io/fcgi-go/internal/fastcgi"
)

// Config is everything besides the request that shapes the variables.
type Config struct {
	Script       string
	DocumentRoot string
	Protected    []string
	// PathInfo and Query override the derived values when non-nil.
	PathInfo   func(*http.Request) string
	Query      func(*http.Request) string
	Gateway    map[string]string
	ScriptVars map[string]string
	ParamsFunc func(*http.Request) map[string]string
	// BodyLength is the exact length of the spooled body; HasBody tells
	// whether the request carried a message-body at all (RFC 3875 4.1.2).
	BodyLength int64
	HasBody    bool
}

// hopByHop fields describe one connection and never reach the script.
var hopByHop = map[string]bool{
	"Connection": true, "Keep-Alive": true, "Proxy-Connection": true,
	"Te": true, "Transfer-Encoding": true, "Upgrade": true,
	// Credentials for a proxy, not for the application behind us.
	"Proxy-Authorization": true,
}

type env struct {
	order []string
	vals  map[string]string
}

func newEnv() *env { return &env{vals: map[string]string{}} }

func (e *env) set(name, value string) {
	if _, ok := e.vals[name]; !ok {
		e.order = append(e.order, name)
	}
	e.vals[name] = value
}

// override applies stages 3 to 5: an empty value removes the variable.
func (e *env) override(m map[string]string) {
	for _, name := range sortedKeys(m) {
		if m[name] == "" {
			delete(e.vals, name)
			continue
		}
		e.set(name, m[name])
	}
}

func (e *env) params() []fastcgi.Param {
	out := make([]fastcgi.Param, 0, len(e.order))
	for _, name := range e.order {
		if v, ok := e.vals[name]; ok {
			out = append(out, fastcgi.Param{Name: name, Value: v})
		}
	}
	return out
}

// Build returns the FastCGI parameters in the order docs/design.md fixes.
func Build(r *http.Request, c Config) []fastcgi.Param {
	e := newEnv()
	setMeta(e, r, c)
	setHeaders(e, r, c.Protected)
	e.override(c.Gateway)
	e.override(c.ScriptVars)
	if c.ParamsFunc != nil {
		e.override(c.ParamsFunc(r))
	}
	return e.params()
}

func setMeta(e *env, r *http.Request, c Config) {
	e.set("GATEWAY_INTERFACE", "CGI/1.1")
	e.set("SERVER_SOFTWARE", "fcgi-go")
	e.set("SERVER_PROTOCOL", r.Proto)
	name, _ := splitHostPort(r.Host)
	e.set("SERVER_NAME", name)
	if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if _, port, err := net.SplitHostPort(addr.String()); err == nil {
			e.set("SERVER_PORT", port)
		}
	}
	host, port := splitHostPort(r.RemoteAddr)
	e.set("REMOTE_ADDR", host)
	// RFC 3875 section 4.1.9 allows REMOTE_ADDR as the value; a reverse
	// lookup per request would stall the call.
	e.set("REMOTE_HOST", host)
	if port != "" {
		e.set("REMOTE_PORT", port)
	}
	e.set("REQUEST_METHOD", r.Method)
	e.set("QUERY_STRING", queryString(r, c))
	e.set("REQUEST_URI", r.RequestURI)
	scriptName := "/" + c.Script
	e.set("SCRIPT_NAME", scriptName)
	if pi := pathInfo(r, c, scriptName); pi != "" {
		e.set("PATH_INFO", pi)
		e.set("PATH_TRANSLATED", c.DocumentRoot+pi)
	}
	if c.HasBody {
		e.set("CONTENT_LENGTH", strconv.FormatInt(c.BodyLength, 10))
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		e.set("CONTENT_TYPE", ct)
	}
	if r.TLS != nil {
		e.set("HTTPS", "on")
	}
	e.set("DOCUMENT_ROOT", c.DocumentRoot)
	e.set("SCRIPT_FILENAME", c.DocumentRoot+scriptName)
}

func queryString(r *http.Request, c Config) string {
	if c.Query != nil {
		return c.Query(r)
	}
	return r.URL.RawQuery
}

func pathInfo(r *http.Request, c Config, scriptName string) string {
	if c.PathInfo != nil {
		return c.PathInfo(r)
	}
	rest, ok := strings.CutPrefix(r.URL.Path, scriptName)
	if !ok {
		return ""
	}
	return rest
}

// splitHostPort tolerates a missing port, which net.SplitHostPort rejects.
func splitHostPort(s string) (host, port string) {
	if h, p, err := net.SplitHostPort(s); err == nil {
		return h, p
	}
	return strings.Trim(s, "[]"), ""
}

func setHeaders(e *env, r *http.Request, protected []string) {
	drop := connectionTokens(r.Header)
	if r.Host != "" {
		e.set("HTTP_HOST", r.Host)
	}
	for _, name := range sortedKeys(r.Header) {
		if hopByHop[name] || drop[name] || strings.ContainsAny(name, "_.") {
			continue
		}
		if isProtected(name, protected) {
			continue
		}
		e.set("HTTP_"+normalise(name), joinValues(name, r.Header[name]))
	}
}

// connectionTokens returns the canonical names the Connection header lists.
func connectionTokens(h http.Header) map[string]bool {
	out := map[string]bool{}
	for _, v := range h.Values("Connection") {
		for tok := range strings.SplitSeq(v, ",") {
			if tok = strings.TrimSpace(tok); tok != "" {
				out[http.CanonicalHeaderKey(tok)] = true
			}
		}
	}
	return out
}

// joinValues joins repeated fields per RFC 9110 section 5.3; Cookie uses
// "; " because RFC 6265 section 5.4 defines it so.
func joinValues(name string, values []string) string {
	sep := ", "
	if name == "Cookie" {
		sep = "; "
	}
	return strings.Join(values, sep)
}

// normalise upper-cases a name and turns everything outside A-Z0-9 into "_".
func normalise(name string) string {
	b := []byte(strings.ToUpper(name))
	for i, ch := range b {
		if (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
			b[i] = '_'
		}
	}
	return string(b)
}

func isProtected(name string, protected []string) bool {
	n := normalise(name)
	return slices.ContainsFunc(protected, func(p string) bool {
		np := normalise(p)
		if strings.HasSuffix(p, "*") {
			return strings.HasPrefix(n, strings.TrimSuffix(np, "_"))
		}
		return n == np
	})
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
